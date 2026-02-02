# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

ARL-Infra is a Kubernetes Operator for Agentic Reinforcement Learning environments. It provides ultra-low latency code execution through warm pod pools and sidecar injection, bypassing pod startup time.

**Core Concepts:**
- **WarmPool**: Maintains a pool of pre-started pods ready for immediate allocation
- **Sandbox**: An isolated workspace bound to a pod from a warm pool
- **Task**: Execution unit containing file operations (patches) and commands
- **Sidecar**: gRPC service running in each pod, handling file operations and command execution

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
# Generate all code (proto, CRDs, deepcopy, Python SDK)
make generate

# Generate specific components
make proto-go        # Generate Go gRPC code from proto files
make manifests       # Generate CRD manifests
make deepcopy        # Generate deepcopy code
make sdk-python      # Generate Python SDK from CRDs
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
├── warmpool_types.go      # WarmPool CRD
├── sandbox_types.go       # Sandbox CRD (with phase validation)
└── task_types.go          # Task CRD

pkg/
├── controller/            # Kubernetes controllers
│   ├── warmpool_controller.go   # Maintains warm pod pools
│   ├── sandbox_controller.go    # Allocates pods from pools
│   ├── task_controller.go       # Executes tasks via sidecar gRPC
│   └── ttl_controller.go        # Cleans up completed resources
├── webhook/               # Admission webhooks for validation
├── sidecar/              # Sidecar gRPC server implementation
├── pb/                   # Generated protobuf code
├── client/               # Kubernetes client utilities
├── gateway/              # Gateway interfaces
├── metrics/              # Prometheus metrics
├── audit/                # Audit logging
└── middleware/           # Middleware components

cmd/
├── operator/main.go      # Operator entry point
└── sidecar/main.go       # Sidecar entry point

proto/agent.proto         # gRPC service definition
sdk/python/arl/           # Python SDK (auto-generated from CRDs)
charts/arl-operator/      # Helm chart for deployment
```

### Resource Lifecycle

**WarmPool → Sandbox → Task**

1. **WarmPool** creates and maintains N ready pods with sidecar containers
2. **Sandbox** allocates a pod from the pool (phase: Pending → Bound → Ready → Failed)
3. **Task** executes steps on the sandbox via sidecar gRPC (state: Pending → Running → Succeeded/Failed)

### Sidecar gRPC Interface

The sidecar (port 50051) exposes `AgentService` with methods:
- `UpdateFiles`: Apply file patches or overwrites
- `Execute`: Run commands (job mode) or start background services
- `SignalProcess`: Send signals (SIGTERM/SIGKILL) to processes
- `Reset`: Clean workspace
- `InteractiveShell`: Bidirectional streaming for shell sessions

**Executor Container Execution**: Commands can be executed in either the sidecar container (default, fast) or the executor container (slower but has executor-specific tools). Use the `container` field in TaskStep to specify:
- No `container` field or `container: "sidecar"` - Execute in sidecar (1-5ms latency)
- `container: "executor"` - Execute in executor container via kubectl exec (10-50ms latency)

**Use Cases**:
- **Sidecar (default)**: High-frequency operations, file operations, general commands
- **Executor**: Commands requiring executor-specific tools (pip, npm, cargo), package installation, build commands

### Controllers

**WarmPoolController**: Watches WarmPool and Pod resources, maintains desired replica count of warm pods

**SandboxController**: Watches Sandbox, WarmPool, and Pod resources, allocates pods from warm pools, tracks sandbox lifecycle

**TaskController**: Watches Task and Sandbox resources, executes task steps via sidecar gRPC, updates task status with results

**TTLController**: Cleans up completed tasks after TTL expires, removes idle sandboxes after timeout

## Critical Workflow: Architecture Change Management

**ALWAYS perform impact analysis after code changes:**

1. **Check propagation rules** in `architecture/propagation-rules.yaml` to identify affected components
2. **Execute required actions** (e.g., `make manifests`, `make proto-go`, `make sdk-python`)
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

```python
from arl import SandboxSession, WarmPoolManager

# Create a WarmPool (one-time setup)
warmpool_mgr = WarmPoolManager(namespace="default")
warmpool_mgr.create_warmpool(
    name="my-python-pool",
    image="python:3.11-slim",
    replicas=2
)
warmpool_mgr.wait_for_warmpool_ready("my-python-pool")

# Use the pool to execute tasks
with SandboxSession(pool_ref="my-python-pool", namespace="default") as session:
    result = session.execute([
        # Execute in sidecar (default, fast)
        {"name": "hello", "type": "Command", "command": ["echo", "Hello, World!"]},

        # Execute in executor container (has executor tools)
        {"name": "install", "type": "Command",
         "command": ["pip", "install", "requests"],
         "container": "executor"},
    ])
    print(result["status"]["stdout"])
```

### Executor Container Execution

Commands can be executed in either container:
- **Sidecar (default)**: Fast (1-5ms), for general operations
- **Executor**: Slower (10-50ms), but has access to executor-specific tools

```python
# Example: Mixed execution
steps = [
    # File operations always in sidecar
    {"name": "create", "type": "FilePatch", "path": "/workspace/app.py", "content": "..."},

    # Fast command in sidecar
    {"name": "list", "type": "Command", "command": ["ls", "-la"]},

    # Use executor tools
    {"name": "build", "type": "Command",
     "command": ["npm", "run", "build"],
     "container": "executor"},
]
```

### Interactive Shell (WebSocket)

For frontend integration, use the WebSocket server:

```bash
# Start WebSocket server
python -m arl.shell_server

# Server provides:
# - WebSocket: ws://localhost:8000/ws/shell/{namespace}/{pod_name}
# - REST API: http://localhost:8000/api/sandboxes/{namespace}
```

Frontend integration with xterm.js:
```javascript
const ws = new WebSocket('ws://localhost:8000/ws/shell/default/my-pod?container=executor');
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
- `api/v1alpha1/*_types.go` - CRD schemas with kubebuilder markers
- `pkg/controller/*_controller.go` - Reconciliation logic
- `pkg/client/pod_exec.go` - Executor container execution client
- `architecture/*.yaml` - Component catalog, dependencies, propagation rules
- `pyproject.toml` - Python workspace configuration
- `sdk/python/arl/pyproject.toml` - Python SDK package configuration
- `examples/python/09_executor_container.py` - Executor container usage example
- `IMPLEMENTATION_SUMMARY.md` - Executor container feature documentation
- `TEST_REPORT.md` - Feature test results

## Documentation

Full documentation available at: https://lincyaw.github.io/agent-env/

Key sections:
- Overview: Introduction to ARL-Infra concepts
- For Developers: Deploy and manage ARL-Infra
- For SDK Users: Use the Python SDK
- Architecture: System design and components
