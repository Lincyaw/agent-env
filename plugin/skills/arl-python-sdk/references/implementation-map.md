# ARL Python SDK Implementation Map

## Public Surface

| Surface | Purpose | Main files |
| --- | --- | --- |
| `SandboxSession` | High-level session lifecycle, command execution, files, history, replay, tools, trajectory export | `sdk/python/arl/arl/session.py`, `sdk/python/arl/arl/gateway_client.py` |
| `ManagedSession` | Experiment-scoped sessions with server-managed pool creation and cleanup metadata | `sdk/python/arl/arl/session.py`, `pkg/gateway/managed_pool.go`, `pkg/gateway/gateway.go` |
| `WarmPoolManager` | Explicit warm pool create/list/wait/scale/drain/destroy/logs | `sdk/python/arl/arl/warmpool.py`, `sdk/python/arl/arl/gateway_client.py`, `pkg/gateway/gateway.go` |
| `GatewayClient` | Low-level typed HTTP client used by the higher-level classes | `sdk/python/arl/arl/gateway_client.py`, `pkg/gateway/router.go` |
| `InteractiveShellClient` | WebSocket shell / PTY access to a session | `sdk/python/arl/arl/interactive_shell_client.py`, `pkg/gateway/ws_shell.go` |
| Pydantic types | SDK response/request models and validation | `sdk/python/arl/arl/types.py` |
| Auth helpers | API key auth and SSO/OIDC auth plumbing | `sdk/python/arl/arl/auth.py`, `pkg/gateway/auth.go` |
| Config/tools models | Structured config env, secret/configmap injection, tool specs | `sdk/python/arl/arl/configenv.py`, `sdk/python/arl/arl/types.py` |
| Package exports | What `from arl import ...` exposes | `sdk/python/arl/arl/__init__.py` |

## Feature Map

| Feature | Behavior | Files to inspect |
| --- | --- | --- |
| Session creation | Create or attach to sandbox-backed sessions through gateway; image-backed sessions can trigger managed pool creation server-side | `sdk/python/arl/arl/session.py`, `sdk/python/arl/arl/gateway_client.py`, `pkg/gateway/gateway.go`, `pkg/gateway/pool_policy.go` |
| Warm pool selection | Gateway selects a matching scoped pool by profile/image, queues requests when capacity is empty, and scales the selected WarmPool up | `pkg/gateway/pool_policy.go`, `pkg/gateway/sandbox_claim_runtime_allocator.go`, `pkg/gateway/pool_autoscaler.go` |
| Managed experiments | Group sessions by `experiment_id`; gateway ensures a stable image/profile pool and records experiment metadata | `sdk/python/arl/arl/session.py`, `pkg/gateway/managed_pool.go`, `pkg/gateway/gateway.go` |
| Command execution | Execute one or more steps in the sidecar; non-streaming calls use operation IDs for timeout recovery | `sdk/python/arl/arl/session.py`, `sdk/python/arl/arl/gateway_client.py`, `pkg/gateway/gateway.go`, `pkg/gateway/operations.go` |
| Streaming output | SDK switches to SSE when an output callback is provided | `sdk/python/arl/arl/gateway_client.py`, `pkg/gateway/router.go`, `pkg/gateway/gateway.go` |
| File transfer | Upload/download workspace files, including streaming download and SHA256 verification | `sdk/python/arl/arl/session.py`, `sdk/python/arl/arl/gateway_client.py`, `pkg/gateway/file_upload.go` |
| History and snapshots | Each executed step is recorded with snapshot metadata for restore/replay/trajectory | `pkg/gateway/history.go`, `pkg/gateway/gateway.go`, `sdk/python/arl/arl/session.py` |
| Restore and replay | Restore rebuilds a session from recorded steps; replay copies source-session steps into a target session | `pkg/gateway/gateway.go`, `pkg/gateway/history.go`, `sdk/python/arl/arl/session.py` |
| Trajectory export | Export step history as JSONL for RL/SFT-style consumers; gateway can also persist trajectories to ClickHouse | `pkg/gateway/history.go`, `pkg/gateway/gateway.go`, `pkg/audit`, `sdk/python/arl/arl/session.py` |
| Logs | Session logs stream from one sidecar; pool logs fan out across pool pods | `sdk/python/arl/arl/gateway_client.py`, `sdk/python/arl/arl/warmpool.py`, `pkg/gateway/gateway.go`, `pkg/gateway/router.go` |
| Interactive shell | WebSocket PTY protocol for terminal input/output, resize, and signals | `sdk/python/arl/arl/interactive_shell_client.py`, `pkg/gateway/ws_shell.go` |
| Tools | SDK exposes tool registry and tool invocation helpers for images that include an ARL tool registry | `sdk/python/arl/arl/session.py`, `sdk/python/arl/arl/types.py` |
| Fault tolerance | Client timeout recovery, gateway session recovery, runtime-lost tombstones, and pool-claim cleanup live mostly server-side | `sdk/python/arl/arl/gateway_client.py`, `pkg/gateway/operations.go`, `pkg/gateway/session_store.go`, `pkg/gateway/redis_store.go`, `pkg/gateway/memory_store.go`, `pkg/gateway/gateway.go` |

## Runtime Model

- SDK code talks to gateway only; gateway owns Kubernetes interaction.
- Gateway creates `SandboxClaim` objects through the runtime allocator.
- `SandboxClaim` binds a session to the actual sandbox/pod; gateway validates
  this binding before operations that need the pod.
- Session state can be in memory or Redis. Redis-backed deployments keep
  history/tombstones and support recovery after gateway restart.
- Warm pools are `SandboxWarmPool + SandboxTemplate`; explicit pool lifecycle
  is exposed through both SDK and CLI.

## Repository Index

| Area | Files |
| --- | --- |
| SDK package metadata | `sdk/python/arl/pyproject.toml`, `sdk/python/arl/CHANGELOG.md`, `sdk/python/arl/README.md` |
| SDK source | `sdk/python/arl/arl/*.py` |
| SDK tests | `sdk/python/arl/tests/` |
| Python examples/smoke tests | `examples/python/` |
| Gateway routes and types | `pkg/gateway/router.go`, `pkg/gateway/types.go` |
| Session lifecycle and execution | `pkg/gateway/gateway.go`, `pkg/gateway/operations.go`, `pkg/gateway/history.go` |
| Runtime allocation | `pkg/gateway/runtime_allocator.go`, `pkg/gateway/sandbox_claim_runtime_allocator.go`, `pkg/gateway/sandbox_template_builder.go` |
| Pool policy and autoscaling | `pkg/gateway/pool_policy.go`, `pkg/gateway/pool_autoscaler.go`, `pkg/gateway/managed_pool.go` |
| Session persistence and recovery | `pkg/gateway/session_store.go`, `pkg/gateway/memory_store.go`, `pkg/gateway/redis_store.go` |
| Gateway auth/rate limits | `pkg/gateway/auth.go`, `pkg/gateway/ratelimit.go` |
| Helm/runtime configuration | `charts/agent-env/values.yaml`, `charts/agent-env/templates/gateway-deployment.yaml` |
| CLI parity reference | `cmd/arl/`, `plugin/skills/arl-cli/SKILL.md` |
