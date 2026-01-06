# Local Testing Guide with Minikube

This guide helps you set up a complete local testing environment for ARL-Infra using minikube.

## Prerequisites

- Docker (for building images and minikube)
- kubectl
- minikube
- Go 1.25+
- Python 3.10+ with `uv` package manager

## Quick Start

```bash
cd examples/local-test

# 1. Setup minikube and build images
./setup-minikube.sh

# 2. Deploy ARL infrastructure
./deploy.sh

# 3. Run Python examples
./run-examples.sh

# 4. Cleanup (optional)
./cleanup.sh
```

## Step-by-Step Guide

### 1. Start Minikube

```bash
# Start minikube with sufficient resources
minikube start --cpus=4 --memory=8192 --driver=docker

# Enable metrics server (optional but recommended)
minikube addons enable metrics-server

# Verify cluster is running
minikube kubectl -- cluster-info
```

### 2. Build Docker Images

Build images directly in minikube's Docker environment to avoid pushing to a registry:

```bash
# Configure shell to use minikube's Docker daemon
eval $(minikube docker-env)

# Build operator image
docker build -t arl-operator:latest -f ../../Dockerfile.operator ../..

# Build sidecar image  
docker build -t arl-sidecar:latest -f ../../Dockerfile.sidecar ../..

# Verify images are built
docker images | grep arl
```

### 3. Deploy ARL Infrastructure

```bash
# Apply CRDs
minikube kubectl -- apply -f ../../config/crd/

# Deploy operator
minikube kubectl -- apply -f ../../config/operator/deployment.yaml

# Wait for operator to be ready
minikube kubectl -- wait --for=condition=ready pod -l app=arl-operator -n arl-system --timeout=120s

# Deploy WarmPool
minikube kubectl -- apply -f manifests/warmpool.yaml

# Wait for warm pool pods to be ready
minikube kubectl -- wait --for=condition=ready pod -l arl.infra.io/pool=python-39-std --timeout=120s
```

### 4. Verify Deployment

```bash
# Check operator logs
minikube kubectl -- logs -n arl-system -l app=arl-operator --tail=50

# Check WarmPool status
minikube kubectl -- get warmpools
minikube kubectl -- get pods -l arl.infra.io/pool=python-39-std

# Describe WarmPool for details
minikube kubectl -- describe warmpool python-39-std
```

### 5. Setup Python Environment

```bash
cd ../python

# Install dependencies
uv sync

# Verify installation
uv run python -c "import arl; print(f'ARL SDK version: {arl.__version__}')"
```

### 6. Run Python Examples

```bash
# Port-forward to access sidecar (if needed for direct HTTP access)
# minikube kubectl -- port-forward <pod-name> 8080:8080 &

# Run individual examples
uv run python 01_basic_execution.py
uv run python 02_multi_step_pipeline.py
uv run python 03_environment_variables.py
uv run python 04_working_directory.py
uv run python 05_error_handling.py
uv run python 06_long_running_task.py
uv run python 07_sandbox_reuse.py

# Or run all at once
uv run python run_all_examples.py
```

## Troubleshooting

### Operator Not Starting

```bash
# Check operator logs
minikube kubectl -- logs -n arl-system -l app=arl-operator

# Check events
minikube kubectl -- get events -n arl-system --sort-by='.lastTimestamp'
```

### Pods Not Creating

```bash
# Check WarmPool status
minikube kubectl -- describe warmpool python-39-std

# Check operator has proper RBAC permissions
minikube kubectl -- auth can-i create pods --as=system:serviceaccount:arl-system:arl-operator

# Check resource quotas
minikube kubectl -- describe resourcequota -n default
```

### Image Pull Errors

```bash
# Make sure you're using minikube's Docker daemon
eval $(minikube docker-env)

# Verify images exist
docker images | grep arl

# If images are missing, rebuild them
docker build -t arl-operator:latest -f ../../Dockerfile.operator ../..
docker build -t arl-sidecar:latest -f ../../Dockerfile.sidecar ../..
```

### Python Examples Failing

```bash
# Check if WarmPool pods are ready
minikube kubectl -- get pods -l arl.infra.io/pool=python-39-std

# Check sandbox creation
minikube kubectl -- get sandboxes

# Check task status
minikube kubectl -- get tasks
minikube kubectl -- describe task <task-name>

# Check sidecar logs
minikube kubectl -- logs <pod-name> -c sidecar
```

### Port Conflicts

```bash
# If port 8080 is already in use, change the port-forward command
minikube kubectl -- port-forward <pod-name> 9090:8080
```

## Cleanup

```bash
# Delete all ARL resources
minikube kubectl -- delete tasks --all
minikube kubectl -- delete sandboxes --all
minikube kubectl -- delete warmpools --all

# Delete operator
minikube kubectl -- delete -f ../../config/operator/deployment.yaml

# Delete CRDs
minikube kubectl -- delete -f ../../config/crd/

# Stop minikube (optional)
minikube stop

# Delete minikube cluster (complete cleanup)
minikube delete
```

## Using Automation Scripts

All the manual steps above are automated in the provided scripts:

- **setup-minikube.sh**: Starts minikube and builds images
- **deploy.sh**: Deploys ARL infrastructure and WarmPool
- **run-examples.sh**: Runs all Python examples
- **cleanup.sh**: Cleans up all resources

## Development Workflow

For active development with auto-rebuild:

```bash
# Terminal 1: Run skaffold dev
cd ../..
make dev

# Terminal 2: Run examples
cd examples/python
uv run python 01_basic_execution.py
```

This will automatically rebuild and redeploy when you change Go code.

## Alternative: Using Skaffold

If you prefer using Skaffold for deployment:

```bash
# Configure minikube
eval $(minikube docker-env)

# Deploy with samples
cd ../..
make run

# Or just deploy without samples
skaffold run
```

## Notes

- The WarmPool maintains 2 ready pods by default
- Each pod has both executor (Python) and sidecar containers
- Sidecar listens on port 8080 for file and command operations
- Tasks are created as Kubernetes custom resources and executed via the sidecar
- Sandboxes bind to warm pool pods for instan 