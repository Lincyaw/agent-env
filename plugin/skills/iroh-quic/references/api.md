# iroh API Reference (v1.0+)

## Table of Contents

1. [Endpoint](#endpoint)
2. [Connection](#connection)
3. [Streams](#streams)
4. [Address Types](#address-types)
5. [Presets and Configuration](#presets-and-configuration)
6. [Router and ProtocolHandler](#router-and-protocolhandler)
7. [Relay and NAT Traversal](#relay-and-nat-traversal)
8. [Address Discovery](#address-discovery)
9. [Endpoint Hooks](#endpoint-hooks)
10. [Python Bindings](#python-bindings)
11. [Connection Lifecycle](#connection-lifecycle)
12. [QUIC Interaction Patterns](#quic-interaction-patterns)

---

## Endpoint

The core building block. Each Endpoint has an Ed25519 identity (SecretKey → EndpointId).

### Creating

```rust
use iroh::{Endpoint, SecretKey, endpoint::presets};

// Production: full N0 infrastructure
let ep = Endpoint::builder(presets::N0)
    .secret_key(secret_key)
    .alpns(vec![b"my/proto/v1".to_vec()])
    .bind().await?;

// MUST wait for relay + PKARR publication before sharing address
ep.online().await;

// Simple (random identity each time — NOT for production)
let ep = Endpoint::bind(presets::N0).await?;
```

### Builder Methods

| Method | Description |
|--------|-------------|
| `.secret_key(SecretKey)` | Set identity. Random if omitted. |
| `.alpns(Vec<Vec<u8>>)` | Set supported ALPN protocols. |
| `.relay_mode(RelayMode)` | Disabled, Default, Staging, Custom(RelayMap) |
| `.clear_ip_transports()` | Force relay-only (no direct IP) |
| `.ca_tls_config(CaTlsConfig)` | TLS CA config; `insecure_skip_verify()` for testing |
| `.bind()` | Finalize and create (async) |

### Key Methods

| Method | Returns | Description |
|--------|---------|-------------|
| `id()` | `EndpointId` | Local public key |
| `addr()` | `EndpointAddr` | Local address (sync) |
| `secret_key()` | `&SecretKey` | Local secret key |
| `online().await` | `()` | Wait for relay + PKARR publication |
| `close().await` | `()` | Graceful shutdown (ALWAYS await) |
| `accept().await` | `Option<Incoming>` | Accept next connection |
| `connect(addr, alpn).await` | `Connection` | Connect to remote peer |

### Connecting

```rust
// By EndpointId (N0 resolves via DNS — preferred)
let conn = ep.connect(remote_endpoint_id, b"my/alpn").await?;

// By full EndpointAddr
let conn = ep.connect(remote_addr, b"my/alpn").await?;
```

### Accepting (manual loop)

```rust
loop {
    let Some(incoming) = ep.accept().await else { break };
    let accepting = incoming.accept()?;
    let conn = accepting.await?;
    tokio::spawn(async move { handle(conn).await });
}
```

---

## Connection

Represents an active QUIC connection to a remote peer.

### Stream Methods

| Method | Returns | Description |
|--------|---------|-------------|
| `open_bi().await` | `(SendStream, RecvStream)` | Open bidi stream |
| `accept_bi().await` | `Option<(SendStream, RecvStream)>` | Accept bidi stream |
| `open_uni().await` | `SendStream` | Open unidirectional stream |
| `accept_uni().await` | `Option<RecvStream>` | Accept uni stream |

### Other Methods

| Method | Description |
|--------|-------------|
| `close(code, reason)` | Queue close (sync, NOT async) |
| `closed().await` | Wait for connection to close |
| `remote_id()` | Remote peer's EndpointId |
| `send_datagram(data)` | Unreliable datagram |
| `read_datagram().await` | Receive datagram |
| `set_max_concurrent_uni_streams(n)` | Flow control |
| `set_receive_window(n)` | Flow control |

---

## Streams

### SendStream

| Method | Description |
|--------|-------------|
| `write_all(buf).await` | Write all bytes |
| `write(buf).await` | Write some bytes, returns count |
| `finish()` | Signal end-of-stream (sync, just queues FIN) |
| `reset(error_code)` | Abort stream |
| `set_priority(n)` | Set priority |

### RecvStream

| Method | Description |
|--------|-------------|
| `read(buf).await` | Read available bytes, `Ok(None)` = EOF |
| `read_exact(buf).await` | Read exactly N bytes |
| `read_to_end(max).await` | Read all until FIN |
| `read_chunk(max).await` | Read a chunk |
| `stop(code)` | Tell sender to stop |

### Key Rules

1. **Send first on `open_bi()`** — the accepting side cannot see a stream until data is sent by the opener.
2. **`finish()` only signals intent** — it does not wait for delivery. Use `connection.closed().await` on the accepting side for delivery confirmation.
3. **Streams are cheap** — no round-trip to open (unlike connections). One stream per request is idiomatic.

### Framing with LengthDelimitedCodec

For discrete messages over a continuous byte stream:

```rust
use tokio_util::codec::LengthDelimitedCodec;

// Sender
let mut framed = LengthDelimitedCodec::builder().new_write(send);
framed.send(msg_bytes).await?;
framed.get_mut().finish()?;

// Receiver
let mut framed = LengthDelimitedCodec::builder().new_read(recv);
while let Some(msg) = framed.try_next().await? {
    // handle msg
}
```

---

## Address Types

### EndpointId

Type alias for Ed25519 `PublicKey`. The stable peer identifier.

```rust
let id = endpoint.id();
let id_hex = id.to_string();                    // hex string
let id = EndpointId::from_str(&hex_str)?;       // parse hex
```

### EndpointAddr

Combines EndpointId with optional transport addresses.

```rust
pub struct EndpointAddr {
    pub id: EndpointId,
    pub addrs: BTreeSet<TransportAddr>,   // relay URLs + direct IPs
}

// Convert EndpointId → EndpointAddr (for N0 DNS-resolved connections)
let addr = EndpointAddr::from(endpoint_id);
```

### EndpointTicket

Serializable format for sharing connection info.

```rust
let ticket = EndpointTicket::from_addr(ep.addr());
let ticket_str = ticket.to_string();            // starts with "endpoint"
let ticket = EndpointTicket::from_string(&s)?;
let addr = ticket.endpoint_addr();
```

---

## Presets and Configuration

### presets::N0

Production defaults from n0.computer:

- **Relay**: n0 global relays (NA-east, NA-west, EU, APAC)
- **PKARR publisher**: publishes endpoint addresses every 5 min
- **DNS lookup**: resolves `_iroh.<z32-id>.dns.iroh.link` TXT records
- **NAT traversal**: automatic holepunching via relay-observed addresses
- **PKARR default**: relay-only addresses to prevent IP leakage

### presets::Minimal

Crypto provider only. No relay, no discovery, no PKARR.

### Combining presets with overrides

```rust
// Minimal + relay for in-process tests
let ep = Endpoint::builder(presets::Minimal)
    .secret_key(key)
    .alpns(vec![ALPN.to_vec()])
    .relay_mode(RelayMode::Default)
    .bind().await?;
```

### RelayMode

| Variant | Description |
|---------|-------------|
| `Disabled` | No relay |
| `Default` | n0 global relays |
| `Staging` | n0 staging relays |
| `Custom(RelayMap)` | Custom relay servers |

---

## Router and ProtocolHandler

The Router dispatches incoming connections by ALPN to registered handlers.

```rust
use iroh::protocol::{Router, ProtocolHandler};

let router = Router::builder(endpoint)
    .accept(b"my/echo/v1", EchoHandler)
    .accept(b"my/tunnel/v1", TunnelHandler)
    .spawn();   // starts accept loop in background
```

### ProtocolHandler trait

```rust
impl ProtocolHandler for Echo {
    async fn accept(&self, connection: Connection) -> Result<(), AcceptError> {
        let (mut send, mut recv) = connection.accept_bi().await?;
        tokio::io::copy(&mut recv, &mut send).await?;
        send.finish()?;
        connection.closed().await;
        Ok(())
    }
}
```

Optional methods:
- `on_accepting(accepting: Accepting) -> Result<Connection>` — early interception during handshake
- `shutdown()` — graceful termination

### Without Router (manual accept)

```rust
let ep = Endpoint::builder(presets::N0)
    .alpns(vec![ALPN.to_vec()])
    .bind().await?;

while let Some(incoming) = ep.accept().await {
    tokio::spawn(async move {
        let conn = incoming.accept()?.await?;
        EchoHandler.accept(conn).await
    });
}
```

---

## Relay and NAT Traversal

### How NAT traversal works

1. Both peers connect to a shared relay server
2. Relay observes reflective addresses (public IP + port)
3. Peers exchange addresses through relay
4. Simultaneous UDP datagrams punch holes in firewalls
5. If holepunching fails (~10% of cases), traffic routes through relay

### Key facts

- Relays are **stateless** and horizontally scalable
- Traffic is **end-to-end encrypted** — relays cannot read content
- Each relay supports ~60,000 concurrent connections
- Minimum 2 relays in distinct regions recommended for production
- Self-host from `github.com/n0-computer/iroh/tree/main/iroh-relay`

### Path selection (BiasedRttPathSelector)

- Primary: IPv4, IPv6, custom transports
- Backup: Relay
- IPv6 gets 3ms RTT advantage over IPv4
- Switching from relay to direct is **immediate**
- Periodic upgrade attempts every 60s if latency > 10ms

### Custom relays

```rust
use iroh::endpoint::RelayMode;

// Rust
let ep = Endpoint::builder(presets::N0)
    .relay_mode(RelayMode::Custom(RelayMap::from_iter([url1, url2])))
    .bind().await?;
```

```python
# Python
relay_mode = iroh.RelayMode.custom_from_urls(["URL_1", "URL_2"])
ep = await iroh.Endpoint.bind(iroh.EndpointOptions(
    preset=iroh.preset_n0(),
    relay_mode=relay_mode,
))
```

---

## Address Discovery

### DNS/PKARR (default with N0)

Query format: `_iroh.<z32-endpoint-id>.<origin-domain> TXT`

TXT record attributes:
- `relay=<url>` — home relay URL
- `addr=<addr> <addr>` — socket addresses

### Custom DNS server

```rust
use iroh::address_lookup::{DnsAddressLookup, PkarrPublisher};

let ep = Endpoint::builder(presets::Minimal)
    .address_lookup(PkarrPublisher::builder(pkarr_relay_url))
    .address_lookup(DnsAddressLookup::builder(origin_domain))
    .bind().await?;
```

### Optional: DHT (Mainline)

```rust
use iroh_mainline_address_lookup::DhtAddressLookup;
let ep = Endpoint::builder(presets::N0)
    .address_lookup(DhtAddressLookup::builder())
    .bind().await?;
```

### Optional: mDNS (local network)

```rust
use iroh_mdns_address_lookup::MdnsAddressLookup;
let ep = Endpoint::builder(presets::N0)
    .address_lookup(MdnsAddressLookup::builder())
    .bind().await?;
```

---

## Endpoint Hooks

Intercept connections for observation or rejection:

```rust
impl EndpointHooks for MyHooks {
    async fn before_connect(&self, addr: &EndpointAddr, alpn: &[u8]) -> BeforeConnectOutcome {
        BeforeConnectOutcome::Accept
    }
    async fn after_handshake(&self, conn: &Connection) -> AfterHandshakeOutcome {
        AfterHandshakeOutcome::Accept
    }
}

let ep = Endpoint::builder(presets::N0)
    .hooks(MyHooks)
    .bind().await?;
```

**Never store an Endpoint inside a hook** — causes reference-counting cycles.
Use `conn.weak_handle()` instead of cloning connections in hooks.

---

## Python Bindings

Install: `pip install iroh`

### Critical first line

```python
async def main():
    iroh.iroh_ffi.uniffi_set_event_loop(asyncio.get_running_loop())
    # ... all iroh calls go here
```

### Endpoint

```python
ep = await iroh.Endpoint.bind(iroh.EndpointOptions(
    preset=iroh.preset_n0(),    # or preset_minimal()
    alpns=[list(ALPN)],         # ALPN as list of ints
))

ep.id()              # EndpointId
ep.addr()            # EndpointAddr
ep.secret_key()      # SecretKey
await ep.close()     # ALWAYS await
```

### Connecting

```python
remote_id = iroh.EndpointId.from_string(hex_str)
remote_addr = iroh.EndpointAddr(id=remote_id, relay_url=None, addresses=[])
conn = await ep.connect(remote_addr, list(ALPN))
```

### Accepting

```python
incoming = await ep.accept_next()
accepting = await incoming.accept()
conn = await accepting.connect()
```

### Connection

```python
conn.remote_id()           # EndpointId
conn.alpn()                # bytes
conn.close(code, reason)   # immediate close
await conn.closed()        # wait for close
conn.send_datagram(data)   # unreliable
await conn.read_datagram() # receive
```

### Bidirectional Streams

```python
bi = await conn.open_bi()     # initiator
bi = await conn.accept_bi()   # acceptor
send = bi.send()              # SendStream
recv = bi.recv()              # RecvStream
```

### SendStream / RecvStream

```python
await send.write_all(list(b"hello"))  # list of ints, NOT bytes
await send.finish()

data = await recv.read_to_end(max_size)  # bytes
data = await recv.read_exact(n)          # exactly n bytes
```

### Tickets

```python
ticket = iroh.EndpointTicket.from_addr(ep.addr())
ticket_str = str(ticket)                         # "endpoint..."
ticket = iroh.EndpointTicket.from_string(s)
addr = ticket.endpoint_addr()
```

### EndpointBuilder (alternative)

```python
builder = iroh.EndpointBuilder()
builder.apply_minimal()
builder.alpns([list(ALPN)])
ep = await builder.bind()   # builder is consumed
```

### RelayMode

```python
iroh.RelayMode.disabled()
iroh.RelayMode.custom_from_urls(["URL_1", "URL_2"])
```

### Complete Python Example

```python
import asyncio, iroh

ALPN = b"my/echo/v1"

async def receiver():
    iroh.iroh_ffi.uniffi_set_event_loop(asyncio.get_running_loop())
    ep = await iroh.Endpoint.bind(iroh.EndpointOptions(alpns=[list(ALPN)]))
    print("ticket:", str(iroh.EndpointTicket.from_addr(ep.addr())))

    incoming = await ep.accept_next()
    conn = await (await incoming.accept()).connect()
    bi = await conn.accept_bi()
    msg = await bi.recv().read_to_end(1024)
    await bi.send().write_all(msg)
    await bi.send().finish()
    await ep.close()

async def sender(ticket_str):
    iroh.iroh_ffi.uniffi_set_event_loop(asyncio.get_running_loop())
    ep = await iroh.Endpoint.bind(iroh.EndpointOptions())
    addr = iroh.EndpointTicket.from_string(ticket_str).endpoint_addr()
    conn = await ep.connect(addr, list(ALPN))

    bi = await conn.open_bi()
    await bi.send().write_all(list(b"hello"))
    await bi.send().finish()
    data = await bi.recv().read_to_end(1024)
    print("received:", bytes(data))
    await ep.close()
```

---

## Connection Lifecycle

### Graceful shutdown sequence

```
Sender side:
  send.finish()                  # Signal no more data
  conn.close(0, b"bye")         # Queue close
  endpoint.close().await         # Ensure close is sent

Receiver side:
  // read until EOF
  connection.closed().await      # Wait for remote close
```

### Rules

1. `conn.close()` only **queues** the close (sync). Must `await endpoint.close()` to actually send it.
2. The side that last **receives** data should call `connection.close()`.
3. The side that last **sends** data should call `send.finish()` then `connection.closed().await`.

---

## QUIC Interaction Patterns

The 7 canonical patterns from iroh docs:

### 1. Request/Response
```rust
let (mut send, recv) = conn.open_bi().await?;
send.write_all(&request).await?;
send.finish()?;
let response = recv.read_to_end(MAX).await?;
```

### 2. Full Duplex Streaming
```rust
let (send, recv) = conn.open_bi().await?;
tokio::spawn(async move { /* read from recv */ });
// write to send concurrently
```

### 3. Multiplexed Requests (HTTP/3 style)
```rust
loop {
    let Some((send, recv)) = conn.accept_bi().await? else { break };
    tokio::spawn(async move { handle_request(send, recv).await });
}
```

### 4. Ordered Notifications (framed)
Use `LengthDelimitedCodec` over unidirectional stream.

### 5. Request → Multiple Responses
`open_bi()`, send request, read framed responses from recv.

### 6. Unordered Responses
One `open_uni()` for request, multiple `accept_uni()` for responses.

### 7. Graceful Connection Handling
```rust
futures_lite::future::race(run_protocol(conn.clone()), conn.closed()).await;
```
