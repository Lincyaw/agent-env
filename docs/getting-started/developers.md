# Getting Started for Developers

This guide is for developers and operators who need to deploy and manage ARL-Infra on a Kubernetes cluster.

## Prerequisites

Before deploying ARL-Infra, ensure you have:

| Requirement | Version | Purpose |
|-------------|---------|---------|
| Kubernetes | 1.25+ | Container orchestration |
| kubectl | Latest | Kubernetes CLI |
| Helm | 3.x | Package manager |
| Docker | 20.x+ | Container runtime |
| Go | 1.25+ | Building from source |
| Skaffold | Latest | Development workflow |

## Quick Start

### Option 1: Using Skaffold (Recommended)

```bash
# Clone the repository
git clone https://github.com/Lincyaw/agent-env.git
cd agent-env

# Setup K8s prerequisites (ClickHouse operator, Helm dependencies)
make k8s-setup

# Deploy using Skaffold (default profile for minikube)
skaffold run

# Or for development with auto-rebuild
skaffold dev --profile=dev

# Deploy to K8s cluster with registry
skaffold run --profile=k8s

# Deploy with sample resources
skaffold run --profile=with-samples
```

### Option 2: Deploy to Production

```bash
# Deploy to production environment
skaffold run --profile=prod
```

### Verify Deployment

```bash
# Check operator pods
kubectl get pods -n arl-system

# Check CRDs are installed
kubectl get crds | grep arl.infra.io

# Check custom resources
kubectl get warmpools,sandboxes,tasks

# View operator logs
make logs
```

## What's Next?

After deploying ARL-Infra, proceed to:

1. **[Architecture](../developer-guide/architecture.md)** - Understand the system design
2. **[Installation](../developer-guide/installation.md)** - Detailed installation options
3. **[Deployment](../developer-guide/deployment.md)** - Production deployment guide
4. **[CRD Reference](../developer-guide/crd-reference.md)** - Custom Resource definitions

## Quick Test

Create a simple warm pool and execute a task:

```bash
# Create a warm pool
cat <<EOF | kubectl apply -f -
apiVersion: arl.infra.io/v1alpha1
kind: WarmPool
metadata:
  name: test-pool
spec:
  replicas: 1
  template:
    spec:
      containers:
        - name: executor
          image: python:3.9-slim
          command: ["sleep", "infinity"]
EOF

# Wait for pods to be ready
kubectl get pods -w

# Create a sandbox
cat <<EOF | kubectl apply -f -
apiVersion: arl.infra.io/v1alpha1
kind: Sandbox
metadata:
  name: test-sandbox
spec:
  poolRef: test-pool
  keepAlive: true
EOF

# Execute a task
cat <<EOF | kubectl apply -f -
apiVersion: arl.infra.io/v1alpha1
kind: Task
metadata:
  name: test-task
spec:
  sandboxRef: test-sandbox
  timeout: 30s
  steps:
    - name: hello
      type: Command
      command: ["echo", "Hello from ARL-Infra!"]
EOF

# Check the result
kubectl get task test-task -o jsonpath='{.status.stdout}'
```

## Troubleshooting

| Issue | Solution |
|-------|----------|
| Pods not starting | Check `kubectl describe pod` for events |
| CRDs not found | Run `make manifests` and apply CRDs |
| Operator not running | Check `make logs` for operator logs |
| Helm dependency issues | Run `make k8s-setup` to update dependencies |
