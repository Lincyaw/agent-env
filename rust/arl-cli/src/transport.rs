use crate::gateway::{Client, ExecOutput, SessionInfo, UploadResult};
use anyhow::Result;
use prost::Message;

const MSG_TYPE_REQUEST: u8 = 0x01;
const MSG_TYPE_RESPONSE: u8 = 0x02;
const MSG_TYPE_EVENT: u8 = 0x03;
const ALPN: &[u8] = b"arl/executor/v2";
const STREAM_TYPE_TUNNEL: u8 = 0x05;

pub enum Transport {
    Http(Client),
    Quic(QuicTransport),
}

/// Try QUIC first (if irohAddr available), fall back to HTTP.
pub async fn connect(_gateway_url: &str, session: &SessionInfo, client: &Client) -> Transport {
    if !session.iroh_addr.is_empty() {
        match QuicTransport::connect(&session.iroh_addr).await {
            Ok(qt) => {
                eprintln!("transport: quic");
                return Transport::Quic(qt);
            }
            Err(e) => {
                eprintln!("quic unavailable ({}), falling back to http", e);
            }
        }
    }
    Transport::Http(client.clone())
}

impl Transport {
    pub async fn exec(&self, session_id: &str, command: &[String]) -> Result<ExecOutput> {
        match self {
            Transport::Http(client) => client.execute_http(session_id, command).await,
            Transport::Quic(qt) => qt.exec(command).await,
        }
    }

    pub async fn shell(&self, _session_id: &str) -> Result<ExecOutput> {
        match self {
            Transport::Http(_) => {
                anyhow::bail!("interactive shell over HTTP not yet implemented in Rust CLI; use `arl exec` instead");
            }
            Transport::Quic(qt) => qt.shell().await,
        }
    }

    pub async fn upload(
        &self,
        session_id: &str,
        path: &str,
        data: Vec<u8>,
    ) -> Result<UploadResult> {
        match self {
            Transport::Http(client) => client.upload_http(session_id, path, data).await,
            Transport::Quic(qt) => qt.upload(path, data).await,
        }
    }

    pub async fn download(&self, session_id: &str, path: &str) -> Result<Vec<u8>> {
        match self {
            Transport::Http(client) => client.download_http(session_id, path).await,
            Transport::Quic(qt) => qt.download(path).await,
        }
    }
}

pub struct QuicTransport {
    conn: iroh::endpoint::Connection,
    _endpoint: iroh::Endpoint,
}

impl QuicTransport {
    pub async fn connect(iroh_addr_raw: &str) -> Result<Self> {
        use iroh::address_lookup::DnsAddressLookup;
        use iroh::endpoint::presets;
        use std::time::Instant;

        let t0 = Instant::now();

        let parsed = parse_iroh_addr(iroh_addr_raw);

        let mut builder = iroh::Endpoint::builder(presets::Minimal)
            .alpns(vec![ALPN.to_vec()])
            .relay_mode(iroh::RelayMode::Default);

        if parsed.relay_url.is_none() {
            builder = builder.address_lookup(DnsAddressLookup::n0_dns());
        }

        let endpoint = builder.bind().await?;

        let t1 = Instant::now();

        let id_bytes = hex::decode(parsed.id.trim())?;
        let id_array: [u8; 32] = id_bytes
            .try_into()
            .map_err(|_| anyhow::anyhow!("invalid endpoint id length"))?;
        let remote_id = iroh::PublicKey::from_bytes(&id_array)?;

        let mut connect_target = iroh::EndpointAddr::new(remote_id);
        if let Some(ref url) = parsed.relay_url {
            let relay = iroh::RelayUrl::from(url::Url::parse(url)?);
            connect_target = connect_target.with_relay_url(relay);
        }
        for addr in &parsed.direct_addresses {
            connect_target = connect_target.with_ip_addr(*addr);
        }

        let connect_fut = endpoint.connect(connect_target, ALPN);
        let conn = tokio::time::timeout(std::time::Duration::from_secs(15), connect_fut)
            .await
            .map_err(|_| anyhow::anyhow!("iroh connect timeout"))??;

        let t2 = Instant::now();

        if std::env::var("ARL_DEBUG").is_ok() {
            let paths = conn.paths();
            let path_info: Vec<String> = paths.iter()
                .map(|p| format!("{}(rtt={:?})", p.remote_addr(), p.rtt()))
                .collect();
            eprintln!(
                "iroh: bind={}ms connect={}ms total={}ms relay={} direct_addrs={} paths=[{}]",
                (t1 - t0).as_millis(),
                (t2 - t1).as_millis(),
                (t2 - t0).as_millis(),
                parsed.relay_url.as_deref().unwrap_or("none"),
                parsed.direct_addresses.len(),
                path_info.join(", "),
            );
        }

        Ok(Self {
            conn,
            _endpoint: endpoint,
        })
    }

    async fn open_control(&self) -> Result<(iroh::endpoint::SendStream, iroh::endpoint::RecvStream)> {
        Ok(self.conn.open_bi().await?)
    }

    async fn send_typed(
        send: &mut iroh::endpoint::SendStream,
        msg_type: u8,
        data: &[u8],
    ) -> Result<()> {
        let len = data.len() as u32;
        let mut header = [0u8; 5];
        header[0] = msg_type;
        header[1..5].copy_from_slice(&len.to_be_bytes());
        send.write_all(&header).await?;
        send.write_all(data).await?;
        Ok(())
    }

    async fn recv_typed(
        recv: &mut iroh::endpoint::RecvStream,
    ) -> Result<(u8, Vec<u8>)> {
        let mut header = [0u8; 5];
        recv.read_exact(&mut header).await?;
        let msg_type = header[0];
        let len = u32::from_be_bytes([header[1], header[2], header[3], header[4]]) as usize;
        let mut buf = vec![0u8; len];
        recv.read_exact(&mut buf).await?;
        Ok((msg_type, buf))
    }

    pub async fn exec(&self, command: &[String]) -> Result<ExecOutput> {
        use crate::proto;

        let (mut send, mut recv) = self.open_control().await?;

        let req = proto::Request {
            tag: 1,
            kind: Some(proto::request::Kind::Spawn(proto::SpawnRequest {
                command: command.to_vec(),
                ..Default::default()
            })),
        };
        Self::send_typed(&mut send, MSG_TYPE_REQUEST, &req.encode_to_vec()).await?;

        let mut stdout_buf = Vec::new();
        let mut stderr_buf = Vec::new();
        let mut exit_code = 0i32;

        loop {
            let (msg_type, data) = Self::recv_typed(&mut recv).await?;
            match msg_type {
                MSG_TYPE_RESPONSE => {
                    let resp = proto::Response::decode(&data[..])?;
                    if let Some(proto::response::Kind::Error(e)) = resp.kind {
                        anyhow::bail!("executor error: {}", e.message);
                    }
                }
                MSG_TYPE_EVENT => {
                    let evt = proto::Event::decode(&data[..])?;
                    match evt.kind {
                        Some(proto::event::Kind::Stdout(so)) => stdout_buf.extend_from_slice(&so.data),
                        Some(proto::event::Kind::Stderr(se)) => stderr_buf.extend_from_slice(&se.data),
                        Some(proto::event::Kind::Exit(ex)) => {
                            exit_code = ex.exit_code;
                            break;
                        }
                        _ => {}
                    }
                }
                _ => {}
            }
        }

        let _ = send.finish();

        Ok(ExecOutput {
            stdout: String::from_utf8_lossy(&stdout_buf).into_owned(),
            stderr: String::from_utf8_lossy(&stderr_buf).into_owned(),
            exit_code,
        })
    }

    pub async fn shell(&self) -> Result<ExecOutput> {
        use crate::proto;

        let (mut send, mut recv) = self.open_control().await?;

        let (term_cols, term_rows) = crossterm::terminal::size().unwrap_or((80, 24));
        let req = proto::Request {
            tag: 1,
            kind: Some(proto::request::Kind::Spawn(proto::SpawnRequest {
                command: vec!["/bin/sh".into(), "-i".into()],
                pty: true,
                stdin: true,
                rows: term_rows as i32,
                cols: term_cols as i32,
                ..Default::default()
            })),
        };
        Self::send_typed(&mut send, MSG_TYPE_REQUEST, &req.encode_to_vec()).await?;

        // Read spawn response
        let (_, data) = Self::recv_typed(&mut recv).await?;
        let resp = proto::Response::decode(&data[..])?;
        let process_tag = match resp.kind {
            Some(proto::response::Kind::Spawn(s)) => s.process_tag,
            Some(proto::response::Kind::Error(e)) => anyhow::bail!("spawn error: {}", e.message),
            _ => anyhow::bail!("unexpected response"),
        };

        // Set up raw terminal
        let _raw_guard = RawTerminalGuard::new()?;

        // Forward stdin → write_in, stdout events → terminal
        let send = std::sync::Arc::new(tokio::sync::Mutex::new(send));

        let send_clone = send.clone();
        let stdin_task = tokio::spawn(async move {
            use tokio::io::AsyncReadExt;
            let mut stdin = tokio::io::stdin();
            let mut buf = [0u8; 1024];
            loop {
                let n = match stdin.read(&mut buf).await {
                    Ok(0) | Err(_) => break,
                    Ok(n) => n,
                };
                let req = proto::Request {
                    tag: 0,
                    kind: Some(proto::request::Kind::WriteIn(proto::WriteInRequest {
                        process_tag,
                        data: buf[..n].to_vec(),
                    })),
                };
                let mut s = send_clone.lock().await;
                if Self::send_typed(&mut s, MSG_TYPE_REQUEST, &req.encode_to_vec())
                    .await
                    .is_err()
                {
                    break;
                }
            }
        });

        use std::io::Write;
        let mut exit_code = 0i32;
        loop {
            match Self::recv_typed(&mut recv).await {
                Ok((MSG_TYPE_EVENT, data)) => {
                    let evt = proto::Event::decode(&data[..])?;
                    match evt.kind {
                        Some(proto::event::Kind::Stdout(so)) => {
                            let _ = std::io::stdout().write_all(&so.data);
                            let _ = std::io::stdout().flush();
                        }
                        Some(proto::event::Kind::Stderr(se)) => {
                            let _ = std::io::stderr().write_all(&se.data);
                        }
                        Some(proto::event::Kind::Exit(ex)) => {
                            exit_code = ex.exit_code;
                            break;
                        }
                        _ => {}
                    }
                }
                Ok((MSG_TYPE_RESPONSE, _)) => continue,
                Ok(_) => continue,
                Err(_) => break,
            }
        }

        stdin_task.abort();
        let mut s = send.lock().await;
        let _ = s.finish();

        Ok(ExecOutput {
            stdout: String::new(),
            stderr: String::new(),
            exit_code,
        })
    }

    pub async fn upload(&self, path: &str, data: impl AsRef<[u8]>) -> Result<UploadResult> {
        use crate::proto;

        let (mut send, mut recv) = self.open_control().await?;

        let req = proto::Request {
            tag: 1,
            kind: Some(proto::request::Kind::Write(proto::WriteRequest {
                path: path.to_string(),
                ..Default::default()
            })),
        };
        Self::send_typed(&mut send, MSG_TYPE_REQUEST, &req.encode_to_vec()).await?;

        // Send raw data frames
        let data = data.as_ref();
        let len_bytes = (data.len() as u32).to_be_bytes();
        send.write_all(&len_bytes).await?;
        send.write_all(data).await?;
        send.write_all(&0u32.to_be_bytes()).await?;
        let _ = send.finish();

        let (_, resp_data) = Self::recv_typed(&mut recv).await?;
        let resp = proto::Response::decode(&resp_data[..])?;
        match resp.kind {
            Some(proto::response::Kind::Write(w)) => Ok(UploadResult {
                path: path.to_string(),
                bytes_written: w.bytes_written,
                sha256: w.sha256,
            }),
            Some(proto::response::Kind::Error(e)) => anyhow::bail!("write error: {}", e.message),
            _ => anyhow::bail!("unexpected response"),
        }
    }

    pub async fn download(&self, path: &str) -> Result<Vec<u8>> {
        use crate::proto;

        let (mut send, mut recv) = self.open_control().await?;

        let req = proto::Request {
            tag: 1,
            kind: Some(proto::request::Kind::Read(proto::ReadRequest {
                path: path.to_string(),
            })),
        };
        Self::send_typed(&mut send, MSG_TYPE_REQUEST, &req.encode_to_vec()).await?;

        let (_, resp_data) = Self::recv_typed(&mut recv).await?;
        let resp = proto::Response::decode(&resp_data[..])?;
        match &resp.kind {
            Some(proto::response::Kind::Error(e)) => anyhow::bail!("read error: {}", e.message),
            Some(proto::response::Kind::Read(_)) => {}
            _ => anyhow::bail!("unexpected response"),
        }

        let mut file_data = Vec::new();
        loop {
            let mut len_buf = [0u8; 4];
            recv.read_exact(&mut len_buf).await?;
            let chunk_len = u32::from_be_bytes(len_buf) as usize;
            if chunk_len == 0 {
                break;
            }
            let mut chunk = vec![0u8; chunk_len];
            recv.read_exact(&mut chunk).await?;
            file_data.extend_from_slice(&chunk);
        }

        let _ = send.finish();
        Ok(file_data)
    }

    pub async fn tunnel_forward(
        &self,
        remote_host: &str,
        remote_port: u16,
        local_port: u16,
    ) -> Result<()> {
        use crate::proto;

        // Register tunnel on control stream
        let (mut send, mut recv) = self.open_control().await?;
        let tunnel_tag = 1u32;
        let req = proto::Request {
            tag: tunnel_tag,
            kind: Some(proto::request::Kind::Tunnel(proto::TunnelRequest {
                host: remote_host.to_string(),
                port: remote_port as u32,
            })),
        };
        Self::send_typed(&mut send, MSG_TYPE_REQUEST, &req.encode_to_vec()).await?;

        let (_, resp_data) = Self::recv_typed(&mut recv).await?;
        let resp = proto::Response::decode(&resp_data[..])?;
        match resp.kind {
            Some(proto::response::Kind::Tunnel(_)) => {}
            Some(proto::response::Kind::Error(e)) => {
                anyhow::bail!("tunnel error: {}", e.message)
            }
            _ => anyhow::bail!("unexpected tunnel response"),
        }

        // Start local TCP listener
        let listener = tokio::net::TcpListener::bind(format!("127.0.0.1:{local_port}")).await?;
        eprintln!("listening on 127.0.0.1:{local_port}");

        loop {
            let (tcp_stream, peer) = listener.accept().await?;
            eprintln!("tunnel connection from {peer}");

            let conn = self.conn.clone();
            tokio::spawn(async move {
                if let Err(e) = handle_tunnel_conn(conn, tunnel_tag, tcp_stream).await {
                    eprintln!("tunnel connection error: {e}");
                }
            });
        }
    }
}

async fn handle_tunnel_conn(
    conn: iroh::endpoint::Connection,
    tunnel_tag: u32,
    tcp_stream: tokio::net::TcpStream,
) -> Result<()> {
    let (mut quic_send, mut quic_recv) = conn.open_bi().await?;

    // Data stream header: [0x05][tag]
    let mut hdr = [0u8; 5];
    hdr[0] = STREAM_TYPE_TUNNEL;
    hdr[1..5].copy_from_slice(&tunnel_tag.to_be_bytes());
    quic_send.write_all(&hdr).await?;

    let (mut tcp_read, mut tcp_write) = tcp_stream.into_split();

    let (r1, r2) = tokio::join!(
        async {
            use tokio::io::AsyncReadExt;
            let mut buf = [0u8; 8192];
            loop {
                let n = tcp_read.read(&mut buf).await?;
                if n == 0 {
                    break;
                }
                quic_send.write_all(&buf[..n]).await
                    .map_err(|e| std::io::Error::other(format!("{e}")))?;
            }
            let _ = quic_send.finish();
            Ok::<_, std::io::Error>(())
        },
        async {
            use tokio::io::AsyncWriteExt;
            let mut buf = [0u8; 8192];
            loop {
                match quic_recv.read(&mut buf).await {
                    Ok(Some(n)) => tcp_write.write_all(&buf[..n]).await?,
                    Ok(None) => break,
                    Err(e) => {
                        return Err(std::io::Error::other(format!("{e}")));
                    }
                }
            }
            Ok::<_, std::io::Error>(())
        },
    );

    if let Err(e) = r1 {
        eprintln!("tunnel tcp→quic: {e}");
    }
    if let Err(e) = r2 {
        eprintln!("tunnel quic→tcp: {e}");
    }
    Ok(())
}

struct IrohAddr {
    id: String,
    relay_url: Option<String>,
    direct_addresses: Vec<std::net::SocketAddr>,
}

fn parse_iroh_addr(raw: &str) -> IrohAddr {
    let trimmed = raw.trim();
    if let Ok(v) = serde_json::from_str::<serde_json::Value>(trimmed) {
        let id = v["id"].as_str().unwrap_or("").to_string();
        let relay = v["relay_url"].as_str().map(String::from);
        let addrs: Vec<std::net::SocketAddr> = v["direct_addresses"]
            .as_array()
            .map(|arr| {
                arr.iter()
                    .filter_map(|a| a.as_str()?.parse().ok())
                    .collect()
            })
            .unwrap_or_default();
        if !id.is_empty() {
            return IrohAddr { id, relay_url: relay, direct_addresses: addrs };
        }
    }
    IrohAddr { id: trimmed.to_string(), relay_url: None, direct_addresses: vec![] }
}

struct RawTerminalGuard;

impl RawTerminalGuard {
    fn new() -> Result<Self> {
        crossterm::terminal::enable_raw_mode()?;
        Ok(Self)
    }
}

impl Drop for RawTerminalGuard {
    fn drop(&mut self) {
        let _ = crossterm::terminal::disable_raw_mode();
    }
}
