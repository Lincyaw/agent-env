# Changelog

## [Unreleased]

## [0.18.0] - 2026-07-03

### Added
- Add explicit WarmPool destroy support through `arl pool destroy`, the gateway
  `/v1/pools/{name}/destroy` endpoint, and Python SDK helpers.
- Report pool lifecycle state in CLI and SDK pool responses.

### Changed
- Route all session allocation through a matching WarmPool. When capacity is
  empty, the gateway queues the request, scales the selected WarmPool up, and
  waits for ready capacity before assigning a claim.
- Change WarmPool delete semantics to drain and stop: active sessions and
  pool-bound claims are removed, then the WarmPool is scaled to zero and kept
  for later scale-up.
- Stop managed pools during cleanup instead of deleting WarmPool and template
  objects.

### Removed
- Remove user-facing cold-start controls from the CLI, SDK, gateway request
  payloads, and Helm configuration.

## [0.14.1] - 2026-06-29

### Fixed
- Treat managed-session `maxReplicas` as a hard ceiling instead of an eager
  scale-up target.
- Allow managed sessions to pass `idleTimeoutSeconds` and
  `maxLifetimeSeconds` through the Python SDK.
- Preserve source session routing for stale ARL session pods.

### Changed
- Bump Python SDK package metadata and `arl.__version__` to `0.14.1`.

### Added
- **Gateway API**: New REST API component for session management and execution
  - POST/GET/DELETE /v1/sessions - Session lifecycle
  - POST /v1/sessions/{id}/execute - Execute steps synchronously
  - POST /v1/sessions/{id}/restore - Restore workspace to snapshot
  - GET /v1/sessions/{id}/history - Execution history
  - GET /v1/sessions/{id}/trajectory - JSONL trajectory export for RL/SFT
  - WebSocket /v1/sessions/{id}/shell - Interactive shell
  - POST/GET/DELETE /v1/pools - WarmPool management
- **Executor Agent**: Lightweight agent running inside executor containers
  - Receives commands from sidecar via Unix socket
  - Replaces kubectl exec approach for executor container execution
- **Image-Locality Scheduler**: Pod scheduling with image-locality awareness
  - Rendezvous hashing for consistent node selection
  - Prefers nodes that already have required container images
- **Tool Invocation Helpers**: SDK can list and call tools that already exist
  in the executor image under `/opt/arl/tools`
  - Sandbox-backed pool creation currently rejects `tools` provisioning payloads
  - `list_tools` and `call_tool` are image-content helpers, not provisioning
- **Workspace Restore**: Each step receives a snapshot ID equal to its history
  index
  - Restore allocates a fresh runtime and replays history up to that step
- **Trajectory Export**: JSONL export of execution history for RL/SFT training

### Removed
- **Task CRD**: Removed entirely - execution now handled by Gateway API
- **Task Controller**: Replaced by Gateway direct gRPC to sidecar
- **TTL Controller**: Replaced by Gateway session lifecycle management
- **Task Webhook**: No longer needed
- **OpenAPI SDK Generation**: Removed openapi/, hack/generate-sdk.sh, hack/merge-openapi.py
- **Auto-generated arl_client**: Replaced by hand-written GatewayClient
- **Pod Exec Client**: Replaced by Executor Agent (Unix socket)

### Changed
- Python SDK now communicates via Gateway HTTP API instead of Kubernetes API
- SandboxSession takes `gateway_url` parameter instead of direct K8s access
- Execute returns structured ExecuteResponse with per-step results
- Sidecar forwards commands to executor-agent via Unix socket (was kubectl exec)
- Makefile `generate` target no longer includes Python SDK generation
- WarmPool controller handles sidecar and executor-agent injection

## [Previous Versions]
See git history for previous changes.
