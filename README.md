# ARL-Infra

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.25+-00ADD8.svg)](https://golang.org/)
[![Python Version](https://img.shields.io/badge/python-3.10+-3776AB.svg)](https://python.org/)

**Kubernetes Operator for Agentic Reinforcement Learning environments with warm pool and sidecar injection for ultra-low latency code execution.**

## Features

- ⚡ **Ultra-low latency**: Bypasses pod startup time using warm pools
- 🔒 **Isolation**: Each sandbox runs in an isolated environment
- 🔄 **Hot code reload**: Update and execute code without pod restarts
- 🎯 **Flexible execution**: Run commands in sidecar (fast) or executor container (full environment)
- ☸️ **Kubernetes-native**: CRD-based API, standard K8s tooling
- 🐍 **Python SDK**: High-level API for seamless integration

## Documentation

📚 **[Full Documentation](https://lincyaw.github.io/agent-env/)**

| Guide | Description |
|-------|-------------|
| [Overview](https://lincyaw.github.io/agent-env/getting-started/overview/) | Introduction to ARL-Infra concepts |
| [For Developers](https://lincyaw.github.io/agent-env/getting-started/developers/) | Deploy and manage ARL-Infra |
| [For SDK Users](https://lincyaw.github.io/agent-env/getting-started/sdk-users/) | Use the Python SDK |
| [Architecture](https://lincyaw.github.io/agent-env/developer-guide/architecture/) | System design and components |
| [Python SDK](https://lincyaw.github.io/agent-env/user-guide/python-sdk/) | SDK installation and usage |
| [Examples](https://lincyaw.github.io/agent-env/user-guide/examples/) | Code examples |

## Quick Start

### For SDK Users

```bash
pip install arl-env
```

```python
from arl import SandboxSession, WarmPoolManager

# Step 1: Create a WarmPool (one-time setup)
warmpool_mgr = WarmPoolManager(namespace="default")
warmpool_mgr.create_warmpool(
    name="my-python-pool",
    image="python:3.11-slim",
    replicas=2
)
warmpool_mgr.wait_for_warmpool_ready("my-python-pool")

# Step 2: Use the pool to execute tasks
with SandboxSession(pool_ref="my-python-pool", namespace="default") as session:
    result = session.execute([
        # Fast execution in sidecar (default)
        {"name": "hello", "type": "Command", "command": ["echo", "Hello, World!"]},

        # Execute in executor container (has executor tools)
        {"name": "install", "type": "Command",
         "command": ["pip", "install", "requests"],
         "container": "executor"},
    ])
    print(result["status"]["stdout"])
```

**Execution Modes:**
- **Sidecar (default)**: Ultra-fast (1-5ms), for general operations
- **Executor**: Slower (10-50ms), but has access to executor-specific tools (pip, npm, cargo, etc.)

### For Developers

```bash
# Clone repository
git clone https://github.com/Lincyaw/agent-env.git
cd agent-env

# Setup and deploy
make k8s-setup
skaffold run --profile=k8s
```

## Architecture

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   Python SDK    │────▶│  Kubernetes API │────▶│   ARL Operator  │
└─────────────────┘     └─────────────────┘     └─────────────────┘
                                                        │
                        ┌───────────────────────────────┴───────────────────────────────┐
                        ▼                               ▼                               ▼
                ┌───────────────┐               ┌───────────────┐               ┌───────────────┐
                │   WarmPool    │               │   Sandbox     │               │     Task      │
                │  (Pod Pool)   │               │  (Workspace)  │               │  (Execution)  │
                └───────────────┘               └───────────────┘               └───────────────┘
```

## Development

```bash
# Install tools
make install-tools

# Generate code
make generate

# Run quality checks
make check

# View operator logs
make logs
```

See [Development Guide](https://lincyaw.github.io/agent-env/developer-guide/development/) for details.

## License

This project is open source. See the [LICENSE](LICENSE) file for details
