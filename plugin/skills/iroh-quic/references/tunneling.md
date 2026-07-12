# TCP Tunneling over iroh QUIC

## Table of Contents

1. [Architecture](#architecture)
2. [dumbpipe — canonical reference](#dumbpipe)
3. [Rust Implementation](#rust-implementation)
4. [Python Client Implementation](#python-client-implementation)
5. [Best Practices](#best-practices)

---

## Architecture

TCP tunneling over iroh QUIC maps each TCP connection to a separate QUIC bidi stream:

```
Local TCP client ←→ [SDK: local listener] ←→ QUIC bidi stream ←→ [Executor: TCP connect] ←→ Target TCP service
```

- SDK opens a local TCP listener (e.g., `0.0.0.0:2222`)
- Each incoming TCP connection opens a new QUIC bidi stream
- First bytes on the stream carry the target address (length-prefixed)
- After that, raw bytes flow bidirectionally
- When either side closes, the other side's half is shut down

Multiple TCP connections through one iroh QUIC connection — each as a separate stream.

---

## dumbpipe

The `dumbpipe` tool (github.com/n0-computer/dumbpipe) is the canonical TCP tunneling
implementation over iroh QUIC.

### Usage examples

```bash
# Server: forward iroh connections to local TCP port 3000
dumbpipe listen-tcp --host localhost:3000
# Prints a ticket

# Client: expose remote service on local port 3001
dumbpipe connect-tcp --addr 0.0.0.0:3001 <ticket>

# Unix socket forwarding
dumbpipe listen-unix --socket-path /path/to/socket
dumbpipe connect-unix --socket-path /path/to/local-socket <ticket>

# Cross-connect: unix socket on server, TCP on client
dumbpipe listen-unix --socket-path /var/run/my-app.sock
dumbpipe connect-tcp --addr 127.0.0.1:8080 <ticket>
```

---

## Rust Implementation

### Bidirectional forwarding (the critical part)

The naive sequential approach DOES NOT WORK — the first `tokio::io::copy` blocks until
EOF, preventing data from flowing in the other direction.

#### Option A: tokio::join! (recommended)

```rust
async fn forward_tunnel(
    mut quic_send: SendStream,
    mut quic_recv: RecvStream,
    tcp_stream: tokio::net::TcpStream,
) -> Result<()> {
    let (mut tcp_read, mut tcp_write) = tcp_stream.into_split();

    let (r1, r2) = tokio::join!(
        forward_tcp_to_quic(&mut tcp_read, &mut quic_send),
        forward_quic_to_tcp(&mut quic_recv, &mut tcp_write),
    );
    r1.ok();
    r2.ok();
    Ok(())
}
```

#### If SendStream/RecvStream implement AsyncRead/AsyncWrite

```rust
let (mut tcp_read, mut tcp_write) = tcp_stream.into_split();
let (r1, r2) = tokio::join!(
    tokio::io::copy(&mut tcp_read, &mut quic_send),
    tokio::io::copy(&mut quic_recv, &mut tcp_write),
);
```

#### If NOT (manual read/write loop)

```rust
async fn forward_quic_to_tcp(
    recv: &mut RecvStream,
    tcp_write: &mut tokio::net::tcp::OwnedWriteHalf,
) -> Result<()> {
    let mut buf = [0u8; 8192];
    loop {
        match recv.read(&mut buf).await? {
            Some(n) => {
                tokio::io::AsyncWriteExt::write_all(tcp_write, &buf[..n]).await?;
            }
            None => break,  // EOF
        }
    }
    Ok(())
}

async fn forward_tcp_to_quic(
    tcp_read: &mut tokio::net::tcp::OwnedReadHalf,
    send: &mut SendStream,
) -> Result<()> {
    let mut buf = [0u8; 8192];
    loop {
        let n = tokio::io::AsyncReadExt::read(tcp_read, &mut buf).await?;
        if n == 0 { break }  // EOF
        send.write_all(&buf[..n]).await?;
    }
    send.finish()?;
    Ok(())
}
```

### Complete ProtocolHandler for TCP tunneling

```rust
use iroh::protocol::ProtocolHandler;

#[derive(Debug, Clone)]
struct TcpTunnelHandler;

impl ProtocolHandler for TcpTunnelHandler {
    async fn accept(&self, connection: Connection) -> Result<(), AcceptError> {
        // Each new bidi stream = one TCP tunnel
        loop {
            let Some((send, mut recv)) = connection.accept_bi().await? else { break };

            tokio::spawn(async move {
                // Read target address: [2B length][address bytes]
                let mut len_buf = [0u8; 2];
                recv.read_exact(&mut len_buf).await?;
                let addr_len = u16::from_be_bytes(len_buf) as usize;
                let mut addr_buf = vec![0u8; addr_len];
                recv.read_exact(&mut addr_buf).await?;
                let target = String::from_utf8(addr_buf)?;

                // Connect to target TCP service
                let tcp = tokio::net::TcpStream::connect(&target).await?;
                let (mut tcp_read, mut tcp_write) = tcp.into_split();

                // Bidirectional forwarding
                let (r1, r2) = tokio::join!(
                    forward_tcp_to_quic(&mut tcp_read, send),
                    forward_quic_to_tcp(recv, &mut tcp_write),
                );
                r1.ok();
                r2.ok();
                Ok::<_, anyhow::Error>(())
            });
        }
        Ok(())
    }
}
```

### Self-contained bidi stream approach (without Router)

When using a single ALPN with a connection handler that multiplexes by stream content:

```rust
// In the connection accept loop, when a new bidi stream arrives:
async fn handle_tunnel_stream(
    mut send: SendStream,
    mut recv: RecvStream,
) -> Result<()> {
    // Read target: [2B len][addr]
    let mut len_buf = [0u8; 2];
    recv.read_exact(&mut len_buf).await?;
    let addr_len = u16::from_be_bytes(len_buf) as usize;
    let mut addr_buf = vec![0u8; addr_len];
    recv.read_exact(&mut addr_buf).await?;
    let target = String::from_utf8(addr_buf)?;

    let tcp = tokio::net::TcpStream::connect(&target).await?;
    let (mut tcp_read, mut tcp_write) = tcp.into_split();

    let (r1, r2) = tokio::join!(
        async {
            let mut buf = [0u8; 8192];
            loop {
                let n = tokio::io::AsyncReadExt::read(&mut tcp_read, &mut buf).await?;
                if n == 0 { break }
                send.write_all(&buf[..n]).await?;
            }
            send.finish()?;
            Ok::<_, anyhow::Error>(())
        },
        async {
            let mut buf = [0u8; 8192];
            loop {
                match recv.read(&mut buf).await? {
                    Some(n) => {
                        tokio::io::AsyncWriteExt::write_all(&mut tcp_write, &buf[..n]).await?;
                    }
                    None => break,
                }
            }
            Ok::<_, anyhow::Error>(())
        },
    );
    r1.ok();
    r2.ok();
    Ok(())
}
```

---

## Python Client Implementation

### Local port forwarding (SDK side)

```python
import asyncio
import struct
import iroh

ALPN = b"my/tunnel/v1"

async def tunnel_forward(
    conn: iroh.Connection,
    remote_host: str,
    remote_port: int,
    local_port: int,
    local_host: str = "127.0.0.1",
):
    """Forward local_port → remote_host:remote_port through QUIC tunnel."""
    server = await asyncio.start_server(
        lambda r, w: _handle_local_conn(conn, remote_host, remote_port, r, w),
        local_host,
        local_port,
    )
    async with server:
        await server.serve_forever()

async def _handle_local_conn(
    conn: iroh.Connection,
    remote_host: str,
    remote_port: int,
    local_reader: asyncio.StreamReader,
    local_writer: asyncio.StreamWriter,
):
    """Handle one local TCP connection by opening a QUIC bidi stream."""
    try:
        bi = await conn.open_bi()
        send = bi.send()
        recv = bi.recv()

        # Send target address: [2B length][address bytes]
        target = f"{remote_host}:{remote_port}".encode()
        header = struct.pack(">H", len(target)) + target
        await send.write_all(list(header))

        # Bidirectional forwarding
        await asyncio.gather(
            _forward_local_to_quic(local_reader, send),
            _forward_quic_to_local(recv, local_writer),
        )
    except Exception:
        pass
    finally:
        local_writer.close()

async def _forward_local_to_quic(
    reader: asyncio.StreamReader,
    send: iroh.SendStream,
):
    while True:
        data = await reader.read(8192)
        if not data:
            break
        await send.write_all(list(data))
    await send.finish()

async def _forward_quic_to_local(
    recv: iroh.RecvStream,
    writer: asyncio.StreamWriter,
):
    while True:
        try:
            data = await recv.read_to_end(8192)
            if not data:
                break
            writer.write(bytes(data))
            await writer.drain()
        except Exception:
            break
```

---

## Best Practices

### Stream-per-connection mapping
Each TCP connection maps to exactly one QUIC bidi stream. This gives per-connection
flow control, ordering, and independent error handling — all for free from QUIC.

### Buffer size
8192 bytes is a reasonable default for the forwarding buffer. QUIC handles its own
flow control and congestion; you don't need to tune buffer sizes for throughput.

### Shutdown propagation
When TCP sends EOF (read returns 0), call `send.finish()` on the QUIC side.
When QUIC recv returns None/EOF, close the TCP write half.
Use `tokio::select!` or `tokio::join!` to handle both directions — never sequential.

### Error handling
Tunnel streams should not crash the connection. Wrap each stream handler in a
`tokio::spawn` and log errors rather than propagating them.

### Target address protocol
Use a simple length-prefixed string: `[2B big-endian length][UTF-8 "host:port"]`.
This is minimal, unambiguous, and doesn't require protobuf for what is essentially
a raw byte tunnel.

### Security
The tunnel target should be validated on the executor side. At minimum:
- Reject non-localhost targets (unless explicitly configured)
- Rate-limit new tunnel streams
- Log tunnel creation for audit
