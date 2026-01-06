# ARL-Infra: Agentic RL Kubernetes Infrastructure

A Kubernetes Operator for Agentic Reinforcement Learning environments with warm pool and sidecar injection for ultra-low latency code execution.

## Architecture

**Control Plane**: ARL Operator manages resource pools and orchestrates task execution  
**Data Plane**: Warm pool of pre-created pods with sidecar agents for instant code execution

### Core Resources

1. **WarmPool**: Maintains a pool of ready-to-use pods for instant allocation
2. **Sandbox**: Agent's isolated workspace bound to a pod
3. **Task**: Execution unit with file operations and commands
4. **Sidecar**: HTTP agent in each pod for file and process management

## Features

- ✅ **Ultra-low latency**: Bypasses pod startup time using warm pools
- ✅ **Isolation**: Each sandbox runs in an isolated environment
- ✅ **Hot code reload**: Update and execute code without pod restarts
- ✅ **Kubernetes-native**: CRD-based API, standard K8s tooling

## Prerequisites

- Go 1.25+
- Docker
- kubectl
- minikube (for local) or Kubernetes cluster

## Quick Start

### Deploy with Skaffold (Recommended)

```bash
# Local development with auto-rebuild
make dev

# Or build and deploy once
make run

# With samples
make run-samples
```

### Deploy to Kubernetes Cluster

```bash
# Build, push and deploy
make k8s-run

# With samples
make k8s-run-samples
```

### Verify Deployment

```bash
kubectl get pods -n arl-system
kubectl get warmpools,sandboxes,tasks
```

## Usage Examples

### Create a WarmPool

```yaml
apiVersion: arl.infra.io/v1alpha1
kind: WarmPool
metadata:
  name: python-3.9-std
spec:
  replicas: 3  # Maintain 3 idle pods
  template:
    spec:
      containers:
        - name: executor
          image: python:3.9-slim
          command: ["/bin/sh", "-c", "sleep infinity"]
```

### Create a Sandbox

```yaml
apiVersion: arl.infra.io/v1alpha1
kind: Sandbox
metadata:
  name: my-agent-workspace
spec:
  poolRef: python-3.9-std
  keepAlive: true
```

### Execute a Task

```yaml
apiVersion: arl.infra.io/v1alpha1
kind: Task
metadata:
  name: my-task
spec:
  sandboxRef: my-agent-workspace
  timeout: 30s
  steps:
    - name: write_code
      type: FilePatch
      content: |
        print("Hello from ARL!")
    - name: run_test
      type: Command
      command: ["python", "-c", "print('Success!')"]
```

## Python SDK

Python SDK with auto-generated models and high-level wrappers. See [sdk/python/arl/](sdk/python/arl/) for installation and [examples/python/](examples/python/) for usage

### Build Binaries

```bash
make build
```

### Code Generation

Generate CRD manifests and Python SDK:

```bash
# Generate CRD manifests from Go types
make manifests

# Generate deepcopy code
make generate

# Generate Python SDK from CRDs (requires Docker)
make sdk-python
```

The Python SDK is auto-generated from CRD OpenAPI schemas using:
1. `controller-gen` - Generates CRD manifests with OpenAPI schemas
2. Custom script - Creates unified OpenAPI specification
3. `openapi-generator` - Generates Python client code

### Python Code Quality

```bash
# Run all quality checks (Ruff, MyPy, Pytest, Bandit)
make python-quality

# Auto-fix formatting and linting issues
make python-fix

# Install Python dependencies
make python-install-sdk
make python-install-examples
```

### Run Locally

```bash
# Terminal 1: Run operator
go run cmd/operator/main.go

# Terminal 2: Run sidecar (for testing)
go run cmd/sidecar/main.go
```

### Code Formatting

```bash
make fmt
make vet
make tidy
```

## Clean Up

```bash
# Delete all resources
make delete

# Or for K8s cluster
make k8s-delete