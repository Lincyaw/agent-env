# Session State Management (Redis)

The Gateway uses a `SessionStore` interface to abstract session persistence, allowing deployment in single-replica (in-memory) or multi-replica HA (Redis) configurations.

## Architecture

```
                    ┌──────────────────────────────┐
                    │         SessionStore          │
                    │         (interface)            │
                    └──────────┬───────────────────┘
                               │
                ┌──────────────┴──────────────┐
                │                              │
        ┌───────┴───────┐           ┌──────────┴──────────┐
        │  MemoryStore   │           │    RedisStore        │
        │  (sync.Map)    │           │  (cache + Redis)     │
        └───────────────┘           └─────────────────────┘
         Default, single             HA, multi-replica
         replica                     deployments
```

## SessionStore Interface

```go
type SessionStore interface {
    Get(sessionID string) (*session, bool)
    Set(sessionID string, s *session)
    Delete(sessionID string)
    Range(fn func(sessionID string, s *session) bool)
    Count() int64
    IncrCount(delta int64) int64
    Close() error
}
```

All Gateway code accesses sessions through this interface. The implementation is selected at startup based on configuration.

## MemoryStore (Default)

- Wraps `sync.Map` + `atomic.Int64`
- Zero external dependencies
- Session recovery on restart via pod annotations (`arl.infra.io/session`, `arl.infra.io/last-activity`)
- Suitable for single-replica deployments

## RedisStore

- **Write-through cache**: Local `sync.Map` for hot-path reads (avoids Redis round-trip on every `GetSession` / `ExecuteSteps`), with writes persisted to Redis
- Sessions serialized as JSON with configurable TTL (default: 2 hours)
- Session count tracked via Redis `INCR`/`DECRBY` on `arl:session_count` key
- `Sync(sessionID)` method for explicit persistence after in-memory mutations

### Redis Key Schema

| Key Pattern | Type | Description |
|------------|------|-------------|
| `arl:session:<id>` | String (JSON) | Session data including info, history, config |
| `arl:session_count` | Integer | Global active session count |

### Session Data Format

```json
{
  "info": {
    "id": "gw-1710000000000-abcd1234",
    "sandboxName": "gw-1710000000000-abcd1234",
    "namespace": "default",
    "poolRef": "my-pool",
    "podIP": "10.0.0.5",
    "podName": "my-pool-abc123",
    "createdAt": "2024-03-10T10:00:00Z"
  },
  "managed": true,
  "experimentId": "swe-bench-42",
  "lastTaskTime": "2024-03-10T10:05:00Z",
  "idleTimeout": 600000000000,
  "maxLifetime": 3600000000000,
  "createdAt": "2024-03-10T10:00:00Z",
  "historyRecords": [...],
  "historyNextIndex": 5
}
```

## Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `REDIS_ENABLED` | `false` | Enable Redis session store |
| `REDIS_ADDR` | `localhost:6379` | Redis server address |
| `REDIS_PASSWORD` | (empty) | Redis password |
| `REDIS_DB` | `0` | Redis database number |

### Helm Values

```yaml
redis:
  enabled: true
  addr: "redis:6379"
  password: "your-redis-password"  # Stored in K8s Secret
  db: 0
```

## Deployment Patterns

### Single Replica (Default)

```yaml
gateway:
  replicaCount: 1
# redis: not needed
```

Session state is in-memory. On gateway restart, sessions are recovered from pod annotations.

### Multi-Replica HA

```yaml
gateway:
  replicaCount: 3

redis:
  enabled: true
  addr: "redis-master.redis.svc:6379"
  password: "redis-password"
```

All replicas share session state via Redis. Any replica can serve any session. Pod annotation recovery acts as a fallback if Redis is unavailable.

### Redis Deployment

ARL does not include Redis in its Helm chart. Use an external Redis instance or deploy one separately:

```bash
# Using Bitnami Redis chart
helm repo add bitnami https://charts.bitnami.com/bitnami
helm install redis bitnami/redis \
  -n arl-system \
  --set auth.password=your-redis-password \
  --set architecture=standalone
```

## Design Decisions

### Write-Through Cache

The RedisStore maintains a local `sync.Map` cache to avoid Redis round-trips on every request. This is critical for the `ExecuteSteps` hot path where session lookup + history append happens per step.

**Trade-offs:**
- Reads are fast (local cache hit)
- Writes go to both cache and Redis (eventual consistency across replicas)
- A session created on replica A is immediately available locally, and available on replica B after Redis propagation (typically < 1ms)

### TTL vs Explicit Cleanup

Session keys in Redis have a TTL (default: 2 hours). This acts as a safety net — if a gateway crashes without cleaning up, sessions expire automatically. Normal session deletion still explicitly removes the Redis key.

### Why Not etcd?

Redis was chosen over etcd for session state because:
- Higher throughput for read-heavy workloads (session lookups)
- Simpler operational model (don't need to worry about etcd cluster health affecting K8s)
- Native TTL support for automatic cleanup
- Lower latency for small value reads

## Implementation Files

| File | Description |
|------|-------------|
| `pkg/gateway/session_store.go` | `SessionStore` interface definition |
| `pkg/gateway/memory_store.go` | In-memory implementation (sync.Map) |
| `pkg/gateway/redis_store.go` | Redis implementation (go-redis/v9) |
