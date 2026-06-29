# agent-env

`agent-env` is the gateway and SDK layer for running code in Kubernetes
sandboxes managed by `sigs.k8s.io/agent-sandbox`.

The split is:

- `agent-env` owns the public API, Python SDK, sessions, execution, file
  operations, replay, Redis session state, ClickHouse trajectory storage, and
  Prometheus metrics.
- `agent-sandbox` owns Kubernetes resource lifecycle for
  `SandboxTemplate`, `SandboxWarmPool`, `SandboxClaim`, `Sandbox`, and the
  underlying Pods.

## Quick Links

| Task | Page |
| --- | --- |
| Understand the architecture | [Architecture](developer-guide/architecture.md) |
| Deploy the stack | [Deployment](developer-guide/deployment.md) |
| Use the Python SDK | [Python SDK](user-guide/python-sdk.md) |
| Inspect APIs | [API Reference](user-guide/api-reference.md) |
| Configure session persistence | [Session State](developer-guide/session-state.md) |

## Minimal Flow

```text
Python SDK / REST client
  -> agent-env gateway
  -> SandboxTemplate / SandboxWarmPool / SandboxClaim
  -> agent-sandbox-controller
  -> Sandbox / Pod
  -> sidecar gRPC
  -> executor-agent Unix socket
```
