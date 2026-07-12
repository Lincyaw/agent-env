use crate::path_security;
use crate::protocol::{Request, Response};
use crate::pty_util;

use sha2::{Digest, Sha256};
use std::collections::HashSet;
use std::os::fd::AsRawFd;
use std::os::unix::fs::PermissionsExt;
use std::path::PathBuf;
use std::sync::Arc;
use tokio::io::{AsyncBufReadExt, AsyncReadExt, AsyncWriteExt, BufReader};
use tokio::net::UnixListener;
use tokio::process::Command;
use tokio::sync::Mutex;

const FILE_CHUNK_SIZE: usize = 1024 * 1024;
const EXEC_CHUNK_SIZE: usize = 64 * 1024;
const SHELL_CHUNK_SIZE: usize = 4096;
const SIDECAR_SOCKET_GID: u32 = 65532;

pub struct Agent {
    socket_path: PathBuf,
    workspace_dir: PathBuf,
    processes: Arc<Mutex<HashSet<i32>>>,
}

/// Wrapper for serialized writes to the socket. All response writes go through
/// this to avoid interleaved JSON lines from concurrent tasks.
struct Encoder {
    writer: Arc<Mutex<tokio::net::unix::OwnedWriteHalf>>,
}

impl Encoder {
    fn new(writer: tokio::net::unix::OwnedWriteHalf) -> Self {
        Self {
            writer: Arc::new(Mutex::new(writer)),
        }
    }

    fn clone_ref(&self) -> Self {
        Self {
            writer: Arc::clone(&self.writer),
        }
    }

    async fn send(&self, resp: &Response) -> std::io::Result<()> {
        let mut line = serde_json::to_vec(resp).map_err(|e| {
            std::io::Error::other(format!("json encode: {}", e))
        })?;
        line.push(b'\n');
        let mut w = self.writer.lock().await;
        w.write_all(&line).await?;
        w.flush().await
    }
}

impl Agent {
    pub fn new(socket_path: PathBuf, workspace_dir: PathBuf) -> Self {
        Self {
            socket_path,
            workspace_dir,
            processes: Arc::new(Mutex::new(HashSet::new())),
        }
    }

    pub async fn run(self) -> Result<(), Box<dyn std::error::Error>> {
        // Remove stale socket file
        let _ = std::fs::remove_file(&self.socket_path);

        let listener = UnixListener::bind(&self.socket_path)?;

        // Set socket permissions: try chown to sidecar group, fallback to 0666
        let socket_mode: u32 = match nix::unistd::chown(
            &self.socket_path,
            None,
            Some(nix::unistd::Gid::from_raw(SIDECAR_SOCKET_GID)),
        ) {
            Ok(()) => 0o660,
            Err(e) => {
                log::warn!(
                    "chown socket group to {} failed: {}; falling back to 0666",
                    SIDECAR_SOCKET_GID,
                    e
                );
                0o666
            }
        };

        std::fs::set_permissions(&self.socket_path, std::fs::Permissions::from_mode(socket_mode))?;

        log::info!("executor-agent listening on {}", self.socket_path.display());

        let agent = Arc::new(AgentInner {
            workspace_dir: self.workspace_dir,
            processes: self.processes,
        });

        // Graceful shutdown on SIGTERM/SIGINT
        let shutdown = async {
            let mut sigterm =
                tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
                    .expect("register SIGTERM");
            let mut sigint =
                tokio::signal::unix::signal(tokio::signal::unix::SignalKind::interrupt())
                    .expect("register SIGINT");
            tokio::select! {
                _ = sigterm.recv() => log::info!("received SIGTERM"),
                _ = sigint.recv() => log::info!("received SIGINT"),
            }
        };

        tokio::select! {
            _ = shutdown => {
                log::info!("shutting down");
                Ok(())
            }
            result = Self::accept_loop(listener, agent) => result,
        }
    }

    async fn accept_loop(
        listener: UnixListener,
        agent: Arc<AgentInner>,
    ) -> Result<(), Box<dyn std::error::Error>> {
        loop {
            match listener.accept().await {
                Ok((stream, _)) => {
                    let agent = Arc::clone(&agent);
                    tokio::spawn(async move {
                        agent.handle_conn(stream).await;
                    });
                }
                Err(e) => {
                    log::error!("accept error: {}", e);
                }
            }
        }
    }
}

struct AgentInner {
    workspace_dir: PathBuf,
    processes: Arc<Mutex<HashSet<i32>>>,
}

impl AgentInner {
    async fn handle_conn(&self, stream: tokio::net::UnixStream) {
        let (read_half, write_half) = stream.into_split();
        let reader = BufReader::new(read_half);
        let encoder = Encoder::new(write_half);

        let mut lines = reader.lines();

        loop {
            let line = match lines.next_line().await {
                Ok(Some(line)) => line,
                Ok(None) => return, // EOF
                Err(e) => {
                    let resp = Response::error("", format!("read error: {}", e));
                    let _ = encoder.send(&resp).await;
                    return;
                }
            };

            let req: Request = match serde_json::from_str(&line) {
                Ok(r) => r,
                Err(e) => {
                    let resp = Response::error("", format!("invalid request: {}", e));
                    let _ = encoder.send(&resp).await;
                    return;
                }
            };

            match req.req_type.as_str() {
                "exec" => {
                    log::info!(
                        "[exec] id={} cmd={:?} workdir={}",
                        req.id,
                        req.cmd,
                        req.workdir
                    );
                    self.handle_exec(req, &encoder).await;
                }
                "write_file_stream" => {
                    log::info!("[write_file_stream] id={} path={}", req.id, req.path);
                    self.handle_write_file_stream(req, &mut lines, &encoder)
                        .await;
                }
                "read_file_stream" => {
                    log::info!("[read_file] id={} path={}", req.id, req.path);
                    self.handle_read_file_stream(req, &encoder).await;
                }
                "shell" => {
                    log::info!("[shell] id={} workdir={}", req.id, req.workdir);
                    self.handle_shell(req, &mut lines, &encoder).await;
                    return; // shell owns this connection until exit
                }
                "signal" => {
                    log::info!(
                        "[signal] id={} pid={} signal={}",
                        req.id,
                        req.pid,
                        req.signal
                    );
                    self.handle_signal(&req, &encoder).await;
                }
                "ping" => {
                    log::info!("[ping] id={}", req.id);
                    let _ = encoder.send(&Response::done(&req.id)).await;
                }
                _ => {
                    log::info!("[unknown] id={} type={}", req.id, req.req_type);
                    let resp =
                        Response::error(&req.id, format!("unknown type: {}", req.req_type));
                    let _ = encoder.send(&resp).await;
                }
            }
        }
    }

    async fn handle_exec(&self, req: Request, encoder: &Encoder) {
        if req.cmd.is_empty() {
            let _ = encoder
                .send(&Response::error(&req.id, "empty command".to_string()))
                .await;
            return;
        }

        let workdir = if req.workdir.is_empty() {
            self.workspace_dir.clone()
        } else {
            PathBuf::from(&req.workdir)
        };

        let mut cmd = Command::new(&req.cmd[0]);
        cmd.args(&req.cmd[1..]);
        cmd.current_dir(&workdir);

        // Merge env: inherit current env, then overlay request env
        cmd.envs(std::env::vars());
        for (k, v) in &req.env {
            cmd.env(k, v);
        }

        cmd.stdout(std::process::Stdio::piped());
        cmd.stderr(std::process::Stdio::piped());

        // Kill on drop so timeout cancellation works
        cmd.kill_on_drop(true);

        let mut child = match cmd.spawn() {
            Ok(c) => c,
            Err(e) => {
                let _ = encoder
                    .send(&Response::error(
                        &req.id,
                        format!("start: {}", e),
                    ))
                    .await;
                return;
            }
        };

        let pid = match child.id() {
            Some(p) => p as i32,
            None => {
                let _ = encoder
                    .send(&Response::error(
                        &req.id,
                        "process already exited".to_string(),
                    ))
                    .await;
                return;
            }
        };

        // Track PID
        self.track_pid(pid).await;

        let stdout = child.stdout.take().unwrap();
        let stderr = child.stderr.take().unwrap();

        let req_id = req.id.clone();
        let enc_stdout = encoder.clone_ref();
        let enc_stderr = encoder.clone_ref();
        let id_stdout = req_id.clone();
        let id_stderr = req_id.clone();

        let stdout_task = tokio::spawn(async move {
            let mut buf = vec![0u8; EXEC_CHUNK_SIZE];
            let mut reader = stdout;
            loop {
                match reader.read(&mut buf).await {
                    Ok(0) => return,
                    Ok(n) => {
                        let resp = Response {
                            id: id_stdout.clone(),
                            stdout: String::from_utf8_lossy(&buf[..n]).to_string(),
                            ..Default::default()
                        };
                        if enc_stdout.send(&resp).await.is_err() {
                            return;
                        }
                    }
                    Err(_) => return,
                }
            }
        });

        let stderr_task = tokio::spawn(async move {
            let mut buf = vec![0u8; EXEC_CHUNK_SIZE];
            let mut reader = stderr;
            loop {
                match reader.read(&mut buf).await {
                    Ok(0) => return,
                    Ok(n) => {
                        let resp = Response {
                            id: id_stderr.clone(),
                            stderr: String::from_utf8_lossy(&buf[..n]).to_string(),
                            ..Default::default()
                        };
                        if enc_stderr.send(&resp).await.is_err() {
                            return;
                        }
                    }
                    Err(_) => return,
                }
            }
        });

        // Wait for process with optional timeout
        let exit_code = if req.timeout > 0 {
            match tokio::time::timeout(
                std::time::Duration::from_secs(req.timeout as u64),
                child.wait(),
            )
            .await
            {
                Ok(Ok(status)) => status.code().unwrap_or(1),
                Ok(Err(_)) => 1,
                Err(_) => {
                    // Timeout: kill_on_drop will handle cleanup
                    drop(child);
                    1
                }
            }
        } else {
            match child.wait().await {
                Ok(status) => status.code().unwrap_or(1),
                Err(_) => 1,
            }
        };

        // Wait for readers to finish
        let _ = stdout_task.await;
        let _ = stderr_task.await;

        // Remove from tracked processes
        self.untrack_pid(pid).await;

        log::info!("[exec] id={} exit_code={} cmd={:?}", req_id, exit_code, req.cmd);

        let resp = Response {
            id: req_id,
            exit_code: Some(exit_code),
            done: true,
            ..Default::default()
        };
        let _ = encoder.send(&resp).await;
    }

    async fn handle_write_file_stream(
        &self,
        req: Request,
        lines: &mut tokio::io::Lines<BufReader<tokio::net::unix::OwnedReadHalf>>,
        encoder: &Encoder,
    ) {
        if req.path.is_empty() {
            let _ = encoder
                .send(&Response::error(&req.id, "path is required".to_string()))
                .await;
            return;
        }

        let target_path =
            match path_security::resolve_workspace_path(&self.workspace_dir, &req.path) {
                Ok(p) => p,
                Err(e) => {
                    let _ = encoder.send(&Response::error(&req.id, e)).await;
                    return;
                }
            };

        let parent = match target_path.parent() {
            Some(p) => p.to_path_buf(),
            None => {
                let _ = encoder
                    .send(&Response::error(
                        &req.id,
                        "invalid target path".to_string(),
                    ))
                    .await;
                return;
            }
        };

        if let Err(e) = std::fs::create_dir_all(&parent) {
            let _ = encoder
                .send(&Response::error(
                    &req.id,
                    format!("mkdir parent: {}", e),
                ))
                .await;
            return;
        }

        // Create temp file in same directory
        let base_name = target_path
            .file_name()
            .unwrap_or_default()
            .to_string_lossy();
        let tmp_path = parent.join(format!(
            ".{}.{}.tmp",
            base_name,
            std::process::id()
        ));

        let tmp_file = match std::fs::File::create(&tmp_path) {
            Ok(f) => f,
            Err(e) => {
                let _ = encoder
                    .send(&Response::error(
                        &req.id,
                        format!("create temp file: {}", e),
                    ))
                    .await;
                return;
            }
        };

        let mut tmp_writer = std::io::BufWriter::new(tmp_file);
        let mut hasher = Sha256::new();
        let mut written: i64 = 0;
        loop {
            let line = match lines.next_line().await {
                Ok(Some(line)) => line,
                Ok(None) => {
                    let _ = encoder
                        .send(&Response::error(
                            &req.id,
                            "unexpected EOF during file stream".to_string(),
                        ))
                        .await;
                    let _ = std::fs::remove_file(&tmp_path);
                    return;
                }
                Err(e) => {
                    let _ = encoder
                        .send(&Response::error(
                            &req.id,
                            format!("read file chunk: {}", e),
                        ))
                        .await;
                    let _ = std::fs::remove_file(&tmp_path);
                    return;
                }
            };

            let chunk: Request = match serde_json::from_str(&line) {
                Ok(r) => r,
                Err(e) => {
                    let _ = encoder
                        .send(&Response::error(
                            &req.id,
                            format!("read file chunk: {}", e),
                        ))
                        .await;
                    let _ = std::fs::remove_file(&tmp_path);
                    return;
                }
            };

            if !chunk.id.is_empty() && chunk.id != req.id {
                let _ = encoder
                    .send(&Response::error(
                        &req.id,
                        "file chunk id mismatch".to_string(),
                    ))
                    .await;
                let _ = std::fs::remove_file(&tmp_path);
                return;
            }

            match chunk.req_type.as_str() {
                "write_file_chunk" => {
                    if chunk.content.is_empty() {
                        continue;
                    }
                    use std::io::Write;
                    match tmp_writer.write_all(&chunk.content) {
                        Ok(()) => {}
                        Err(e) => {
                            let _ = encoder
                                .send(&Response::error(
                                    &req.id,
                                    format!("write file chunk: {}", e),
                                ))
                                .await;
                            let _ = std::fs::remove_file(&tmp_path);
                            return;
                        }
                    }
                    hasher.update(&chunk.content);
                    written += chunk.content.len() as i64;
                }
                "write_file_finish" => {
                    use std::io::Write;
                    if let Err(e) = tmp_writer.flush() {
                        let _ = encoder
                            .send(&Response::error(
                                &req.id,
                                format!("flush temp file: {}", e),
                            ))
                            .await;
                        let _ = std::fs::remove_file(&tmp_path);
                        return;
                    }
                    drop(tmp_writer);

                    let actual_sha = hex::encode(hasher.finalize());

                    if !req.expected_sha256.is_empty()
                        && !req
                            .expected_sha256
                            .eq_ignore_ascii_case(&actual_sha)
                    {
                        let _ = encoder
                            .send(&Response::error(
                                &req.id,
                                format!(
                                    "sha256 mismatch: expected {} got {}",
                                    req.expected_sha256, actual_sha
                                ),
                            ))
                            .await;
                        let _ = std::fs::remove_file(&tmp_path);
                        return;
                    }

                    // chmod 0644
                    if let Err(e) =
                        std::fs::set_permissions(&tmp_path, std::fs::Permissions::from_mode(0o644))
                    {
                        let _ = encoder
                            .send(&Response::error(
                                &req.id,
                                format!("chmod temp file: {}", e),
                            ))
                            .await;
                        let _ = std::fs::remove_file(&tmp_path);
                        return;
                    }

                    // Atomic rename
                    if let Err(e) = std::fs::rename(&tmp_path, &target_path) {
                        let _ = encoder
                            .send(&Response::error(
                                &req.id,
                                format!("commit file: {}", e),
                            ))
                            .await;
                        let _ = std::fs::remove_file(&tmp_path);
                        return;
                    }

                    let resp = Response {
                        id: req.id.clone(),
                        bytes_written: Some(written),
                        sha256: actual_sha,
                        done: true,
                        ..Default::default()
                    };
                    let _ = encoder.send(&resp).await;
                    return;
                }
                _ => {
                    let _ = encoder
                        .send(&Response::error(
                            &req.id,
                            format!("unexpected file stream message: {}", chunk.req_type),
                        ))
                        .await;
                    let _ = std::fs::remove_file(&tmp_path);
                    return;
                }
            }
        }
    }

    async fn handle_read_file_stream(&self, req: Request, encoder: &Encoder) {
        if req.path.is_empty() {
            let _ = encoder
                .send(&Response::error(&req.id, "path is required".to_string()))
                .await;
            return;
        }

        let target_path =
            match path_security::resolve_workspace_path(&self.workspace_dir, &req.path) {
                Ok(p) => p,
                Err(e) => {
                    let _ = encoder.send(&Response::error(&req.id, e)).await;
                    return;
                }
            };

        let mut file = match tokio::fs::File::open(&target_path).await {
            Ok(f) => f,
            Err(e) => {
                let _ = encoder
                    .send(&Response::error(
                        &req.id,
                        format!("open file: {}", e),
                    ))
                    .await;
                return;
            }
        };

        let mut hasher = Sha256::new();
        let mut buf = vec![0u8; FILE_CHUNK_SIZE];
        let mut offset: i64 = 0;

        loop {
            match file.read(&mut buf).await {
                Ok(0) => {
                    // EOF
                    let size = offset;
                    let sha = hex::encode(hasher.finalize());
                    let resp = Response {
                        id: req.id.clone(),
                        size_bytes: Some(size),
                        sha256: sha,
                        done: true,
                        ..Default::default()
                    };
                    let _ = encoder.send(&resp).await;
                    return;
                }
                Ok(n) => {
                    let chunk = buf[..n].to_vec();
                    hasher.update(&chunk);
                    let resp = Response {
                        id: req.id.clone(),
                        offset,
                        content: chunk,
                        ..Default::default()
                    };
                    if encoder.send(&resp).await.is_err() {
                        return;
                    }
                    offset += n as i64;
                }
                Err(e) => {
                    let _ = encoder
                        .send(&Response::error(
                            &req.id,
                            format!("read file: {}", e),
                        ))
                        .await;
                    return;
                }
            }
        }
    }

    async fn handle_signal(&self, req: &Request, encoder: &Encoder) {
        // Only allow signaling tracked PIDs
        let tracked = {
            let procs = self.processes.lock().await;
            procs.contains(&req.pid)
        };

        if !tracked {
            let _ = encoder
                .send(&Response::error(
                    &req.id,
                    format!("unknown or untracked PID: {}", req.pid),
                ))
                .await;
            return;
        }

        let sig = match req.signal.to_uppercase().as_str() {
            "SIGTERM" => nix::sys::signal::Signal::SIGTERM,
            "SIGKILL" => nix::sys::signal::Signal::SIGKILL,
            "SIGINT" => nix::sys::signal::Signal::SIGINT,
            _ => {
                let _ = encoder
                    .send(&Response::error(
                        &req.id,
                        format!("unsupported signal: {}", req.signal),
                    ))
                    .await;
                return;
            }
        };

        let pid = nix::unistd::Pid::from_raw(req.pid);
        if let Err(e) = nix::sys::signal::kill(pid, sig) {
            let _ = encoder
                .send(&Response::error(
                    &req.id,
                    format!("signal: {}", e),
                ))
                .await;
            return;
        }

        let _ = encoder.send(&Response::done(&req.id)).await;
    }

    async fn handle_shell(
        &self,
        req: Request,
        lines: &mut tokio::io::Lines<BufReader<tokio::net::unix::OwnedReadHalf>>,
        encoder: &Encoder,
    ) {
        // Prefer bash, fall back to /bin/sh
        let shell_path = if std::path::Path::new("/bin/bash").exists() {
            "/bin/bash"
        } else {
            "/bin/sh"
        };

        let workdir = if req.workdir.is_empty() {
            self.workspace_dir.clone()
        } else {
            PathBuf::from(&req.workdir)
        };

        let rows: u16 = if req.rows > 0 { req.rows as u16 } else { 24 };
        let cols: u16 = if req.cols > 0 { req.cols as u16 } else { 80 };

        // Open PTY
        let pty_pair = match pty_util::open_pty(rows, cols) {
            Ok(p) => p,
            Err(e) => {
                let _ = encoder
                    .send(&Response::error(
                        &req.id,
                        format!("start shell: {}", e),
                    ))
                    .await;
                return;
            }
        };

        // Build env
        let mut env_vars: Vec<(String, String)> = std::env::vars().collect();
        let has_term = env_vars.iter().any(|(k, _)| k == "TERM");
        if !has_term {
            env_vars.push(("TERM".to_string(), "xterm-256color".to_string()));
        }
        for (k, v) in &req.env {
            // Remove existing and re-add to overlay
            env_vars.retain(|(key, _)| key != k);
            env_vars.push((k.clone(), v.clone()));
        }

        let slave_fd = pty_pair.slave.as_raw_fd();
        let master_fd = pty_pair.master.as_raw_fd();

        // Fork and exec shell in child with PTY slave as controlling terminal.
        // We use std::process::Command with unsafe pre_exec to set up the PTY.
        let mut cmd = std::process::Command::new(shell_path);
        cmd.arg("-i");
        cmd.current_dir(&workdir);
        cmd.env_clear();
        for (k, v) in &env_vars {
            cmd.env(k, v);
        }

        // Use pre_exec to setup slave terminal in child process
        use std::os::unix::process::CommandExt;

        // We need to dup the slave fd before pre_exec since OwnedFd will be moved
        let slave_fd_dup = nix::unistd::dup(slave_fd)
            .map_err(std::io::Error::other);
        let slave_fd_dup = match slave_fd_dup {
            Ok(fd) => fd,
            Err(e) => {
                let _ = encoder
                    .send(&Response::error(
                        &req.id,
                        format!("start shell: dup slave: {}", e),
                    ))
                    .await;
                return;
            }
        };

        // Close master in child, setup slave as controlling terminal
        unsafe {
            cmd.pre_exec(move || {
                // Close master fd in child
                let _ = libc::close(master_fd);
                pty_util::setup_slave_terminal(slave_fd_dup)
            });
        }

        // Ensure stdin/stdout/stderr are not inherited from parent
        cmd.stdin(std::process::Stdio::null());
        cmd.stdout(std::process::Stdio::null());
        cmd.stderr(std::process::Stdio::null());

        let child = match cmd.spawn() {
            Ok(c) => c,
            Err(e) => {
                let _ = encoder
                    .send(&Response::error(
                        &req.id,
                        format!("start shell: {}", e),
                    ))
                    .await;
                return;
            }
        };

        let pid = child.id() as i32;

        // Drop slave fd in parent -- child has its own copy
        drop(pty_pair.slave);

        // Track PID
        self.track_pid(pid).await;

        let req_id = req.id.clone();
        let enc_reader = encoder.clone_ref();
        let id_reader = req_id.clone();

        // Make master fd non-blocking for async I/O
        let master_raw = pty_pair.master.as_raw_fd();
        let flags = nix::fcntl::fcntl(master_raw, nix::fcntl::FcntlArg::F_GETFL)
            .unwrap_or(0);
        let mut oflags = nix::fcntl::OFlag::from_bits_truncate(flags);
        oflags.insert(nix::fcntl::OFlag::O_NONBLOCK);
        let _ = nix::fcntl::fcntl(
            master_raw,
            nix::fcntl::FcntlArg::F_SETFL(oflags),
        );

        // Wrap master fd in tokio AsyncFd for reading
        let master_fd_for_read = pty_pair.master;
        let async_master =
            match tokio::io::unix::AsyncFd::new(master_fd_for_read) {
                Ok(a) => Arc::new(a),
                Err(e) => {
                    let _ = encoder
                        .send(&Response::error(
                            &req.id,
                            format!("start shell: async fd: {}", e),
                        ))
                        .await;
                    self.untrack_pid(pid).await;
                    return;
                }
            };

        let async_master_reader = Arc::clone(&async_master);
        let async_master_writer = Arc::clone(&async_master);

        // Reader task: read PTY output
        let reader_task = tokio::spawn(async move {
            let mut buf = vec![0u8; SHELL_CHUNK_SIZE];
            loop {
                let result = async_master_reader.readable().await;
                let mut guard = match result {
                    Ok(g) => g,
                    Err(_) => return,
                };

                match guard.try_io(|inner| {
                    let fd = inner.as_raw_fd();
                    let n = unsafe {
                        libc::read(fd, buf.as_mut_ptr() as *mut libc::c_void, buf.len())
                    };
                    if n < 0 {
                        Err(std::io::Error::last_os_error())
                    } else {
                        Ok(n as usize)
                    }
                }) {
                    Ok(Ok(0)) => return,
                    Ok(Ok(n)) => {
                        let resp = Response {
                            id: id_reader.clone(),
                            stdout: String::from_utf8_lossy(&buf[..n]).to_string(),
                            ..Default::default()
                        };
                        if enc_reader.send(&resp).await.is_err() {
                            return;
                        }
                    }
                    Ok(Err(e)) => {
                        if e.raw_os_error() == Some(libc::EIO) {
                            // PTY slave closed (shell exited)
                            return;
                        }
                        return;
                    }
                    Err(_would_block) => continue,
                }
            }
        });

        // Writer task: read JSON from connection, dispatch stdin/resize/signal
        // Read sub-messages from the connection for this shell session
        loop {
            let line = match lines.next_line().await {
                Ok(Some(line)) => line,
                Ok(None) => break, // EOF, connection closed
                Err(_) => break,
            };

            let sub: Request = match serde_json::from_str(&line) {
                Ok(r) => r,
                Err(_) => break,
            };

            match sub.req_type.as_str() {
                "stdin" => {
                    let data = sub.data.as_bytes();
                    // Write to PTY master
                    let result = async_master_writer.writable().await;
                    if let Ok(mut guard) = result {
                        let _ = guard.try_io(|inner| {
                            let fd = inner.as_raw_fd();
                            let n = unsafe {
                                libc::write(
                                    fd,
                                    data.as_ptr() as *const libc::c_void,
                                    data.len(),
                                )
                            };
                            if n < 0 {
                                Err(std::io::Error::last_os_error())
                            } else {
                                Ok(n as usize)
                            }
                        });
                    }
                }
                "resize"
                    if sub.rows > 0 && sub.cols > 0 => {
                        let fd = async_master_writer.as_raw_fd();
                        let _ = pty_util::set_winsize(fd, sub.rows as u16, sub.cols as u16);
                    }
                "signal" => {
                    let sig = match sub.signal.to_uppercase().as_str() {
                        "SIGTERM" => Some(nix::sys::signal::Signal::SIGTERM),
                        "SIGKILL" => Some(nix::sys::signal::Signal::SIGKILL),
                        "SIGINT" => Some(nix::sys::signal::Signal::SIGINT),
                        _ => None,
                    };
                    if let Some(sig) = sig {
                        let _ = nix::sys::signal::kill(nix::unistd::Pid::from_raw(pid), sig);
                    }
                }
                _ => {}
            }
        }

        // Connection closed or reader done; wait for shell process to exit
        // Use waitpid in a blocking task to avoid blocking the tokio runtime
        let exit_code = tokio::task::spawn_blocking(move || {
            let mut status: i32 = 0;
            let ret = unsafe { libc::waitpid(pid, &mut status, 0) };
            if ret < 0 {
                1
            } else if libc::WIFEXITED(status) {
                libc::WEXITSTATUS(status)
            } else {
                1
            }
        })
        .await
        .unwrap_or(1);

        // Wait for reader task to finish
        let _ = reader_task.await;

        // Untrack PID
        self.untrack_pid(pid).await;

        log::info!("[shell] id={} exit_code={}", req_id, exit_code);

        let resp = Response {
            id: req_id,
            exit_code: Some(exit_code),
            done: true,
            ..Default::default()
        };
        let _ = encoder.send(&resp).await;
    }

    async fn track_pid(&self, pid: i32) {
        let mut procs = self.processes.lock().await;
        procs.insert(pid);
    }

    async fn untrack_pid(&self, pid: i32) {
        let mut procs = self.processes.lock().await;
        procs.remove(&pid);
    }
}

