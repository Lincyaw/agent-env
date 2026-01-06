# Kubernetes Testing Patterns

## Overview

Common patterns for testing and validating Kubernetes deployments, operators, and workloads.

## Testing Workflow

### 1. Pre-Deployment Validation

Before deploying resources:

```bash
# Validate YAML syntax
kubectl apply --dry-run=client -f <file.yaml>

# Validate with server-side checks
kubectl apply --dry-run=server -f <file.yaml>

# Check for issues
kubectl apply --dry-run=client -f <file.yaml> -o yaml | kubectl diff -f -
```

### 2. Deploy and Wait

```bash
# Deploy resources
kubectl apply -f <file.yaml>

# Wait for deployment
kubectl rollout status deployment/<name> -n <namespace>

# Or use the wait_for_pod.sh script
scripts/wait_for_pod.sh <namespace> <pod-pattern> 300
```

### 3. Verify Deployment Health

```bash
# Use the verification script
python3 scripts/verify_deployment.py <deployment-name> <namespace>

# Manual verification
kubectl get deployment <name> -n <namespace>
kubectl get pods -n <namespace> -l app=<name>
```

### 4. Functional Testing

Execute tests inside the pod:

```bash
# Direct kubectl exec
kubectl exec -n <namespace> <pod-name> -- <command>

# Using exec_test.sh script
scripts/exec_test.sh <namespace> <pod-pattern> <command>
```

### 5. Debug Failures

When things go wrong:

```bash
# Use debug script for comprehensive info
scripts/debug_pod.sh <namespace> <pod-name>

# Check specific container logs
kubectl logs <pod-name> -n <namespace> -c <container-name>

# Check events
kubectl get events -n <namespace> --sort-by='.lastTimestamp'

# Describe resources
kubectl describe pod <pod-name> -n <namespace>
kubectl describe deployment <name> -n <namespace>
```

## Testing Custom Resources (CRDs)

### Operator Testing Pattern

```bash
# 1. Deploy the operator
kubectl apply -f config/operator/deployment.yaml

# 2. Verify operator is running
scripts/verify_deployment.py <operator-name> <namespace>

# 3. Deploy a test CR
kubectl apply -f config/samples/<resource>.yaml

# 4. Watch for status updates
kubectl get <resource-type> <name> -n <namespace> -w

# 5. Check operator logs
kubectl logs -n <namespace> -l app=<operator-name> --tail=100 -f

# 6. Verify expected behavior
# - Check CR status fields
# - Verify created resources
# - Test state transitions
```

### Example: Testing a Sandbox Resource

```bash
# Deploy operator
kubectl apply -f config/operator/deployment.yaml

# Wait for operator
scripts/wait_for_pod.sh default operator 120

# Create test sandbox
kubectl apply -f config/samples/sandbox.yaml

# Watch status
kubectl get sandbox test-sandbox -o jsonpath='{.status.phase}' -w

# Verify sidecar pod was created
kubectl get pods -l sandbox=test-sandbox

# Test sidecar endpoint
POD=$(kubectl get pods -l sandbox=test-sandbox -o jsonpath='{.items[0].metadata.name}')
kubectl exec $POD -- curl -s http://localhost:8080/health
```

## Integration Testing Patterns

### End-to-End Test Structure

```bash
# 1. Setup
setup() {
    kubectl create namespace test-$$
    NAMESPACE=test-$$
}

# 2. Deploy components
deploy() {
    kubectl apply -f manifests/ -n $NAMESPACE
}

# 3. Wait for readiness
wait_ready() {
    kubectl wait --for=condition=ready pod -l app=myapp -n $NAMESPACE --timeout=300s
}

# 4. Run tests
run_tests() {
    scripts/exec_test.sh $NAMESPACE myapp "curl -f http://localhost:8080/api/test"
}

# 5. Cleanup
cleanup() {
    kubectl delete namespace $NAMESPACE
}

# Execute
trap cleanup EXIT
setup
deploy
wait_ready
run_tests
```

### State Transition Testing

For operators that manage state transitions:

```bash
# Test initial state
kubectl get <resource> <name> -o jsonpath='{.status.phase}'
# Expected: Pending

# Wait for state change
sleep 5
kubectl get <resource> <name> -o jsonpath='{.status.phase}'
# Expected: Running

# Trigger next transition (e.g., delete a dependent resource)
kubectl delete pod <related-pod>

# Verify state update
kubectl get <resource> <name> -o jsonpath='{.status.phase}'
# Expected: Failed or Recreating
```

## Performance Testing

### Resource Usage Monitoring

```bash
# Monitor pod resource usage
kubectl top pods -n <namespace>

# Monitor node resource usage
kubectl top nodes

# Get detailed metrics
kubectl get --raw /apis/metrics.k8s.io/v1beta1/namespaces/<namespace>/pods/<pod-name>
```

### Load Testing Pattern

```bash
# 1. Deploy baseline
kubectl apply -f app.yaml

# 2. Generate load (example using Apache Bench)
POD_IP=$(kubectl get pod <name> -o jsonpath='{.status.podIP}')
ab -n 10000 -c 100 http://${POD_IP}:8080/

# 3. Monitor during load
kubectl top pod <name> --containers

# 4. Check for issues
kubectl get events --sort-by='.lastTimestamp'
```

## Common Test Scenarios

### Verify HTTP Endpoint

```bash
# Port-forward for testing
kubectl port-forward -n <namespace> <pod-name> 8080:8080 &
PF_PID=$!

# Test endpoint
curl -f http://localhost:8080/health

# Cleanup
kill $PF_PID
```

### Verify Environment Variables

```bash
kubectl exec -n <namespace> <pod-name> -- env | grep <VAR_NAME>
```

### Verify ConfigMap/Secret Mount

```bash
kubectl exec -n <namespace> <pod-name> -- cat /path/to/mounted/file
```

### Verify Network Connectivity

```bash
# Test DNS
kubectl exec -n <namespace> <pod-name> -- nslookup kubernetes.default

# Test service connectivity
kubectl exec -n <namespace> <pod-name> -- curl -f http://<service-name>:<port>

# Test external connectivity
kubectl exec -n <namespace> <pod-name> -- curl -f https://www.google.com
```

## Troubleshooting Checklist

When tests fail, check in order:

1. **Pod Status**: `kubectl get pods -n <namespace>`
2. **Events**: `kubectl get events -n <namespace> --sort-by='.lastTimestamp'`
3. **Logs**: `kubectl logs <pod-name> -n <namespace> --tail=100`
4. **Describe**: `kubectl describe pod <pod-name> -n <namespace>`
5. **Resource Constraints**: `kubectl top pods -n <namespace>`
6. **Network**: Test connectivity between pods
7. **RBAC**: Check service account permissions
8. **Image**: Verify image exists and is pullable
