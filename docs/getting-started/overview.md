# Overview

`agent-env` provides a user-facing session API on top of
`agent-sandbox` resources.

## Responsibilities

`agent-env`:

- exposes REST APIs and the Python SDK;
- manages session metadata and lifecycle;
- persists session state in Redis when enabled;
- writes trajectory data to ClickHouse when enabled;
- exposes gateway Prometheus metrics;
- creates `SandboxWarmPool` and `SandboxClaim` resources.

`agent-sandbox`:

- reconciles `SandboxTemplate`, `SandboxWarmPool`, `SandboxClaim`, and
  `Sandbox`;
- keeps warm sandboxes ready;
- adopts sandboxes for claims;
- manages Kubernetes Pods and related resources.

## Session Lifecycle

1. The client creates a session with an `image` and optional `profile`.
2. The gateway selects or creates a sandbox-backed pool.
3. The gateway creates a `SandboxClaim`.
4. `agent-sandbox-controller` binds the claim to a ready sandbox and updates
   status.
5. The gateway connects to the sandbox sidecar and executes commands.
6. Deleting the session deletes the claim; `agent-sandbox` handles cleanup.

## Key Resources

Use the agent-sandbox CRDs to inspect runtime state:

```bash
kubectl get sandboxwarmpools,sandboxclaims,sandboxes -A
```
