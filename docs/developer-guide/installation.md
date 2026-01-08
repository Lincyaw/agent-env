# Installation

This guide covers all installation options for ARL-Infra.

## Prerequisites

### Required Tools

| Tool | Version | Installation |
|------|---------|--------------|
| Go | 1.25+ | [golang.org/dl](https://golang.org/dl/) |
| Docker | 20.x+ | [docker.com](https://docs.docker.com/get-docker/) |
| kubectl | Latest | [kubernetes.io](https://kubernetes.io/docs/tasks/tools/) |
| Helm | 3.x | [helm.sh](https://helm.sh/docs/intro/install/) |

### Optional Tools

| Tool | Purpose | Installation |
|------|---------|--------------|
| minikube | Local development | [minikube.sigs.k8s.io](https://minikube.sigs.k8s.io/docs/start/) |
| kind | Local development | [kind.sigs.k8s.io](https://kind.sigs.k8s.io/) |
| Skaffold | Development workflow | [skaffold.dev](https://skaffold.dev/docs/install/) |

## Kubernetes Cluster Setup

### Option 1: minikube (Recommended for Development)

```bash
# Start minikube
minikube start --cpus=4 --memory=8192

# Enable required addons
minikube addons enable ingress
minikube addons enable metrics-server

# Verify
kubectl cluster-info
```

### Option 2: kind

```bash
# Create cluster
kind create cluster --name arl-dev

# Verify
kubectl cluster-info
```

### Option 3: Existing Cluster

Ensure you have:

- Kubernetes 1.25 or higher
- Cluster admin access
- Storage class available

```bash
# Verify cluster access
kubectl cluster-info
kubectl get nodes
```

## Install Development Tools

Install all required development tools:

```bash
# Clone the repository
git clone https://github.com/Lincyaw/agent-env.git
cd agent-env

# Install all tools (protoc, Go tools, Python tools)
make install-tools
```

This installs:

- Protocol Buffers compiler (protoc)
- Go tools (protoc-gen-go, protoc-gen-go-grpc, controller-gen)
- Python tools (via uv sync)

### Manual Installation

If you prefer manual installation:

=== "Protocol Buffers"

    ```bash
    # Installs protoc v28.3
    make install-protoc
    ```

=== "Go Tools"

    ```bash
    # Installs protoc-gen-go, protoc-gen-go-grpc, controller-gen
    make install-go-tools
    ```

=== "Python Tools"

    ```bash
    # Installs Python dependencies via uv
    make install-python-tools
    ```

## Build from Source

### Generate Code

```bash
# Generate all code (proto, CRDs, deepcopy, Python SDK)
make generate

# Or individually:
make proto-go      # Generate Go gRPC code from proto files
make manifests     # Generate CRD manifests
make deepcopy      # Generate deepcopy code
make sdk-python    # Generate Python SDK from CRDs
```

### Code Quality

```bash
# Run all code quality checks (Go fmt, vet, tidy + Python ruff, mypy)
make check

# Or individually:
make fmt           # Run go fmt
make vet           # Run go vet
make tidy          # Run go mod tidy
```

### Build Python SDK

```bash
# Build Python SDK package
make build-sdk

# Publish to Test PyPI
make publish-test

# Publish to Production PyPI
make publish

# Clean SDK build artifacts
make clean-sdk
```

## Install CRDs

Install Custom Resource Definitions:

```bash
# Generate and install CRDs
make manifests
kubectl apply -f config/crd/
```

Verify CRDs are installed:

```bash
kubectl get crds | grep arl.infra.io
```

Expected output:

```
sandboxes.arl.infra.io           2024-01-01T00:00:00Z
tasks.arl.infra.io               2024-01-01T00:00:00Z
warmpools.arl.infra.io           2024-01-01T00:00:00Z
```

## Next Steps

After installation, proceed to:

- [Deployment Guide](deployment.md) - Deploy ARL-Infra to your cluster
- [Development Guide](development.md) - Set up development environment

## Troubleshooting

### Common Issues

!!! warning "CRD not found"
    
    If you see "no matches for kind" errors:
    
    ```bash
    # Regenerate and apply CRDs
    make manifests
    kubectl apply -f config/crd/
    ```

!!! warning "Permission denied"
    
    If you see RBAC errors:
    
    ```bash
    # Ensure you have cluster-admin access
    kubectl auth can-i create deployments --all-namespaces
    ```

!!! warning "Image pull errors"
    
    If pods fail to start due to image issues:
    
    ```bash
    # For minikube, use local images
    eval $(minikube docker-env)
    make build
    ```

### Verify Installation

Run the verification script:

```bash
# Check all prerequisites
kubectl cluster-info
kubectl get crds | grep arl
docker version
go version
```
