use crate::path_security::resolve_workspace_path;
use crate::protocol_v2::*;
use crate::pty_util;

use base64::Engine as _;
use sha2::{Digest, Sha256};
use std::collections::HashMap;
use std::io::{self, Read, Write as _};
use std::os::fd::{AsRawFd, FromRawFd, OwnedFd};
use std::os::unix::net::UnixListener;
use std::process::{Child, Command, Stdio};
use std::sync::{Arc, Mutex};
use std::{fs, thread};
use tokio::sync::watch;

const FILE_CHUNK_SIZE: usize = 1024 * 1024;
const SIDECAR_SOCKET_GID: u32 = 65532;

/// State for a single spawned process.
struct ProcessHandle {
    child: Option<Child>,
    /// PTY master fd (if PTY mode).
    pty_master: Option<OwnedFd>,
    /// Stdin pipe writer (if non-PTY stdin mode). We store the raw fd and
    /// wrap it in a File only when writing, because Child owns the original.
    stdin_pipe: Option<std::process::ChildStdin>,
    pid: u32,
}

/// V2 executor agent.
pub struct AgentV2 {
    socket_path: String,
    workspace_dir: String,
}

impl AgentV2 {
    pub fn new(socket_path: String, workspace_dir: String) -> Self {
        Self {
            socket_path,
            workspace_dir,
        }
    }

    pub fn run(&self, shutdown: watch::Receiver<bool>) -> io::Result<()> {
        let _ = fs::remove_file(&self.socket_path);
        let listener = UnixListener::bind(&self.socket_path)?;
        set_socket_permissions(&self.socket_path)?;
        log::info!("executor-agent v2 listening on {}", self.socket_path);

        listener.set_nonblocking(true)?;

        let workspace = self.workspace_dir.clone();
        loop {
            if *shutdown.borrow() {
                break;
            }
            match listener.accept() {
                Ok((stream, _)) => {
                    let ws = workspace.clone();
                    let sd = shutdown.clone();
                    thread::spawn(move || {
                        if let Err(e) = handle_conn(stream, &ws, sd) {
                            log::error!("connection error: {e}");
                        }
                    });
                }
                Err(ref e) if e.kind() == io::ErrorKind::WouldBlock => {
                    thread::sleep(std::time::Duration::from_millis(50));
                    continue;
                }
                Err(e) => {
                    log::error!("accept error: {e}");
                    continue;
                }
            }
        }
        Ok(())
    }
}

fn set_socket_permissions(path: &str) -> io::Result<()> {
    use std::ffi::CString;
    let c_path = CString::new(path).unwrap();
    let ret = unsafe { libc::chown(c_path.as_ptr(), u32::MAX, SIDECAR_SOCKET_GID) };
    let mode = if ret != 0 {
        log::warn!("chown socket group to {SIDECAR_SOCKET_GID} failed; falling back to 0666");
        0o666
    } else {
        0o660
    };
    let ret = unsafe { libc::chmod(c_path.as_ptr(), mode) };
    if ret != 0 {
        return Err(io::Error::last_os_error());
    }
    Ok(())
}

/// Shared writer for sending responses and events on the same connection.
type SharedWriter = Arc<Mutex<std::os::unix::net::UnixStream>>;

fn send_json(writer: &SharedWriter, value: &impl serde::Serialize) -> io::Result<()> {
    let mut w = writer.lock().unwrap();
    serde_json::to_writer(&mut *w, value)
        .map_err(io::Error::other)?;
    w.write_all(b"\n")?;
    w.flush()
}

fn handle_conn(
    stream: std::os::unix::net::UnixStream,
    workspace: &str,
    shutdown: watch::Receiver<bool>,
) -> io::Result<()> {
    let reader = io::BufReader::new(stream.try_clone()?);
    let writer: SharedWriter = Arc::new(Mutex::new(stream));
    let processes: Arc<Mutex<HashMap<String, ProcessHandle>>> =
        Arc::new(Mutex::new(HashMap::new()));

    let result = handle_messages(reader, writer.clone(), workspace, &processes, &shutdown);

    // On disconnect, kill all processes spawned on this connection.
    let mut procs = processes.lock().unwrap();
    for (handle, ph) in procs.iter_mut() {
        if let Some(ref mut child) = ph.child {
            log::info!("[cleanup] killing handle={handle} pid={}", ph.pid);
            let _ = child.kill();
            let _ = child.wait();
        }
    }
    procs.clear();

    result
}

fn handle_messages(
    reader: io::BufReader<std::os::unix::net::UnixStream>,
    writer: SharedWriter,
    workspace: &str,
    processes: &Arc<Mutex<HashMap<String, ProcessHandle>>>,
    shutdown: &watch::Receiver<bool>,
) -> io::Result<()> {
    let de = serde_json::Deserializer::from_reader(reader);
    for value in de.into_iter::<V2Request>() {
        if *shutdown.borrow() {
            break;
        }
        let req = match value {
            Ok(r) => r,
            Err(e) => {
                if e.is_eof() {
                    return Ok(());
                }
                let _ = send_json(
                    &writer,
                    &V2Response::err(String::new(), "PARSE_ERROR", format!("{e}")),
                );
                return Ok(());
            }
        };

        match req {
            V2Request::Ping { id } => {
                log::info!("[v2:ping] id={id}");
                let _ = send_json(&writer, &V2Response::ok(id));
            }
            V2Request::Spawn { id, params } => {
                log::info!("[v2:spawn] id={id} cmd={:?}", params.cmd);
                handle_spawn(&id, params, workspace, &writer, processes);
            }
            V2Request::Stdin { id, params } => {
                handle_stdin(&id, params, &writer, processes);
            }
            V2Request::Signal { id, params } => {
                handle_signal(&id, params, &writer, processes);
            }
            V2Request::Resize { id, params } => {
                handle_resize(&id, params, &writer, processes);
            }
            V2Request::WriteFile { id, params } => {
                log::info!("[v2:write_file] id={id} path={}", params.path);
                // Write-file is a multi-message flow; we need to consume
                // subsequent FileChunk/FileDone from the stream. Since we're
                // iterating over a serde_json stream, we can't easily do that
                // here. Instead, we handle it by returning "not yet implemented".
                // For now, use the single-message flow variant below.
                let _ = send_json(
                    &writer,
                    &V2Response::err(
                        id,
                        "NOT_IMPLEMENTED",
                        "use spawned write_file flow".into(),
                    ),
                );
            }
            V2Request::FileChunk { id, .. } | V2Request::FileDone { id } => {
                let _ = send_json(
                    &writer,
                    &V2Response::err(id, "UNEXPECTED", "file_chunk/file_done without write_file".into()),
                );
            }
            V2Request::ReadFile { id, params } => {
                log::info!("[v2:read_file] id={id} path={}", params.path);
                handle_read_file(&id, params, workspace, &writer);
            }
            V2Request::Stat { id, params } => {
                log::info!("[v2:stat] id={id} path={}", params.path);
                handle_stat(&id, params, workspace, &writer);
            }
            V2Request::ListDir { id, params } => {
                log::info!("[v2:list_dir] id={id} path={}", params.path);
                handle_list_dir(&id, params, workspace, &writer);
            }
        }
    }
    Ok(())
}

// ---------------------------------------------------------------------------
// spawn
// ---------------------------------------------------------------------------

fn handle_spawn(
    id: &str,
    params: SpawnParams,
    workspace: &str,
    writer: &SharedWriter,
    processes: &Arc<Mutex<HashMap<String, ProcessHandle>>>,
) {
    if params.cmd.is_empty() {
        let _ = send_json(
            writer,
            &V2Response::err(id.into(), "SPAWN_FAILED", "empty command".into()),
        );
        return;
    }

    let workdir = params
        .workdir
        .as_deref()
        .unwrap_or(workspace)
        .to_string();

    let handle = format!("proc-{}", &uuid::Uuid::new_v4().to_string()[..8]);

    if params.pty.is_some() {
        handle_spawn_pty(id, &handle, params, &workdir, writer, processes);
    } else {
        handle_spawn_pipe(id, &handle, params, &workdir, writer, processes);
    }
}

fn handle_spawn_pipe(
    id: &str,
    handle: &str,
    params: SpawnParams,
    workdir: &str,
    writer: &SharedWriter,
    processes: &Arc<Mutex<HashMap<String, ProcessHandle>>>,
) {
    let mut cmd = Command::new(&params.cmd[0]);
    cmd.args(&params.cmd[1..]);
    cmd.current_dir(workdir);
    cmd.stdout(Stdio::piped());
    cmd.stderr(Stdio::piped());

    if params.stdin {
        cmd.stdin(Stdio::piped());
    } else {
        cmd.stdin(Stdio::null());
    }

    for (k, v) in &params.env {
        cmd.env(k, v);
    }

    let mut child = match cmd.spawn() {
        Ok(c) => c,
        Err(e) => {
            let _ = send_json(
                writer,
                &V2Response::err(id.into(), "SPAWN_FAILED", format!("{e}")),
            );
            return;
        }
    };

    let pid = child.id();
    let stdout = child.stdout.take().unwrap();
    let stderr = child.stderr.take().unwrap();
    let stdin_pipe = child.stdin.take();

    let ph = ProcessHandle {
        child: Some(child),
        pty_master: None,
        stdin_pipe,
        pid,
    };
    processes.lock().unwrap().insert(handle.to_string(), ph);

    // Send spawn response
    let _ = send_json(
        writer,
        &V2Response::result(
            id.into(),
            serde_json::to_value(SpawnResult {
                handle: handle.into(),
            })
            .unwrap(),
        ),
    );

    // Spawn stdout reader
    let h1 = handle.to_string();
    let w1 = writer.clone();
    thread::spawn(move || {
        let mut buf = [0u8; 64 * 1024];
        let mut r = stdout;
        loop {
            match r.read(&mut buf) {
                Ok(0) | Err(_) => return,
                Ok(n) => {
                    let _ = send_json(
                        &w1,
                        &V2Event::Stdout {
                            handle: h1.clone(),
                            data: String::from_utf8_lossy(&buf[..n]).into_owned(),
                        },
                    );
                }
            }
        }
    });

    // Spawn stderr reader
    let h2 = handle.to_string();
    let w2 = writer.clone();
    thread::spawn(move || {
        let mut buf = [0u8; 64 * 1024];
        let mut r = stderr;
        loop {
            match r.read(&mut buf) {
                Ok(0) | Err(_) => return,
                Ok(n) => {
                    let _ = send_json(
                        &w2,
                        &V2Event::Stderr {
                            handle: h2.clone(),
                            data: String::from_utf8_lossy(&buf[..n]).into_owned(),
                        },
                    );
                }
            }
        }
    });

    // Spawn waiter
    let h3 = handle.to_string();
    let w3 = writer.clone();
    let procs = processes.clone();
    let timeout = params.timeout;
    thread::spawn(move || {
        wait_and_exit(&h3, &w3, &procs, timeout);
    });
}

fn handle_spawn_pty(
    id: &str,
    handle: &str,
    params: SpawnParams,
    workdir: &str,
    writer: &SharedWriter,
    processes: &Arc<Mutex<HashMap<String, ProcessHandle>>>,
) {
    let pty_params = params.pty.unwrap();
    let pty_pair = match pty_util::open_pty(pty_params.rows, pty_params.cols) {
        Ok(pair) => pair,
        Err(e) => {
            let _ = send_json(
                writer,
                &V2Response::err(id.into(), "SPAWN_FAILED", format!("{e}")),
            );
            return;
        }
    };
    let master = pty_pair.master;
    let slave = pty_pair.slave;

    // Build command using the slave as stdin/stdout/stderr
    let mut cmd = Command::new(&params.cmd[0]);
    cmd.args(&params.cmd[1..]);
    cmd.current_dir(workdir);

    // Set TERM if not already set
    let mut has_term = false;
    for (k, v) in &params.env {
        cmd.env(k, v);
        if k == "TERM" {
            has_term = true;
        }
    }
    if !has_term && std::env::var("TERM").is_err() {
        cmd.env("TERM", "xterm-256color");
    }

    // Redirect child stdio to the PTY slave
    use std::os::unix::process::CommandExt;
    let slave_fd = slave.as_raw_fd();
    unsafe {
        cmd.pre_exec(move || {
            // Create a new session so the child becomes session leader
            if libc::setsid() == -1 {
                return Err(io::Error::last_os_error());
            }
            // Set controlling terminal
            if libc::ioctl(slave_fd, libc::TIOCSCTTY, 0) == -1 {
                return Err(io::Error::last_os_error());
            }
            // Dup slave fd to stdin/stdout/stderr
            if libc::dup2(slave_fd, 0) == -1
                || libc::dup2(slave_fd, 1) == -1
                || libc::dup2(slave_fd, 2) == -1
            {
                return Err(io::Error::last_os_error());
            }
            // Close the original slave fd if it's not 0/1/2
            if slave_fd > 2 {
                libc::close(slave_fd);
            }
            Ok(())
        });
    }

    cmd.stdin(Stdio::null()); // overridden by pre_exec
    cmd.stdout(Stdio::null());
    cmd.stderr(Stdio::null());

    let child = match cmd.spawn() {
        Ok(c) => c,
        Err(e) => {
            let _ = send_json(
                writer,
                &V2Response::err(id.into(), "SPAWN_FAILED", format!("{e}")),
            );
            return;
        }
    };

    let pid = child.id();

    // Close the slave fd in the parent after spawn
    drop(slave);

    let ph = ProcessHandle {
        child: Some(child),
        pty_master: Some(master),
        stdin_pipe: None,
        pid,
    };
    processes.lock().unwrap().insert(handle.to_string(), ph);

    // Send spawn response
    let _ = send_json(
        writer,
        &V2Response::result(
            id.into(),
            serde_json::to_value(SpawnResult {
                handle: handle.into(),
            })
            .unwrap(),
        ),
    );

    // Read from PTY master -> stdout events
    // We need to dup the master fd for the reader thread since ProcessHandle owns it.
    let master_read_fd = {
        let procs = processes.lock().unwrap();
        let ph = procs.get(handle).unwrap();
        let fd = ph.pty_master.as_ref().unwrap().as_raw_fd();
        // dup the fd so the reader can own its copy
        let duped = unsafe { libc::dup(fd) };
        if duped == -1 {
            log::error!("failed to dup pty master fd");
            return;
        }
        unsafe { OwnedFd::from_raw_fd(duped) }
    };

    let h1 = handle.to_string();
    let w1 = writer.clone();
    thread::spawn(move || {
        use std::os::fd::IntoRawFd;
        let raw = master_read_fd.into_raw_fd();
        let mut file = unsafe { std::fs::File::from_raw_fd(raw) };
        let mut buf = [0u8; 4096];
        loop {
            match file.read(&mut buf) {
                Ok(0) | Err(_) => return,
                Ok(n) => {
                    let _ = send_json(
                        &w1,
                        &V2Event::Stdout {
                            handle: h1.clone(),
                            data: String::from_utf8_lossy(&buf[..n]).into_owned(),
                        },
                    );
                }
            }
        }
    });

    // Spawn waiter
    let h2 = handle.to_string();
    let w2 = writer.clone();
    let procs = processes.clone();
    let timeout = params.timeout;
    thread::spawn(move || {
        wait_and_exit(&h2, &w2, &procs, timeout);
    });
}

fn wait_and_exit(
    handle: &str,
    writer: &SharedWriter,
    processes: &Arc<Mutex<HashMap<String, ProcessHandle>>>,
    timeout: Option<u64>,
) {
    // Take the child out of the process handle to wait on it
    let mut child = {
        let mut procs = processes.lock().unwrap();
        match procs.get_mut(handle) {
            Some(ph) => match ph.child.take() {
                Some(c) => c,
                None => return,
            },
            None => return,
        }
    };

    let exit_code = if let Some(secs) = timeout {
        // Spawn a timeout killer
        let pid = child.id();
        let dur = std::time::Duration::from_secs(secs);
        let (done_tx, done_rx) = std::sync::mpsc::channel::<()>();

        let killer = thread::spawn(move || {
            if done_rx.recv_timeout(dur).is_err() {
                // Timeout expired, kill the process
                let _ = nix::sys::signal::kill(
                    nix::unistd::Pid::from_raw(pid as i32),
                    nix::sys::signal::Signal::SIGKILL,
                );
            }
        });

        let code = match child.wait() {
            Ok(status) => status.code().unwrap_or(1),
            Err(_) => 1,
        };
        let _ = done_tx.send(());
        killer.join().ok();
        code
    } else {
        match child.wait() {
            Ok(status) => status.code().unwrap_or(1),
            Err(_) => 1,
        }
    };

    // Small delay to let output readers flush before we send exit
    thread::sleep(std::time::Duration::from_millis(50));

    // Clean up process handle
    let mut procs = processes.lock().unwrap();
    if let Some(ph) = procs.get_mut(handle) {
        // Drop PTY master to close it
        ph.pty_master.take();
        ph.stdin_pipe.take();
    }
    procs.remove(handle);

    log::info!("[v2:exit] handle={handle} exit_code={exit_code}");
    let _ = send_json(
        writer,
        &V2Event::Exit {
            handle: handle.into(),
            exit_code,
        },
    );
}

// ---------------------------------------------------------------------------
// stdin
// ---------------------------------------------------------------------------

fn handle_stdin(
    id: &str,
    params: StdinParams,
    writer: &SharedWriter,
    processes: &Arc<Mutex<HashMap<String, ProcessHandle>>>,
) {
    let mut procs = processes.lock().unwrap();
    let ph = match procs.get_mut(&params.handle) {
        Some(p) => p,
        None => {
            let _ = send_json(
                writer,
                &V2Response::err(
                    id.into(),
                    "UNKNOWN_HANDLE",
                    format!("no process with handle {}", params.handle),
                ),
            );
            return;
        }
    };

    let data = params.data.as_bytes();

    if let Some(ref master) = ph.pty_master {
        // PTY mode: write to master fd
        let fd = master.as_raw_fd();
        let ret = unsafe { libc::write(fd, data.as_ptr() as *const libc::c_void, data.len()) };
        if ret < 0 {
            let _ = send_json(
                writer,
                &V2Response::err(id.into(), "STDIN_FAILED", format!("{}", io::Error::last_os_error())),
            );
            return;
        }
    } else if let Some(ref mut stdin_pipe) = ph.stdin_pipe {
        if let Err(e) = stdin_pipe.write_all(data) {
            let _ = send_json(
                writer,
                &V2Response::err(id.into(), "STDIN_FAILED", format!("{e}")),
            );
            return;
        }
        if let Err(e) = stdin_pipe.flush() {
            let _ = send_json(
                writer,
                &V2Response::err(id.into(), "STDIN_FAILED", format!("{e}")),
            );
            return;
        }
    } else {
        let _ = send_json(
            writer,
            &V2Response::err(
                id.into(),
                "STDIN_DISABLED",
                "process was not spawned with stdin enabled".into(),
            ),
        );
        return;
    }

    let _ = send_json(writer, &V2Response::ok(id.into()));
}

// ---------------------------------------------------------------------------
// signal
// ---------------------------------------------------------------------------

fn handle_signal(
    id: &str,
    params: SignalParams,
    writer: &SharedWriter,
    processes: &Arc<Mutex<HashMap<String, ProcessHandle>>>,
) {
    let procs = processes.lock().unwrap();
    let ph = match procs.get(&params.handle) {
        Some(p) => p,
        None => {
            let _ = send_json(
                writer,
                &V2Response::err(
                    id.into(),
                    "UNKNOWN_HANDLE",
                    format!("no process with handle {}", params.handle),
                ),
            );
            return;
        }
    };

    let sig = match params.signal.to_uppercase().as_str() {
        "SIGTERM" => nix::sys::signal::Signal::SIGTERM,
        "SIGKILL" => nix::sys::signal::Signal::SIGKILL,
        "SIGINT" => nix::sys::signal::Signal::SIGINT,
        "SIGHUP" => nix::sys::signal::Signal::SIGHUP,
        "SIGUSR1" => nix::sys::signal::Signal::SIGUSR1,
        "SIGUSR2" => nix::sys::signal::Signal::SIGUSR2,
        other => {
            let _ = send_json(
                writer,
                &V2Response::err(id.into(), "INVALID_SIGNAL", format!("unsupported signal: {other}")),
            );
            return;
        }
    };

    let pid = nix::unistd::Pid::from_raw(ph.pid as i32);
    if let Err(e) = nix::sys::signal::kill(pid, sig) {
        let _ = send_json(
            writer,
            &V2Response::err(id.into(), "SIGNAL_FAILED", format!("{e}")),
        );
        return;
    }

    let _ = send_json(writer, &V2Response::ok(id.into()));
}

// ---------------------------------------------------------------------------
// resize
// ---------------------------------------------------------------------------

fn handle_resize(
    id: &str,
    params: ResizeParams,
    writer: &SharedWriter,
    processes: &Arc<Mutex<HashMap<String, ProcessHandle>>>,
) {
    let procs = processes.lock().unwrap();
    let ph = match procs.get(&params.handle) {
        Some(p) => p,
        None => {
            let _ = send_json(
                writer,
                &V2Response::err(
                    id.into(),
                    "UNKNOWN_HANDLE",
                    format!("no process with handle {}", params.handle),
                ),
            );
            return;
        }
    };

    let master = match &ph.pty_master {
        Some(m) => m,
        None => {
            let _ = send_json(
                writer,
                &V2Response::err(id.into(), "NOT_PTY", "process has no PTY".into()),
            );
            return;
        }
    };

    if let Err(e) = pty_util::set_winsize(master.as_raw_fd(), params.rows, params.cols) {
        let _ = send_json(
            writer,
            &V2Response::err(id.into(), "RESIZE_FAILED", format!("{e}")),
        );
        return;
    }

    let _ = send_json(writer, &V2Response::ok(id.into()));
}

// ---------------------------------------------------------------------------
// read_file
// ---------------------------------------------------------------------------

fn handle_read_file(
    id: &str,
    params: ReadFileParams,
    workspace: &str,
    writer: &SharedWriter,
) {
    let target = match resolve_workspace_path(std::path::Path::new(workspace), &params.path) {
        Ok(p) => p,
        Err(e) => {
            let _ = send_json(writer, &V2Response::err(id.into(), "PATH_ERROR", e));
            return;
        }
    };

    let mut file = match fs::File::open(&target) {
        Ok(f) => f,
        Err(e) => {
            let _ = send_json(
                writer,
                &V2Response::err(id.into(), "READ_FAILED", format!("{e}")),
            );
            return;
        }
    };

    let mut hasher = Sha256::new();
    let mut buf = vec![0u8; FILE_CHUNK_SIZE];
    let mut offset: i64 = 0;

    loop {
        match file.read(&mut buf) {
            Ok(0) => break,
            Ok(n) => {
                hasher.update(&buf[..n]);
                let encoded = base64::engine::general_purpose::STANDARD.encode(&buf[..n]);
                let _ = send_json(
                    writer,
                    &V2Response::chunk(
                        id.into(),
                        serde_json::to_value(ReadFileChunk {
                            offset,
                            content: encoded,
                        })
                        .unwrap(),
                    ),
                );
                offset += n as i64;
            }
            Err(e) => {
                let _ = send_json(
                    writer,
                    &V2Response::err(id.into(), "READ_FAILED", format!("{e}")),
                );
                return;
            }
        }
    }

    let sha = hex::encode(hasher.finalize());
    let _ = send_json(
        writer,
        &V2Response::result(
            id.into(),
            serde_json::to_value(ReadFileResult {
                size_bytes: offset,
                sha256: sha,
            })
            .unwrap(),
        ),
    );
}

// ---------------------------------------------------------------------------
// stat
// ---------------------------------------------------------------------------

fn handle_stat(
    id: &str,
    params: StatParams,
    workspace: &str,
    writer: &SharedWriter,
) {
    let target = match resolve_workspace_path(std::path::Path::new(workspace), &params.path) {
        Ok(p) => p,
        Err(e) => {
            let _ = send_json(writer, &V2Response::err(id.into(), "PATH_ERROR", e));
            return;
        }
    };

    match fs::metadata(&target) {
        Ok(meta) => {
            use std::os::unix::fs::MetadataExt;
            let modified = meta
                .modified()
                .ok()
                .map(|t| {
                    let dt: chrono::DateTime<chrono::Utc> = t.into();
                    dt.to_rfc3339()
                });
            let mode_val = meta.mode();
            let _ = send_json(
                writer,
                &V2Response::result(
                    id.into(),
                    serde_json::to_value(StatResult {
                        exists: true,
                        is_dir: Some(meta.is_dir()),
                        size: Some(meta.len()),
                        mode: Some(format!("{:04o}", mode_val & 0o7777)),
                        modified,
                    })
                    .unwrap(),
                ),
            );
        }
        Err(e) if e.kind() == io::ErrorKind::NotFound => {
            let _ = send_json(
                writer,
                &V2Response::result(
                    id.into(),
                    serde_json::to_value(StatResult {
                        exists: false,
                        is_dir: None,
                        size: None,
                        mode: None,
                        modified: None,
                    })
                    .unwrap(),
                ),
            );
        }
        Err(e) => {
            let _ = send_json(
                writer,
                &V2Response::err(id.into(), "STAT_FAILED", format!("{e}")),
            );
        }
    }
}

// ---------------------------------------------------------------------------
// list_dir
// ---------------------------------------------------------------------------

fn handle_list_dir(
    id: &str,
    params: ListDirParams,
    workspace: &str,
    writer: &SharedWriter,
) {
    let target = match resolve_workspace_path(std::path::Path::new(workspace), &params.path) {
        Ok(p) => p,
        Err(e) => {
            let _ = send_json(writer, &V2Response::err(id.into(), "PATH_ERROR", e));
            return;
        }
    };

    let mut entries = Vec::new();
    if let Err(e) = collect_entries(&target, &mut entries, params.recursive) {
        let _ = send_json(
            writer,
            &V2Response::err(id.into(), "LIST_FAILED", format!("{e}")),
        );
        return;
    }

    let _ = send_json(
        writer,
        &V2Response::result(
            id.into(),
            serde_json::to_value(ListDirResult { entries }).unwrap(),
        ),
    );
}

fn collect_entries(
    dir: &std::path::Path,
    entries: &mut Vec<DirEntry>,
    recursive: bool,
) -> io::Result<()> {
    for entry in fs::read_dir(dir)? {
        let entry = entry?;
        let meta = entry.metadata()?;
        let name = if meta.is_dir() {
            format!("{}/", entry.file_name().to_string_lossy())
        } else {
            entry.file_name().to_string_lossy().into_owned()
        };
        entries.push(DirEntry {
            name,
            is_dir: meta.is_dir(),
            size: meta.len(),
        });
        if recursive && meta.is_dir() {
            collect_entries(&entry.path(), entries, true)?;
        }
    }
    Ok(())
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::{BufRead, BufReader, Write};
    use std::os::unix::net::UnixStream;

    fn start_test_agent(workspace: &str) -> (String, watch::Sender<bool>) {
        let dir = tempfile::tempdir().unwrap();
        let sock_path = dir.path().join("test.sock").to_str().unwrap().to_string();
        let (tx, rx) = watch::channel(false);

        let ws = workspace.to_string();
        let sp = sock_path.clone();
        thread::spawn(move || {
            let agent = AgentV2::new(sp, ws);
            agent.run(rx).ok();
        });

        // Wait for socket to appear
        for _ in 0..100 {
            if std::path::Path::new(&sock_path).exists() {
                break;
            }
            thread::sleep(std::time::Duration::from_millis(20));
        }

        // Leak the tempdir so it isn't cleaned up before tests finish
        std::mem::forget(dir);

        (sock_path, tx)
    }

    fn send_request(stream: &mut UnixStream, json: &str) {
        stream.write_all(json.as_bytes()).unwrap();
        stream.write_all(b"\n").unwrap();
        stream.flush().unwrap();
    }

    fn read_line(reader: &mut BufReader<UnixStream>) -> String {
        let mut line = String::new();
        reader.read_line(&mut line).unwrap();
        line
    }

    #[test]
    fn test_ping() {
        let ws = tempfile::tempdir().unwrap();
        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let stream = UnixStream::connect(&sock).unwrap();
        let mut writer = stream.try_clone().unwrap();
        let mut reader = BufReader::new(stream);

        send_request(&mut writer, r#"{"id":"p1","method":"ping"}"#);
        let resp = read_line(&mut reader);
        let v: serde_json::Value = serde_json::from_str(&resp).unwrap();
        assert_eq!(v["id"], "p1");
        assert!(v["result"].is_object());
    }

    #[test]
    fn test_spawn_and_exit() {
        let ws = tempfile::tempdir().unwrap();
        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let stream = UnixStream::connect(&sock).unwrap();
        let mut writer = stream.try_clone().unwrap();
        let mut reader = BufReader::new(stream);

        send_request(
            &mut writer,
            r#"{"id":"s1","method":"spawn","params":{"cmd":["echo","hello v2"]}}"#,
        );

        // Collect all messages until we see an exit event
        let mut got_spawn_result = false;
        let mut got_stdout = false;
        let mut got_exit = false;

        for _ in 0..20 {
            let line = read_line(&mut reader);
            if line.trim().is_empty() {
                continue;
            }
            let v: serde_json::Value = serde_json::from_str(&line).unwrap();
            if v.get("result").is_some() && v.get("id").is_some() && v["id"] == "s1" {
                got_spawn_result = true;
                assert!(v["result"]["handle"].as_str().unwrap().starts_with("proc-"));
            } else if v.get("event").is_some() && v["event"] == "stdout" {
                got_stdout = true;
                assert!(v["data"].as_str().unwrap().contains("hello v2"));
            } else if v.get("event").is_some() && v["event"] == "exit" {
                got_exit = true;
                assert_eq!(v["exit_code"], 0);
                break;
            }
        }

        assert!(got_spawn_result, "expected spawn result");
        assert!(got_stdout, "expected stdout event");
        assert!(got_exit, "expected exit event");
    }

    #[test]
    fn test_spawn_with_stdin() {
        let ws = tempfile::tempdir().unwrap();
        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let stream = UnixStream::connect(&sock).unwrap();
        stream
            .set_read_timeout(Some(std::time::Duration::from_secs(5)))
            .unwrap();
        let mut writer = stream.try_clone().unwrap();
        let mut reader = BufReader::new(stream);

        // cat will read from stdin and echo it
        send_request(
            &mut writer,
            r#"{"id":"s2","method":"spawn","params":{"cmd":["cat"],"stdin":true}}"#,
        );

        // Read spawn response
        let line = read_line(&mut reader);
        let v: serde_json::Value = serde_json::from_str(&line).unwrap();
        let handle = v["result"]["handle"].as_str().unwrap().to_string();

        // Send stdin data
        let stdin_msg = format!(
            r#"{{"id":"i1","method":"stdin","params":{{"handle":"{handle}","data":"test input\n"}}}}"#
        );
        send_request(&mut writer, &stdin_msg);

        // Collect messages: wait for stdout echo, then signal to terminate
        let mut got_output = false;
        let mut got_exit = false;
        let mut signaled = false;
        for _ in 0..30 {
            let line = match read_line_timeout(&mut reader) {
                Some(l) => l,
                None => break,
            };
            if line.trim().is_empty() {
                continue;
            }
            let v: serde_json::Value = serde_json::from_str(&line).unwrap();

            // Skip stdin ack responses
            if v.get("id").is_some() && v.get("result").is_some() {
                continue;
            }
            if v.get("event").is_some() && v["event"] == "stdout" {
                if v["data"].as_str().unwrap().contains("test input") {
                    got_output = true;
                    // Now that we got the echo, signal to terminate
                    if !signaled {
                        signaled = true;
                        let sig_msg = format!(
                            r#"{{"id":"i2","method":"signal","params":{{"handle":"{handle}","signal":"SIGTERM"}}}}"#
                        );
                        send_request(&mut writer, &sig_msg);
                    }
                }
            }
            if v.get("event").is_some() && v["event"] == "exit" {
                got_exit = true;
                break;
            }
        }

        assert!(got_output, "expected cat to echo our stdin data");
        assert!(got_exit, "expected exit event");
    }

    #[test]
    fn test_stat_existing_file() {
        let ws = tempfile::tempdir().unwrap();
        fs::write(ws.path().join("hello.txt"), "content").unwrap();
        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let stream = UnixStream::connect(&sock).unwrap();
        let mut writer = stream.try_clone().unwrap();
        let mut reader = BufReader::new(stream);

        send_request(
            &mut writer,
            r#"{"id":"st1","method":"stat","params":{"path":"hello.txt"}}"#,
        );

        let line = read_line(&mut reader);
        let v: serde_json::Value = serde_json::from_str(&line).unwrap();
        assert_eq!(v["id"], "st1");
        assert_eq!(v["result"]["exists"], true);
        assert_eq!(v["result"]["is_dir"], false);
        assert_eq!(v["result"]["size"], 7); // "content" = 7 bytes
    }

    #[test]
    fn test_stat_nonexistent() {
        let ws = tempfile::tempdir().unwrap();
        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let stream = UnixStream::connect(&sock).unwrap();
        let mut writer = stream.try_clone().unwrap();
        let mut reader = BufReader::new(stream);

        send_request(
            &mut writer,
            r#"{"id":"st2","method":"stat","params":{"path":"nope.txt"}}"#,
        );

        let line = read_line(&mut reader);
        let v: serde_json::Value = serde_json::from_str(&line).unwrap();
        assert_eq!(v["id"], "st2");
        assert_eq!(v["result"]["exists"], false);
    }

    #[test]
    fn test_list_dir() {
        let ws = tempfile::tempdir().unwrap();
        fs::write(ws.path().join("a.txt"), "aaa").unwrap();
        fs::create_dir(ws.path().join("sub")).unwrap();
        fs::write(ws.path().join("sub/b.txt"), "bbb").unwrap();

        // We need to stat a relative subdir, so create one level
        let sub = ws.path().join("mydir");
        fs::create_dir(&sub).unwrap();
        fs::write(sub.join("c.txt"), "ccc").unwrap();

        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let stream = UnixStream::connect(&sock).unwrap();
        let mut writer = stream.try_clone().unwrap();
        let mut reader = BufReader::new(stream);

        send_request(
            &mut writer,
            r#"{"id":"ld1","method":"list_dir","params":{"path":"mydir"}}"#,
        );

        let line = read_line(&mut reader);
        let v: serde_json::Value = serde_json::from_str(&line).unwrap();
        assert_eq!(v["id"], "ld1");
        let entries = v["result"]["entries"].as_array().unwrap();
        assert_eq!(entries.len(), 1);
        assert_eq!(entries[0]["name"], "c.txt");
    }

    #[test]
    fn test_read_file() {
        let ws = tempfile::tempdir().unwrap();
        let content = "hello file read";
        fs::write(ws.path().join("data.txt"), content).unwrap();

        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let stream = UnixStream::connect(&sock).unwrap();
        let mut writer = stream.try_clone().unwrap();
        let mut reader = BufReader::new(stream);

        send_request(
            &mut writer,
            r#"{"id":"rf1","method":"read_file","params":{"path":"data.txt"}}"#,
        );

        let mut chunks = Vec::new();
        let mut final_result = None;

        for _ in 0..10 {
            let line = read_line(&mut reader);
            if line.trim().is_empty() {
                continue;
            }
            let v: serde_json::Value = serde_json::from_str(&line).unwrap();
            if v.get("chunk").is_some() {
                let b64 = v["chunk"]["content"].as_str().unwrap();
                let decoded = base64::engine::general_purpose::STANDARD
                    .decode(b64)
                    .unwrap();
                chunks.extend_from_slice(&decoded);
            } else if v.get("result").is_some() {
                final_result = Some(v);
                break;
            }
        }

        assert_eq!(String::from_utf8(chunks).unwrap(), content);
        let result = final_result.unwrap();
        assert_eq!(result["result"]["size_bytes"], content.len() as i64);
        assert!(!result["result"]["sha256"].as_str().unwrap().is_empty());
    }

    #[test]
    fn test_spawn_with_pty() {
        let ws = tempfile::tempdir().unwrap();
        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let stream = UnixStream::connect(&sock).unwrap();
        stream
            .set_read_timeout(Some(std::time::Duration::from_secs(5)))
            .unwrap();
        let mut writer = stream.try_clone().unwrap();
        let mut reader = BufReader::new(stream);

        send_request(
            &mut writer,
            r#"{"id":"p1","method":"spawn","params":{"cmd":["echo","pty-test"],"pty":{"rows":24,"cols":80}}}"#,
        );

        let line = read_line(&mut reader);
        let v: serde_json::Value = serde_json::from_str(&line).unwrap();
        let handle = v["result"]["handle"].as_str().unwrap().to_string();
        assert!(handle.starts_with("proc-"));

        // Collect until exit
        let mut got_output = false;
        let mut got_exit = false;
        for _ in 0..20 {
            match read_line_timeout(&mut reader) {
                Some(line) => {
                    if line.trim().is_empty() {
                        continue;
                    }
                    let v: serde_json::Value = serde_json::from_str(&line).unwrap();
                    if v.get("event").is_some() && v["event"] == "stdout" {
                        if v["data"].as_str().unwrap().contains("pty-test") {
                            got_output = true;
                        }
                    }
                    if v.get("event").is_some() && v["event"] == "exit" {
                        got_exit = true;
                        assert_eq!(v["exit_code"], 0);
                        break;
                    }
                }
                None => break,
            }
        }

        assert!(got_output, "expected pty output");
        assert!(got_exit, "expected exit event");
    }

    fn read_line_timeout(reader: &mut BufReader<UnixStream>) -> Option<String> {
        let mut line = String::new();
        match reader.read_line(&mut line) {
            Ok(0) => None,
            Ok(_) => Some(line),
            Err(_) => None,
        }
    }
}
