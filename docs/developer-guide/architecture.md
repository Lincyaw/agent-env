# Architecture

`agent-env` is the upper platform layer. `agent-sandbox` is the Kubernetes
resource lifecycle layer.

```text
Client / Python SDK
  -> agent-env gateway
  -> SandboxTemplate / SandboxWarmPool / SandboxClaim
  -> agent-sandbox-controller
  -> Sandbox / Pod
  -> sidecar gRPC
  -> executor-agent Unix socket
```

## Gateway

The gateway owns user-facing behavior:

- session lifecycle;
- execution API;
- file upload/download;
- restore and replay;
- managed sessions;
- Redis-backed session persistence;
- ClickHouse trajectory writes;
- Prometheus metrics.

It should not directly own Kubernetes Pod lifecycle. It creates and deletes
agent-sandbox CRDs and reads their status.

## agent-sandbox

`agent-sandbox-controller` owns Kubernetes resources:

- `SandboxTemplate`
- `SandboxWarmPool`
- `SandboxClaim`
- `Sandbox`
- Pods, PVCs, Services, and cleanup

The gateway and controller communicate through Kubernetes API state, not direct
RPC.

## Data Plane

Each sandbox pod contains:

- executor container: the user image;
- sidecar container: gRPC API exposed inside the pod;
- executor-agent binary copied into the executor container and reached over a
  Unix socket.

## Observability

Gateway metrics are exposed from the internal gateway port on `/metrics`.
ClickHouse stores trajectory data when enabled. Redis stores session
state when enabled.
