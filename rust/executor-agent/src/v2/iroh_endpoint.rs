use super::connection::ConnectionHandler;
use iroh::{Endpoint, RelayMode, SecretKey, endpoint::presets};
use log::{error, info, warn};
use std::io;
use std::path::PathBuf;
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

        let mut builder = Endpoint::builder(presets::N0)
            .secret_key(secret_key)
            .alpns(vec![ALPN.to_vec()]);

        if let Ok(relay_url) = std::env::var("IROH_RELAY_URL") {
            info!("using custom relay: {relay_url}");
            let url = iroh::RelayUrl::from(url::Url::parse(&relay_url)?);
            let relay_map = iroh::RelayMap::from_iter([url]);
            builder = builder.relay_mode(RelayMode::Custom(relay_map));
        }

        let endpoint = builder.bind().await?;

        endpoint.online().await;
        info!("iroh endpoint online, id={}", endpoint.id());

        let ep = IrohEndpoint { endpoint, addr_file };
        ep.write_addr_file()?;
        Ok(ep)
    }

    pub async fn serve(
        &self,
        workspace: String,
    ) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
        info!("iroh endpoint serving, id={}", self.endpoint.id());

        loop {
            let incoming = match self.endpoint.accept().await {
                Some(incoming) => incoming,
                None => {
                    info!("iroh endpoint closed");
                    return Ok(());
                }
            };

            let conn = match incoming.accept() {
                Ok(accepting) => match accepting.await {
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
                let handler = ConnectionHandler::new(conn, workspace, handle);
                if let Err(e) = handler.run().await {
                    error!("iroh connection error: {e}");
                }
            });
        }
    }

    fn write_addr_file(&self) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
        if let Some(parent) = self.addr_file.parent() {
            std::fs::create_dir_all(parent)?;
        }

        // Write JSON with both id and relay_url for fast client connection.
        // Clients parse the JSON to get relay_url (skipping DNS lookup).
        // Fallback: if content is a plain hex string, treat as EndpointId-only.
        let id_str = self.endpoint.id().to_string();
        let addr = self.endpoint.addr();
        let relay_url = addr.relay_urls().next().map(|u| u.to_string());

        let json = serde_json::json!({
            "id": id_str,
            "relay_url": relay_url,
        });
        let content = serde_json::to_string(&json).unwrap_or(id_str.clone());
        std::fs::write(&self.addr_file, &content)?;
        info!("iroh endpoint addr written to {} (id={}, relay={:?})", self.addr_file.display(), id_str, relay_url);
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

    let key = SecretKey::generate();
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
    use super::super::agent::MSG_TYPE_RESPONSE;
    use super::super::connection::SyncRecvStream;
    use super::super::proto;
    use prost::Message;

    #[tokio::test]
    async fn test_iroh_ping_pong() {
        // Tests use Minimal + RelayMode::Default to avoid depending on
        // n0 DNS infrastructure. Both sides are in-process so direct
        // addr exchange works.
        let server_key = SecretKey::generate();
        let server = Endpoint::builder(presets::Minimal)
            .secret_key(server_key)
            .alpns(vec![ALPN.to_vec()])
            .relay_mode(RelayMode::Default)
            .bind()
            .await
            .expect("bind server");

        let server_addr = server.addr();
        let server_for_close = server.clone();

        let workspace = std::env::temp_dir()
            .join("iroh_test_workspace_pb")
            .to_str()
            .unwrap()
            .to_string();
        std::fs::create_dir_all(&workspace).ok();

        let server_handle = Handle::current();
        let ws = workspace.clone();
        let server_task = tokio::spawn(async move {
            let incoming = server.accept().await.expect("accept incoming");
            let conn = incoming.accept().expect("accept").await.expect("accepting");

            let handler = ConnectionHandler::new(conn, ws, server_handle);
            handler.run().await.expect("connection handler");
        });

        let client = Endpoint::builder(presets::Minimal)
            .relay_mode(RelayMode::Default)
            .bind()
            .await
            .expect("bind client");

        let conn = client
            .connect(server_addr, ALPN)
            .await
            .expect("connect to server");

        let (mut send, recv) = conn.open_bi().await.expect("open bi");

        let ping = proto::Request {
            tag: 1,
            kind: Some(proto::request::Kind::Ping(proto::PingRequest {})),
        };
        let encoded = ping.encode_to_vec();
        let len = encoded.len() as u32;
        send.write_all(&[super::super::agent::MSG_TYPE_REQUEST])
            .await
            .expect("write type");
        send.write_all(&len.to_be_bytes())
            .await
            .expect("write len");
        send.write_all(&encoded).await.expect("write ping");

        let handle = Handle::current();
        let response = tokio::task::spawn_blocking(move || {
            let mut reader = SyncRecvStream::new(recv, handle);
            let mut type_buf = [0u8; 1];
            std::io::Read::read_exact(&mut reader, &mut type_buf).expect("read type");
            assert_eq!(type_buf[0], MSG_TYPE_RESPONSE);
            let mut len_buf = [0u8; 4];
            std::io::Read::read_exact(&mut reader, &mut len_buf).expect("read len");
            let msg_len = u32::from_be_bytes(len_buf) as usize;
            let mut msg_buf = vec![0u8; msg_len];
            std::io::Read::read_exact(&mut reader, &mut msg_buf).expect("read msg");
            proto::Response::decode(&msg_buf[..]).expect("decode response")
        })
        .await
        .expect("blocking read");

        send.finish().expect("finish send");

        assert_eq!(response.tag, 1);
        assert!(matches!(response.kind, Some(proto::response::Kind::Ping(_))));

        server_task.await.ok();
        server_for_close.close().await;
        client.close().await;
        std::fs::remove_dir_all(&workspace).ok();
    }
}
