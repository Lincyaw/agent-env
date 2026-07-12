---
name: iroh-quic
description: >
  Reference for using the iroh P2P QUIC library (iroh 1.0+) by n0-computer in Rust and Python.
  Use this skill whenever writing or modifying code that touches iroh — creating Endpoints,
  connecting peers, opening QUIC streams, implementing protocol handlers, TCP tunneling,
  or using the Python iroh bindings. Also use when debugging iroh connection issues, NAT
  traversal problems, or relay connectivity. Triggers on: iroh, QUIC, EndpointId, iroh
  transport, iroh tunnel, iroh relay, iroh connect, direct-connect, P2P QUIC, iroh Python,
  dumbpipe, iroh protocol handler, ALPN.
---

# iroh P2P QUIC Library Reference

iroh is a P2P networking library built on QUIC. It provides encrypted connections between
peers identified by Ed25519 public keys (EndpointId), with automatic NAT traversal via
relay servers and address resolution via DNS/PKARR.

**Before writing any iroh code, read `references/api.md` for the full API reference.**

For tunneling/port-forwarding patterns, read `references/tunneling.md`.

For NAT traversal, relay deployment, and diagnosing direct-connect issues, read `references/nat-traversal.md`.

## Critical Rules

These are the most common mistakes. Violating any of these will cause silent failures:

1. **Always call `endpoint.online().await` before sharing the address** — without it,
   the relay isn't connected and PKARR hasn't published. Remote peers cannot find you.

2. **Python: call `iroh.iroh_ffi.uniffi_set_event_loop(asyncio.get_running_loop())`
   as the FIRST line** in any async function before any iroh call. Without it, Rust
   callbacks cannot reach the Python event loop.

3. **Always `await endpoint.close()`** before dropping — without it, queued close
   frames are never sent, causing 30-second timeouts on the remote side.

4. **Send first on `open_bi()`** — the accepting side cannot see a stream until
   the opening side has sent data on it.

5. **One Endpoint per application** — do not create multiple endpoints. One endpoint
   manages all connections.

6. **Persist SecretKey for stable identity** — if you generate a new key each restart,
   the EndpointId changes and peers can't find you via cached IDs.

7. **With `presets::N0`, only EndpointId is needed to connect** — addresses are
   published via PKARR and resolved via DNS automatically. Do not pass IP addresses
   or construct full EndpointAddr manually when using N0.

8. **Streams are cheap** — one stream per request is idiomatic. Do not multiplex
   multiple logical operations on a single stream.

## Presets

| Preset | Provides | Use case |
|--------|----------|----------|
| `presets::N0` | Relay + DNS/PKARR + NAT traversal | Production |
| `presets::Minimal` | Crypto only | Tests (add `RelayMode::Default` for in-process tests) |

## Quick Reference

### Rust — Create and connect
```rust
use iroh::{Endpoint, SecretKey, endpoint::presets};

let endpoint = Endpoint::builder(presets::N0)
    .secret_key(my_key)
    .alpns(vec![b"my/proto/v1".to_vec()])
    .bind().await?;
endpoint.online().await;

// Connect by EndpointId (N0 resolves via DNS)
let conn = endpoint.connect(remote_id, b"my/proto/v1").await?;
let (send, recv) = conn.open_bi().await?;
```

### Python — Create and connect
```python
import asyncio, iroh

async def main():
    iroh.iroh_ffi.uniffi_set_event_loop(asyncio.get_running_loop())
    ep = await iroh.Endpoint.bind(iroh.EndpointOptions(
        preset=iroh.preset_n0(),
        alpns=[list(ALPN)],
    ))
    remote_id = iroh.EndpointId.from_string(hex_str)
    remote_addr = iroh.EndpointAddr(id=remote_id, relay_url=None, addresses=[])
    conn = await ep.connect(remote_addr, list(ALPN))
    bi = await conn.open_bi()
    await bi.send().write_all(list(b"hello"))
    resp = await bi.recv().read_to_end(65536)
    await ep.close()
```

### Router pattern (serve multiple protocols)
```rust
use iroh::protocol::{Router, ProtocolHandler};

let router = Router::builder(endpoint)
    .accept(b"my/echo/v1", EchoHandler)
    .accept(b"my/tunnel/v1", TunnelHandler)
    .spawn();
```
