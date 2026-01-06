# ARL-Infra: Agentic RL Kubernetes Infrastructure

A Kubernetes Operator implementation for Agentic Reinforcement Learning environments with warm pool and sidecar injection for ultra-low latency code execution.

## Architecture Overview

The system implements a control plane and data plane separation:

- **Control Plane**: ARL Operator manages resource pools and orchestrates task execution
- **Data Plane**: Warm pool of pre-created pods with sidecar agents for instant code execution

### Key Components

1. **WarmPool**: Maintains a pool of ready-to-use pods for instant allocation
2. **Sandbox**: Represents an agent's isolated workspace bound to a pod
3. **Task**: Execution unit containing file operations and commands
4. **Sidecar**: HTTP agent running in each pod for file and process management

## Features

- ✅ **Ultra-low latency**: Bypasses pod startup time using warm pools
- ✅ **Isolation**: Each sandbox runs in an isolated environment
- ✅ **Hot code reload**: Update and execute code without pod restarts
- ✅ **Mixed workloads**: Support for both short-lived jobs and long-running services
- ✅ **Kubernetes-native**: CRD-based API, standard K8s tooling

## Prerequisites

- Go 1.25+
- Docker
- kubectl
- minikube (for local testing) or access to a Kubernetes cluster

## Quick Start

### Option 1: Deploy to Minikube (Local Development)

#### 1. Build Docker Images

```bash
make docker-build
```

This builds:
- `arl-operator:latest` - The Kubernetes operator
- `arl-sidecar:latest` - The sidecar agent

#### 2. Start Minikube

```bash
make minikube-start
```

#### 3. Load Images into Minikube

```bash
make minikube-load-images
```

#### 4. Deploy to Kubernetes

```bash
make deploy
```

This will:
- Install CRDs (WarmPool, Sandbox, Task)
- Deploy the operator to `arl-system` namespace
- Set up RBAC

#### 5. Create Sample Resources

```bash
kubectl apply -f config/samples/
```

### Option 2: Deploy to Standard Kubernetes Cluster

#### 1. Build and Push Images

```bash
# Build and push images to registry
make k8s-build-push

# Or use custom registry
REGISTRY=your-registry.com/your-namespace make k8s-build-push
```

This builds and pushes:
- `10.10.10.240/library/arl-operator:latest`
- `10.10.10.240/library/arl-sidecar:latest`

#### 2. Deploy to Cluster

```bash
# Deploy CRDs and operator
make k8s-deploy
```

#### 3. Create Sample Resources

```bash
# Use K8s-specific samples (with proper image registry)
kubectl apply -f config/samples/
```

### Verify Deployment

```bash
# Check operator is running
kubectl get pods -n arl-system

# Check WarmPool status
kubectl get warmpools

# Check Sandbox status
kubectl get sandboxes

# Check Task status
kubectl get tasks
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

ARL Infrastructure provides a Python SDK for programmatic access to ARL resources. The SDK is auto-generated from CRD OpenAPI schemas and includes high-level wrappers for common operations.

### Installation

```bash
cd sdk/python/arl-client
pip install -e .
```

Or install directly from the repository:

```bash
pip install git+https://github.com/Lincyaw/agent-env.git#subdirectory=sdk/python/arl-client
```

### Quick Start

```python
from arl_client.session import SandboxSession

# Using context manager (recommended)
with SandboxSession("python-3.9-std", namespace="default") as session:
    result = session.execute([
        {
            "name": "write_script",
            "type": "FilePatch",
            "path": "/workspace/hello.py",
            "content": "print('Hello from ARL!')"
        },
        {
            "name": "run_script",
            "type": "Command",
            "command": ["python", "/workspace/hello.py"]
        }
    ])
    
    print(f"Output: {result['status']['stdout']}")
    print(f"Exit Code: {result['status']['exitCode']}")
```

### Features

- **Auto-generated models**: Type-safe Python models for all ARL resources
- **High-level wrappers**: `SandboxSession` context manager for easy resource management
- **Kubernetes integration**: Built on official Kubernetes Python client
- **Complete examples**: See `examples/python/` for usage patterns

For detailed SDK documentation, see [sdk/python/arl-client/README.md](sdk/python/arl-client/README.md).

## API Reference

### WarmPool

Maintains a pool of pre-created pods for instant allocation.

**Spec:**
- `replicas`: Number of idle pods to maintain
- `template`: Pod template specification

**Status:**
- `readyReplicas`: Number of ready idle pods
- `allocatedReplicas`: Number of allocated pods

### Sandbox

Represents an agent's isolated workspace.

**Spec:**
- `poolRef`: Name of the WarmPool to allocate from
- `keepAlive`: Keep pod alive after tasks complete
- `resources`: Resource requirements

**Status:**
- `phase`: Pending | Bound | Ready | Failed
- `podName`: Name of the allocated pod
- `podIP`: IP address of the pod
- `workDir`: Working directory path

### Task

Execution unit with file operations and commands.

**Spec:**
- `sandboxRef`: Target sandbox name
- `timeout`: Maximum execution time
- `steps`: Array of FilePatch or Command steps

**Status:**
- `state`: Pending | Running | Succeeded | Failed
- `exitCode`: Exit code of execution
- `stdout`: Standard output
- `stderr`: Standard error output
- `duration`: Execution duration

## Development

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

## Testing

### Integration Tests

```bash
make test-integration
```

### View Operator Logs

```bash
make logs
```

## Clean Up

### Minikube

```bash
# Remove sample resources
make undeploy-samples

# Remove operator and CRDs
make undeploy

# Delete minikube cluster
make minikube-delete
```

### Standard Kubernetes

```bash
# Remove sample resources
kubectl delete -f config/samples/

# Remove operator and CRDs
make k8s-undeploy
```

## Architecture Details

### Warm Pool Strategy

The operator maintains a pool of pre-created pods in "idle" state. When a Sandbox is created:

1. Operator finds an idle pod from the specified pool
2. Marks the pod as "allocated" and binds it to the Sandbox
3. Pod transitions to "ready" state when all containers are running
4. Tasks can now execute instantly via sidecar HTTP API

### Sidecar Communication

The sidecar exposes an HTTP API on port 8080:

- `POST /files` - Update files
- `POST /execute` - Execute commands
- `POST /signal` - Send process signals
- `POST /reset` - Clean workspace

The operator communicates with sidecars directly using pod IPs for minimal latency.

### Execution Flow

1. **WarmPool Controller**: Maintains desired number of idle pods
2. **Sandbox Controller**: Allocates pods from pool on-demand
3. **Task Controller**: Executes steps via sidecar HTTP API
4. **Sidecar**: Manages files, processes, and workspace

## Troubleshooting

### Pods not starting

```bash
kubectl describe warmpool <name>
kubectl describe pod <pod-name>
```

### Task execution fails

```bash
kubectl describe task <name>
kubectl logs <pod-name> -c sidecar
```

### Operator issues

```bash
kubectl logs -n arl-system -l app=arl-operator
```

## License

MIT
