use crate::path_security::resolve_workspace_path;
use crate::pty_util;
use super::protocol::*;

use base64::Engine as _;
use inotify::{EventMask, Inotify, WatchDescriptor, WatchMask};
use sha2::{Digest, Sha256};
use std::collections::HashMap;
use std::io::{self, Read, Write as _};
use std::net::TcpStream;
use std::os::fd::{AsRawFd, FromRawFd, OwnedFd};
use std::os::unix::net::UnixListener;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::sync::atomic::{AtomicBool, Ordering};
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
    /// Subscription IDs watching for this process to exit.
    exit_subscribers: Vec<String>,
}

/// State for an active TCP tunnel.
struct TunnelState {
    /// Write half of the TCP connection (reader is owned by its thread).
    tcp_writer: TcpStream,
}

/// Tracks resources owned by a single subscription.
struct SubscriptionState {
    /// Shutdown flags for inotify watcher threads.
    fs_shutdowns: Vec<Arc<AtomicBool>>,
    /// Process handles whose exit_subscribers list contains this subscription.
    watched_process_handles: Vec<String>,
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

/// Transport-agnostic shared writer for responses and events.
pub type SharedWriter = Arc<Mutex<Box<dyn io::Write + Send>>>;

fn send_json(writer: &SharedWriter, value: &impl serde::Serialize) -> io::Result<()> {
    let mut w = writer.lock().unwrap();
    serde_json::to_writer(&mut **w, value)
        .map_err(io::Error::other)?;
    w.write_all(b"\n")?;
    w.flush()
}

fn handle_conn(
    stream: std::os::unix::net::UnixStream,
    workspace: &str,
    _shutdown: watch::Receiver<bool>,
) -> io::Result<()> {
    let reader = io::BufReader::new(stream.try_clone()?);
    let writer: SharedWriter = Arc::new(Mutex::new(Box::new(stream)));
    handle_v2_session(reader, writer, workspace)
}

/// Transport-agnostic V2 session handler. Called from both Unix socket and iroh QUIC paths.
pub fn handle_v2_session(
    reader: impl io::BufRead,
    writer: SharedWriter,
    workspace: &str,
) -> io::Result<()> {
    let processes: Arc<Mutex<HashMap<String, ProcessHandle>>> =
        Arc::new(Mutex::new(HashMap::new()));
    let tunnels: Arc<Mutex<HashMap<String, TunnelState>>> =
        Arc::new(Mutex::new(HashMap::new()));
    let subscriptions: Arc<Mutex<HashMap<String, SubscriptionState>>> =
        Arc::new(Mutex::new(HashMap::new()));

    let result = handle_messages(
        reader,
        writer.clone(),
        workspace,
        &processes,
        &tunnels,
        &subscriptions,
    );

    // Cleanup on disconnect
    let mut procs = processes.lock().unwrap();
    for (handle, ph) in procs.iter_mut() {
        if let Some(ref mut child) = ph.child {
            log::info!("[cleanup] killing handle={handle} pid={}", ph.pid);
            let _ = child.kill();
            let _ = child.wait();
        }
    }
    procs.clear();

    let mut tuns = tunnels.lock().unwrap();
    for (handle, ts) in tuns.drain() {
        log::info!("[cleanup] closing tunnel handle={handle}");
        let _ = ts.tcp_writer.shutdown(std::net::Shutdown::Both);
    }

    let mut subs = subscriptions.lock().unwrap();
    for (sub_id, state) in subs.drain() {
        log::info!("[cleanup] removing subscription id={sub_id}");
        for flag in &state.fs_shutdowns {
            flag.store(true, Ordering::Relaxed);
        }
    }

    result
}

fn handle_messages(
    reader: impl io::BufRead,
    writer: SharedWriter,
    workspace: &str,
    processes: &Arc<Mutex<HashMap<String, ProcessHandle>>>,
    tunnels: &Arc<Mutex<HashMap<String, TunnelState>>>,
    subscriptions: &Arc<Mutex<HashMap<String, SubscriptionState>>>,
) -> io::Result<()> {
    let mut lines = reader.lines();
    while let Some(line_result) = lines.next() {
        let line = match line_result {
            Ok(l) => l,
            Err(e) => {
                if e.kind() == io::ErrorKind::UnexpectedEof {
                    return Ok(());
                }
                let _ = send_json(
                    &writer,
                    &V2Response::err(String::new(), "READ_ERROR", format!("{e}")),
                );
                return Ok(());
            }
        };
        if line.trim().is_empty() {
            continue;
        }
        let req: V2Request = match serde_json::from_str(&line) {
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
                handle_write_file(&id, params, workspace, &writer, &mut lines);
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
            V2Request::Tunnel { id, params } => {
                log::info!("[v2:tunnel] id={id} target={}", params.target);
                handle_tunnel(&id, params, &writer, tunnels);
            }
            V2Request::TunnelData { id, params } => {
                handle_tunnel_data(&id, params, &writer, tunnels);
            }
            V2Request::TunnelClose { id, params } => {
                log::info!("[v2:tunnel_close] id={id} handle={}", params.handle);
                handle_tunnel_close(&id, params, &writer, tunnels);
            }
            V2Request::Subscribe { id, params } => {
                log::info!("[v2:subscribe] id={id} events={}", params.events.len());
                handle_subscribe(&id, params, workspace, &writer, processes, subscriptions);
            }
            V2Request::Unsubscribe { id, params } => {
                log::info!("[v2:unsubscribe] id={id} sub={}", params.subscription_id);
                handle_unsubscribe(&id, params, &writer, processes, subscriptions);
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
        exit_subscribers: Vec::new(),
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
        exit_subscribers: Vec::new(),
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

    // Clean up process handle and collect exit subscribers
    let exit_subs = {
        let mut procs = processes.lock().unwrap();
        let subs = if let Some(ph) = procs.get_mut(handle) {
            ph.pty_master.take();
            ph.stdin_pipe.take();
            std::mem::take(&mut ph.exit_subscribers)
        } else {
            Vec::new()
        };
        procs.remove(handle);
        subs
    };

    log::info!("[v2:exit] handle={handle} exit_code={exit_code}");
    let _ = send_json(
        writer,
        &V2Event::Exit {
            handle: handle.into(),
            exit_code,
        },
    );

    // Notify process-exit subscribers
    for sub_id in exit_subs {
        let _ = send_json(
            writer,
            &V2Event::ProcessExit {
                subscription_id: sub_id,
                handle: handle.into(),
                exit_code,
            },
        );
    }
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
// write_file
// ---------------------------------------------------------------------------

fn handle_write_file(
    id: &str,
    params: WriteFileParams,
    workspace: &str,
    writer: &SharedWriter,
    lines: &mut impl Iterator<Item = io::Result<String>>,
) {
    let target = match resolve_workspace_path(std::path::Path::new(workspace), &params.path) {
        Ok(p) => p,
        Err(e) => {
            let _ = send_json(writer, &V2Response::err(id.into(), "PATH_ERROR", e));
            return;
        }
    };

    let parent = match target.parent() {
        Some(p) => p.to_path_buf(),
        None => {
            let _ = send_json(writer, &V2Response::err(id.into(), "PATH_ERROR", "invalid target".into()));
            return;
        }
    };

    if let Err(e) = fs::create_dir_all(&parent) {
        let _ = send_json(writer, &V2Response::err(id.into(), "WRITE_FAILED", format!("mkdir: {e}")));
        return;
    }

    let base_name = target.file_name().unwrap_or_default().to_string_lossy();
    let tmp_path = parent.join(format!(".{base_name}.{}.tmp", std::process::id()));

    let tmp_file = match fs::File::create(&tmp_path) {
        Ok(f) => f,
        Err(e) => {
            let _ = send_json(writer, &V2Response::err(id.into(), "WRITE_FAILED", format!("create temp: {e}")));
            return;
        }
    };

    let mut tmp_writer = io::BufWriter::new(tmp_file);
    let mut hasher = Sha256::new();
    let mut written: i64 = 0;

    let result = (|| -> Result<(), String> {
        for line_result in lines {
            let line = line_result.map_err(|e| format!("read: {e}"))?;
            if line.trim().is_empty() {
                continue;
            }
            let chunk: V2Request = serde_json::from_str(&line)
                .map_err(|e| format!("parse chunk: {e}"))?;

            match chunk {
                V2Request::FileChunk { id: chunk_id, params: chunk_params } => {
                    if !chunk_id.is_empty() && chunk_id != id {
                        return Err("file chunk id mismatch".into());
                    }
                    let bytes = base64::engine::general_purpose::STANDARD
                        .decode(&chunk_params.content)
                        .map_err(|e| format!("base64 decode: {e}"))?;
                    if bytes.is_empty() {
                        continue;
                    }
                    use io::Write;
                    tmp_writer.write_all(&bytes).map_err(|e| format!("write: {e}"))?;
                    hasher.update(&bytes);
                    written += bytes.len() as i64;
                }
                V2Request::FileDone { .. } => {
                    use io::Write;
                    tmp_writer.flush().map_err(|e| format!("flush: {e}"))?;
                    drop(tmp_writer);

                    let actual_sha = hex::encode(hasher.finalize());

                    if let Some(ref expected) = params.sha256 {
                        if !expected.eq_ignore_ascii_case(&actual_sha) {
                            return Err(format!("sha256 mismatch: expected {expected} got {actual_sha}"));
                        }
                    }

                    use std::os::unix::fs::PermissionsExt;
                    fs::set_permissions(&tmp_path, fs::Permissions::from_mode(0o644))
                        .map_err(|e| format!("chmod: {e}"))?;
                    fs::rename(&tmp_path, &target)
                        .map_err(|e| format!("rename: {e}"))?;

                    let _ = send_json(
                        writer,
                        &V2Response::result(
                            id.into(),
                            serde_json::to_value(WriteFileResult {
                                bytes_written: written,
                                sha256: actual_sha,
                            }).unwrap(),
                        ),
                    );
                    return Ok(());
                }
                _ => {
                    return Err(format!("unexpected message during file upload"));
                }
            }
        }
        Err("unexpected EOF during file upload".into())
    })();

    if let Err(e) = result {
        let _ = fs::remove_file(&tmp_path);
        let _ = send_json(writer, &V2Response::err(id.into(), "WRITE_FAILED", e));
    }
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

// ---------------------------------------------------------------------------
// tunnel
// ---------------------------------------------------------------------------

fn handle_tunnel(
    id: &str,
    params: TunnelParams,
    writer: &SharedWriter,
    tunnels: &Arc<Mutex<HashMap<String, TunnelState>>>,
) {
    let tcp_stream = match TcpStream::connect(&params.target) {
        Ok(s) => s,
        Err(e) => {
            let _ = send_json(
                writer,
                &V2Response::err(id.into(), "TUNNEL_FAILED", format!("{e}")),
            );
            return;
        }
    };

    let handle = params
        .handle
        .unwrap_or_else(|| format!("tun-{}", &uuid::Uuid::new_v4().to_string()[..8]));

    let reader_stream = match tcp_stream.try_clone() {
        Ok(s) => s,
        Err(e) => {
            let _ = send_json(
                writer,
                &V2Response::err(id.into(), "TUNNEL_FAILED", format!("{e}")),
            );
            return;
        }
    };

    tunnels.lock().unwrap().insert(
        handle.clone(),
        TunnelState {
            tcp_writer: tcp_stream,
        },
    );

    let _ = send_json(
        writer,
        &V2Response::result(
            id.into(),
            serde_json::to_value(TunnelResult {
                handle: handle.clone(),
            })
            .unwrap(),
        ),
    );

    // Spawn a reader thread that pushes data from TCP -> events
    let w = writer.clone();
    let h = handle.clone();
    let tuns = tunnels.clone();
    thread::spawn(move || {
        let mut buf = [0u8; 64 * 1024];
        let mut stream = reader_stream;
        loop {
            match stream.read(&mut buf) {
                Ok(0) => {
                    let _ = send_json(
                        &w,
                        &V2Event::TunnelClosed {
                            handle: h.clone(),
                            reason: "remote closed".into(),
                        },
                    );
                    break;
                }
                Ok(n) => {
                    let encoded =
                        base64::engine::general_purpose::STANDARD.encode(&buf[..n]);
                    let _ = send_json(
                        &w,
                        &V2Event::TunnelData {
                            handle: h.clone(),
                            data: encoded,
                        },
                    );
                }
                Err(e) => {
                    let _ = send_json(
                        &w,
                        &V2Event::TunnelClosed {
                            handle: h.clone(),
                            reason: format!("{e}"),
                        },
                    );
                    break;
                }
            }
        }
        tuns.lock().unwrap().remove(&h);
    });
}

fn handle_tunnel_data(
    id: &str,
    params: TunnelDataParams,
    writer: &SharedWriter,
    tunnels: &Arc<Mutex<HashMap<String, TunnelState>>>,
) {
    let decoded = match base64::engine::general_purpose::STANDARD.decode(&params.data) {
        Ok(d) => d,
        Err(e) => {
            let _ = send_json(
                writer,
                &V2Response::err(id.into(), "INVALID_DATA", format!("base64: {e}")),
            );
            return;
        }
    };

    let mut tuns = tunnels.lock().unwrap();
    let ts = match tuns.get_mut(&params.handle) {
        Some(t) => t,
        None => {
            let _ = send_json(
                writer,
                &V2Response::err(
                    id.into(),
                    "UNKNOWN_HANDLE",
                    format!("no tunnel with handle {}", params.handle),
                ),
            );
            return;
        }
    };

    if let Err(e) = ts.tcp_writer.write_all(&decoded) {
        let _ = send_json(
            writer,
            &V2Response::err(id.into(), "TUNNEL_WRITE_FAILED", format!("{e}")),
        );
        return;
    }
    if let Err(e) = ts.tcp_writer.flush() {
        let _ = send_json(
            writer,
            &V2Response::err(id.into(), "TUNNEL_WRITE_FAILED", format!("{e}")),
        );
        return;
    }

    let _ = send_json(writer, &V2Response::ok(id.into()));
}

fn handle_tunnel_close(
    id: &str,
    params: TunnelCloseParams,
    writer: &SharedWriter,
    tunnels: &Arc<Mutex<HashMap<String, TunnelState>>>,
) {
    let mut tuns = tunnels.lock().unwrap();
    match tuns.remove(&params.handle) {
        Some(ts) => {
            let _ = ts.tcp_writer.shutdown(std::net::Shutdown::Both);
            let _ = send_json(writer, &V2Response::ok(id.into()));
        }
        None => {
            let _ = send_json(
                writer,
                &V2Response::err(
                    id.into(),
                    "UNKNOWN_HANDLE",
                    format!("no tunnel with handle {}", params.handle),
                ),
            );
        }
    }
}

// ---------------------------------------------------------------------------
// subscribe / unsubscribe
// ---------------------------------------------------------------------------

fn handle_subscribe(
    id: &str,
    params: SubscribeParams,
    workspace: &str,
    writer: &SharedWriter,
    processes: &Arc<Mutex<HashMap<String, ProcessHandle>>>,
    subscriptions: &Arc<Mutex<HashMap<String, SubscriptionState>>>,
) {
    let sub_id = format!("sub-{}", &uuid::Uuid::new_v4().to_string()[..8]);
    let mut state = SubscriptionState {
        fs_shutdowns: Vec::new(),
        watched_process_handles: Vec::new(),
    };

    for spec in &params.events {
        match spec {
            SubscribeEventSpec::FsChange { path, recursive } => {
                let watch_path = resolve_watch_path(Path::new(workspace), path);
                let watch_path = match watch_path {
                    Ok(p) => p,
                    Err(e) => {
                        let _ = send_json(
                            writer,
                            &V2Response::err(id.into(), "SUBSCRIBE_FAILED", e),
                        );
                        // Clean up anything already registered for this sub
                        cleanup_partial_subscription(&state, processes);
                        return;
                    }
                };

                let shutdown = Arc::new(AtomicBool::new(false));
                state.fs_shutdowns.push(shutdown.clone());

                let w = writer.clone();
                let sid = sub_id.clone();
                let recursive = *recursive;
                thread::spawn(move || {
                    run_fs_watcher(watch_path, recursive, &sid, &shutdown, &w);
                });
            }
            SubscribeEventSpec::ProcessExit { handle } => {
                let mut procs = processes.lock().unwrap();
                match procs.get_mut(handle) {
                    Some(ph) => {
                        ph.exit_subscribers.push(sub_id.clone());
                        state.watched_process_handles.push(handle.clone());
                    }
                    None => {
                        drop(procs);
                        let _ = send_json(
                            writer,
                            &V2Response::err(
                                id.into(),
                                "UNKNOWN_HANDLE",
                                format!("no process with handle {handle}"),
                            ),
                        );
                        cleanup_partial_subscription(&state, processes);
                        return;
                    }
                }
            }
        }
    }

    subscriptions
        .lock()
        .unwrap()
        .insert(sub_id.clone(), state);

    let _ = send_json(
        writer,
        &V2Response::result(
            id.into(),
            serde_json::to_value(SubscribeResult {
                subscription_id: sub_id,
            })
            .unwrap(),
        ),
    );
}

fn handle_unsubscribe(
    id: &str,
    params: UnsubscribeParams,
    writer: &SharedWriter,
    processes: &Arc<Mutex<HashMap<String, ProcessHandle>>>,
    subscriptions: &Arc<Mutex<HashMap<String, SubscriptionState>>>,
) {
    let mut subs = subscriptions.lock().unwrap();
    match subs.remove(&params.subscription_id) {
        Some(state) => {
            // Stop inotify watchers
            for flag in &state.fs_shutdowns {
                flag.store(true, Ordering::Relaxed);
            }
            // Remove from process exit subscriber lists
            let mut procs = processes.lock().unwrap();
            for ph_handle in &state.watched_process_handles {
                if let Some(ph) = procs.get_mut(ph_handle) {
                    ph.exit_subscribers
                        .retain(|s| s != &params.subscription_id);
                }
            }
            let _ = send_json(writer, &V2Response::ok(id.into()));
        }
        None => {
            let _ = send_json(
                writer,
                &V2Response::err(
                    id.into(),
                    "UNKNOWN_SUBSCRIPTION",
                    format!("no subscription with id {}", params.subscription_id),
                ),
            );
        }
    }
}

/// Undo partially registered subscription resources on error.
fn cleanup_partial_subscription(
    state: &SubscriptionState,
    processes: &Arc<Mutex<HashMap<String, ProcessHandle>>>,
) {
    for flag in &state.fs_shutdowns {
        flag.store(true, Ordering::Relaxed);
    }
    // We don't have the sub_id here to remove from exit_subscribers,
    // but since the subscription was never fully registered, the sub_id
    // entries will be harmless (they'll fire once and be ignored by the
    // client which never got a subscription_id).
    let _ = processes; // suppress unused warning
}

/// Resolve a watch path, accepting both absolute (within workspace) and
/// relative paths.
fn resolve_watch_path(workspace: &Path, path: &str) -> Result<PathBuf, String> {
    let resolved = if Path::new(path).is_absolute() {
        PathBuf::from(path)
    } else {
        workspace.join(path)
    };

    let canon_workspace = workspace
        .canonicalize()
        .map_err(|e| format!("resolve workspace: {e}"))?;

    // For the watch path, it must exist (inotify requires it)
    let canon_path = resolved
        .canonicalize()
        .map_err(|e| format!("resolve watch path: {e}"))?;

    if !canon_path.starts_with(&canon_workspace) {
        return Err("watch path must be within workspace".to_string());
    }

    Ok(canon_path)
}

/// Run an inotify watcher in a polling loop until shutdown is signaled.
fn run_fs_watcher(
    path: PathBuf,
    recursive: bool,
    subscription_id: &str,
    shutdown: &AtomicBool,
    writer: &SharedWriter,
) {
    let mut inotify = match Inotify::init() {
        Ok(i) => i,
        Err(e) => {
            log::error!("[fs_watch] inotify init failed: {e}");
            return;
        }
    };

    // Set non-blocking so we can poll with a shutdown check
    let fd = inotify.as_raw_fd();
    unsafe {
        let flags = libc::fcntl(fd, libc::F_GETFL);
        libc::fcntl(fd, libc::F_SETFL, flags | libc::O_NONBLOCK);
    }

    let mask = WatchMask::CREATE
        | WatchMask::MODIFY
        | WatchMask::DELETE
        | WatchMask::MOVED_FROM
        | WatchMask::MOVED_TO;

    let mut wd_to_path: HashMap<WatchDescriptor, PathBuf> = HashMap::new();

    match inotify.watches().add(&path, mask) {
        Ok(wd) => {
            wd_to_path.insert(wd, path.clone());
        }
        Err(e) => {
            log::error!("[fs_watch] add watch failed for {}: {e}", path.display());
            return;
        }
    }

    // For recursive watches, add watches on all existing subdirectories
    if recursive {
        add_recursive_watches(&mut inotify, &path, mask, &mut wd_to_path);
    }

    let mut buf = [0u8; 4096];
    loop {
        if shutdown.load(Ordering::Relaxed) {
            break;
        }

        match inotify.read_events(&mut buf) {
            Ok(events) => {
                for event in events {
                    let dir_path = match wd_to_path.get(&event.wd) {
                        Some(p) => p.clone(),
                        None => continue,
                    };

                    let file_name = match event.name {
                        Some(name) => name.to_string_lossy().into_owned(),
                        None => continue,
                    };

                    let full_path = dir_path.join(&file_name);
                    let change_type = mask_to_change_type(event.mask);

                    if let Some(ct) = change_type {
                        let _ = send_json(
                            writer,
                            &V2Event::FsChange {
                                subscription_id: subscription_id.to_string(),
                                path: full_path.to_string_lossy().into_owned(),
                                change_type: ct.to_string(),
                            },
                        );
                    }

                    // If a new directory was created and we're recursive, watch it
                    if recursive && event.mask.contains(EventMask::ISDIR) && event.mask.contains(EventMask::CREATE) {
                        if let Ok(wd) = inotify.watches().add(&full_path, mask) {
                            wd_to_path.insert(wd, full_path);
                        }
                    }
                }
            }
            Err(ref e) if e.kind() == io::ErrorKind::WouldBlock => {
                thread::sleep(std::time::Duration::from_millis(100));
            }
            Err(e) => {
                log::error!("[fs_watch] read error: {e}");
                break;
            }
        }
    }
}

fn add_recursive_watches(
    inotify: &mut Inotify,
    dir: &Path,
    mask: WatchMask,
    wd_to_path: &mut HashMap<WatchDescriptor, PathBuf>,
) {
    let entries = match fs::read_dir(dir) {
        Ok(e) => e,
        Err(_) => return,
    };
    for entry in entries.flatten() {
        if let Ok(meta) = entry.metadata() {
            if meta.is_dir() {
                let sub = entry.path();
                if let Ok(wd) = inotify.watches().add(&sub, mask) {
                    wd_to_path.insert(wd, sub.clone());
                }
                add_recursive_watches(inotify, &sub, mask, wd_to_path);
            }
        }
    }
}

fn mask_to_change_type(mask: EventMask) -> Option<&'static str> {
    if mask.contains(EventMask::CREATE) || mask.contains(EventMask::MOVED_TO) {
        Some("created")
    } else if mask.contains(EventMask::MODIFY) {
        Some("modified")
    } else if mask.contains(EventMask::DELETE) || mask.contains(EventMask::MOVED_FROM) {
        Some("deleted")
    } else {
        None
    }
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

    #[test]
    fn test_tunnel_echo() {
        use std::net::TcpListener;

        // Start a simple TCP echo server
        let echo_listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let echo_addr = echo_listener.local_addr().unwrap();
        thread::spawn(move || {
            if let Ok((mut stream, _)) = echo_listener.accept() {
                let mut buf = [0u8; 4096];
                loop {
                    match stream.read(&mut buf) {
                        Ok(0) | Err(_) => break,
                        Ok(n) => {
                            if stream.write_all(&buf[..n]).is_err() {
                                break;
                            }
                            stream.flush().ok();
                        }
                    }
                }
            }
        });

        let ws = tempfile::tempdir().unwrap();
        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let stream = UnixStream::connect(&sock).unwrap();
        stream
            .set_read_timeout(Some(std::time::Duration::from_secs(5)))
            .unwrap();
        let mut writer = stream.try_clone().unwrap();
        let mut reader = BufReader::new(stream);

        // Open tunnel
        let tunnel_req = format!(
            r#"{{"id":"t1","method":"tunnel","params":{{"target":"{}","handle":"tun-echo"}}}}"#,
            echo_addr
        );
        send_request(&mut writer, &tunnel_req);

        let line = read_line(&mut reader);
        let v: serde_json::Value = serde_json::from_str(&line).unwrap();
        assert_eq!(v["id"], "t1");
        assert_eq!(v["result"]["handle"], "tun-echo");

        // Send data through the tunnel
        let payload = b"hello tunnel";
        let b64 = base64::engine::general_purpose::STANDARD.encode(payload);
        let data_req = format!(
            r#"{{"id":"d1","method":"tunnel_data","params":{{"handle":"tun-echo","data":"{b64}"}}}}"#
        );
        send_request(&mut writer, &data_req);

        // Expect tunnel_data ack and echo event
        let mut got_ack = false;
        let mut got_echo = false;
        for _ in 0..20 {
            let line = match read_line_timeout(&mut reader) {
                Some(l) => l,
                None => break,
            };
            if line.trim().is_empty() {
                continue;
            }
            let v: serde_json::Value = serde_json::from_str(&line).unwrap();
            if v.get("id").is_some() && v["id"] == "d1" && v.get("result").is_some() {
                got_ack = true;
            }
            if v.get("event").is_some() && v["event"] == "tunnel_data" {
                assert_eq!(v["handle"], "tun-echo");
                let decoded = base64::engine::general_purpose::STANDARD
                    .decode(v["data"].as_str().unwrap())
                    .unwrap();
                if decoded == payload {
                    got_echo = true;
                    break;
                }
            }
        }

        assert!(got_ack, "expected tunnel_data ack");
        assert!(got_echo, "expected echoed data from tunnel");

        // Close the tunnel
        send_request(
            &mut writer,
            r#"{"id":"c1","method":"tunnel_close","params":{"handle":"tun-echo"}}"#,
        );
        let line = read_line(&mut reader);
        let v: serde_json::Value = serde_json::from_str(&line).unwrap();
        assert_eq!(v["id"], "c1");
        assert!(v["result"].is_object());
    }

    #[test]
    fn test_subscribe_fs_change() {
        let ws = tempfile::tempdir().unwrap();
        let output_dir = ws.path().join("output");
        fs::create_dir(&output_dir).unwrap();

        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let stream = UnixStream::connect(&sock).unwrap();
        stream
            .set_read_timeout(Some(std::time::Duration::from_secs(5)))
            .unwrap();
        let mut writer = stream.try_clone().unwrap();
        let mut reader = BufReader::new(stream);

        // Subscribe to fs_change on the output directory
        send_request(
            &mut writer,
            r#"{"id":"sub1","method":"subscribe","params":{"events":[{"type":"fs_change","path":"output"}]}}"#,
        );

        let line = read_line(&mut reader);
        let v: serde_json::Value = serde_json::from_str(&line).unwrap();
        assert_eq!(v["id"], "sub1");
        let sub_id = v["result"]["subscription_id"].as_str().unwrap().to_string();
        assert!(sub_id.starts_with("sub-"));

        // Give the inotify watcher time to start
        thread::sleep(std::time::Duration::from_millis(200));

        // Create a file in the watched directory
        fs::write(output_dir.join("model.bin"), "data").unwrap();

        // Wait for fs_change event
        let mut got_event = false;
        for _ in 0..30 {
            let line = match read_line_timeout(&mut reader) {
                Some(l) => l,
                None => break,
            };
            if line.trim().is_empty() {
                continue;
            }
            let v: serde_json::Value = serde_json::from_str(&line).unwrap();
            if v.get("event").is_some() && v["event"] == "fs_change" {
                assert_eq!(v["subscription_id"], sub_id);
                let path = v["path"].as_str().unwrap();
                assert!(path.contains("model.bin"), "path should contain model.bin, got: {path}");
                got_event = true;
                break;
            }
        }

        assert!(got_event, "expected fs_change event");
    }

    #[test]
    fn test_unsubscribe() {
        let ws = tempfile::tempdir().unwrap();
        let output_dir = ws.path().join("watched");
        fs::create_dir(&output_dir).unwrap();

        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let stream = UnixStream::connect(&sock).unwrap();
        stream
            .set_read_timeout(Some(std::time::Duration::from_secs(2)))
            .unwrap();
        let mut writer = stream.try_clone().unwrap();
        let mut reader = BufReader::new(stream);

        // Subscribe
        send_request(
            &mut writer,
            r#"{"id":"sub1","method":"subscribe","params":{"events":[{"type":"fs_change","path":"watched"}]}}"#,
        );

        let line = read_line(&mut reader);
        let v: serde_json::Value = serde_json::from_str(&line).unwrap();
        let sub_id = v["result"]["subscription_id"].as_str().unwrap().to_string();

        // Give the watcher time to start
        thread::sleep(std::time::Duration::from_millis(200));

        // Unsubscribe
        let unsub_req = format!(
            r#"{{"id":"u1","method":"unsubscribe","params":{{"subscription_id":"{sub_id}"}}}}"#
        );
        send_request(&mut writer, &unsub_req);

        let line = read_line(&mut reader);
        let v: serde_json::Value = serde_json::from_str(&line).unwrap();
        assert_eq!(v["id"], "u1");
        assert!(v["result"].is_object());

        // Give the watcher thread time to stop
        thread::sleep(std::time::Duration::from_millis(300));

        // Create a file after unsubscribe
        fs::write(output_dir.join("should_not_fire.txt"), "data").unwrap();

        // Wait a bit, then try to read - should not get any fs_change event
        thread::sleep(std::time::Duration::from_millis(300));

        // Send a ping to flush the connection, then check there's no fs_change
        send_request(&mut writer, r#"{"id":"p1","method":"ping"}"#);

        let mut got_fs_event = false;
        for _ in 0..10 {
            let line = match read_line_timeout(&mut reader) {
                Some(l) => l,
                None => break,
            };
            if line.trim().is_empty() {
                continue;
            }
            let v: serde_json::Value = serde_json::from_str(&line).unwrap();
            if v.get("event").is_some() && v["event"] == "fs_change" {
                got_fs_event = true;
            }
            if v.get("id").is_some() && v["id"] == "p1" {
                break;
            }
        }

        assert!(!got_fs_event, "should not receive fs_change after unsubscribe");
    }

    #[test]
    fn test_write_file() {
        let ws = tempfile::tempdir().unwrap();
        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let stream = UnixStream::connect(&sock).unwrap();
        let mut writer = stream.try_clone().unwrap();
        let mut reader = BufReader::new(stream);

        let content = b"hello write_file v2!";
        let content_b64 = base64::engine::general_purpose::STANDARD.encode(content);
        let expected_sha = hex::encode(sha2::Sha256::digest(content));

        send_request(
            &mut writer,
            &format!(r#"{{"id":"wf1","method":"write_file","params":{{"path":"test_output.txt","sha256":"{expected_sha}"}}}}"#),
        );
        send_request(
            &mut writer,
            &format!(r#"{{"id":"wf1","method":"file_chunk","params":{{"content":"{content_b64}"}}}}"#),
        );
        send_request(&mut writer, r#"{"id":"wf1","method":"file_done"}"#);

        let line = read_line(&mut reader);
        let v: serde_json::Value = serde_json::from_str(&line).unwrap();
        assert_eq!(v["id"], "wf1");
        assert_eq!(v["result"]["bytes_written"], content.len() as i64);
        assert_eq!(v["result"]["sha256"], expected_sha);

        let written = std::fs::read(ws.path().join("test_output.txt")).unwrap();
        assert_eq!(written, content);
    }

    #[test]
    fn test_write_file_sha_mismatch() {
        let ws = tempfile::tempdir().unwrap();
        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let stream = UnixStream::connect(&sock).unwrap();
        let mut writer = stream.try_clone().unwrap();
        let mut reader = BufReader::new(stream);

        let content_b64 = base64::engine::general_purpose::STANDARD.encode(b"data");

        send_request(
            &mut writer,
            r#"{"id":"wf2","method":"write_file","params":{"path":"bad.txt","sha256":"0000000000000000000000000000000000000000000000000000000000000000"}}"#,
        );
        send_request(
            &mut writer,
            &format!(r#"{{"id":"wf2","method":"file_chunk","params":{{"content":"{content_b64}"}}}}"#),
        );
        send_request(&mut writer, r#"{"id":"wf2","method":"file_done"}"#);

        let line = read_line(&mut reader);
        let v: serde_json::Value = serde_json::from_str(&line).unwrap();
        assert_eq!(v["id"], "wf2");
        assert!(v["error"]["message"].as_str().unwrap().contains("sha256 mismatch"));
        assert!(!ws.path().join("bad.txt").exists());
    }

    #[test]
    fn test_write_read_roundtrip_large() {
        let ws = tempfile::tempdir().unwrap();
        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let stream = UnixStream::connect(&sock).unwrap();
        let mut writer = stream.try_clone().unwrap();
        let mut reader = BufReader::new(stream);

        // 2MB payload in 4 chunks
        let chunk_data = vec![0x42u8; 512 * 1024];
        let chunk_b64 = base64::engine::general_purpose::STANDARD.encode(&chunk_data);

        let mut full_hasher = sha2::Sha256::new();
        for _ in 0..4 {
            full_hasher.update(&chunk_data);
        }
        let expected_sha = hex::encode(full_hasher.finalize());

        send_request(
            &mut writer,
            r#"{"id":"wf3","method":"write_file","params":{"path":"large.bin"}}"#,
        );
        for _ in 0..4 {
            send_request(
                &mut writer,
                &format!(r#"{{"id":"wf3","method":"file_chunk","params":{{"content":"{chunk_b64}"}}}}"#),
            );
        }
        send_request(&mut writer, r#"{"id":"wf3","method":"file_done"}"#);

        let line = read_line(&mut reader);
        let v: serde_json::Value = serde_json::from_str(&line).unwrap();
        assert_eq!(v["id"], "wf3");
        assert_eq!(v["result"]["bytes_written"], 2 * 1024 * 1024);
        assert_eq!(v["result"]["sha256"], expected_sha);

        // Read it back and verify
        send_request(
            &mut writer,
            r#"{"id":"rf3","method":"read_file","params":{"path":"large.bin"}}"#,
        );

        let mut chunks = Vec::new();
        loop {
            let line = read_line(&mut reader);
            if line.trim().is_empty() { continue; }
            let v: serde_json::Value = serde_json::from_str(&line).unwrap();
            if v.get("chunk").is_some() {
                let b64 = v["chunk"]["content"].as_str().unwrap();
                chunks.extend(base64::engine::general_purpose::STANDARD.decode(b64).unwrap());
            } else if v.get("result").is_some() {
                assert_eq!(v["result"]["size_bytes"], 2 * 1024 * 1024);
                assert_eq!(v["result"]["sha256"], expected_sha);
                break;
            }
        }
        assert_eq!(chunks.len(), 2 * 1024 * 1024);
    }
}
