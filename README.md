# ARL-Infra

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.26+-00ADD8.svg)](https://golang.org/)
[![Python Version](https://img.shields.io/badge/python-3.10+-3776AB.svg)](https://python.org/)

**Gateway and Python SDK for agent-sandbox-backed Agentic Reinforcement Learning environments with warm pools and sidecar execution.**

## Features

- ⚡ **Ultra-low latency**: Bypasses pod startup time using warm pools
- 🔒 **Isolation**: Each sandbox runs in an isolated environment
- 🔄 **Stateful workspaces**: Reuse sessions, transfer files, restore by replay, and export trajectories
- 🌐 **Gateway API**: REST API for session management and execution
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
from arl import SandboxSession

with SandboxSession(
    image="python:3.12",
    profile="my-python-pool",
    gateway_url="http://localhost:8080",
) as session:
    result = session.execute([
        {"name": "hello", "command": ["echo", "Hello, World!"]},
    ])
    print(result.results[0].output.stdout)
```

### For Developers

```bash
# Clone repository
git clone https://github.com/Lincyaw/agent-env.git
cd agent-env

# Enter the pinned local toolchain (recommended; see go.mod/devbox.json)
devbox shell

# Install chart dependencies
helm dependency build charts/agent-env

# Build local runtime images
docker build -f Dockerfile.gateway -t arl-gateway:dev .
docker build -f Dockerfile.sidecar -t arl-sidecar:dev .
docker build -f Dockerfile.executor-agent -t arl-executor-agent:dev .
docker build -f Dockerfile.image-locality-scheduler -t arl-image-locality-scheduler:dev .

# Local trusted install. Production should keep auth enabled and set auth.apiKeys.
helm upgrade --install agent-env charts/agent-env \
  -n arl --create-namespace \
  --set auth.enabled=false \
  --set grafana.adminPassword="$GRAFANA_ADMIN_PASSWORD" \
  --set image.injectedPullPolicy=IfNotPresent \
  --set gateway.image.tag=dev \
  --set sidecar.image.tag=dev \
  --set executorAgent.image.tag=dev \
  --set imageLocalityScheduler.image.tag=dev
```

## Architecture

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   Python SDK    │────▶│    Gateway      │────▶│ agent-sandbox   │
└─────────────────┘     └─────────────────┘     └─────────────────┘
                               │                        │
                               │ gRPC          ┌───────┴───────┐
                               ▼               ▼               ▼
                        ┌───────────┐   ┌───────────┐   ┌───────────┐
                        │  Sidecar  │   │ Sandbox-  │   │  Sandbox  │
                        │  (Pod)    │   │ WarmPool  │   │  (CRD)    │
                        └───────────┘   └───────────┘   └───────────┘
```

## Development

```bash
# Use the pinned local toolchain from devbox.json
devbox run -- go version

# Generate code
devbox run -- make generate

# Run quality checks
devbox run -- make check

# View gateway logs
make logs
```

See [Development Guide](https://lincyaw.github.io/agent-env/developer-guide/development/) for details.

## License

This project is open source. See the [LICENSE](LICENSE) file for details
