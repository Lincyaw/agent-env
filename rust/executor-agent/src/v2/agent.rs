use crate::path_security::resolve_workspace_path;
use crate::pty_util;
use super::proto;

use inotify::{EventMask, Inotify, WatchDescriptor, WatchMask};
use prost::Message;
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

struct ProcessHandle {
    child: Option<Child>,
    pty_master: Option<OwnedFd>,
    stdin_pipe: Option<std::process::ChildStdin>,
    pid: u32,
    exit_subscribers: Vec<String>,
}

struct TunnelState {
    tcp_writer: TcpStream,
}

struct SubscriptionState {
    fs_shutdowns: Vec<Arc<AtomicBool>>,
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

/// Write a length-delimited protobuf Envelope to the shared writer.
pub fn send_envelope(writer: &SharedWriter, envelope: proto::Envelope) -> io::Result<()> {
    let encoded = envelope.encode_to_vec();
    let len = encoded.len() as u32;
    let mut w = writer.lock().unwrap();
    w.write_all(&len.to_be_bytes())?;
    w.write_all(&encoded)?;
    w.flush()
}

fn send_response(writer: &SharedWriter, id: String, result: proto::response::Result) -> io::Result<()> {
    send_envelope(writer, proto::Envelope {
        payload: Some(proto::envelope::Payload::Response(proto::Response {
            id,
            result: Some(result),
        })),
    })
}

fn send_ok(writer: &SharedWriter, id: String) -> io::Result<()> {
    send_response(writer, id, proto::response::Result::Ok(proto::OkResponse {}))
}

fn send_error(writer: &SharedWriter, id: String, code: &str, message: String) -> io::Result<()> {
    send_response(writer, id, proto::response::Result::Error(proto::ErrorResponse {
        code: code.to_string(),
        message,
    }))
}

fn send_event(writer: &SharedWriter, event: proto::event::Event) -> io::Result<()> {
    send_envelope(writer, proto::Envelope {
        payload: Some(proto::envelope::Payload::Event(proto::Event {
            event: Some(event),
        })),
    })
}

/// Read a single length-delimited protobuf Envelope from a reader.
/// Returns None on clean EOF.
pub fn read_envelope(reader: &mut impl io::Read) -> io::Result<Option<proto::Envelope>> {
    let mut len_buf = [0u8; 4];
    match reader.read_exact(&mut len_buf) {
        Ok(()) => {}
        Err(e) if e.kind() == io::ErrorKind::UnexpectedEof => return Ok(None),
        Err(e) => return Err(e),
    }
    let len = u32::from_be_bytes(len_buf) as usize;
    let mut msg_buf = vec![0u8; len];
    reader.read_exact(&mut msg_buf)?;
    let envelope = proto::Envelope::decode(&msg_buf[..])
        .map_err(|e| io::Error::other(format!("protobuf decode: {e}")))?;
    Ok(Some(envelope))
}

fn handle_conn(
    stream: std::os::unix::net::UnixStream,
    workspace: &str,
    _shutdown: watch::Receiver<bool>,
) -> io::Result<()> {
    let reader = stream.try_clone()?;
    let writer: SharedWriter = Arc::new(Mutex::new(Box::new(stream)));
    handle_v2_session(reader, writer, workspace)
}

/// Transport-agnostic V2 session handler. Called from both Unix socket and iroh QUIC paths.
pub fn handle_v2_session(
    reader: impl io::Read,
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
    mut reader: impl io::Read,
    writer: SharedWriter,
    workspace: &str,
    processes: &Arc<Mutex<HashMap<String, ProcessHandle>>>,
    tunnels: &Arc<Mutex<HashMap<String, TunnelState>>>,
    subscriptions: &Arc<Mutex<HashMap<String, SubscriptionState>>>,
) -> io::Result<()> {
    loop {
        let envelope = match read_envelope(&mut reader)? {
            Some(e) => e,
            None => return Ok(()),
        };

        let request = match envelope.payload {
            Some(proto::envelope::Payload::Request(req)) => req,
            _ => {
                let _ = send_error(&writer, String::new(), "PARSE_ERROR", "expected Request envelope".into());
                continue;
            }
        };

        let id = request.id.clone();
        let method = match request.method {
            Some(m) => m,
            None => {
                let _ = send_error(&writer, id, "PARSE_ERROR", "missing method".into());
                continue;
            }
        };

        match method {
            proto::request::Method::Ping(_) => {
                log::info!("[v2:ping] id={id}");
                let _ = send_ok(&writer, id);
            }
            proto::request::Method::Spawn(params) => {
                log::info!("[v2:spawn] id={id} cmd={:?}", params.cmd);
                handle_spawn(&id, params, workspace, &writer, processes);
            }
            proto::request::Method::Stdin(params) => {
                handle_stdin(&id, params, &writer, processes);
            }
            proto::request::Method::Signal(params) => {
                handle_signal(&id, params, &writer, processes);
            }
            proto::request::Method::Resize(params) => {
                handle_resize(&id, params, &writer, processes);
            }
            proto::request::Method::WriteFile(params) => {
                log::info!("[v2:write_file] id={id} path={}", params.path);
                handle_write_file(&id, params, workspace, &writer, &mut reader);
            }
            proto::request::Method::FileChunk(_) | proto::request::Method::FileDone(_) => {
                let _ = send_error(
                    &writer,
                    id,
                    "UNEXPECTED",
                    "file_chunk/file_done without write_file".into(),
                );
            }
            proto::request::Method::ReadFile(params) => {
                log::info!("[v2:read_file] id={id} path={}", params.path);
                handle_read_file(&id, params, workspace, &writer);
            }
            proto::request::Method::Stat(params) => {
                log::info!("[v2:stat] id={id} path={}", params.path);
                handle_stat(&id, params, workspace, &writer);
            }
            proto::request::Method::ListDir(params) => {
                log::info!("[v2:list_dir] id={id} path={}", params.path);
                handle_list_dir(&id, params, workspace, &writer);
            }
            proto::request::Method::Tunnel(params) => {
                log::info!("[v2:tunnel] id={id} target={}", params.target);
                handle_tunnel(&id, params, &writer, tunnels);
            }
            proto::request::Method::TunnelData(params) => {
                handle_tunnel_data(&id, params, &writer, tunnels);
            }
            proto::request::Method::TunnelClose(params) => {
                log::info!("[v2:tunnel_close] id={id} handle={}", params.handle);
                handle_tunnel_close(&id, params, &writer, tunnels);
            }
            proto::request::Method::Subscribe(params) => {
                log::info!("[v2:subscribe] id={id} events={}", params.events.len());
                handle_subscribe(&id, params, workspace, &writer, processes, subscriptions);
            }
            proto::request::Method::Unsubscribe(params) => {
                log::info!("[v2:unsubscribe] id={id} sub={}", params.subscription_id);
                handle_unsubscribe(&id, params, &writer, processes, subscriptions);
            }
        }
    }
}

// ---------------------------------------------------------------------------
// spawn
// ---------------------------------------------------------------------------

fn handle_spawn(
    id: &str,
    params: proto::SpawnRequest,
    workspace: &str,
    writer: &SharedWriter,
    processes: &Arc<Mutex<HashMap<String, ProcessHandle>>>,
) {
    if params.cmd.is_empty() {
        let _ = send_error(writer, id.into(), "SPAWN_FAILED", "empty command".into());
        return;
    }

    let workdir = if params.workdir.is_empty() {
        workspace.to_string()
    } else {
        params.workdir.clone()
    };

    let handle = format!("proc-{}", &uuid::Uuid::new_v4().to_string()[..8]);
    let handle_tag = rand_tag();

    if params.pty.is_some() {
        handle_spawn_pty(id, &handle, handle_tag, params, &workdir, writer, processes);
    } else {
        handle_spawn_pipe(id, &handle, handle_tag, params, &workdir, writer, processes);
    }
}

fn rand_tag() -> u32 {
    use std::hash::{Hash, Hasher};
    let mut h = std::collections::hash_map::DefaultHasher::new();
    std::time::SystemTime::now().hash(&mut h);
    std::thread::current().id().hash(&mut h);
    h.finish() as u32
}

fn handle_spawn_pipe(
    id: &str,
    handle: &str,
    handle_tag: u32,
    params: proto::SpawnRequest,
    workdir: &str,
    writer: &SharedWriter,
    processes: &Arc<Mutex<HashMap<String, ProcessHandle>>>,
) {
    let mut cmd = Command::new(&params.cmd[0]);
    cmd.args(&params.cmd[1..]);
    cmd.current_dir(workdir);
    cmd.stdout(Stdio::piped());
    cmd.stderr(Stdio::piped());

    if params.stdin_enabled {
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
            let _ = send_error(writer, id.into(), "SPAWN_FAILED", format!("{e}"));
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

    let _ = send_response(
        writer,
        id.into(),
        proto::response::Result::Spawn(proto::SpawnResponse {
            handle: handle.into(),
            handle_tag,
        }),
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
                    let _ = send_event(
                        &w1,
                        proto::event::Event::Stdout(proto::StdoutEvent {
                            handle: h1.clone(),
                            data: buf[..n].to_vec(),
                        }),
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
                    let _ = send_event(
                        &w2,
                        proto::event::Event::Stderr(proto::StderrEvent {
                            handle: h2.clone(),
                            data: buf[..n].to_vec(),
                        }),
                    );
                }
            }
        }
    });

    // Spawn waiter
    let h3 = handle.to_string();
    let w3 = writer.clone();
    let procs = processes.clone();
    let timeout = if params.timeout_secs > 0 {
        Some(params.timeout_secs)
    } else {
        None
    };
    thread::spawn(move || {
        wait_and_exit(&h3, &w3, &procs, timeout);
    });
}

fn handle_spawn_pty(
    id: &str,
    handle: &str,
    handle_tag: u32,
    params: proto::SpawnRequest,
    workdir: &str,
    writer: &SharedWriter,
    processes: &Arc<Mutex<HashMap<String, ProcessHandle>>>,
) {
    let pty_cfg = params.pty.unwrap();
    let rows = if pty_cfg.rows == 0 { 24 } else { pty_cfg.rows as u16 };
    let cols = if pty_cfg.cols == 0 { 80 } else { pty_cfg.cols as u16 };

    let pty_pair = match pty_util::open_pty(rows, cols) {
        Ok(pair) => pair,
        Err(e) => {
            let _ = send_error(writer, id.into(), "SPAWN_FAILED", format!("{e}"));
            return;
        }
    };
    let master = pty_pair.master;
    let slave = pty_pair.slave;

    let mut cmd = Command::new(&params.cmd[0]);
    cmd.args(&params.cmd[1..]);
    cmd.current_dir(workdir);

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

    use std::os::unix::process::CommandExt;
    let slave_fd = slave.as_raw_fd();
    unsafe {
        cmd.pre_exec(move || {
            if libc::setsid() == -1 {
                return Err(io::Error::last_os_error());
            }
            if libc::ioctl(slave_fd, libc::TIOCSCTTY, 0) == -1 {
                return Err(io::Error::last_os_error());
            }
            if libc::dup2(slave_fd, 0) == -1
                || libc::dup2(slave_fd, 1) == -1
                || libc::dup2(slave_fd, 2) == -1
            {
                return Err(io::Error::last_os_error());
            }
            if slave_fd > 2 {
                libc::close(slave_fd);
            }
            Ok(())
        });
    }

    cmd.stdin(Stdio::null());
    cmd.stdout(Stdio::null());
    cmd.stderr(Stdio::null());

    let child = match cmd.spawn() {
        Ok(c) => c,
        Err(e) => {
            let _ = send_error(writer, id.into(), "SPAWN_FAILED", format!("{e}"));
            return;
        }
    };

    let pid = child.id();
    drop(slave);

    let ph = ProcessHandle {
        child: Some(child),
        pty_master: Some(master),
        stdin_pipe: None,
        pid,
        exit_subscribers: Vec::new(),
    };
    processes.lock().unwrap().insert(handle.to_string(), ph);

    let _ = send_response(
        writer,
        id.into(),
        proto::response::Result::Spawn(proto::SpawnResponse {
            handle: handle.into(),
            handle_tag,
        }),
    );

    // Read from PTY master -> stdout events
    let master_read_fd = {
        let procs = processes.lock().unwrap();
        let ph = procs.get(handle).unwrap();
        let fd = ph.pty_master.as_ref().unwrap().as_raw_fd();
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
                    let _ = send_event(
                        &w1,
                        proto::event::Event::Stdout(proto::StdoutEvent {
                            handle: h1.clone(),
                            data: buf[..n].to_vec(),
                        }),
                    );
                }
            }
        }
    });

    // Spawn waiter
    let h2 = handle.to_string();
    let w2 = writer.clone();
    let procs = processes.clone();
    let timeout = if params.timeout_secs > 0 {
        Some(params.timeout_secs)
    } else {
        None
    };
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
        let pid = child.id();
        let dur = std::time::Duration::from_secs(secs);
        let (done_tx, done_rx) = std::sync::mpsc::channel::<()>();

        let killer = thread::spawn(move || {
            if done_rx.recv_timeout(dur).is_err() {
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

    thread::sleep(std::time::Duration::from_millis(50));

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
    let _ = send_event(
        writer,
        proto::event::Event::Exit(proto::ExitEvent {
            handle: handle.into(),
            exit_code,
        }),
    );

    for sub_id in exit_subs {
        let _ = send_event(
            writer,
            proto::event::Event::ProcessExit(proto::ProcessExitEvent {
                subscription_id: sub_id,
                handle: handle.into(),
                exit_code,
            }),
        );
    }
}

// ---------------------------------------------------------------------------
// stdin
// ---------------------------------------------------------------------------

fn handle_stdin(
    id: &str,
    params: proto::StdinRequest,
    writer: &SharedWriter,
    processes: &Arc<Mutex<HashMap<String, ProcessHandle>>>,
) {
    let mut procs = processes.lock().unwrap();
    let ph = match procs.get_mut(&params.handle) {
        Some(p) => p,
        None => {
            let _ = send_error(
                writer,
                id.into(),
                "UNKNOWN_HANDLE",
                format!("no process with handle {}", params.handle),
            );
            return;
        }
    };

    let data = &params.data;

    if let Some(ref master) = ph.pty_master {
        let fd = master.as_raw_fd();
        let ret = unsafe { libc::write(fd, data.as_ptr() as *const libc::c_void, data.len()) };
        if ret < 0 {
            let _ = send_error(
                writer,
                id.into(),
                "STDIN_FAILED",
                format!("{}", io::Error::last_os_error()),
            );
            return;
        }
    } else if let Some(ref mut stdin_pipe) = ph.stdin_pipe {
        if let Err(e) = stdin_pipe.write_all(data) {
            let _ = send_error(writer, id.into(), "STDIN_FAILED", format!("{e}"));
            return;
        }
        if let Err(e) = stdin_pipe.flush() {
            let _ = send_error(writer, id.into(), "STDIN_FAILED", format!("{e}"));
            return;
        }
    } else {
        let _ = send_error(
            writer,
            id.into(),
            "STDIN_DISABLED",
            "process was not spawned with stdin enabled".into(),
        );
        return;
    }

    let _ = send_ok(writer, id.into());
}

// ---------------------------------------------------------------------------
// signal
// ---------------------------------------------------------------------------

fn handle_signal(
    id: &str,
    params: proto::SignalRequest,
    writer: &SharedWriter,
    processes: &Arc<Mutex<HashMap<String, ProcessHandle>>>,
) {
    let procs = processes.lock().unwrap();
    let ph = match procs.get(&params.handle) {
        Some(p) => p,
        None => {
            let _ = send_error(
                writer,
                id.into(),
                "UNKNOWN_HANDLE",
                format!("no process with handle {}", params.handle),
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
            let _ = send_error(
                writer,
                id.into(),
                "INVALID_SIGNAL",
                format!("unsupported signal: {other}"),
            );
            return;
        }
    };

    let pid = nix::unistd::Pid::from_raw(ph.pid as i32);
    if let Err(e) = nix::sys::signal::kill(pid, sig) {
        let _ = send_error(writer, id.into(), "SIGNAL_FAILED", format!("{e}"));
        return;
    }

    let _ = send_ok(writer, id.into());
}

// ---------------------------------------------------------------------------
// resize
// ---------------------------------------------------------------------------

fn handle_resize(
    id: &str,
    params: proto::ResizeRequest,
    writer: &SharedWriter,
    processes: &Arc<Mutex<HashMap<String, ProcessHandle>>>,
) {
    let procs = processes.lock().unwrap();
    let ph = match procs.get(&params.handle) {
        Some(p) => p,
        None => {
            let _ = send_error(
                writer,
                id.into(),
                "UNKNOWN_HANDLE",
                format!("no process with handle {}", params.handle),
            );
            return;
        }
    };

    let master = match &ph.pty_master {
        Some(m) => m,
        None => {
            let _ = send_error(writer, id.into(), "NOT_PTY", "process has no PTY".into());
            return;
        }
    };

    if let Err(e) = pty_util::set_winsize(master.as_raw_fd(), params.rows as u16, params.cols as u16) {
        let _ = send_error(writer, id.into(), "RESIZE_FAILED", format!("{e}"));
        return;
    }

    let _ = send_ok(writer, id.into());
}

// ---------------------------------------------------------------------------
// read_file
// ---------------------------------------------------------------------------

fn handle_read_file(
    id: &str,
    params: proto::ReadFileRequest,
    workspace: &str,
    writer: &SharedWriter,
) {
    let target = match resolve_workspace_path(std::path::Path::new(workspace), &params.path) {
        Ok(p) => p,
        Err(e) => {
            let _ = send_error(writer, id.into(), "PATH_ERROR", e);
            return;
        }
    };

    let mut file = match fs::File::open(&target) {
        Ok(f) => f,
        Err(e) => {
            let _ = send_error(writer, id.into(), "READ_FAILED", format!("{e}"));
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
                let _ = send_response(
                    writer,
                    id.into(),
                    proto::response::Result::FileChunk(proto::ReadFileChunk {
                        offset,
                        content: buf[..n].to_vec(),
                    }),
                );
                offset += n as i64;
            }
            Err(e) => {
                let _ = send_error(writer, id.into(), "READ_FAILED", format!("{e}"));
                return;
            }
        }
    }

    let sha = hex::encode(hasher.finalize());
    let _ = send_response(
        writer,
        id.into(),
        proto::response::Result::FileDone(proto::ReadFileComplete {
            size_bytes: offset,
            sha256: sha,
        }),
    );
}

// ---------------------------------------------------------------------------
// write_file
// ---------------------------------------------------------------------------

fn handle_write_file(
    id: &str,
    params: proto::WriteFileRequest,
    workspace: &str,
    writer: &SharedWriter,
    reader: &mut impl io::Read,
) {
    let target = match resolve_workspace_path(std::path::Path::new(workspace), &params.path) {
        Ok(p) => p,
        Err(e) => {
            let _ = send_error(writer, id.into(), "PATH_ERROR", e);
            return;
        }
    };

    let parent = match target.parent() {
        Some(p) => p.to_path_buf(),
        None => {
            let _ = send_error(writer, id.into(), "PATH_ERROR", "invalid target".into());
            return;
        }
    };

    if let Err(e) = fs::create_dir_all(&parent) {
        let _ = send_error(writer, id.into(), "WRITE_FAILED", format!("mkdir: {e}"));
        return;
    }

    let base_name = target.file_name().unwrap_or_default().to_string_lossy();
    let tmp_path = parent.join(format!(".{base_name}.{}.tmp", std::process::id()));

    let tmp_file = match fs::File::create(&tmp_path) {
        Ok(f) => f,
        Err(e) => {
            let _ = send_error(writer, id.into(), "WRITE_FAILED", format!("create temp: {e}"));
            return;
        }
    };

    let mut tmp_writer = io::BufWriter::new(tmp_file);
    let mut hasher = Sha256::new();
    let mut written: i64 = 0;

    let result = (|| -> Result<(), String> {
        loop {
            let envelope = read_envelope(reader)
                .map_err(|e| format!("read: {e}"))?
                .ok_or_else(|| "unexpected EOF during file upload".to_string())?;

            let request = match envelope.payload {
                Some(proto::envelope::Payload::Request(req)) => req,
                _ => return Err("expected Request envelope during file upload".into()),
            };

            let method = request.method.ok_or("missing method")?;

            match method {
                proto::request::Method::FileChunk(chunk) => {
                    if !request.id.is_empty() && request.id != id {
                        return Err("file chunk id mismatch".into());
                    }
                    if chunk.content.is_empty() {
                        continue;
                    }
                    use io::Write;
                    tmp_writer.write_all(&chunk.content).map_err(|e| format!("write: {e}"))?;
                    hasher.update(&chunk.content);
                    written += chunk.content.len() as i64;
                }
                proto::request::Method::FileDone(_) => {
                    use io::Write;
                    tmp_writer.flush().map_err(|e| format!("flush: {e}"))?;
                    drop(tmp_writer);

                    let actual_sha = hex::encode(hasher.finalize());

                    if !params.expected_sha256.is_empty()
                        && !params.expected_sha256.eq_ignore_ascii_case(&actual_sha)
                    {
                        return Err(format!(
                            "sha256 mismatch: expected {} got {actual_sha}",
                            params.expected_sha256
                        ));
                    }

                    use std::os::unix::fs::PermissionsExt;
                    fs::set_permissions(&tmp_path, fs::Permissions::from_mode(0o644))
                        .map_err(|e| format!("chmod: {e}"))?;
                    fs::rename(&tmp_path, &target)
                        .map_err(|e| format!("rename: {e}"))?;

                    let _ = send_response(
                        writer,
                        id.into(),
                        proto::response::Result::WriteFile(proto::WriteFileResponse {
                            bytes_written: written,
                            sha256: actual_sha,
                        }),
                    );
                    return Ok(());
                }
                _ => {
                    return Err("unexpected message during file upload".into());
                }
            }
        }
    })();

    if let Err(e) = result {
        let _ = fs::remove_file(&tmp_path);
        let _ = send_error(writer, id.into(), "WRITE_FAILED", e);
    }
}

// ---------------------------------------------------------------------------
// stat
// ---------------------------------------------------------------------------

fn handle_stat(
    id: &str,
    params: proto::StatRequest,
    workspace: &str,
    writer: &SharedWriter,
) {
    let target = match resolve_workspace_path(std::path::Path::new(workspace), &params.path) {
        Ok(p) => p,
        Err(e) => {
            let _ = send_error(writer, id.into(), "PATH_ERROR", e);
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
                })
                .unwrap_or_default();
            let mode_val = meta.mode();
            let _ = send_response(
                writer,
                id.into(),
                proto::response::Result::Stat(proto::StatResponse {
                    exists: true,
                    is_dir: meta.is_dir(),
                    size: meta.len(),
                    mode: format!("{:04o}", mode_val & 0o7777),
                    modified,
                }),
            );
        }
        Err(e) if e.kind() == io::ErrorKind::NotFound => {
            let _ = send_response(
                writer,
                id.into(),
                proto::response::Result::Stat(proto::StatResponse {
                    exists: false,
                    is_dir: false,
                    size: 0,
                    mode: String::new(),
                    modified: String::new(),
                }),
            );
        }
        Err(e) => {
            let _ = send_error(writer, id.into(), "STAT_FAILED", format!("{e}"));
        }
    }
}

// ---------------------------------------------------------------------------
// list_dir
// ---------------------------------------------------------------------------

fn handle_list_dir(
    id: &str,
    params: proto::ListDirRequest,
    workspace: &str,
    writer: &SharedWriter,
) {
    let target = match resolve_workspace_path(std::path::Path::new(workspace), &params.path) {
        Ok(p) => p,
        Err(e) => {
            let _ = send_error(writer, id.into(), "PATH_ERROR", e);
            return;
        }
    };

    let mut entries = Vec::new();
    if let Err(e) = collect_entries(&target, &mut entries, params.recursive) {
        let _ = send_error(writer, id.into(), "LIST_FAILED", format!("{e}"));
        return;
    }

    let _ = send_response(
        writer,
        id.into(),
        proto::response::Result::ListDir(proto::ListDirResponse { entries }),
    );
}

// ---------------------------------------------------------------------------
// tunnel
// ---------------------------------------------------------------------------

fn handle_tunnel(
    id: &str,
    params: proto::TunnelRequest,
    writer: &SharedWriter,
    tunnels: &Arc<Mutex<HashMap<String, TunnelState>>>,
) {
    let tcp_stream = match TcpStream::connect(&params.target) {
        Ok(s) => s,
        Err(e) => {
            let _ = send_error(writer, id.into(), "TUNNEL_FAILED", format!("{e}"));
            return;
        }
    };

    let handle = if params.handle.is_empty() {
        format!("tun-{}", &uuid::Uuid::new_v4().to_string()[..8])
    } else {
        params.handle.clone()
    };
    let handle_tag = rand_tag();

    let reader_stream = match tcp_stream.try_clone() {
        Ok(s) => s,
        Err(e) => {
            let _ = send_error(writer, id.into(), "TUNNEL_FAILED", format!("{e}"));
            return;
        }
    };

    tunnels.lock().unwrap().insert(
        handle.clone(),
        TunnelState {
            tcp_writer: tcp_stream,
        },
    );

    let _ = send_response(
        writer,
        id.into(),
        proto::response::Result::Tunnel(proto::TunnelResponse {
            handle: handle.clone(),
            handle_tag,
        }),
    );

    let w = writer.clone();
    let h = handle.clone();
    let tuns = tunnels.clone();
    thread::spawn(move || {
        let mut buf = [0u8; 64 * 1024];
        let mut stream = reader_stream;
        loop {
            match stream.read(&mut buf) {
                Ok(0) => {
                    let _ = send_event(
                        &w,
                        proto::event::Event::TunnelClosed(proto::TunnelClosedEvent {
                            handle: h.clone(),
                            reason: "remote closed".into(),
                        }),
                    );
                    break;
                }
                Ok(n) => {
                    let _ = send_event(
                        &w,
                        proto::event::Event::TunnelData(proto::TunnelDataEvent {
                            handle: h.clone(),
                            data: buf[..n].to_vec(),
                        }),
                    );
                }
                Err(e) => {
                    let _ = send_event(
                        &w,
                        proto::event::Event::TunnelClosed(proto::TunnelClosedEvent {
                            handle: h.clone(),
                            reason: format!("{e}"),
                        }),
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
    params: proto::TunnelDataRequest,
    writer: &SharedWriter,
    tunnels: &Arc<Mutex<HashMap<String, TunnelState>>>,
) {
    let mut tuns = tunnels.lock().unwrap();
    let ts = match tuns.get_mut(&params.handle) {
        Some(t) => t,
        None => {
            let _ = send_error(
                writer,
                id.into(),
                "UNKNOWN_HANDLE",
                format!("no tunnel with handle {}", params.handle),
            );
            return;
        }
    };

    if let Err(e) = ts.tcp_writer.write_all(&params.data) {
        let _ = send_error(writer, id.into(), "TUNNEL_WRITE_FAILED", format!("{e}"));
        return;
    }
    if let Err(e) = ts.tcp_writer.flush() {
        let _ = send_error(writer, id.into(), "TUNNEL_WRITE_FAILED", format!("{e}"));
        return;
    }

    let _ = send_ok(writer, id.into());
}

fn handle_tunnel_close(
    id: &str,
    params: proto::TunnelCloseRequest,
    writer: &SharedWriter,
    tunnels: &Arc<Mutex<HashMap<String, TunnelState>>>,
) {
    let mut tuns = tunnels.lock().unwrap();
    match tuns.remove(&params.handle) {
        Some(ts) => {
            let _ = ts.tcp_writer.shutdown(std::net::Shutdown::Both);
            let _ = send_ok(writer, id.into());
        }
        None => {
            let _ = send_error(
                writer,
                id.into(),
                "UNKNOWN_HANDLE",
                format!("no tunnel with handle {}", params.handle),
            );
        }
    }
}

// ---------------------------------------------------------------------------
// subscribe / unsubscribe
// ---------------------------------------------------------------------------

fn handle_subscribe(
    id: &str,
    params: proto::SubscribeRequest,
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
        match &spec.spec {
            Some(proto::subscribe_event_spec::Spec::FsChange(fs_spec)) => {
                let watch_path = resolve_watch_path(Path::new(workspace), &fs_spec.path);
                let watch_path = match watch_path {
                    Ok(p) => p,
                    Err(e) => {
                        let _ = send_error(writer, id.into(), "SUBSCRIBE_FAILED", e);
                        cleanup_partial_subscription(&state, processes);
                        return;
                    }
                };

                let shutdown = Arc::new(AtomicBool::new(false));
                state.fs_shutdowns.push(shutdown.clone());

                let w = writer.clone();
                let sid = sub_id.clone();
                let recursive = fs_spec.recursive;
                thread::spawn(move || {
                    run_fs_watcher(watch_path, recursive, &sid, &shutdown, &w);
                });
            }
            Some(proto::subscribe_event_spec::Spec::ProcessExit(pe_spec)) => {
                let mut procs = processes.lock().unwrap();
                match procs.get_mut(&pe_spec.handle) {
                    Some(ph) => {
                        ph.exit_subscribers.push(sub_id.clone());
                        state.watched_process_handles.push(pe_spec.handle.clone());
                    }
                    None => {
                        drop(procs);
                        let _ = send_error(
                            writer,
                            id.into(),
                            "UNKNOWN_HANDLE",
                            format!("no process with handle {}", pe_spec.handle),
                        );
                        cleanup_partial_subscription(&state, processes);
                        return;
                    }
                }
            }
            None => {
                let _ = send_error(writer, id.into(), "SUBSCRIBE_FAILED", "empty event spec".into());
                cleanup_partial_subscription(&state, processes);
                return;
            }
        }
    }

    subscriptions
        .lock()
        .unwrap()
        .insert(sub_id.clone(), state);

    let _ = send_response(
        writer,
        id.into(),
        proto::response::Result::Subscribe(proto::SubscribeResponse {
            subscription_id: sub_id,
        }),
    );
}

fn handle_unsubscribe(
    id: &str,
    params: proto::UnsubscribeRequest,
    writer: &SharedWriter,
    processes: &Arc<Mutex<HashMap<String, ProcessHandle>>>,
    subscriptions: &Arc<Mutex<HashMap<String, SubscriptionState>>>,
) {
    let mut subs = subscriptions.lock().unwrap();
    match subs.remove(&params.subscription_id) {
        Some(state) => {
            for flag in &state.fs_shutdowns {
                flag.store(true, Ordering::Relaxed);
            }
            let mut procs = processes.lock().unwrap();
            for ph_handle in &state.watched_process_handles {
                if let Some(ph) = procs.get_mut(ph_handle) {
                    ph.exit_subscribers
                        .retain(|s| s != &params.subscription_id);
                }
            }
            let _ = send_ok(writer, id.into());
        }
        None => {
            let _ = send_error(
                writer,
                id.into(),
                "UNKNOWN_SUBSCRIPTION",
                format!("no subscription with id {}", params.subscription_id),
            );
        }
    }
}

fn cleanup_partial_subscription(
    state: &SubscriptionState,
    processes: &Arc<Mutex<HashMap<String, ProcessHandle>>>,
) {
    for flag in &state.fs_shutdowns {
        flag.store(true, Ordering::Relaxed);
    }
    let _ = processes;
}

fn resolve_watch_path(workspace: &Path, path: &str) -> Result<PathBuf, String> {
    let resolved = if Path::new(path).is_absolute() {
        PathBuf::from(path)
    } else {
        workspace.join(path)
    };

    let canon_workspace = workspace
        .canonicalize()
        .map_err(|e| format!("resolve workspace: {e}"))?;

    let canon_path = resolved
        .canonicalize()
        .map_err(|e| format!("resolve watch path: {e}"))?;

    if !canon_path.starts_with(&canon_workspace) {
        return Err("watch path must be within workspace".to_string());
    }

    Ok(canon_path)
}

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
                        let _ = send_event(
                            writer,
                            proto::event::Event::FsChange(proto::FsChangeEvent {
                                subscription_id: subscription_id.to_string(),
                                path: full_path.to_string_lossy().into_owned(),
                                change_type: ct.to_string(),
                            }),
                        );
                    }

                    if recursive
                        && event.mask.contains(EventMask::ISDIR)
                        && event.mask.contains(EventMask::CREATE)
                    {
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
    entries: &mut Vec<proto::DirEntry>,
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
        entries.push(proto::DirEntry {
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
    use prost::Message;
    use std::io::{Read, Write};
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

        for _ in 0..100 {
            if std::path::Path::new(&sock_path).exists() {
                break;
            }
            thread::sleep(std::time::Duration::from_millis(20));
        }

        std::mem::forget(dir);
        (sock_path, tx)
    }

    fn send_request_pb(stream: &mut UnixStream, id: &str, method: proto::request::Method) {
        let envelope = proto::Envelope {
            payload: Some(proto::envelope::Payload::Request(proto::Request {
                id: id.to_string(),
                method: Some(method),
            })),
        };
        let encoded = envelope.encode_to_vec();
        let len = encoded.len() as u32;
        stream.write_all(&len.to_be_bytes()).unwrap();
        stream.write_all(&encoded).unwrap();
        stream.flush().unwrap();
    }

    fn read_envelope_from(stream: &mut UnixStream) -> proto::Envelope {
        let mut len_buf = [0u8; 4];
        stream.read_exact(&mut len_buf).unwrap();
        let len = u32::from_be_bytes(len_buf) as usize;
        let mut msg_buf = vec![0u8; len];
        stream.read_exact(&mut msg_buf).unwrap();
        proto::Envelope::decode(&msg_buf[..]).unwrap()
    }

    fn extract_response(env: &proto::Envelope) -> &proto::Response {
        match &env.payload {
            Some(proto::envelope::Payload::Response(r)) => r,
            _ => panic!("expected Response"),
        }
    }

    fn extract_event(env: &proto::Envelope) -> &proto::Event {
        match &env.payload {
            Some(proto::envelope::Payload::Event(e)) => e,
            _ => panic!("expected Event"),
        }
    }

    #[test]
    fn test_ping() {
        let ws = tempfile::tempdir().unwrap();
        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let mut stream = UnixStream::connect(&sock).unwrap();

        send_request_pb(&mut stream, "p1", proto::request::Method::Ping(proto::PingRequest {}));

        let env = read_envelope_from(&mut stream);
        let resp = extract_response(&env);
        assert_eq!(resp.id, "p1");
        assert!(matches!(resp.result, Some(proto::response::Result::Ok(_))));
    }

    #[test]
    fn test_spawn_and_exit() {
        let ws = tempfile::tempdir().unwrap();
        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let mut stream = UnixStream::connect(&sock).unwrap();

        send_request_pb(&mut stream, "s1", proto::request::Method::Spawn(proto::SpawnRequest {
            cmd: vec!["echo".into(), "hello v2".into()],
            ..Default::default()
        }));

        let mut got_spawn = false;
        let mut got_stdout = false;
        let mut got_exit = false;

        for _ in 0..20 {
            let env = read_envelope_from(&mut stream);
            match &env.payload {
                Some(proto::envelope::Payload::Response(resp)) => {
                    if resp.id == "s1" {
                        if let Some(proto::response::Result::Spawn(s)) = &resp.result {
                            assert!(s.handle.starts_with("proc-"));
                            got_spawn = true;
                        }
                    }
                }
                Some(proto::envelope::Payload::Event(evt)) => {
                    match &evt.event {
                        Some(proto::event::Event::Stdout(so)) => {
                            let text = String::from_utf8_lossy(&so.data);
                            if text.contains("hello v2") {
                                got_stdout = true;
                            }
                        }
                        Some(proto::event::Event::Exit(ex)) => {
                            assert_eq!(ex.exit_code, 0);
                            got_exit = true;
                            break;
                        }
                        _ => {}
                    }
                }
                _ => {}
            }
        }

        assert!(got_spawn, "expected spawn result");
        assert!(got_stdout, "expected stdout event");
        assert!(got_exit, "expected exit event");
    }

    #[test]
    fn test_spawn_with_stdin() {
        let ws = tempfile::tempdir().unwrap();
        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let mut stream = UnixStream::connect(&sock).unwrap();
        stream
            .set_read_timeout(Some(std::time::Duration::from_secs(5)))
            .unwrap();

        send_request_pb(&mut stream, "s2", proto::request::Method::Spawn(proto::SpawnRequest {
            cmd: vec!["cat".into()],
            stdin_enabled: true,
            ..Default::default()
        }));

        // Read spawn response
        let env = read_envelope_from(&mut stream);
        let resp = extract_response(&env);
        let handle = match &resp.result {
            Some(proto::response::Result::Spawn(s)) => s.handle.clone(),
            _ => panic!("expected spawn response"),
        };

        // Send stdin
        send_request_pb(&mut stream, "i1", proto::request::Method::Stdin(proto::StdinRequest {
            handle: handle.clone(),
            data: b"test input\n".to_vec(),
        }));

        let mut got_output = false;
        let mut got_exit = false;
        let mut signaled = false;

        for _ in 0..30 {
            let env = match read_envelope_timeout(&mut stream) {
                Some(e) => e,
                None => break,
            };
            match &env.payload {
                Some(proto::envelope::Payload::Response(_)) => continue,
                Some(proto::envelope::Payload::Event(evt)) => {
                    match &evt.event {
                        Some(proto::event::Event::Stdout(so)) => {
                            let text = String::from_utf8_lossy(&so.data);
                            if text.contains("test input") {
                                got_output = true;
                                if !signaled {
                                    signaled = true;
                                    send_request_pb(&mut stream, "i2", proto::request::Method::Signal(proto::SignalRequest {
                                        handle: handle.clone(),
                                        signal: "SIGTERM".into(),
                                    }));
                                }
                            }
                        }
                        Some(proto::event::Event::Exit(_)) => {
                            got_exit = true;
                            break;
                        }
                        _ => {}
                    }
                }
                _ => {}
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

        let mut stream = UnixStream::connect(&sock).unwrap();

        send_request_pb(&mut stream, "st1", proto::request::Method::Stat(proto::StatRequest {
            path: "hello.txt".into(),
        }));

        let env = read_envelope_from(&mut stream);
        let resp = extract_response(&env);
        assert_eq!(resp.id, "st1");
        match &resp.result {
            Some(proto::response::Result::Stat(s)) => {
                assert!(s.exists);
                assert!(!s.is_dir);
                assert_eq!(s.size, 7);
            }
            _ => panic!("expected stat response"),
        }
    }

    #[test]
    fn test_stat_nonexistent() {
        let ws = tempfile::tempdir().unwrap();
        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let mut stream = UnixStream::connect(&sock).unwrap();

        send_request_pb(&mut stream, "st2", proto::request::Method::Stat(proto::StatRequest {
            path: "nope.txt".into(),
        }));

        let env = read_envelope_from(&mut stream);
        let resp = extract_response(&env);
        assert_eq!(resp.id, "st2");
        match &resp.result {
            Some(proto::response::Result::Stat(s)) => {
                assert!(!s.exists);
            }
            _ => panic!("expected stat response"),
        }
    }

    #[test]
    fn test_list_dir() {
        let ws = tempfile::tempdir().unwrap();
        let sub = ws.path().join("mydir");
        fs::create_dir(&sub).unwrap();
        fs::write(sub.join("c.txt"), "ccc").unwrap();

        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let mut stream = UnixStream::connect(&sock).unwrap();

        send_request_pb(&mut stream, "ld1", proto::request::Method::ListDir(proto::ListDirRequest {
            path: "mydir".into(),
            recursive: false,
        }));

        let env = read_envelope_from(&mut stream);
        let resp = extract_response(&env);
        assert_eq!(resp.id, "ld1");
        match &resp.result {
            Some(proto::response::Result::ListDir(ld)) => {
                assert_eq!(ld.entries.len(), 1);
                assert_eq!(ld.entries[0].name, "c.txt");
            }
            _ => panic!("expected list_dir response"),
        }
    }

    #[test]
    fn test_read_file() {
        let ws = tempfile::tempdir().unwrap();
        let content = "hello file read";
        fs::write(ws.path().join("data.txt"), content).unwrap();

        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let mut stream = UnixStream::connect(&sock).unwrap();

        send_request_pb(&mut stream, "rf1", proto::request::Method::ReadFile(proto::ReadFileRequest {
            path: "data.txt".into(),
        }));

        let mut chunks = Vec::new();
        let mut final_result = None;

        for _ in 0..10 {
            let env = read_envelope_from(&mut stream);
            let resp = extract_response(&env);
            match &resp.result {
                Some(proto::response::Result::FileChunk(fc)) => {
                    chunks.extend_from_slice(&fc.content);
                }
                Some(proto::response::Result::FileDone(fd)) => {
                    final_result = Some(fd.clone());
                    break;
                }
                _ => {}
            }
        }

        assert_eq!(String::from_utf8(chunks).unwrap(), content);
        let result = final_result.unwrap();
        assert_eq!(result.size_bytes, content.len() as i64);
        assert!(!result.sha256.is_empty());
    }

    #[test]
    fn test_spawn_with_pty() {
        let ws = tempfile::tempdir().unwrap();
        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let mut stream = UnixStream::connect(&sock).unwrap();
        stream
            .set_read_timeout(Some(std::time::Duration::from_secs(5)))
            .unwrap();

        send_request_pb(&mut stream, "p1", proto::request::Method::Spawn(proto::SpawnRequest {
            cmd: vec!["echo".into(), "pty-test".into()],
            pty: Some(proto::PtyConfig { rows: 24, cols: 80 }),
            ..Default::default()
        }));

        let env = read_envelope_from(&mut stream);
        let resp = extract_response(&env);
        let handle = match &resp.result {
            Some(proto::response::Result::Spawn(s)) => s.handle.clone(),
            _ => panic!("expected spawn response"),
        };
        assert!(handle.starts_with("proc-"));

        let mut got_output = false;
        let mut got_exit = false;
        for _ in 0..20 {
            match read_envelope_timeout(&mut stream) {
                Some(env) => {
                    match &env.payload {
                        Some(proto::envelope::Payload::Event(evt)) => {
                            match &evt.event {
                                Some(proto::event::Event::Stdout(so)) => {
                                    let text = String::from_utf8_lossy(&so.data);
                                    if text.contains("pty-test") {
                                        got_output = true;
                                    }
                                }
                                Some(proto::event::Event::Exit(ex)) => {
                                    assert_eq!(ex.exit_code, 0);
                                    got_exit = true;
                                    break;
                                }
                                _ => {}
                            }
                        }
                        _ => {}
                    }
                }
                None => break,
            }
        }

        assert!(got_output, "expected pty output");
        assert!(got_exit, "expected exit event");
    }

    fn read_envelope_timeout(stream: &mut UnixStream) -> Option<proto::Envelope> {
        let mut len_buf = [0u8; 4];
        match stream.read_exact(&mut len_buf) {
            Ok(()) => {}
            Err(_) => return None,
        }
        let len = u32::from_be_bytes(len_buf) as usize;
        let mut msg_buf = vec![0u8; len];
        match stream.read_exact(&mut msg_buf) {
            Ok(()) => {}
            Err(_) => return None,
        }
        proto::Envelope::decode(&msg_buf[..]).ok()
    }

    #[test]
    fn test_tunnel_echo() {
        use std::net::TcpListener;

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

        let mut stream = UnixStream::connect(&sock).unwrap();
        stream
            .set_read_timeout(Some(std::time::Duration::from_secs(5)))
            .unwrap();

        send_request_pb(&mut stream, "t1", proto::request::Method::Tunnel(proto::TunnelRequest {
            target: echo_addr.to_string(),
            handle: "tun-echo".into(),
        }));

        let env = read_envelope_from(&mut stream);
        let resp = extract_response(&env);
        assert_eq!(resp.id, "t1");
        match &resp.result {
            Some(proto::response::Result::Tunnel(t)) => {
                assert_eq!(t.handle, "tun-echo");
            }
            _ => panic!("expected tunnel response"),
        }

        let payload = b"hello tunnel";
        send_request_pb(&mut stream, "d1", proto::request::Method::TunnelData(proto::TunnelDataRequest {
            handle: "tun-echo".into(),
            data: payload.to_vec(),
        }));

        let mut got_ack = false;
        let mut got_echo = false;
        for _ in 0..20 {
            let env = match read_envelope_timeout(&mut stream) {
                Some(e) => e,
                None => break,
            };
            match &env.payload {
                Some(proto::envelope::Payload::Response(resp)) => {
                    if resp.id == "d1" {
                        got_ack = true;
                    }
                }
                Some(proto::envelope::Payload::Event(evt)) => {
                    if let Some(proto::event::Event::TunnelData(td)) = &evt.event {
                        assert_eq!(td.handle, "tun-echo");
                        if td.data == payload {
                            got_echo = true;
                            break;
                        }
                    }
                }
                _ => {}
            }
        }

        assert!(got_ack, "expected tunnel_data ack");
        assert!(got_echo, "expected echoed data from tunnel");

        send_request_pb(&mut stream, "c1", proto::request::Method::TunnelClose(proto::TunnelCloseRequest {
            handle: "tun-echo".into(),
        }));
        let env = read_envelope_from(&mut stream);
        let resp = extract_response(&env);
        assert_eq!(resp.id, "c1");
        assert!(matches!(resp.result, Some(proto::response::Result::Ok(_))));
    }

    #[test]
    fn test_subscribe_fs_change() {
        let ws = tempfile::tempdir().unwrap();
        let output_dir = ws.path().join("output");
        fs::create_dir(&output_dir).unwrap();

        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let mut stream = UnixStream::connect(&sock).unwrap();
        stream
            .set_read_timeout(Some(std::time::Duration::from_secs(5)))
            .unwrap();

        send_request_pb(&mut stream, "sub1", proto::request::Method::Subscribe(proto::SubscribeRequest {
            events: vec![proto::SubscribeEventSpec {
                spec: Some(proto::subscribe_event_spec::Spec::FsChange(proto::FsChangeSpec {
                    path: "output".into(),
                    recursive: false,
                })),
            }],
        }));

        let env = read_envelope_from(&mut stream);
        let resp = extract_response(&env);
        assert_eq!(resp.id, "sub1");
        let sub_id = match &resp.result {
            Some(proto::response::Result::Subscribe(s)) => s.subscription_id.clone(),
            _ => panic!("expected subscribe response"),
        };
        assert!(sub_id.starts_with("sub-"));

        thread::sleep(std::time::Duration::from_millis(200));

        fs::write(output_dir.join("model.bin"), "data").unwrap();

        let mut got_event = false;
        for _ in 0..30 {
            let env = match read_envelope_timeout(&mut stream) {
                Some(e) => e,
                None => break,
            };
            if let Some(proto::envelope::Payload::Event(evt)) = &env.payload {
                if let Some(proto::event::Event::FsChange(fc)) = &evt.event {
                    assert_eq!(fc.subscription_id, sub_id);
                    assert!(fc.path.contains("model.bin"), "path should contain model.bin, got: {}", fc.path);
                    got_event = true;
                    break;
                }
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

        let mut stream = UnixStream::connect(&sock).unwrap();
        stream
            .set_read_timeout(Some(std::time::Duration::from_secs(2)))
            .unwrap();

        send_request_pb(&mut stream, "sub1", proto::request::Method::Subscribe(proto::SubscribeRequest {
            events: vec![proto::SubscribeEventSpec {
                spec: Some(proto::subscribe_event_spec::Spec::FsChange(proto::FsChangeSpec {
                    path: "watched".into(),
                    recursive: false,
                })),
            }],
        }));

        let env = read_envelope_from(&mut stream);
        let resp = extract_response(&env);
        let sub_id = match &resp.result {
            Some(proto::response::Result::Subscribe(s)) => s.subscription_id.clone(),
            _ => panic!("expected subscribe response"),
        };

        thread::sleep(std::time::Duration::from_millis(200));

        send_request_pb(&mut stream, "u1", proto::request::Method::Unsubscribe(proto::UnsubscribeRequest {
            subscription_id: sub_id.clone(),
        }));

        let env = read_envelope_from(&mut stream);
        let resp = extract_response(&env);
        assert_eq!(resp.id, "u1");
        assert!(matches!(resp.result, Some(proto::response::Result::Ok(_))));

        thread::sleep(std::time::Duration::from_millis(300));

        fs::write(output_dir.join("should_not_fire.txt"), "data").unwrap();

        thread::sleep(std::time::Duration::from_millis(300));

        send_request_pb(&mut stream, "p1", proto::request::Method::Ping(proto::PingRequest {}));

        let mut got_fs_event = false;
        for _ in 0..10 {
            let env = match read_envelope_timeout(&mut stream) {
                Some(e) => e,
                None => break,
            };
            match &env.payload {
                Some(proto::envelope::Payload::Event(evt)) => {
                    if matches!(&evt.event, Some(proto::event::Event::FsChange(_))) {
                        got_fs_event = true;
                    }
                }
                Some(proto::envelope::Payload::Response(resp)) => {
                    if resp.id == "p1" {
                        break;
                    }
                }
                _ => {}
            }
        }

        assert!(!got_fs_event, "should not receive fs_change after unsubscribe");
    }

    #[test]
    fn test_write_file() {
        let ws = tempfile::tempdir().unwrap();
        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let mut stream = UnixStream::connect(&sock).unwrap();

        let content = b"hello write_file v2!";
        let expected_sha = hex::encode(sha2::Sha256::digest(content));

        send_request_pb(&mut stream, "wf1", proto::request::Method::WriteFile(proto::WriteFileRequest {
            path: "test_output.txt".into(),
            expected_sha256: expected_sha.clone(), stream_tag: 0,
        }));
        send_request_pb(&mut stream, "wf1", proto::request::Method::FileChunk(proto::FileChunkData {
            content: content.to_vec(),
        }));
        send_request_pb(&mut stream, "wf1", proto::request::Method::FileDone(proto::FileDoneRequest {}));

        let env = read_envelope_from(&mut stream);
        let resp = extract_response(&env);
        assert_eq!(resp.id, "wf1");
        match &resp.result {
            Some(proto::response::Result::WriteFile(wf)) => {
                assert_eq!(wf.bytes_written, content.len() as i64);
                assert_eq!(wf.sha256, expected_sha);
            }
            _ => panic!("expected write_file response"),
        }

        let written = std::fs::read(ws.path().join("test_output.txt")).unwrap();
        assert_eq!(written, content);
    }

    #[test]
    fn test_write_file_sha_mismatch() {
        let ws = tempfile::tempdir().unwrap();
        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let mut stream = UnixStream::connect(&sock).unwrap();

        send_request_pb(&mut stream, "wf2", proto::request::Method::WriteFile(proto::WriteFileRequest {
            path: "bad.txt".into(),
            expected_sha256: "0000000000000000000000000000000000000000000000000000000000000000".into(),
            stream_tag: 0,
        }));
        send_request_pb(&mut stream, "wf2", proto::request::Method::FileChunk(proto::FileChunkData {
            content: b"data".to_vec(),
        }));
        send_request_pb(&mut stream, "wf2", proto::request::Method::FileDone(proto::FileDoneRequest {}));

        let env = read_envelope_from(&mut stream);
        let resp = extract_response(&env);
        assert_eq!(resp.id, "wf2");
        match &resp.result {
            Some(proto::response::Result::Error(e)) => {
                assert!(e.message.contains("sha256 mismatch"));
            }
            _ => panic!("expected error response"),
        }
        assert!(!ws.path().join("bad.txt").exists());
    }

    #[test]
    fn test_write_read_roundtrip_large() {
        let ws = tempfile::tempdir().unwrap();
        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let mut stream = UnixStream::connect(&sock).unwrap();

        let chunk_data = vec![0x42u8; 512 * 1024];
        let mut full_hasher = sha2::Sha256::new();
        for _ in 0..4 {
            full_hasher.update(&chunk_data);
        }
        let expected_sha = hex::encode(full_hasher.finalize());

        send_request_pb(&mut stream, "wf3", proto::request::Method::WriteFile(proto::WriteFileRequest {
            path: "large.bin".into(),
            expected_sha256: String::new(),
            stream_tag: 0,
        }));
        for _ in 0..4 {
            send_request_pb(&mut stream, "wf3", proto::request::Method::FileChunk(proto::FileChunkData {
                content: chunk_data.clone(),
            }));
        }
        send_request_pb(&mut stream, "wf3", proto::request::Method::FileDone(proto::FileDoneRequest {}));

        let env = read_envelope_from(&mut stream);
        let resp = extract_response(&env);
        assert_eq!(resp.id, "wf3");
        match &resp.result {
            Some(proto::response::Result::WriteFile(wf)) => {
                assert_eq!(wf.bytes_written, 2 * 1024 * 1024);
                assert_eq!(wf.sha256, expected_sha);
            }
            _ => panic!("expected write_file response"),
        }

        // Read it back
        send_request_pb(&mut stream, "rf3", proto::request::Method::ReadFile(proto::ReadFileRequest {
            path: "large.bin".into(),
        }));

        let mut chunks = Vec::new();
        loop {
            let env = read_envelope_from(&mut stream);
            let resp = extract_response(&env);
            match &resp.result {
                Some(proto::response::Result::FileChunk(fc)) => {
                    chunks.extend_from_slice(&fc.content);
                }
                Some(proto::response::Result::FileDone(fd)) => {
                    assert_eq!(fd.size_bytes, 2 * 1024 * 1024);
                    assert_eq!(fd.sha256, expected_sha);
                    break;
                }
                _ => {}
            }
        }
        assert_eq!(chunks.len(), 2 * 1024 * 1024);
    }
}
