use super::agent::handle_v2_session;
use iroh::{Endpoint, RelayMode, SecretKey};
use iroh::endpoint::{RecvStream, SendStream};
use log::{error, info, warn};
use std::io::{self, BufReader, Read, Write};
use std::path::PathBuf;
use std::sync::{Arc, Mutex};
use tokio::runtime::Handle;

pub const ALPN: &[u8] = b"arl/executor/v2";

const SECRET_KEY_FILE: &str = "/var/run/arl/iroh-secret-key";

pub struct IrohEndpoint {
    endpoint: Endpoint,
    addr_file: PathBuf,
}

impl IrohEndpoint {
    pub async fn new(addr_file: PathBuf) -> Result<Self, Box<dyn std::error::Error + Send + Sync>> {
        let secret_key = load_or_generate_secret_key();

        let endpoint = Endpoint::builder()
            .secret_key(secret_key)
            .alpns(vec![ALPN.to_vec()])
            .relay_mode(RelayMode::Disabled)
            .clear_discovery()
            .bind()
            .await?;

        let ep = IrohEndpoint { endpoint, addr_file };
        ep.write_addr_file().await?;
        Ok(ep)
    }

    pub async fn serve(
        &self,
        workspace: String,
    ) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
        info!("iroh endpoint listening, node_id={}", self.endpoint.node_id());

        loop {
            let incoming = match self.endpoint.accept().await {
                Some(incoming) => incoming,
                None => {
                    info!("iroh endpoint closed");
                    return Ok(());
                }
            };

            let conn = match incoming.accept() {
                Ok(connecting) => match connecting.await {
                    Ok(conn) => conn,
                    Err(e) => {
                        warn!("iroh connection failed: {e}");
                        continue;
                    }
                },
                Err(e) => {
                    warn!("iroh accept error: {e}");
                    continue;
                }
            };

            let workspace = workspace.clone();
            let handle = Handle::current();

            tokio::spawn(async move {
                if let Err(e) = handle_connection(conn, &workspace, handle).await {
                    error!("iroh connection error: {e}");
                }
            });
        }
    }

    async fn write_addr_file(&self) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
        if let Some(parent) = self.addr_file.parent() {
            std::fs::create_dir_all(parent)?;
        }

        let addr = self.endpoint.node_addr().await?;
        let serialized = serde_json::to_string(&addr)?;
        std::fs::write(&self.addr_file, &serialized)?;
        info!("iroh addr written to {}", self.addr_file.display());
        Ok(())
    }
}

async fn handle_connection(
    conn: iroh::endpoint::Connection,
    workspace: &str,
    handle: Handle,
) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let peer_id = conn.remote_node_id()?;
    info!("iroh connection from {peer_id}");

    loop {
        let (send_stream, recv_stream) = match conn.accept_bi().await {
            Ok(streams) => streams,
            Err(e) => {
                info!("iroh connection closed: {e}");
                return Ok(());
            }
        };

        let workspace = workspace.to_string();
        let handle = handle.clone();

        std::thread::spawn(move || {
            let reader = BufReader::new(SyncRecvStream::new(recv_stream, handle.clone()));
            let writer: Arc<Mutex<Box<dyn Write + Send>>> =
                Arc::new(Mutex::new(Box::new(SyncSendStream::new(send_stream, handle))));

            if let Err(e) = handle_v2_session(reader, writer, &workspace) {
                error!("iroh v2 session error: {e}");
            }
        });
    }
}

struct SyncRecvStream {
    inner: RecvStream,
    handle: Handle,
}

impl SyncRecvStream {
    fn new(inner: RecvStream, handle: Handle) -> Self {
        Self { inner, handle }
    }
}

impl Read for SyncRecvStream {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        self.handle.block_on(async {
            match self.inner.read(buf).await {
                Ok(Some(n)) => Ok(n),
                Ok(None) => Ok(0),
                Err(e) => Err(io::Error::other(format!("{e}"))),
            }
        })
    }
}

struct SyncSendStream {
    inner: SendStream,
    handle: Handle,
}

impl SyncSendStream {
    fn new(inner: SendStream, handle: Handle) -> Self {
        Self { inner, handle }
    }
}

impl Write for SyncSendStream {
    fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
        self.handle.block_on(async {
            self.inner
                .write(buf)
                .await
                .map_err(|e| io::Error::other(format!("{e}")))
        })
    }

    fn flush(&mut self) -> io::Result<()> {
        Ok(())
    }
}

fn load_or_generate_secret_key() -> SecretKey {
    if let Ok(bytes) = std::fs::read(SECRET_KEY_FILE) {
        if bytes.len() == 32 {
            let mut arr = [0u8; 32];
            arr.copy_from_slice(&bytes);
            return SecretKey::from_bytes(&arr);
        }
        warn!("invalid secret key file, generating new key");
    }

    let key = SecretKey::generate(rand_core::OsRng);
    if let Err(e) = write_secret_key_file(&key) {
        warn!("failed to persist iroh secret key: {e}");
    }
    key
}

fn write_secret_key_file(key: &SecretKey) -> io::Result<()> {
    let path = std::path::Path::new(SECRET_KEY_FILE);
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent)?;
    }
    std::fs::write(path, key.to_bytes())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::BufRead;

    #[tokio::test]
    async fn test_iroh_ping_pong() {
        let server_key = SecretKey::generate(rand_core::OsRng);
        let server = Endpoint::builder()
            .secret_key(server_key)
            .alpns(vec![ALPN.to_vec()])
            .relay_mode(RelayMode::Disabled)
            .clear_discovery()
            .bind()
            .await
            .expect("bind server");

        let server_addr = server.node_addr().await.expect("server addr");
        let server_for_close = server.clone();

        let workspace = std::env::temp_dir()
            .join("iroh_test_workspace")
            .to_str()
            .unwrap()
            .to_string();
        std::fs::create_dir_all(&workspace).ok();

        let server_handle = Handle::current();
        let ws = workspace.clone();
        let server_task = tokio::spawn(async move {
            let incoming = server.accept().await.expect("accept incoming");
            let conn = incoming.accept().expect("accept").await.expect("connecting");
            let (send_stream, recv_stream) = conn.accept_bi().await.expect("accept bi");

            let handle = server_handle;
            tokio::task::spawn_blocking(move || {
                let reader = BufReader::new(SyncRecvStream::new(recv_stream, handle.clone()));
                let writer: Arc<Mutex<Box<dyn Write + Send>>> =
                    Arc::new(Mutex::new(Box::new(SyncSendStream::new(send_stream, handle))));
                handle_v2_session(reader, writer, &ws).expect("v2 session");
            })
            .await
            .expect("blocking task");
        });

        let client = Endpoint::builder()
            .relay_mode(RelayMode::Disabled)
            .clear_discovery()
            .bind()
            .await
            .expect("bind client");

        let conn = client
            .connect(server_addr, ALPN)
            .await
            .expect("connect to server");

        let (mut send, recv) = conn.open_bi().await.expect("open bi");

        let ping = r#"{"id":"test-1","method":"ping"}"#;
        send.write_all(ping.as_bytes())
            .await
            .expect("write ping");
        send.write_all(b"\n").await.expect("write newline");

        let handle = Handle::current();
        let response = tokio::task::spawn_blocking(move || {
            let mut reader = BufReader::new(SyncRecvStream::new(recv, handle));
            let mut line = String::new();
            reader.read_line(&mut line).expect("read response");
            line
        })
        .await
        .expect("blocking read");

        send.finish().expect("finish send");

        let parsed: serde_json::Value =
            serde_json::from_str(response.trim()).expect("parse response JSON");
        assert_eq!(parsed["id"], "test-1");
        assert!(parsed["result"].is_object());

        server_task.await.ok();
        server_for_close.close().await;
        client.close().await;
        std::fs::remove_dir_all(&workspace).ok();
    }
}
