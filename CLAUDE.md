# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

ARL-Infra is a Kubernetes Operator for Agentic Reinforcement Learning environments. It provides ultra-low latency code execution through warm pod pools and sidecar injection, bypassing pod startup time.

**Core Concepts:**
- **WarmPool**: Maintains a pool of pre-started pods ready for immediate allocation. Supports ToolsSpec for pre-provisioning tools in executor containers.
- **Sandbox**: An isolated workspace bound to a pod from a warm pool
- **Gateway**: REST API service that manages sessions and forwards execution to sidecar gRPC. Replaces the old Task CRD approach.
- **Sidecar**: gRPC service running in each pod, handling file operations and command execution
- **Executor Agent**: Lightweight agent running inside the executor container, receiving commands from sidecar via Unix socket

## Development Commands

### Setup & Installation
```bash
# Install all development tools (protoc, Go tools, Python tools)
make install-tools

# Setup new K8s cluster (ClickHouse operator, Helm deps, CRDs)
make k8s-setup
```

### Code Generation
```bash
# Generate all code (proto, CRDs, deepcopy)
make generate

# Generate specific components
make proto-go        # Generate Go gRPC code from proto files
make manifests       # Generate CRD manifests
make deepcopy        # Generate deepcopy code
```

### Build
```bash
# Build all Go binaries
make build

# Build individual components
make build-gateway          # Build gateway binary
make build-executor-agent   # Build executor agent binary
make build-sidecar          # Build sidecar binary
make build-operator         # Build operator binary
```

### Code Quality
```bash
# Run all quality checks (fmt, vet, tidy, ruff, mypy)
make check

# Individual checks
make fmt             # Run go fmt
make vet             # Run go vet
make tidy            # Run go mod tidy
```

### Deployment
```bash
# Deploy to K8s cluster with registry
skaffold run --profile=k8s

# Deploy to production
skaffold run --profile=prod

# Development mode with auto-sync
skaffold dev --profile=dev

# Deploy with sample resources
skaffold run --profile=with-samples
```

### Python SDK
```bash
# Build Python SDK package
make build-sdk

# Publish to Test PyPI (requires UV_PUBLISH_TOKEN)
make publish-test

# Publish to Production PyPI (requires UV_PUBLISH_TOKEN)
make publish

# Clean build artifacts
make clean-sdk
```

### Testing & Debugging
```bash
# View operator logs
make logs

# Run Python with uv
uv run python <script.py>

# Install Python package
uv add <package>

# Batch prefetch WarmPool images (for SWE-Bench/R2E-Gym datasets)
uv run --group prefetch python scripts/batch_prefetch.py --dry-run  # Preview
uv run --group prefetch python scripts/batch_prefetch.py             # Execute
uv run --group prefetch python scripts/batch_prefetch.py --dataset r2egym --concurrency 20
```

### Architecture Validation
```bash
# Validate architecture documentation consistency
make arch-check
```

## Architecture

### Component Structure

```
api/v1alpha1/              # CRD type definitions
├── warmpool_types.go      # WarmPool CRD (includes ToolsSpec, ImageLocalitySpec)
└── sandbox_types.go       # Sandbox CRD (with phase validation)

pkg/
├── controller/            # Kubernetes controllers
│   ├── warmpool_controller.go   # Maintains warm pod pools
│   └── sandbox_controller.go    # Allocates pods from pools
├── gateway/               # Gateway REST API server
│   ├── gateway.go         # Session/execution logic
│   ├── router.go          # HTTP route handlers
│   ├── types.go           # Request/response types
│   ├── history.go         # Step history tracking
│   └── ws_shell.go        # WebSocket shell handler
├── execagent/             # Executor agent (runs inside executor container)
│   ├── agent.go           # Unix socket server, command execution
│   └── protocol.go        # JSON-over-socket request/response types
├── scheduler/             # Image-locality aware pod scheduling
│   ├── image_scheduler.go # Node watcher with Rendezvous hashing
│   └── rendezvous.go      # HRW hashing implementation
├── webhook/               # Admission webhooks for validation
├── sidecar/               # Sidecar gRPC server implementation
├── pb/                    # Generated protobuf code
├── client/                # gRPC client for sidecar communication
├── interfaces/            # Shared interfaces (SidecarClient, AuditWriter, etc.)
├── metrics/               # Prometheus metrics
├── audit/                 # Audit logging (ClickHouse)
└── middleware/            # Middleware components

cmd/
├── operator/main.go       # Operator entry point
├── gateway/main.go        # Gateway entry point
├── sidecar/main.go        # Sidecar entry point
└── executor-agent/main.go # Executor agent entry point

proto/agent.proto          # gRPC service definition
sdk/python/arl/            # Python SDK (Gateway-based, not auto-generated)
charts/arl-operator/       # Helm chart for deployment
```

### Resource Lifecycle

**WarmPool -> Sandbox -> Gateway (execution)**

1. **WarmPool** creates and maintains N ready pods with sidecar and executor-agent containers
2. **Sandbox** allocates a pod from the pool (phase: Pending -> Bound -> Ready -> Failed)
3. **Gateway** receives execution requests via REST API and forwards them to the sidecar gRPC service. No Task CRD is created; execution is synchronous.

### Gateway

The Gateway (port 8080) provides a REST API for session and execution management. It replaces the old Task controller by directly calling sidecar gRPC.

**REST API Endpoints:**

| Method | Path | Description |
|--------|------|-------------|
| POST | `/v1/sessions` | Create a session (allocates a sandbox from a pool) |
| GET | `/v1/sessions/{id}` | Get session info |
| DELETE | `/v1/sessions/{id}` | Delete session and its sandbox |
| POST | `/v1/sessions/{id}/execute` | Execute steps synchronously via sidecar gRPC |
| POST | `/v1/sessions/{id}/restore` | Restore workspace to a previous snapshot |
| WS | `/v1/sessions/{id}/shell` | Interactive shell via WebSocket |
| GET | `/v1/sessions/{id}/history` | Get execution history |
| GET | `/v1/sessions/{id}/trajectory` | Export trajectory as JSONL (for RL/SFT) |
| POST | `/v1/pools` | Create a WarmPool (with optional ToolsSpec) |
| GET | `/v1/pools/{name}` | Get pool status |
| PATCH | `/v1/pools/{name}` | Scale a pool (update replicas and resources) |
| DELETE | `/v1/pools/{name}` | Delete a pool |
| GET | `/healthz` | Health check |
| GET | `/metrics` | Prometheus metrics endpoint |

**Key features:**
- Synchronous execution (no polling needed)
- Per-step snapshot IDs for restore/rollback
- Step history tracking for trajectory export
- Pool health checks before session creation
- HTTP proxy support (respects `http_proxy`/`HTTP_PROXY` env vars)
- Prometheus metrics for monitoring (sessions, steps, pool utilization, pod lifecycle)

### Sidecar gRPC Interface

The sidecar (port 50051) exposes `AgentService` with methods:
- `UpdateFiles`: Apply file patches or overwrites
- `Execute`: Run commands (job mode) or start background services
- `SignalProcess`: Send signals (SIGTERM/SIGKILL) to processes
- `Reset`: Clean workspace
- `InteractiveShell`: Bidirectional streaming for shell sessions

### Executor Agent

The executor agent runs inside the executor container and communicates with the sidecar via a Unix socket at a shared volume mount. It replaces the old `kubectl exec` approach for executor container execution.

**Protocol**: JSON-over-Unix-socket (newline-delimited JSON). Request types:
- `exec`: Execute a command with optional timeout, env, and working directory. Streams stdout/stderr back.
- `shell`: Start an interactive shell session with stdin/stdout/stderr streaming.
- `signal`: Send a signal (SIGTERM/SIGKILL/SIGINT) to a running process by PID.
- `ping`: Health check.

**Advantages over kubectl exec**:
- Lower latency (Unix socket vs API server round-trip)
- Streaming stdout/stderr
- Process tracking and signal delivery
- No dependency on kubectl or API server availability

### Scheduler

The `ImageScheduler` provides image-locality-aware pod scheduling using Rendezvous (Highest Random Weight) hashing. It watches Node resources and maintains a cache of schedulable nodes.

**How it works:**
1. Watches Node create/update/delete events
2. Maintains a list of schedulable (Ready, not cordoned) nodes
3. `SelectNodes(image, k)` returns top-k preferred nodes for a given image using HRW hashing
4. WarmPool controller uses this to set preferred NodeAffinity, minimizing redundant image pulls

**Configuration** via `WarmPoolSpec.ImageLocality`:
- `enabled`: Activate image-locality scheduling (default: true)
- `spreadFactor`: Controls preferred node count: k = ceil(replicas * spreadFactor) (default: 1.0)
- `weight`: NodeAffinity weight 1-100 (default: 80)

### Controllers

**WarmPoolController**: Watches WarmPool and Pod resources, maintains desired replica count of warm pods. Integrates with ImageScheduler for node affinity. Includes rate limiting and metrics for pod lifecycle events (startup latency, scale duration, image pull errors).

**SandboxController**: Watches Sandbox, WarmPool, and Pod resources, allocates pods from warm pools, tracks sandbox lifecycle. Records end-to-end sandbox allocation latency (creation → Ready).

**Performance Tuning**: Both controllers support rate limiting and concurrency control via environment variables:
- `WARMPOOL_MAX_CONCURRENT`: Max concurrent WarmPool reconciliations (default: 20)
- `SANDBOX_MAX_CONCURRENT`: Max concurrent Sandbox reconciliations (default: 10)
- `WARMPOOL_RATE_QPS`: Rate limit QPS for WarmPool controller (default: 50)
- `WARMPOOL_RATE_BURST`: Rate limit burst for WarmPool controller (default: 100)
- `K8S_CLIENT_QPS`: Kubernetes client QPS (default: 100)
- `K8S_CLIENT_BURST`: Kubernetes client burst (default: 200)

Note: Execution is handled by the Gateway, not by a controller. There is no TaskController or TTLController.

## Monitoring & Observability

The system includes comprehensive Prometheus metrics and Grafana dashboards:

**Metrics Categories:**
- **Gateway metrics**: Active sessions, step execution duration, step results (success/error)
- **Pool metrics**: Pool utilization (ready/allocated), pending pods, pod lifecycle events
- **Pod metrics**: Startup latency (creation to ready), scale-out duration, image pull errors
- **Sidecar metrics**: gRPC call duration and results

**Deployment:**
- Prometheus and Grafana can be enabled via Helm values (`prometheus.enabled`, `grafana.enabled`)
- Metrics exposed at `/metrics` endpoint on gateway and operator
- VictoriaMetrics ServiceScrape and Prometheus ServiceMonitor supported
- Pre-configured Grafana dashboard for ARL-Infra monitoring

## Critical Workflow: Architecture Change Management

**ALWAYS perform impact analysis after code changes:**

1. **Check propagation rules** in `architecture/propagation-rules.yaml` to identify affected components
2. **Execute required actions** (e.g., `make manifests`, `make proto-go`)
3. **Update architecture files** when adding/removing components or changing interfaces:
   - `architecture/components.yaml` - Component catalog
   - `architecture/dependencies.yaml` - Component relationships
   - `architecture/propagation-rules.yaml` - Impact rules
4. **Validate** with `make arch-check`

## Code Style & Conventions

### Go
- Go 1.25.0 - use latest best practices
- English only for code, comments, variable names
- Run `make check` before committing (fmt, vet, tidy)
- Do not create test files unless explicitly requested

### Python
- Python 3.10+ with modern type hints (`dict[str, int]`, `list[str] | None`)
- Use `uv` exclusively for package management (not pip/poetry/conda)
- Pydantic models for business data (never raw dictionaries)
- Raise exceptions instead of returning error codes
- Run `make check` before committing (ruff, mypy)
- Avoid `Any` type, use extensive type hints
- Refactor aggressively - no backward compatibility needed

### General
- Documentation in markdown can use Chinese if appropriate
- Do not write documentation unless specifically requested
- Comments only where necessary for clarity/design rationale

## Python SDK Usage

The SDK communicates with the Gateway REST API via `GatewayClient`. No direct Kubernetes API calls.

**HTTP Proxy Support**: The SDK respects standard `http_proxy` and `HTTP_PROXY` environment variables for proxy configuration.

```python
from arl import SandboxSession, GatewayClient

# Basic usage with context manager
with SandboxSession(pool_ref="my-python-pool", gateway_url="http://localhost:8080") as session:
    result = session.execute([
        {"name": "hello", "command": ["echo", "Hello, World!"]},
        {"name": "install", "command": ["pip", "install", "requests"]},
    ])
    # Access per-step results
    print(result.results[0].output.stdout)       # "Hello, World!\n"
    print(result.results[0].snapshot_id)          # snapshot ID for restore
    print(result.total_duration_ms)               # total execution time
```

### Pool Management via Gateway

```python
from arl import GatewayClient, ToolsSpec, ToolsImageSource

client = GatewayClient(base_url="http://localhost:8080")

# Create a pool with pre-provisioned tools
client.create_pool(
    name="my-python-pool",
    namespace="default",
    image="python:3.11-slim",
    replicas=2,
    tools=ToolsSpec(images=[ToolsImageSource(image="my-tools:latest")]),
)

# Check pool status
pool = client.get_pool("my-python-pool", namespace="default")
print(f"Ready: {pool.ready_replicas}/{pool.replicas}")

# Scale pool (update replicas and optionally resources)
from arl import ResourceRequirements
pool = client.scale_pool(
    name="my-python-pool",
    replicas=5,
    namespace="default",
    resources=ResourceRequirements(
        requests={"cpu": "500m", "memory": "512Mi"},
        limits={"cpu": "1", "memory": "1Gi"}
    )
)
```

### Restore (Rollback to Snapshot)

```python
with SandboxSession(pool_ref="my-python-pool", gateway_url="http://localhost:8080") as session:
    r1 = session.execute([{"name": "step1", "command": ["echo", "first"]}])
    snap = r1.results[0].snapshot_id

    r2 = session.execute([{"name": "step2", "command": ["rm", "-rf", "/workspace/*"]}])

    # Rollback to state after step1
    session.restore(snap)
```

### Trajectory Export (JSONL for RL/SFT)

```python
with SandboxSession(pool_ref="my-python-pool", gateway_url="http://localhost:8080") as session:
    session.execute([{"name": "step1", "command": ["echo", "hello"]}])
    session.execute([{"name": "step2", "command": ["echo", "world"]}])

    # Export as JSONL (one entry per step with action/observation pairs)
    jsonl = session.export_trajectory()
    print(jsonl)
```

### Tool Invocation

```python
with SandboxSession(pool_ref="my-python-pool", gateway_url="http://localhost:8080") as session:
    # List available tools from /opt/arl/tools/registry.json
    registry = session.list_tools()
    for tool in registry.tools:
        print(f"{tool.name}: {tool.description}")

    # Call a tool by name with JSON parameters
    result = session.call_tool("my-tool", params={"key": "value"})
    print(result.parsed)       # parsed JSON output
    print(result.exit_code)    # tool exit code
```

### Interactive Shell (WebSocket)

The Gateway provides WebSocket-based interactive shell sessions:

```python
from arl import InteractiveShellClient

# Connect to a session's shell via Gateway WebSocket
client = InteractiveShellClient(gateway_url="http://localhost:8080")
client.connect("session-123")
client.send_input("ls -la\n")
output = client.read_output()
print(output)
client.close()
```

Frontend integration with xterm.js:
```javascript
const ws = new WebSocket('ws://localhost:8080/v1/sessions/session-123/shell');
term.onData(data => ws.send(JSON.stringify({ type: 'input', data })));
ws.onmessage = (e) => {
  const msg = JSON.parse(e.data);
  if (msg.type === 'output') term.write(msg.data);
};
```

See `examples/frontend/interactive_shell.html` for complete example.

## Key Files

- `Makefile` - All development commands
- `skaffold.yaml` - Deployment profiles (k8s, prod, dev, with-samples)
- `proto/agent.proto` - Sidecar gRPC interface definition
- `api/v1alpha1/warmpool_types.go` - WarmPool CRD schema (ToolsSpec, ImageLocalitySpec)
- `api/v1alpha1/sandbox_types.go` - Sandbox CRD schema
- `pkg/controller/warmpool_controller.go` - WarmPool reconciliation logic
- `pkg/controller/sandbox_controller.go` - Sandbox reconciliation logic
- `pkg/gateway/gateway.go` - Gateway session/execution logic
- `pkg/gateway/router.go` - Gateway HTTP route handlers
- `pkg/gateway/types.go` - Gateway request/response types
- `cmd/gateway/main.go` - Gateway entry point
- `pkg/execagent/agent.go` - Executor agent (Unix socket server)
- `pkg/execagent/protocol.go` - Executor agent JSON protocol
- `cmd/executor-agent/main.go` - Executor agent entry point
- `pkg/scheduler/image_scheduler.go` - Image-locality aware scheduler
- `pkg/scheduler/rendezvous.go` - Rendezvous (HRW) hashing
- `sdk/python/arl/arl/gateway_client.py` - Python SDK Gateway HTTP client
- `sdk/python/arl/arl/session.py` - Python SDK SandboxSession
- `sdk/python/arl/arl/types.py` - Python SDK Pydantic models
- `architecture/*.yaml` - Component catalog, dependencies, propagation rules
- `pyproject.toml` - Python workspace configuration
- `sdk/python/arl/pyproject.toml` - Python SDK package configuration
- `examples/python/test_arl_sdk.py` - SDK usage example
- `examples/python/bench_gateway.py` - Gateway benchmark
- `examples/python/test_interactive_shell.py` - Interactive shell example
- `scripts/batch_prefetch.py` - Batch WarmPool image prefetch for SWE-Bench/R2E-Gym datasets

## Documentation

Full documentation available at: https://lincyaw.github.io/agent-env/

Key sections:
- Overview: Introduction to ARL-Infra concepts
- For Developers: Deploy and manage ARL-Infra
- For SDK Users: Use the Python SDK
- Architecture: System design and components
