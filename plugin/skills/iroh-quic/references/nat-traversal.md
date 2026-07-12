# iroh NAT Traversal & Direct Connect Reference

## Connection Path Selection

iroh connections can use two paths simultaneously:
1. **Relay path** — encrypted traffic relayed through a relay server (always available)
2. **Direct path** — peer-to-peer UDP (established via hole-punching after relay coordination)

iroh automatically attempts hole-punching after connecting via relay. Use `conn.paths()`
to see which paths are active. Direct connect succeeds in ~90% of network configurations.

## How Hole-Punching Works

1. Both peers connect to the same relay server
2. Relay observes each peer's public IP:port (reflective address via STUN)
3. Peers exchange reflective addresses through the relay
4. Both send UDP datagrams to each other's reflective address simultaneously
5. NAT mappings form → direct path established → relay path becomes backup

## When Hole-Punching Fails

- **Symmetric NAT** (port varies per destination) — most common failure cause
- **IPv4/IPv6 mismatch** — one peer IPv4, other IPv6-only → cannot UDP to each other
- **Strict corporate firewalls** blocking outbound UDP
- **Double NAT** (carrier-grade NAT + home router)
- **NAT mapping expired** before punch packets arrive (high-latency relay)

## Diagnosing Connection Type

### Rust CLI (ARL_DEBUG=1)
```bash
ARL_DEBUG=1 arl exec <session> -- echo test
# Output: iroh: bind=55ms connect=876ms total=931ms relay=... direct_addrs=2 paths=[...]
```

Paths output shows:
- `relay:https://.../(rtt=Xms)` — relay path active
- `<ip:port>(rtt=Xms)` — direct path active (hole-punch succeeded)

### Programmatic (Rust)
```rust
let conn = endpoint.connect(addr, ALPN).await?;
// Immediately after connect: typically only relay path
let paths = conn.paths();

// Monitor for direct path establishment:
let mut events = conn.path_events();
while let Some(event) = events.next().await {
    // PathEvent::Opened with a non-relay addr means direct connect succeeded
}
```

### Key Indicators
- `connect` time ~50-100ms → likely direct (same region)
- `connect` time ~200-400ms → relay (same continent)
- `connect` time ~800-1500ms → relay (cross-continent)

## Configuration for Best Results

### Executor (server-side)
```rust
// Use presets::N0 for full NAT traversal support (STUN + PKARR + DNS)
let endpoint = Endpoint::builder(presets::N0)
    .secret_key(key)
    .alpns(vec![ALPN.to_vec()])
    .bind().await?;
endpoint.online().await;

// After online(), addr() includes STUN-discovered reflective addresses
let addr = endpoint.addr();
// addr.ip_addrs() → e.g. ["118.145.192.99:65412", "172.31.250.45:51630"]
```

### Client-side
```rust
// Use presets::Minimal + explicit relay (no PKARR publishing needed for clients)
let endpoint = Endpoint::builder(presets::Minimal)
    .alpns(vec![ALPN.to_vec()])
    .relay_mode(iroh::RelayMode::Default)
    .bind().await?;

// Provide ALL known addresses for fastest connect:
let target = EndpointAddr::new(remote_id)
    .with_relay_url(relay)
    .with_ip_addr("118.145.192.99:65412".parse()?);  // direct address hint
let conn = endpoint.connect(target, ALPN).await?;
```

### Custom Relay (in-cluster, low-latency)
```rust
let relay_url = iroh::RelayUrl::from(url::Url::parse("http://my-relay:3340")?);
let relay_map = iroh::RelayMap::from_iter([relay_url]);
let endpoint = Endpoint::builder(presets::N0)
    .relay_mode(RelayMode::Custom(relay_map))
    .bind().await?;
```

## Relay Configuration Split (Kubernetes)

In K8s, executor pods cannot reach the relay's LoadBalancer external IP (hairpin NAT).
Use two URLs:

| Config | Value | Used by |
|--------|-------|---------|
| `IROH_RELAY_URL` | `http://relay-svc.ns.svc:3340` | Executor (cluster-internal) |
| `IROH_RELAY_EXTERNAL_URL` | `http://<LB-IP>:3340` | Gateway rewrites addr for clients |

Gateway's `rewriteIrohAddr` replaces `relay_url` with the external URL before returning
to clients, while executor uses the internal URL to connect to the relay.

## Deploying iroh-relay

iroh-relay is a stateless relay server. Deploy in-cluster to eliminate cross-region latency.

```yaml
# Minimal Deployment
image: <registry>/arl-iroh-relay:v1.0.2
command: ["iroh-relay", "--dev"]  # HTTP mode, no TLS (in-cluster)
port: 3340
```

- `--dev` mode: HTTP (no TLS), suitable for in-cluster where TLS terminates at LB
- Stateless: no persistence needed, simple horizontal scaling
- Expose via LoadBalancer for external clients

## Performance Characteristics

| Scenario | Connect | Throughput |
|----------|---------|------------|
| Same cluster (direct via pod IP) | 5ms | Wire speed |
| Same region (relay, in-cluster) | 5-10ms | Wire speed |
| Same region (direct, hole-punch success) | 10-50ms | Wire speed |
| Cross-region (relay) | 100-400ms | Limited by relay bandwidth |
| Cross-continent (n0 public relay) | 800-1500ms | Rate-limited |

## Troubleshooting

1. **"iroh connect timeout"** → relay unreachable or ALPN mismatch
2. **Only relay path, no direct** → check IPv4/IPv6 compatibility, NAT type
3. **Slow transfers** → check relay location vs peers (use regional relay)
4. **executor has no iroh-addr file** → relay URL unreachable from pod (hairpin NAT)
5. **`direct_addresses: []`** → STUN hasn't completed yet (call `online().await` + wait)
