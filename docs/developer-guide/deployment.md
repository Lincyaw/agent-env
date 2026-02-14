# Deployment

This guide covers deploying ARL-Infra to various environments.

## Deployment Options

| Method | Best For | Auto-Rebuild |
|--------|----------|--------------|
| Skaffold (default) | Local minikube | No |
| Skaffold (dev) | Development with auto-sync | Yes |
| Skaffold (k8s) | K8s clusters with registry | No |
| Skaffold (prod) | Production | No |
| Helm | Custom deployments | No |

## Skaffold Deployment (Recommended)

### Prerequisites Setup

Before first deployment, run the setup command:

```bash
# Setup K8s prerequisites (ClickHouse operator, Helm dependencies)
make k8s-setup
```

This will:
1. Install ClickHouse operator
2. Update Helm dependencies
3. Prepare CRDs for installation

### Local Development (minikube)

For iterative development with auto-rebuild:

```bash
# Start development mode with file watching
skaffold dev --profile=dev
```

Or for a one-time deployment:

```bash
# Deploy to minikube
skaffold run
```

### Deploy to Kubernetes Cluster

For staging or production clusters with a container registry:

```bash
# Deploy using k8s profile
skaffold run --profile=k8s

# With sample resources
skaffold run --profile=k8s-with-samples
```

### Deploy to Production

```bash
# Deploy to production
skaffold run --profile=prod
```

### Delete Deployment

```bash
# Delete resources deployed by Skaffold
skaffold delete

# Or with specific profile
skaffold delete --profile=k8s
```

## Helm Deployment

### Prerequisites

```bash
# Add Helm repository for dependencies
helm repo add clickhouse-operator https://helm.altinity.com
helm repo update

# Update chart dependencies
cd charts/arl-operator
helm dependency update
```

### Install

```bash
# Install with default values
helm install arl-operator charts/arl-operator -n arl-system --create-namespace

# Install with custom values
helm install arl-operator charts/arl-operator \
  -n arl-system \
  --create-namespace \
  -f my-values.yaml
```

### Configuration

Create a `values.yaml` file:

```yaml
operator:
  image:
    repository: your-registry/arl-operator
    tag: v1.0.0
  replicas: 1
  resources:
    limits:
      cpu: 500m
      memory: 512Mi
    requests:
      cpu: 100m
      memory: 128Mi

sidecar:
  image:
    repository: your-registry/arl-sidecar
    tag: v1.0.0
```

### Upgrade

```bash
helm upgrade arl-operator charts/arl-operator -n arl-system -f my-values.yaml
```

### Uninstall

```bash
helm uninstall arl-operator -n arl-system
```

## Manual Deployment

### 1. Install CRDs

```bash
kubectl apply -f config/crd/
```

### 2. Create Namespace

```bash
kubectl create namespace arl-system
```

### 3. Deploy Operator

```bash
# Build and push images
docker build -f Dockerfile.operator -t your-registry/arl-operator:latest .
docker push your-registry/arl-operator:latest

# Deploy
kubectl apply -f config/deployment/operator.yaml -n arl-system
```

### 4. Verify Deployment

```bash
kubectl get pods -n arl-system
kubectl logs -n arl-system -l app=arl-operator
```

## Post-Deployment Setup

### Create a WarmPool

After deploying ARL-Infra, create at least one WarmPool:

```yaml
# warmpool.yaml
apiVersion: arl.infra.io/v1alpha1
kind: WarmPool
metadata:
  name: python-pool
spec:
  replicas: 3
  template:
    spec:
      containers:
        - name: executor
          image: python:3.9-slim
          command: ["sleep", "infinity"]
          volumeMounts:
            - name: workspace
              mountPath: /workspace
      volumes:
        - name: workspace
          emptyDir: {}
```

```bash
kubectl apply -f warmpool.yaml
```

### Verify WarmPool

```bash
# Check WarmPool status
kubectl get warmpools

# Check pods created by the pool
kubectl get pods -l arl.infra.io/warmpool=python-pool
```

## Production Considerations

### High Availability

For production deployments:

```yaml
# values.yaml
operator:
  replicas: 3
  affinity:
    podAntiAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        - labelSelector:
            matchLabels:
              app: arl-operator
          topologyKey: kubernetes.io/hostname
```

### Resource Quotas

Set resource quotas for namespaces:

```yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  name: arl-quota
  namespace: arl-system
spec:
  hard:
    pods: "100"
    requests.cpu: "10"
    requests.memory: 20Gi
    limits.cpu: "20"
    limits.memory: 40Gi
```

### Monitoring

Enable Prometheus metrics:

```yaml
# values.yaml
operator:
  metrics:
    enabled: true
    port: 8080
```

### Network Policies

Restrict network access:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: arl-operator-policy
  namespace: arl-system
spec:
  podSelector:
    matchLabels:
      app: arl-operator
  policyTypes:
    - Ingress
    - Egress
  ingress:
    - from:
        - namespaceSelector: {}
  egress:
    - to:
        - namespaceSelector: {}
```

## Clean Up

### Using Skaffold

```bash
# Delete all deployed resources
skaffold delete

# Delete with specific profile
skaffold delete --profile=k8s
```

### Manual Cleanup

```bash
# Delete all sandboxes
kubectl delete sandboxes --all

# Delete all warmpools
kubectl delete warmpools --all

# Delete Helm release
helm uninstall arl-operator -n arl-system

# Delete CRDs
kubectl delete -f config/crd/

# Delete namespace
kubectl delete namespace arl-system
```

## Troubleshooting

### Operator Not Starting

```bash
# Check pod status
kubectl get pods -n arl-system

# Check logs using make target
make logs

# Describe pod for events
kubectl describe pod -n arl-system -l app=arl-operator
```

### WarmPool Pods Not Creating

```bash
# Check WarmPool status
kubectl describe warmpool <name>

# Check operator logs for errors
make logs
```

### Execution Not Working

```bash
# Check Gateway is running
kubectl get pods -n arl-system -l app=arl-gateway

# Check Gateway logs
kubectl logs -n arl-system -l app=arl-gateway

# Check sandbox status
kubectl describe sandbox <sandbox-name>

# Check sidecar logs in the pod
kubectl logs <pod-name> -c sidecar
```
