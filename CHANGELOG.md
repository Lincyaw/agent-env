# Changelog

## [Unreleased]

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
- **Tools Provisioning**: WarmPool CRD supports pre-provisioned tools
  - ToolsSpec with images, configmaps, and inline tool definitions
  - Tools mounted at /opt/arl/tools/ in executor containers
  - Tool invocation via SDK (list_tools, call_tool)
- **Workspace Snapshots**: Auto-snapshot workspace after each step (git-based)
  - Restore to any previous step's snapshot
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
