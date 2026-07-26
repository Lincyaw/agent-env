use super::connection::ConnectionHandler;
use iroh::{Endpoint, RelayMode, SecretKey, endpoint::presets};
use log::{error, info, warn};
use std::io;
use std::path::PathBuf;
use tokio::runtime::Handle;

pub const ALPN: &[u8] = b"arl/executor";

const SECRET_KEY_FILE: &str = "/var/run/arl/iroh-secret-key";

/// How long to wait for a relay connection before giving up and serving with
/// direct addresses only. Startup is not blocked by this: the endpoint runs on
/// its own task while the Unix socket and TCP listeners are already serving.
const RELAY_ONLINE_TIMEOUT: std::time::Duration = std::time::Duration::from_secs(10);

/// Where the endpoint publishes its address, recorded so request handlers can
/// serve it without threading the path through every call site.
static ADDR_FILE: std::sync::OnceLock<PathBuf> = std::sync::OnceLock::new();

/// The address JSON this executor has published, or an empty string when the
/// endpoint has not bound yet (or iroh is disabled). Callers re-read later.
pub fn published_addr() -> String {
    ADDR_FILE
        .get()
        .and_then(|p| std::fs::read_to_string(p).ok())
        .unwrap_or_default()
}

pub struct IrohEndpoint {
    endpoint: Endpoint,
    addr_file: PathBuf,
}

impl IrohEndpoint {
    pub async fn new(addr_file: PathBuf) -> Result<Self, Box<dyn std::error::Error + Send + Sync>> {
        let _ = ADDR_FILE.set(addr_file.clone());
        let secret_key = load_or_generate_secret_key();

        let mut builder = Endpoint::builder(presets::N0)
            .secret_key(secret_key)
            .alpns(vec![ALPN.to_vec()]);

        if let Ok(relay_url) = std::env::var("IROH_RELAY_URL") {
            match url::Url::parse(&relay_url) {
                Ok(parsed) => {
                    info!("using custom relay: {relay_url}");
                    let url = iroh::RelayUrl::from(parsed);
                    let relay_map = iroh::RelayMap::from_iter([url]);
                    builder = builder.relay_mode(RelayMode::Custom(relay_map));
                }
                Err(e) => {
                    warn!("invalid IROH_RELAY_URL '{relay_url}': {e}, falling back to default relay");
                }
            }
        }

        let endpoint = builder.bind().await?;

        let ep = IrohEndpoint { endpoint, addr_file };

        // Publish direct addresses immediately. Once bind() returns the
        // endpoint has local addresses and is dialable by anyone on the same
        // network, which in-cluster is every client we care about.
        ep.write_addr_file()?;

        // Only then wait for a relay, and never unconditionally: online()
        // blocks until a relay connects, so an unreachable relay would hang
        // startup forever and the address file would never be written. The
        // relay only buys NAT traversal for clients off this network.
        match tokio::time::timeout(RELAY_ONLINE_TIMEOUT, ep.endpoint.online()).await {
            Ok(()) => {
                info!("iroh endpoint online, id={}", ep.endpoint.id());
                // Re-publish so clients also learn the relay URL.
                ep.write_addr_file()?;
            }
            Err(_) => warn!(
                "iroh relay not connected after {}s; serving with direct addresses only. \
                 Clients on this network connect directly; clients behind NAT may not \
                 reach this sandbox.",
                RELAY_ONLINE_TIMEOUT.as_secs()
            ),
        }

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
        let direct_addrs: Vec<String> = addr.ip_addrs().map(|a| a.to_string()).collect();

        let json = serde_json::json!({
            "id": id_str,
            "relay_url": relay_url,
            "direct_addresses": direct_addrs,
        });
        let content = serde_json::to_string(&json).expect("json serialize");
        // Write-then-rename: the gateway reads this file concurrently and must
        // never observe a partially written JSON document.
        let tmp = self.addr_file.with_extension("tmp");
        std::fs::write(&tmp, &content)?;
        std::fs::rename(&tmp, &self.addr_file)?;
        info!("iroh endpoint addr written to {} (id={}, relay={:?}, direct={:?})", self.addr_file.display(), id_str, relay_url, direct_addrs);
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
    use std::os::unix::fs::OpenOptionsExt;
    let path = std::path::Path::new(SECRET_KEY_FILE);
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent)?;
    }
    let mut f = std::fs::OpenOptions::new()
        .write(true)
        .create(true)
        .truncate(true)
        .mode(0o600)
        .open(path)?;
    io::Write::write_all(&mut f, &key.to_bytes())
}

#[cfg(test)]
mod tests {
    use super::*;
    use super::super::agent::MSG_TYPE_RESPONSE;
    use super::super::connection::SyncRecvStream;
    use super::super::proto;
    use prost::Message;

    /// An unreachable relay must not stop the endpoint from publishing its
    /// address. This is the regression guard for the hang that left
    /// /var/run/arl/iroh-addr missing and silently disabled QUIC direct
    /// connect across the whole cluster.
    #[tokio::test]
    async fn test_addr_file_written_when_relay_unreachable() {
        let dir = tempfile::tempdir().unwrap();
        let addr_file = dir.path().join("iroh-addr");

        // A relay URL that cannot resolve, mirroring a sandbox pod whose DNS
        // cannot see in-cluster service names.
        unsafe {
            std::env::set_var("IROH_RELAY_URL", "http://relay.invalid.unresolvable:3340");
        }
        let started = std::time::Instant::now();
        let ep = IrohEndpoint::new(addr_file.clone()).await;
        unsafe {
            std::env::remove_var("IROH_RELAY_URL");
        }

        let ep = ep.expect("endpoint must bind even when the relay is unreachable");
        assert!(
            started.elapsed() < RELAY_ONLINE_TIMEOUT + std::time::Duration::from_secs(15),
            "startup should give up on the relay, took {:?}",
            started.elapsed()
        );

        let content = std::fs::read_to_string(&addr_file)
            .expect("address file must exist even without a relay");
        let json: serde_json::Value = serde_json::from_str(&content).unwrap();
        assert_eq!(json["id"].as_str().unwrap(), ep.endpoint.id().to_string());
        assert!(
            !json["direct_addresses"].as_array().unwrap().is_empty(),
            "direct addresses are what same-network clients dial: {content}"
        );
    }

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

        // QUIC is a remote transport, so the control stream must authenticate
        // before anything else is served.
        super::super::auth::reset_for_test();
        let frames = [
            proto::Request {
                tag: 0,
                kind: Some(proto::request::Kind::Auth(proto::AuthRequest {
                    token: "test-token".into(),
                })),
            },
            proto::Request {
                tag: 1,
                kind: Some(proto::request::Kind::Ping(proto::PingRequest {})),
            },
        ];
        for frame in &frames {
            let encoded = frame.encode_to_vec();
            let len = encoded.len() as u32;
            send.write_all(&[super::super::agent::MSG_TYPE_REQUEST])
                .await
                .expect("write type");
            send.write_all(&len.to_be_bytes())
                .await
                .expect("write len");
            send.write_all(&encoded).await.expect("write frame");
        }

        let handle = Handle::current();
        let (auth_response, response) = tokio::task::spawn_blocking(move || {
            let mut reader = SyncRecvStream::new(recv, handle);
            let mut read_one = |reader: &mut SyncRecvStream| {
                let mut type_buf = [0u8; 1];
                std::io::Read::read_exact(reader, &mut type_buf).expect("read type");
                assert_eq!(type_buf[0], MSG_TYPE_RESPONSE);
                let mut len_buf = [0u8; 4];
                std::io::Read::read_exact(reader, &mut len_buf).expect("read len");
                let msg_len = u32::from_be_bytes(len_buf) as usize;
                let mut msg_buf = vec![0u8; msg_len];
                std::io::Read::read_exact(reader, &mut msg_buf).expect("read msg");
                proto::Response::decode(&msg_buf[..]).expect("decode response")
            };
            let auth = read_one(&mut reader);
            let ping = read_one(&mut reader);
            (auth, ping)
        })
        .await
        .expect("blocking read");

        match auth_response.kind {
            Some(proto::response::Kind::Auth(a)) => assert!(a.claimed, "first auth claims"),
            other => panic!("expected auth response, got {other:?}"),
        }

        send.finish().expect("finish send");

        assert_eq!(response.tag, 1);
        assert!(matches!(response.kind, Some(proto::response::Kind::Ping(_))));

        server_task.await.ok();
        server_for_close.close().await;
        client.close().await;
        std::fs::remove_dir_all(&workspace).ok();
    }
}
