use super::agent::{handle_v2_session, SharedWriter, TunnelRegistry};
use super::streams;
use iroh::endpoint::Connection;
use log::{error, info, warn};
use std::collections::HashMap;
use std::io::{self, Read, Write};
use std::sync::{Arc, Mutex};
use tokio::runtime::Handle;

pub struct ConnectionHandler {
    conn: Connection,
    workspace: String,
    handle: Handle,
}

impl ConnectionHandler {
    pub fn new(conn: Connection, workspace: String, handle: Handle) -> Self {
        Self {
            conn,
            workspace,
            handle,
        }
    }

    pub async fn run(self) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
        let peer_id = self.conn.remote_id();
        info!("iroh connection from {peer_id}");

        let (send_stream, recv_stream) = match self.conn.accept_bi().await {
            Ok(streams) => streams,
            Err(e) => {
                info!("iroh connection closed before control stream: {e}");
                return Ok(());
            }
        };

        let tunnel_registry: TunnelRegistry = Arc::new(Mutex::new(HashMap::new()));

        let workspace = self.workspace.clone();
        let handle = self.handle.clone();
        let tunnels_for_control = tunnel_registry.clone();

        let control_task = tokio::task::spawn_blocking(move || {
            let reader = SyncRecvStream::new(recv_stream, handle.clone());
            let writer: SharedWriter =
                Arc::new(Mutex::new(Box::new(SyncSendStream::new(send_stream, handle))));

            if let Err(e) = handle_v2_session(reader, writer, &workspace, Some(tunnels_for_control)) {
                error!("iroh v2 session error: {e}");
            }
        });

        let conn = self.conn.clone();
        let tunnels_for_data = tunnel_registry.clone();
        let data_task = tokio::spawn(async move {
            while let Ok((send, mut recv)) = conn.accept_bi().await {
                let tunnels = tunnels_for_data.clone();
                tokio::spawn(async move {
                    match streams::read_stream_header(&mut recv).await {
                        Ok((stream_type, tag)) => {
                            info!("data stream accepted: type=0x{stream_type:02x} tag={tag}");
                            match stream_type {
                                streams::STREAM_TYPE_TUNNEL => {
                                    handle_tunnel_stream(send, recv, tag, &tunnels).await;
                                }
                                streams::STREAM_TYPE_STDIN => {
                                    info!("stdin data stream tag={tag} (not yet routed)");
                                }
                                streams::STREAM_TYPE_FILE_WRITE => {
                                    info!("file_write data stream tag={tag} (not yet routed)");
                                }
                                _ => {
                                    warn!("unknown data stream type 0x{stream_type:02x}");
                                }
                            }
                        }
                        Err(e) => {
                            warn!("failed to read data stream header: {e}");
                        }
                    }
                });
            }
        });

        let _ = control_task.await;
        data_task.abort();
        Ok(())
    }
}

async fn handle_tunnel_stream(
    mut send: iroh::endpoint::SendStream,
    recv: iroh::endpoint::RecvStream,
    tag: u32,
    tunnels: &TunnelRegistry,
) {
    let target_addr = {
        let reg = match tunnels.lock() {
            Ok(g) => g,
            Err(e) => {
                error!("tunnel registry lock poisoned: {e}");
                return;
            }
        };
        match reg.get(&tag) {
            Some(target) => format!("{}:{}", target.host, target.port),
            None => {
                warn!("tunnel data stream tag={tag}: no registered tunnel");
                return;
            }
        }
    };

    info!("tunnel data stream tag={tag} connecting to {target_addr}");

    let tcp = match tokio::net::TcpStream::connect(&target_addr).await {
        Ok(s) => s,
        Err(e) => {
            error!("tunnel tag={tag}: TCP connect to {target_addr} failed: {e}");
            return;
        }
    };

    let (mut tcp_read, mut tcp_write) = tcp.into_split();

    let (r1, r2) = tokio::join!(
        forward_tcp_to_quic(&mut tcp_read, &mut send, tag),
        forward_quic_to_tcp(recv, &mut tcp_write, tag),
    );

    if let Err(e) = r1 {
        info!("tunnel tag={tag} tcp→quic ended: {e}");
    }
    if let Err(e) = r2 {
        info!("tunnel tag={tag} quic→tcp ended: {e}");
    }
    info!("tunnel data stream tag={tag} closed");
}

async fn forward_tcp_to_quic(
    tcp_read: &mut tokio::net::tcp::OwnedReadHalf,
    quic_send: &mut iroh::endpoint::SendStream,
    tag: u32,
) -> io::Result<()> {
    use tokio::io::AsyncReadExt;
    let mut buf = [0u8; 8192];
    loop {
        let n = tcp_read.read(&mut buf).await?;
        if n == 0 {
            break;
        }
        quic_send
            .write_all(&buf[..n])
            .await
            .map_err(|e| io::Error::other(format!("tunnel tag={tag} quic write: {e}")))?;
    }
    let _ = quic_send.finish();
    Ok(())
}

async fn forward_quic_to_tcp(
    mut quic_recv: iroh::endpoint::RecvStream,
    tcp_write: &mut tokio::net::tcp::OwnedWriteHalf,
    tag: u32,
) -> io::Result<()> {
    use tokio::io::AsyncWriteExt;
    let mut buf = [0u8; 8192];
    loop {
        match quic_recv.read(&mut buf).await {
            Ok(Some(n)) => {
                tcp_write.write_all(&buf[..n]).await?;
            }
            Ok(None) => break,
            Err(e) => {
                return Err(io::Error::other(format!(
                    "tunnel tag={tag} quic read: {e}"
                )));
            }
        }
    }
    Ok(())
}

pub struct SyncRecvStream {
    inner: iroh::endpoint::RecvStream,
    handle: Handle,
}

impl SyncRecvStream {
    pub fn new(inner: iroh::endpoint::RecvStream, handle: Handle) -> Self {
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

pub struct SyncSendStream {
    inner: iroh::endpoint::SendStream,
    handle: Handle,
}

impl SyncSendStream {
    pub fn new(inner: iroh::endpoint::SendStream, handle: Handle) -> Self {
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
