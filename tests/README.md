# Integration Tests

This directory contains integration test manifests and test procedures for the ARL Infrastructure Operator.

## Test Files

### Basic Tests
- [`01-basic-resources.yaml`](01-basic-resources.yaml) - Basic WarmPool, Sandbox, and Task creation
- [`02-complex-task.yaml`](02-complex-task.yaml) - Multi-step task with file operations and environment variables
- [`03-failure-cases.yaml`](03-failure-cases.yaml) - Error handling and failure scenarios
- [`04-multi-sandbox.yaml`](04-multi-sandbox.yaml) - Concurrent execution across multiple sandboxes

### Environment-Specific Examples
- [`minikube/`](minikube/) - Examples for local Minikube development
- [`k8s/`](k8s/) - Examples for production Kubernetes clusters

## Test Procedures

### 1. Deploy the Operator

```bash
# For Minikube
make minikube-start
make minikube-load-images
make deploy

# For production K8s
make k8s-build-push
make k8s-deploy
```

### 2. Verify Operator is Running

```bash
kubectl get all -n arl-system
```

Expected output:
```
NAME                              READY   STATUS    RESTARTS   AGE
pod/arl-operator-xxxxxxxxx-xxxxx  1/1     Running   0          30s
```

### 3. Run Basic Tests

```bash
# Apply basic resources
kubectl apply -f tests/01-basic-resources.yaml

# Wait for WarmPool to create pods (10-15 seconds)
kubectl get pods -w

# Check resource status
kubectl get warmpool,sandbox,task

# View task output
kubectl get task hello-task -o yaml | grep -A 50 status
```

Expected results:
- WarmPool creates 2 pods
- Sandbox binds to one pod (status: Ready)
- Task completes successfully (phase: Succeeded)

### 4. Run Complex Task Test

```bash
kubectl apply -f tests/02-complex-task.yaml

# Watch task execution
kubectl get task complex-task -w

# View detailed output
kubectl get task complex-task -o jsonpath='{.status.stdout}' | head -20
```

Expected: All 5 steps execute successfully, Fibonacci computation completes

### 5. Test Failure Handling

```bash
kubectl apply -f tests/03-failure-cases.yaml

# Wait for tasks to complete
sleep 10

kubectl get task failing-task -o yaml | grep -A 10 status
kubectl get task nonexistent-sandbox-task -o yaml | grep -A 10 status
```

Expected results:
- `failing-task`: Fails at step 2 with exit code 42
- `nonexistent-sandbox-task`: Fails with sandbox not found error

### 6. Test Concurrent Execution

```bash
kubectl apply -f tests/04-multi-sandbox.yaml

# Watch all resources
watch kubectl get warmpool,sandbox,task

# Verify WarmPool scales up
kubectl get warmpool python-pool -o jsonpath='{.status.available}'
```

Expected:
- WarmPool automatically scales to accommodate 3 sandboxes + 2 idle
- Tasks run concurrently in different pods
- All tasks complete successfully

### 7. View Logs

```bash
# Operator logs
kubectl logs -n arl-system deployment/arl-operator -f

# Sidecar logs (example pod)
POD_NAME=$(kubectl get pods -l app=arl-sandbox -o jsonpath='{.items[0].metadata.name}')
kubectl logs $POD_NAME -c sidecar

# Executor container logs
kubectl logs $POD_NAME -c executor
```

### 8. Cleanup

```bash
# Delete test resources
kubectl delete -f tests/04-multi-sandbox.yaml
kubectl delete -f tests/03-failure-cases.yaml
kubectl delete -f tests/02-complex-task.yaml
kubectl delete -f tests/01-basic-resources.yaml

# Or delete all at once
kubectl delete warmpool,sandbox,task --all

# Verify cleanup
kubectl get warmpool,sandbox,task
kubectl get pods -l app=arl-sandbox
```

## Performance Benchmarks

Based on testing with Minikube (4 CPUs, 8GB RAM):

| Metric | Value |
|--------|-------|
| Pod creation time | ~5s (with cached images) |
| Sandbox binding | <1s (from warm pool) |
| Task execution | ~850ms (simple commands) |
| Concurrent sandboxes | 3+ (tested) |
| WarmPool scale-up | ~5s per pod |

## Common Issues

### Issue: Pods stuck in Pending
**Cause**: Insufficient cluster resources
**Solution**: Increase Minikube memory: `minikube start --memory=8192 --cpus=4`

### Issue: Image pull errors
**Cause**: Wrong registry or missing images
**Solution**: 
- For Minikube: `make minikube-load-images`
- For K8s: Update `REGISTRY` in Makefile and `make k8s-build-push`

### Issue: Task fails with "sandbox not ready"
**Cause**: WarmPool hasn't created pods yet
**Solution**: Wait 10-15 seconds after creating WarmPool before creating Sandboxes

### Issue: Sidecar connection refused
**Cause**: Sidecar container not running
**Solution**: Check pod logs: `kubectl logs <pod> -c sidecar`

## Test Coverage

- ✅ CRD installation and validation
- ✅ WarmPool creation and scaling
- ✅ Sandbox lifecycle (create, bind, ready, delete)
- ✅ Task execution (single and multi-step)
- ✅ Command execution with environment variables
- ✅ File operations (via shell commands)
- ✅ Error handling (exit codes, missing resources)
- ✅ Concurrent execution
- ✅ Status reporting and updates
- ✅ Pod reuse from warm pool
- ✅ Automatic pool replenishment
