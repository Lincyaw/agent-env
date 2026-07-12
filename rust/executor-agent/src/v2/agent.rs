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
use std::sync::atomic::{AtomicBool, AtomicU32, Ordering};
use std::sync::{Arc, Mutex};
use std::{fs, thread};
use tokio::sync::watch;

const FILE_CHUNK_SIZE: usize = 1024 * 1024;
const SIDECAR_SOCKET_GID: u32 = 65532;

// Wire-level message type bytes.
pub const MSG_TYPE_REQUEST: u8 = 0x01;
pub const MSG_TYPE_RESPONSE: u8 = 0x02;
pub const MSG_TYPE_EVENT: u8 = 0x03;

struct ProcessHandle {
    child: Option<Child>,
    pty_master: Option<OwnedFd>,
    stdin_pipe: Option<std::process::ChildStdin>,
    pid: u32,
}

struct WatchState {
    fs_shutdowns: Vec<Arc<AtomicBool>>,
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

/// Write a typed, length-delimited protobuf message.
/// Wire format: [1B type][4B big-endian length][protobuf bytes]
fn write_typed_message(writer: &SharedWriter, msg_type: u8, encoded: &[u8]) -> io::Result<()> {
    let len = encoded.len() as u32;
    let mut w = writer.lock().unwrap();
    w.write_all(&[msg_type])?;
    w.write_all(&len.to_be_bytes())?;
    w.write_all(encoded)?;
    w.flush()
}

fn send_response(writer: &SharedWriter, tag: u32, kind: proto::response::Kind) -> io::Result<()> {
    let resp = proto::Response {
        tag,
        kind: Some(kind),
    };
    write_typed_message(writer, MSG_TYPE_RESPONSE, &resp.encode_to_vec())
}

fn send_error(writer: &SharedWriter, tag: u32, code: i32, message: String) -> io::Result<()> {
    send_response(writer, tag, proto::response::Kind::Error(proto::ErrorResponse {
        code,
        message,
    }))
}

fn send_event(writer: &SharedWriter, tag: u32, kind: proto::event::Kind) -> io::Result<()> {
    let evt = proto::Event {
        tag,
        kind: Some(kind),
    };
    write_typed_message(writer, MSG_TYPE_EVENT, &evt.encode_to_vec())
}

/// Read a single typed, length-delimited protobuf Request from a reader.
/// Wire format: [1B type][4B big-endian length][protobuf bytes]
/// Returns None on clean EOF.
pub fn read_request(reader: &mut impl io::Read) -> io::Result<Option<proto::Request>> {
    let mut type_buf = [0u8; 1];
    match reader.read_exact(&mut type_buf) {
        Ok(()) => {}
        Err(e) if e.kind() == io::ErrorKind::UnexpectedEof => return Ok(None),
        Err(e) => return Err(e),
    }
    if type_buf[0] != MSG_TYPE_REQUEST {
        return Err(io::Error::other(format!(
            "expected message type 0x{:02x}, got 0x{:02x}",
            MSG_TYPE_REQUEST, type_buf[0]
        )));
    }
    let mut len_buf = [0u8; 4];
    reader.read_exact(&mut len_buf)?;
    let len = u32::from_be_bytes(len_buf) as usize;
    let mut msg_buf = vec![0u8; len];
    reader.read_exact(&mut msg_buf)?;
    let req = proto::Request::decode(&msg_buf[..])
        .map_err(|e| io::Error::other(format!("protobuf decode: {e}")))?;
    Ok(Some(req))
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
    let processes: Arc<Mutex<HashMap<u32, ProcessHandle>>> =
        Arc::new(Mutex::new(HashMap::new()));
    let watches: Arc<Mutex<HashMap<u32, WatchState>>> =
        Arc::new(Mutex::new(HashMap::new()));
    let watch_counter = Arc::new(AtomicU32::new(1));

    let result = handle_messages(
        reader,
        writer.clone(),
        workspace,
        &processes,
        &watches,
        &watch_counter,
    );

    // Cleanup on disconnect
    let mut procs = processes.lock().unwrap();
    for (ptag, ph) in procs.iter_mut() {
        if let Some(ref mut child) = ph.child {
            log::info!("[cleanup] killing process_tag={ptag} pid={}", ph.pid);
            let _ = child.kill();
            let _ = child.wait();
        }
    }
    procs.clear();

    let mut ws = watches.lock().unwrap();
    for (wid, state) in ws.drain() {
        log::info!("[cleanup] removing watch id={wid}");
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
    processes: &Arc<Mutex<HashMap<u32, ProcessHandle>>>,
    watches: &Arc<Mutex<HashMap<u32, WatchState>>>,
    watch_counter: &Arc<AtomicU32>,
) -> io::Result<()> {
    loop {
        let request = match read_request(&mut reader)? {
            Some(r) => r,
            None => return Ok(()),
        };

        let tag = request.tag;
        let kind = match request.kind {
            Some(k) => k,
            None => {
                let _ = send_error(&writer, tag, 1, "missing request kind".into());
                continue;
            }
        };

        match kind {
            proto::request::Kind::Ping(_) => {
                log::info!("[v2:ping] tag={tag}");
                let _ = send_response(&writer, tag, proto::response::Kind::Ping(proto::PingResponse {}));
            }
            proto::request::Kind::Spawn(params) => {
                log::info!("[v2:spawn] tag={tag} cmd={:?}", params.command);
                handle_spawn(tag, params, workspace, &writer, processes);
            }
            proto::request::Kind::WriteIn(params) => {
                handle_write_in(tag, params, &writer, processes);
            }
            proto::request::Kind::Signal(params) => {
                handle_signal(tag, params, &writer, processes);
            }
            proto::request::Kind::Resize(params) => {
                handle_resize(tag, params, &writer, processes);
            }
            proto::request::Kind::Read(params) => {
                log::info!("[v2:read] tag={tag} path={}", params.path);
                handle_read(tag, params, workspace, &writer);
            }
            proto::request::Kind::Write(params) => {
                log::info!("[v2:write] tag={tag} path={}", params.path);
                handle_write(tag, params, workspace, &writer, &mut reader);
            }
            proto::request::Kind::Stat(params) => {
                log::info!("[v2:stat] tag={tag} path={}", params.path);
                handle_stat(tag, params, workspace, &writer);
            }
            proto::request::Kind::List(params) => {
                log::info!("[v2:list] tag={tag} path={}", params.path);
                handle_list(tag, params, workspace, &writer);
            }
            proto::request::Kind::Tunnel(params) => {
                log::info!("[v2:tunnel] tag={tag} host={} port={}", params.host, params.port);
                handle_tunnel(tag, params, &writer);
            }
            proto::request::Kind::Watch(params) => {
                log::info!("[v2:watch] tag={tag} path={}", params.path);
                handle_watch(tag, params, workspace, &writer, watches, watch_counter);
            }
            proto::request::Kind::Unwatch(params) => {
                log::info!("[v2:unwatch] tag={tag} watch_id={}", params.watch_id);
                handle_unwatch(tag, params, &writer, watches);
            }
        }
    }
}

// ---------------------------------------------------------------------------
// spawn
// ---------------------------------------------------------------------------

fn handle_spawn(
    tag: u32,
    params: proto::SpawnRequest,
    workspace: &str,
    writer: &SharedWriter,
    processes: &Arc<Mutex<HashMap<u32, ProcessHandle>>>,
) {
    if params.command.is_empty() {
        let _ = send_error(writer, tag, 2, "empty command".into());
        return;
    }

    let workdir = if params.working_dir.is_empty() {
        workspace.to_string()
    } else {
        params.working_dir.clone()
    };

    // Use the request tag as the process_tag.
    let process_tag = tag;

    if params.pty {
        handle_spawn_pty(tag, process_tag, params, &workdir, writer, processes);
    } else {
        handle_spawn_pipe(tag, process_tag, params, &workdir, writer, processes);
    }
}

fn handle_spawn_pipe(
    tag: u32,
    process_tag: u32,
    params: proto::SpawnRequest,
    workdir: &str,
    writer: &SharedWriter,
    processes: &Arc<Mutex<HashMap<u32, ProcessHandle>>>,
) {
    let mut cmd = Command::new(&params.command[0]);
    cmd.args(&params.command[1..]);
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
            let _ = send_error(writer, tag, 2, format!("{e}"));
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
    processes.lock().unwrap().insert(process_tag, ph);

    let _ = send_response(
        writer,
        tag,
        proto::response::Kind::Spawn(proto::SpawnResponse {
            process_tag,
            pid: pid as i32,
        }),
    );

    // Spawn stdout reader
    let pt1 = process_tag;
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
                        0,
                        proto::event::Kind::Stdout(proto::StdoutEvent {
                            process_tag: pt1,
                            data: buf[..n].to_vec(),
                        }),
                    );
                }
            }
        }
    });

    // Spawn stderr reader
    let pt2 = process_tag;
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
                        0,
                        proto::event::Kind::Stderr(proto::StderrEvent {
                            process_tag: pt2,
                            data: buf[..n].to_vec(),
                        }),
                    );
                }
            }
        }
    });

    // Spawn waiter
    let pt3 = process_tag;
    let w3 = writer.clone();
    let procs = processes.clone();
    let timeout = if params.timeout_seconds > 0 {
        Some(params.timeout_seconds as u64)
    } else {
        None
    };
    thread::spawn(move || {
        wait_and_exit(pt3, &w3, &procs, timeout);
    });
}

fn handle_spawn_pty(
    tag: u32,
    process_tag: u32,
    params: proto::SpawnRequest,
    workdir: &str,
    writer: &SharedWriter,
    processes: &Arc<Mutex<HashMap<u32, ProcessHandle>>>,
) {
    let rows = if params.rows == 0 { 24 } else { params.rows as u16 };
    let cols = if params.cols == 0 { 80 } else { params.cols as u16 };

    let pty_pair = match pty_util::open_pty(rows, cols) {
        Ok(pair) => pair,
        Err(e) => {
            let _ = send_error(writer, tag, 2, format!("{e}"));
            return;
        }
    };
    let master = pty_pair.master;
    let slave = pty_pair.slave;

    let mut cmd = Command::new(&params.command[0]);
    cmd.args(&params.command[1..]);
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
            let _ = send_error(writer, tag, 2, format!("{e}"));
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
    };
    processes.lock().unwrap().insert(process_tag, ph);

    let _ = send_response(
        writer,
        tag,
        proto::response::Kind::Spawn(proto::SpawnResponse {
            process_tag,
            pid: pid as i32,
        }),
    );

    // Read from PTY master -> stdout events
    let master_read_fd = {
        let procs = processes.lock().unwrap();
        let ph = procs.get(&process_tag).unwrap();
        let fd = ph.pty_master.as_ref().unwrap().as_raw_fd();
        let duped = unsafe { libc::dup(fd) };
        if duped == -1 {
            log::error!("failed to dup pty master fd");
            return;
        }
        unsafe { OwnedFd::from_raw_fd(duped) }
    };

    let pt1 = process_tag;
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
                        0,
                        proto::event::Kind::Stdout(proto::StdoutEvent {
                            process_tag: pt1,
                            data: buf[..n].to_vec(),
                        }),
                    );
                }
            }
        }
    });

    // Spawn waiter
    let pt2 = process_tag;
    let w2 = writer.clone();
    let procs = processes.clone();
    let timeout = if params.timeout_seconds > 0 {
        Some(params.timeout_seconds as u64)
    } else {
        None
    };
    thread::spawn(move || {
        wait_and_exit(pt2, &w2, &procs, timeout);
    });
}

fn wait_and_exit(
    process_tag: u32,
    writer: &SharedWriter,
    processes: &Arc<Mutex<HashMap<u32, ProcessHandle>>>,
    timeout: Option<u64>,
) {
    let mut child = {
        let mut procs = processes.lock().unwrap();
        match procs.get_mut(&process_tag) {
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

    {
        let mut procs = processes.lock().unwrap();
        if let Some(ph) = procs.get_mut(&process_tag) {
            ph.pty_master.take();
            ph.stdin_pipe.take();
        }
        procs.remove(&process_tag);
    }

    log::info!("[v2:exit] process_tag={process_tag} exit_code={exit_code}");
    let _ = send_event(
        writer,
        0,
        proto::event::Kind::Exit(proto::ExitEvent {
            process_tag,
            exit_code,
        }),
    );
}

// ---------------------------------------------------------------------------
// write_in (was: stdin)
// ---------------------------------------------------------------------------

fn handle_write_in(
    tag: u32,
    params: proto::WriteInRequest,
    writer: &SharedWriter,
    processes: &Arc<Mutex<HashMap<u32, ProcessHandle>>>,
) {
    let mut procs = processes.lock().unwrap();
    let ph = match procs.get_mut(&params.process_tag) {
        Some(p) => p,
        None => {
            let _ = send_error(
                writer,
                tag,
                3,
                format!("no process with tag {}", params.process_tag),
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
                tag,
                4,
                format!("{}", io::Error::last_os_error()),
            );
            return;
        }
    } else if let Some(ref mut stdin_pipe) = ph.stdin_pipe {
        if let Err(e) = stdin_pipe.write_all(data) {
            let _ = send_error(writer, tag, 4, format!("{e}"));
            return;
        }
        if let Err(e) = stdin_pipe.flush() {
            let _ = send_error(writer, tag, 4, format!("{e}"));
            return;
        }
    } else {
        let _ = send_error(
            writer,
            tag,
            4,
            "process was not spawned with stdin enabled".into(),
        );
        return;
    }

    let _ = send_response(writer, tag, proto::response::Kind::WriteIn(proto::WriteInResponse {}));
}

// ---------------------------------------------------------------------------
// signal
// ---------------------------------------------------------------------------

fn handle_signal(
    tag: u32,
    params: proto::SignalRequest,
    writer: &SharedWriter,
    processes: &Arc<Mutex<HashMap<u32, ProcessHandle>>>,
) {
    let procs = processes.lock().unwrap();
    let ph = match procs.get(&params.process_tag) {
        Some(p) => p,
        None => {
            let _ = send_error(
                writer,
                tag,
                3,
                format!("no process with tag {}", params.process_tag),
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
                tag,
                5,
                format!("unsupported signal: {other}"),
            );
            return;
        }
    };

    let pid = nix::unistd::Pid::from_raw(ph.pid as i32);
    if let Err(e) = nix::sys::signal::kill(pid, sig) {
        let _ = send_error(writer, tag, 5, format!("{e}"));
        return;
    }

    let _ = send_response(writer, tag, proto::response::Kind::Signal(proto::SignalResponse {}));
}

// ---------------------------------------------------------------------------
// resize
// ---------------------------------------------------------------------------

fn handle_resize(
    tag: u32,
    params: proto::ResizeRequest,
    writer: &SharedWriter,
    processes: &Arc<Mutex<HashMap<u32, ProcessHandle>>>,
) {
    let procs = processes.lock().unwrap();
    let ph = match procs.get(&params.process_tag) {
        Some(p) => p,
        None => {
            let _ = send_error(
                writer,
                tag,
                3,
                format!("no process with tag {}", params.process_tag),
            );
            return;
        }
    };

    let master = match &ph.pty_master {
        Some(m) => m,
        None => {
            let _ = send_error(writer, tag, 6, "process has no PTY".into());
            return;
        }
    };

    if let Err(e) = pty_util::set_winsize(master.as_raw_fd(), params.rows as u16, params.cols as u16) {
        let _ = send_error(writer, tag, 6, format!("{e}"));
        return;
    }

    let _ = send_response(writer, tag, proto::response::Kind::Resize(proto::ResizeResponse {}));
}

// ---------------------------------------------------------------------------
// read (file)
// ---------------------------------------------------------------------------

fn handle_read(
    tag: u32,
    params: proto::ReadRequest,
    workspace: &str,
    writer: &SharedWriter,
) {
    if let Err(e) = handle_read_inner(tag, params, workspace, writer) {
        log::error!("[v2:read] tag={tag} error: {e}");
    }
}

fn handle_read_inner(
    tag: u32,
    params: proto::ReadRequest,
    workspace: &str,
    writer: &SharedWriter,
) -> io::Result<()> {
    let target = match resolve_workspace_path(std::path::Path::new(workspace), &params.path) {
        Ok(p) => p,
        Err(e) => {
            let _ = send_error(writer, tag, 7, e);
            return Ok(());
        }
    };

    let data = match fs::read(&target) {
        Ok(d) => d,
        Err(e) => {
            let _ = send_error(writer, tag, 8, format!("{e}"));
            return Ok(());
        }
    };

    let sha = hex::encode(Sha256::digest(&data));
    let size = data.len() as i64;

    let resp = proto::Response {
        tag,
        kind: Some(proto::response::Kind::Read(proto::ReadResponse {
            size_bytes: size,
            sha256: sha,
        })),
    };
    let encoded = resp.encode_to_vec();

    let mut w = writer.lock().unwrap();
    w.write_all(&[MSG_TYPE_RESPONSE])?;
    w.write_all(&(encoded.len() as u32).to_be_bytes())?;
    w.write_all(&encoded)?;

    // Raw data frames
    let mut offset = 0;
    while offset < data.len() {
        let end = std::cmp::min(offset + FILE_CHUNK_SIZE, data.len());
        let chunk = &data[offset..end];
        w.write_all(&(chunk.len() as u32).to_be_bytes())?;
        w.write_all(chunk)?;
        offset = end;
    }
    w.write_all(&0u32.to_be_bytes())?;
    w.flush()
}

// ---------------------------------------------------------------------------
// write (file)
// ---------------------------------------------------------------------------

fn handle_write(
    tag: u32,
    params: proto::WriteRequest,
    workspace: &str,
    writer: &SharedWriter,
    reader: &mut impl io::Read,
) {
    let target = match resolve_workspace_path(std::path::Path::new(workspace), &params.path) {
        Ok(p) => p,
        Err(e) => {
            let _ = send_error(writer, tag, 7, e);
            return;
        }
    };

    let parent = match target.parent() {
        Some(p) => p.to_path_buf(),
        None => {
            let _ = send_error(writer, tag, 7, "invalid target".into());
            return;
        }
    };

    if let Err(e) = fs::create_dir_all(&parent) {
        let _ = send_error(writer, tag, 9, format!("mkdir: {e}"));
        return;
    }

    let base_name = target.file_name().unwrap_or_default().to_string_lossy();
    let tmp_path = parent.join(format!(".{base_name}.{}.tmp", std::process::id()));

    let tmp_file = match fs::File::create(&tmp_path) {
        Ok(f) => f,
        Err(e) => {
            let _ = send_error(writer, tag, 9, format!("create temp: {e}"));
            return;
        }
    };

    let mut tmp_writer = io::BufWriter::new(tmp_file);
    let mut hasher = Sha256::new();
    let mut written: i64 = 0;

    // Read raw data frames from socket until zero-length terminator
    let result = (|| -> Result<(), String> {
        loop {
            let mut len_buf = [0u8; 4];
            reader.read_exact(&mut len_buf).map_err(|e| format!("read frame length: {e}"))?;
            let len = u32::from_be_bytes(len_buf) as usize;
            if len == 0 {
                break;
            }
            let mut buf = vec![0u8; len];
            reader.read_exact(&mut buf).map_err(|e| format!("read frame data: {e}"))?;

            use io::Write;
            tmp_writer.write_all(&buf).map_err(|e| format!("write: {e}"))?;
            hasher.update(&buf);
            written += buf.len() as i64;
        }

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
            tag,
            proto::response::Kind::Write(proto::WriteResponse {
                bytes_written: written,
                sha256: actual_sha,
            }),
        );
        Ok(())
    })();

    if let Err(e) = result {
        let _ = fs::remove_file(&tmp_path);
        let _ = send_error(writer, tag, 9, e);
    }
}

// ---------------------------------------------------------------------------
// stat
// ---------------------------------------------------------------------------

fn handle_stat(
    tag: u32,
    params: proto::StatRequest,
    workspace: &str,
    writer: &SharedWriter,
) {
    let target = match resolve_workspace_path(std::path::Path::new(workspace), &params.path) {
        Ok(p) => p,
        Err(e) => {
            let _ = send_error(writer, tag, 7, e);
            return;
        }
    };

    match fs::metadata(&target) {
        Ok(meta) => {
            use std::os::unix::fs::MetadataExt;
            let mod_time = meta
                .modified()
                .ok()
                .and_then(|t| t.duration_since(std::time::UNIX_EPOCH).ok())
                .map(|d| d.as_secs() as i64)
                .unwrap_or(0);
            let mode_val = meta.mode();
            let _ = send_response(
                writer,
                tag,
                proto::response::Kind::Stat(proto::StatResponse {
                    path: params.path,
                    is_dir: meta.is_dir(),
                    size_bytes: meta.len() as i64,
                    mode: format!("{:04o}", mode_val & 0o7777),
                    mod_time_unix: mod_time,
                }),
            );
        }
        Err(e) if e.kind() == io::ErrorKind::NotFound => {
            let _ = send_error(writer, tag, 10, format!("not found: {}", params.path));
        }
        Err(e) => {
            let _ = send_error(writer, tag, 10, format!("{e}"));
        }
    }
}

// ---------------------------------------------------------------------------
// list (directory)
// ---------------------------------------------------------------------------

fn handle_list(
    tag: u32,
    params: proto::ListRequest,
    workspace: &str,
    writer: &SharedWriter,
) {
    let target = match resolve_workspace_path(std::path::Path::new(workspace), &params.path) {
        Ok(p) => p,
        Err(e) => {
            let _ = send_error(writer, tag, 7, e);
            return;
        }
    };

    let mut entries = Vec::new();
    if let Err(e) = collect_entries(&target, &mut entries, params.recursive) {
        let _ = send_error(writer, tag, 11, format!("{e}"));
        return;
    }

    let _ = send_response(
        writer,
        tag,
        proto::response::Kind::List(proto::ListResponse { entries }),
    );
}

// ---------------------------------------------------------------------------
// tunnel
// ---------------------------------------------------------------------------

fn handle_tunnel(
    tag: u32,
    params: proto::TunnelRequest,
    writer: &SharedWriter,
) {
    let addr = format!("{}:{}", params.host, params.port);
    match TcpStream::connect(&addr) {
        Ok(_tcp) => {
            // Connected successfully. On Unix socket, bidirectional data
            // transfer requires QUIC data streams; the TCP connection is
            // kept alive for the session lifetime but data cannot flow
            // inline on the control channel.
            let _ = send_response(
                writer,
                tag,
                proto::response::Kind::Tunnel(proto::TunnelResponse {}),
            );
        }
        Err(e) => {
            let _ = send_error(writer, tag, 12, format!("{e}"));
        }
    }
}

// ---------------------------------------------------------------------------
// watch / unwatch (was: subscribe / unsubscribe)
// ---------------------------------------------------------------------------

fn handle_watch(
    tag: u32,
    params: proto::WatchRequest,
    workspace: &str,
    writer: &SharedWriter,
    watches: &Arc<Mutex<HashMap<u32, WatchState>>>,
    watch_counter: &Arc<AtomicU32>,
) {
    let watch_id = watch_counter.fetch_add(1, Ordering::Relaxed);

    let watch_path = resolve_watch_path(Path::new(workspace), &params.path);
    let watch_path = match watch_path {
        Ok(p) => p,
        Err(e) => {
            let _ = send_error(writer, tag, 13, e);
            return;
        }
    };

    let shutdown = Arc::new(AtomicBool::new(false));
    let state = WatchState {
        fs_shutdowns: vec![shutdown.clone()],
    };

    watches.lock().unwrap().insert(watch_id, state);

    let _ = send_response(
        writer,
        tag,
        proto::response::Kind::Watch(proto::WatchResponse { watch_id }),
    );

    let w = writer.clone();
    let recursive = params.recursive;
    thread::spawn(move || {
        run_fs_watcher(watch_path, recursive, watch_id, &shutdown, &w);
    });
}

fn handle_unwatch(
    tag: u32,
    params: proto::UnwatchRequest,
    writer: &SharedWriter,
    watches: &Arc<Mutex<HashMap<u32, WatchState>>>,
) {
    let mut ws = watches.lock().unwrap();
    match ws.remove(&params.watch_id) {
        Some(state) => {
            for flag in &state.fs_shutdowns {
                flag.store(true, Ordering::Relaxed);
            }
            let _ = send_response(writer, tag, proto::response::Kind::Unwatch(proto::UnwatchResponse {}));
        }
        None => {
            let _ = send_error(
                writer,
                tag,
                14,
                format!("no watch with id {}", params.watch_id),
            );
        }
    }
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
    watch_id: u32,
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
                    let event_type = mask_to_event_type(event.mask);

                    if let Some(et) = event_type {
                        let _ = send_event(
                            writer,
                            0,
                            proto::event::Kind::FsChange(proto::FsChangeEvent {
                                watch_id,
                                path: full_path.to_string_lossy().into_owned(),
                                event_type: et.to_string(),
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

fn mask_to_event_type(mask: EventMask) -> Option<&'static str> {
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
        use std::os::unix::fs::MetadataExt;
        let mod_time = meta
            .modified()
            .ok()
            .and_then(|t| t.duration_since(std::time::UNIX_EPOCH).ok())
            .map(|d| d.as_secs() as i64)
            .unwrap_or(0);
        entries.push(proto::DirEntry {
            name,
            is_dir: meta.is_dir(),
            size_bytes: meta.len() as i64,
            mode: format!("{:04o}", meta.mode() & 0o7777),
            mod_time_unix: mod_time,
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

    fn send_request_pb(stream: &mut UnixStream, tag: u32, kind: proto::request::Kind) {
        let request = proto::Request {
            tag,
            kind: Some(kind),
        };
        let encoded = request.encode_to_vec();
        let len = encoded.len() as u32;
        stream.write_all(&[MSG_TYPE_REQUEST]).unwrap();
        stream.write_all(&len.to_be_bytes()).unwrap();
        stream.write_all(&encoded).unwrap();
        stream.flush().unwrap();
    }

    /// Read a typed message. Returns (type_byte, raw_bytes).
    fn read_typed_msg(stream: &mut UnixStream) -> (u8, Vec<u8>) {
        let mut type_buf = [0u8; 1];
        stream.read_exact(&mut type_buf).unwrap();
        let mut len_buf = [0u8; 4];
        stream.read_exact(&mut len_buf).unwrap();
        let len = u32::from_be_bytes(len_buf) as usize;
        let mut msg_buf = vec![0u8; len];
        stream.read_exact(&mut msg_buf).unwrap();
        (type_buf[0], msg_buf)
    }

    fn read_response(stream: &mut UnixStream) -> proto::Response {
        let (typ, data) = read_typed_msg(stream);
        assert_eq!(typ, MSG_TYPE_RESPONSE, "expected Response type byte");
        proto::Response::decode(&data[..]).unwrap()
    }

    /// Read next server message, which is either a Response or Event.
    enum ServerMsg {
        Response(proto::Response),
        Event(proto::Event),
    }

    fn read_server_msg(stream: &mut UnixStream) -> Option<ServerMsg> {
        let mut type_buf = [0u8; 1];
        match stream.read_exact(&mut type_buf) {
            Ok(()) => {}
            Err(_) => return None,
        }
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
        match type_buf[0] {
            MSG_TYPE_RESPONSE => Some(ServerMsg::Response(proto::Response::decode(&msg_buf[..]).unwrap())),
            MSG_TYPE_EVENT => Some(ServerMsg::Event(proto::Event::decode(&msg_buf[..]).unwrap())),
            _ => None,
        }
    }

    #[test]
    fn test_ping() {
        let ws = tempfile::tempdir().unwrap();
        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let mut stream = UnixStream::connect(&sock).unwrap();

        send_request_pb(&mut stream, 1, proto::request::Kind::Ping(proto::PingRequest {}));

        let resp = read_response(&mut stream);
        assert_eq!(resp.tag, 1);
        assert!(matches!(resp.kind, Some(proto::response::Kind::Ping(_))));
    }

    #[test]
    fn test_spawn_and_exit() {
        let ws = tempfile::tempdir().unwrap();
        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let mut stream = UnixStream::connect(&sock).unwrap();
        stream.set_read_timeout(Some(std::time::Duration::from_secs(5))).unwrap();

        send_request_pb(&mut stream, 10, proto::request::Kind::Spawn(proto::SpawnRequest {
            command: vec!["echo".into(), "hello v2".into()],
            ..Default::default()
        }));

        let mut got_spawn = false;
        let mut got_stdout = false;
        let mut got_exit = false;

        for _ in 0..20 {
            match read_server_msg(&mut stream) {
                Some(ServerMsg::Response(resp)) => {
                    if resp.tag == 10 {
                        if let Some(proto::response::Kind::Spawn(s)) = &resp.kind {
                            assert_eq!(s.process_tag, 10);
                            got_spawn = true;
                        }
                    }
                }
                Some(ServerMsg::Event(evt)) => {
                    match &evt.kind {
                        Some(proto::event::Kind::Stdout(so)) => {
                            let text = String::from_utf8_lossy(&so.data);
                            if text.contains("hello v2") {
                                got_stdout = true;
                            }
                        }
                        Some(proto::event::Kind::Exit(ex)) => {
                            assert_eq!(ex.exit_code, 0);
                            got_exit = true;
                            break;
                        }
                        _ => {}
                    }
                }
                None => break,
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

        send_request_pb(&mut stream, 20, proto::request::Kind::Spawn(proto::SpawnRequest {
            command: vec!["cat".into()],
            stdin: true,
            ..Default::default()
        }));

        // Read spawn response
        let resp = read_response(&mut stream);
        assert!(matches!(resp.kind, Some(proto::response::Kind::Spawn(_))));
        let process_tag = match &resp.kind {
            Some(proto::response::Kind::Spawn(s)) => s.process_tag,
            _ => panic!("expected spawn response"),
        };

        // Send write_in
        send_request_pb(&mut stream, 21, proto::request::Kind::WriteIn(proto::WriteInRequest {
            process_tag,
            data: b"test input\n".to_vec(),
        }));

        let mut got_output = false;
        let mut got_exit = false;
        let mut signaled = false;

        for _ in 0..30 {
            match read_server_msg(&mut stream) {
                Some(ServerMsg::Response(_)) => continue,
                Some(ServerMsg::Event(evt)) => {
                    match &evt.kind {
                        Some(proto::event::Kind::Stdout(so)) => {
                            let text = String::from_utf8_lossy(&so.data);
                            if text.contains("test input") {
                                got_output = true;
                                if !signaled {
                                    signaled = true;
                                    send_request_pb(&mut stream, 22, proto::request::Kind::Signal(proto::SignalRequest {
                                        process_tag,
                                        signal: "SIGTERM".into(),
                                    }));
                                }
                            }
                        }
                        Some(proto::event::Kind::Exit(_)) => {
                            got_exit = true;
                            break;
                        }
                        _ => {}
                    }
                }
                None => break,
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

        send_request_pb(&mut stream, 30, proto::request::Kind::Stat(proto::StatRequest {
            path: "hello.txt".into(),
        }));

        let resp = read_response(&mut stream);
        assert_eq!(resp.tag, 30);
        match &resp.kind {
            Some(proto::response::Kind::Stat(s)) => {
                assert!(!s.is_dir);
                assert_eq!(s.size_bytes, 7);
            }
            _ => panic!("expected stat response"),
        }
    }

    #[test]
    fn test_stat_nonexistent() {
        let ws = tempfile::tempdir().unwrap();
        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let mut stream = UnixStream::connect(&sock).unwrap();

        send_request_pb(&mut stream, 31, proto::request::Kind::Stat(proto::StatRequest {
            path: "nope.txt".into(),
        }));

        let resp = read_response(&mut stream);
        assert_eq!(resp.tag, 31);
        match &resp.kind {
            Some(proto::response::Kind::Error(e)) => {
                assert!(e.message.contains("not found"));
            }
            _ => panic!("expected error response for nonexistent file"),
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

        send_request_pb(&mut stream, 40, proto::request::Kind::List(proto::ListRequest {
            path: "mydir".into(),
            recursive: false,
        }));

        let resp = read_response(&mut stream);
        assert_eq!(resp.tag, 40);
        match &resp.kind {
            Some(proto::response::Kind::List(ld)) => {
                assert_eq!(ld.entries.len(), 1);
                assert_eq!(ld.entries[0].name, "c.txt");
            }
            _ => panic!("expected list response"),
        }
    }

    #[test]
    fn test_read_file() {
        let ws = tempfile::tempdir().unwrap();
        let content = "hello file read";
        fs::write(ws.path().join("data.txt"), content).unwrap();

        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let mut stream = UnixStream::connect(&sock).unwrap();

        send_request_pb(&mut stream, 50, proto::request::Kind::Read(proto::ReadRequest {
            path: "data.txt".into(),
        }));

        // Read ReadResponse
        let resp = read_response(&mut stream);
        assert_eq!(resp.tag, 50);
        let (resp_size, resp_sha) = match &resp.kind {
            Some(proto::response::Kind::Read(r)) => (r.size_bytes, r.sha256.clone()),
            _ => panic!("expected read response"),
        };
        assert_eq!(resp_size, content.len() as i64);
        assert!(!resp_sha.is_empty());

        // Read raw data frames
        let mut data = Vec::new();
        loop {
            let mut len_buf = [0u8; 4];
            stream.read_exact(&mut len_buf).unwrap();
            let len = u32::from_be_bytes(len_buf) as usize;
            if len == 0 {
                break;
            }
            let mut buf = vec![0u8; len];
            stream.read_exact(&mut buf).unwrap();
            data.extend_from_slice(&buf);
        }
        assert_eq!(String::from_utf8(data).unwrap(), content);
    }

    #[test]
    fn test_spawn_with_pty() {
        let ws = tempfile::tempdir().unwrap();
        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let mut stream = UnixStream::connect(&sock).unwrap();
        stream
            .set_read_timeout(Some(std::time::Duration::from_secs(5)))
            .unwrap();

        send_request_pb(&mut stream, 60, proto::request::Kind::Spawn(proto::SpawnRequest {
            command: vec!["echo".into(), "pty-test".into()],
            pty: true,
            rows: 24,
            cols: 80,
            ..Default::default()
        }));

        let resp = read_response(&mut stream);
        match &resp.kind {
            Some(proto::response::Kind::Spawn(s)) => {
                assert_eq!(s.process_tag, 60);
            }
            _ => panic!("expected spawn response"),
        }

        let mut got_output = false;
        let mut got_exit = false;
        for _ in 0..20 {
            match read_server_msg(&mut stream) {
                Some(ServerMsg::Event(evt)) => {
                    match &evt.kind {
                        Some(proto::event::Kind::Stdout(so)) => {
                            let text = String::from_utf8_lossy(&so.data);
                            if text.contains("pty-test") {
                                got_output = true;
                            }
                        }
                        Some(proto::event::Kind::Exit(ex)) => {
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

        assert!(got_output, "expected pty output");
        assert!(got_exit, "expected exit event");
    }

    #[test]
    fn test_write_file() {
        let ws = tempfile::tempdir().unwrap();
        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let mut stream = UnixStream::connect(&sock).unwrap();

        let content = b"hello write v2!";
        let expected_sha = hex::encode(sha2::Sha256::digest(content));

        // Send WriteRequest
        send_request_pb(&mut stream, 70, proto::request::Kind::Write(proto::WriteRequest {
            path: "test_output.txt".into(),
            expected_sha256: expected_sha.clone(),
            size_hint: content.len() as i64,
        }));

        // Send raw data frames
        let len = content.len() as u32;
        stream.write_all(&len.to_be_bytes()).unwrap();
        stream.write_all(content).unwrap();
        // terminator
        stream.write_all(&0u32.to_be_bytes()).unwrap();
        stream.flush().unwrap();

        let resp = read_response(&mut stream);
        assert_eq!(resp.tag, 70);
        match &resp.kind {
            Some(proto::response::Kind::Write(wf)) => {
                assert_eq!(wf.bytes_written, content.len() as i64);
                assert_eq!(wf.sha256, expected_sha);
            }
            _ => panic!("expected write response, got {:?}", resp.kind),
        }

        let written = std::fs::read(ws.path().join("test_output.txt")).unwrap();
        assert_eq!(written, content);
    }

    #[test]
    fn test_write_file_sha_mismatch() {
        let ws = tempfile::tempdir().unwrap();
        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let mut stream = UnixStream::connect(&sock).unwrap();

        send_request_pb(&mut stream, 71, proto::request::Kind::Write(proto::WriteRequest {
            path: "bad.txt".into(),
            expected_sha256: "0000000000000000000000000000000000000000000000000000000000000000".into(),
            size_hint: 0,
        }));

        // Send raw data
        let data = b"data";
        stream.write_all(&(data.len() as u32).to_be_bytes()).unwrap();
        stream.write_all(data).unwrap();
        stream.write_all(&0u32.to_be_bytes()).unwrap();
        stream.flush().unwrap();

        let resp = read_response(&mut stream);
        assert_eq!(resp.tag, 71);
        match &resp.kind {
            Some(proto::response::Kind::Error(e)) => {
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

        // Write
        send_request_pb(&mut stream, 72, proto::request::Kind::Write(proto::WriteRequest {
            path: "large.bin".into(),
            expected_sha256: String::new(),
            size_hint: 0,
        }));
        for _ in 0..4 {
            let len = chunk_data.len() as u32;
            stream.write_all(&len.to_be_bytes()).unwrap();
            stream.write_all(&chunk_data).unwrap();
        }
        stream.write_all(&0u32.to_be_bytes()).unwrap();
        stream.flush().unwrap();

        let resp = read_response(&mut stream);
        assert_eq!(resp.tag, 72);
        match &resp.kind {
            Some(proto::response::Kind::Write(wf)) => {
                assert_eq!(wf.bytes_written, 2 * 1024 * 1024);
                assert_eq!(wf.sha256, expected_sha);
            }
            _ => panic!("expected write response"),
        }

        // Read it back
        send_request_pb(&mut stream, 73, proto::request::Kind::Read(proto::ReadRequest {
            path: "large.bin".into(),
        }));

        let resp = read_response(&mut stream);
        assert_eq!(resp.tag, 73);
        match &resp.kind {
            Some(proto::response::Kind::Read(r)) => {
                assert_eq!(r.size_bytes, 2 * 1024 * 1024);
                assert_eq!(r.sha256, expected_sha);
            }
            _ => panic!("expected read response"),
        }

        // Read raw data
        let mut data = Vec::new();
        loop {
            let mut len_buf = [0u8; 4];
            stream.read_exact(&mut len_buf).unwrap();
            let len = u32::from_be_bytes(len_buf) as usize;
            if len == 0 {
                break;
            }
            let mut buf = vec![0u8; len];
            stream.read_exact(&mut buf).unwrap();
            data.extend_from_slice(&buf);
        }
        assert_eq!(data.len(), 2 * 1024 * 1024);
    }

    #[test]
    fn test_watch_fs_change() {
        let ws = tempfile::tempdir().unwrap();
        let output_dir = ws.path().join("output");
        fs::create_dir(&output_dir).unwrap();

        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let mut stream = UnixStream::connect(&sock).unwrap();
        stream
            .set_read_timeout(Some(std::time::Duration::from_secs(5)))
            .unwrap();

        send_request_pb(&mut stream, 80, proto::request::Kind::Watch(proto::WatchRequest {
            path: "output".into(),
            recursive: false,
            event_types: vec![],
        }));

        let resp = read_response(&mut stream);
        assert_eq!(resp.tag, 80);
        let watch_id = match &resp.kind {
            Some(proto::response::Kind::Watch(w)) => w.watch_id,
            _ => panic!("expected watch response"),
        };
        assert!(watch_id > 0);

        thread::sleep(std::time::Duration::from_millis(200));

        fs::write(output_dir.join("model.bin"), "data").unwrap();

        let mut got_event = false;
        for _ in 0..30 {
            match read_server_msg(&mut stream) {
                Some(ServerMsg::Event(evt)) => {
                    if let Some(proto::event::Kind::FsChange(fc)) = &evt.kind {
                        assert_eq!(fc.watch_id, watch_id);
                        assert!(fc.path.contains("model.bin"), "path should contain model.bin, got: {}", fc.path);
                        got_event = true;
                        break;
                    }
                }
                _ => {}
            }
        }

        assert!(got_event, "expected fs_change event");
    }

    #[test]
    fn test_unwatch() {
        let ws = tempfile::tempdir().unwrap();
        let output_dir = ws.path().join("watched");
        fs::create_dir(&output_dir).unwrap();

        let (sock, _tx) = start_test_agent(ws.path().to_str().unwrap());

        let mut stream = UnixStream::connect(&sock).unwrap();
        stream
            .set_read_timeout(Some(std::time::Duration::from_secs(2)))
            .unwrap();

        send_request_pb(&mut stream, 90, proto::request::Kind::Watch(proto::WatchRequest {
            path: "watched".into(),
            recursive: false,
            event_types: vec![],
        }));

        let resp = read_response(&mut stream);
        let watch_id = match &resp.kind {
            Some(proto::response::Kind::Watch(w)) => w.watch_id,
            _ => panic!("expected watch response"),
        };

        thread::sleep(std::time::Duration::from_millis(200));

        send_request_pb(&mut stream, 91, proto::request::Kind::Unwatch(proto::UnwatchRequest {
            watch_id,
        }));

        let resp = read_response(&mut stream);
        assert_eq!(resp.tag, 91);
        assert!(matches!(resp.kind, Some(proto::response::Kind::Unwatch(_))));

        thread::sleep(std::time::Duration::from_millis(300));

        fs::write(output_dir.join("should_not_fire.txt"), "data").unwrap();

        thread::sleep(std::time::Duration::from_millis(300));

        send_request_pb(&mut stream, 92, proto::request::Kind::Ping(proto::PingRequest {}));

        let mut got_fs_event = false;
        for _ in 0..10 {
            match read_server_msg(&mut stream) {
                Some(ServerMsg::Event(evt)) => {
                    if matches!(&evt.kind, Some(proto::event::Kind::FsChange(_))) {
                        got_fs_event = true;
                    }
                }
                Some(ServerMsg::Response(resp)) => {
                    if resp.tag == 92 {
                        break;
                    }
                }
                None => break,
            }
        }

        assert!(!got_fs_event, "should not receive fs_change after unwatch");
    }
}
