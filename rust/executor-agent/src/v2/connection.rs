use super::agent::{handle_v2_session, SharedWriter};
use super::streams;
use iroh::endpoint::Connection;
use log::{error, info, warn};
use std::io::{self, Read, Write};
use std::sync::{Arc, Mutex};
use tokio::runtime::Handle;

/// Per-connection handler for iroh QUIC, supporting multi-stream.
///
/// The first accepted bidirectional stream is the control stream, carrying
/// length-delimited protobuf Envelopes. Additional streams are data streams,
/// identified by a 5-byte header `[1B type][4B tag]`.
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
        let peer_id = self.conn.remote_node_id()?;
        info!("iroh connection from {peer_id}");

        // Accept the control stream (first bidi stream)
        let (send_stream, recv_stream) = match self.conn.accept_bi().await {
            Ok(streams) => streams,
            Err(e) => {
                info!("iroh connection closed before control stream: {e}");
                return Ok(());
            }
        };

        let workspace = self.workspace.clone();
        let handle = self.handle.clone();

        // Run the control stream handler in a blocking thread
        // (handle_v2_session uses synchronous I/O)
        let control_task = tokio::task::spawn_blocking(move || {
            let reader = SyncRecvStream::new(recv_stream, handle.clone());
            let writer: SharedWriter =
                Arc::new(Mutex::new(Box::new(SyncSendStream::new(send_stream, handle))));

            if let Err(e) = handle_v2_session(reader, writer, &workspace) {
                error!("iroh v2 session error: {e}");
            }
        });

        // Accept additional data streams in the background
        let conn = self.conn.clone();
        let data_task = tokio::spawn(async move {
            while let Ok((_send, mut recv)) = conn.accept_bi().await {
                tokio::spawn(async move {
                    match streams::read_stream_header(&mut recv).await {
                        Ok((stream_type, tag)) => {
                            info!(
                                "data stream accepted: type=0x{stream_type:02x} tag={tag}"
                            );
                            // Data streams are forwarded as raw bytes.
                            // The control-stream handler manages all state; a full
                            // implementation would route by (type, tag).
                            let _ = recv;
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

/// Synchronous wrapper around an iroh RecvStream for use with std::io::Read.
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

/// Synchronous wrapper around an iroh SendStream for use with std::io::Write.
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
