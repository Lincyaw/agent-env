# Development

This guide covers the development workflow for contributing to ARL-Infra.

## Development Environment

### Prerequisites

Ensure you have completed the [Installation](installation.md) steps.

### Project Structure

```
agent-env/
├── api/                    # CRD type definitions (Go)
│   └── v1alpha1/
├── cmd/
│   ├── operator/          # Operator entrypoint
│   ├── sidecar/           # Sidecar entrypoint
│   ├── gateway/           # Gateway entrypoint
│   └── executor-agent/    # Executor agent entrypoint
├── config/
│   ├── crd/               # Generated CRD manifests
│   └── samples/           # Sample resources
├── pkg/
│   ├── controller/        # Reconciliation logic
│   ├── gateway/           # Gateway REST API
│   ├── execagent/         # Executor agent logic
│   ├── scheduler/         # Image-locality pod scheduling
│   ├── sidecar/           # Sidecar gRPC server
│   ├── pb/                # Generated protobuf code
│   └── webhook/           # Validation webhooks
├── proto/                 # Protocol buffer definitions
├── sdk/python/            # Python SDK
│   └── arl/
├── charts/                # Helm charts
│   └── arl-operator/
├── examples/              # Example code
│   └── python/
└── docs/                  # Documentation
```

## Development Workflow

### 1. Start Development Environment

```bash
# Setup K8s prerequisites (first time only)
make k8s-setup

# Start development with auto-rebuild
skaffold dev --profile=dev
```

This watches for file changes and automatically rebuilds/redeploys.

### 2. Make Changes

Edit code in the relevant directories:

| Change Type | Directory |
|-------------|-----------|
| CRD definitions | `api/v1alpha1/` |
| Controller logic | `pkg/controller/` |
| Gateway logic | `pkg/gateway/` |
| Sidecar code | `cmd/sidecar/`, `pkg/sidecar/` |
| Executor agent | `cmd/executor-agent/`, `pkg/execagent/` |
| gRPC definitions | `proto/` |
| Python SDK | `sdk/python/arl/` |

### 3. Regenerate Code

After modifying certain files, regenerate derived code:

```bash
# After changing api/v1alpha1/*.go
make manifests    # Regenerate CRD manifests
make deepcopy     # Regenerate deepcopy methods

# After changing proto/*.proto
make proto-go     # Regenerate Go gRPC code

# Or regenerate everything
make generate
```

### 4. Run Quality Checks

```bash
# Run all checks (Go + Python)
make check
```

This runs:
- `go fmt ./...`
- `go vet ./...`
- `go mod tidy`
- `ruff check` and `ruff format` for Python
- `mypy` for Python type checking

### 5. Test Changes

```bash
# Deploy with samples for testing
skaffold run --profile=with-samples

# View operator logs
make logs

# Check resource status
kubectl get warmpools,sandboxes
```

## Code Generation Details

### CRD Manifests

Generated from Go types using controller-gen:

```bash
make manifests
```

Output: `config/crd/*.yaml`

### DeepCopy Methods

Generated for Kubernetes objects:

```bash
make deepcopy
```

Output: `api/v1alpha1/zz_generated.deepcopy.go`

### gRPC Code

Generated from Protocol Buffer definitions:

```bash
make proto-go
```

Input: `proto/agent.proto`
Output: `pkg/pb/*.pb.go`

## Architecture Validation

Validate architecture documentation:

```bash
make arch-check
```

This checks:
- Component definitions in `architecture/components.yaml`
- Dependency mappings in `architecture/dependencies.yaml`
- Propagation rules in `architecture/propagation-rules.yaml`

## Common Development Tasks

### Adding a New CRD Field

1. Edit the type definition in `api/v1alpha1/`:

    ```go
    type SandboxSpec struct {
        PoolRef   string `json:"poolRef"`
        KeepAlive bool   `json:"keepAlive,omitempty"`
        NewField  string `json:"newField,omitempty"` // Add new field
    }
    ```

2. Regenerate code:

    ```bash
    make manifests deepcopy
    ```

3. Update controller logic in `pkg/controller/`

4. Run checks:

    ```bash
    make check
    ```

### Adding a New Controller

1. Create controller file in `pkg/controller/`
2. Register in `cmd/operator/main.go`
3. Add necessary RBAC markers
4. Regenerate manifests:

    ```bash
    make manifests
    ```

### Modifying gRPC API

1. Edit `proto/agent.proto`
2. Regenerate Go code:

    ```bash
    make proto-go
    ```

3. Update sidecar implementation in `pkg/sidecar/`

## Debugging

### View Operator Logs

```bash
# Stream logs
make logs

# Or manually
kubectl logs -n arl-system -l app=arl-operator -f
```

### View Sidecar Logs

```bash
# Get pod name
kubectl get pods -l arl.infra.io/warmpool

# View sidecar logs
kubectl logs <pod-name> -c sidecar
```

### Debug with Delve

For local debugging:

```bash
# Run operator locally
go run cmd/operator/main.go

# Or with delve
dlv debug cmd/operator/main.go
```

## Release Process

### Build and Publish SDK

```bash
# Build SDK package
make build-sdk

# Publish to Test PyPI (for testing)
make publish-test

# Publish to Production PyPI
make publish
```

### Clean Build Artifacts

```bash
# Clean Python SDK artifacts
make clean-sdk
```

## Useful Commands Reference

| Command | Description |
|---------|-------------|
| `make help` | Show all available make targets |
| `make install-tools` | Install all development tools |
| `make k8s-setup` | Setup K8s prerequisites |
| `make generate` | Generate all code |
| `make check` | Run all quality checks |
| `make logs` | Stream operator logs |
| `make build-sdk` | Build Python SDK |
| `make arch-check` | Validate architecture docs |
| `skaffold dev --profile=dev` | Development with auto-rebuild |
| `skaffold run` | Deploy to cluster |
| `skaffold delete` | Delete deployment |
