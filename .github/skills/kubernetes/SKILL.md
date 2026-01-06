---
name: kubernetes
description: Kubernetes operations, testing, and validation. Use when working with Kubernetes clusters for deploying resources, verifying deployments, testing operators/CRDs, debugging pods, monitoring workloads, or performing end-to-end testing and validation of K8s applications.
---

# Kubernetes Operations and Testing

## Overview

This skill provides comprehensive Kubernetes operations and testing capabilities for both standard K8s resources and custom operators with CRDs. It includes automation scripts for deployment verification, pod debugging, testing workflows, and detailed kubectl command references. Use this skill for any K8s-related deployment, testing, or troubleshooting tasks.

**Project Context:**
- Custom Kubernetes Operator: ARL-Infra (Agentic RL Infrastructure)
- Custom CRDs: WarmPool, Sandbox, Task
- Python tooling: Managed with `uv` (PyYAML installed)
- Deployment targets: Minikube (local) and standard K8s clusters

## Quick Start

### Deploy and Verify Operator

```bash
# Minikube: Build and deploy operator with CRDs using Helm
make docker-build && helm upgrade --install arl-operator charts/arl-operator \
  --namespace arl-system --create-namespace --set crds.install=true --wait

# Standard K8s: Build, push and deploy
REGISTRY=your-registry.com make k8s-build-push && \
  helm upgrade --install arl-operator charts/arl-operator \
  --namespace arl-system --create-namespace \
  --set crds.install=true \
  --set image.repository=your-registry.com/arl-operator \
  --set sidecar.image.repository=your-registry.com/arl-sidecar \
  --wait

# Verify operator is running
kubectl get pods -n arl-system

# Check CRDs are installed
kubectl get crds | grep arl.infra.io
```

### Deploy Custom Resources

```bash
# Deploy sample WarmPool, Sandbox, Task
kubectl apply -f config/samples/

# Check resource status
kubectl get warmpools,sandboxes,tasks
kubectl get warmpool python-3.9-std -o jsonpath='{.status.phase}'
```

### Deploy and Verify Standard Resources

```bash
# Deploy resources
kubectl apply -f <file.yaml>

# Verify deployment health (automated)
uv run python scripts/verify_deployment.py <deployment-name> <namespace>

# Wait for pod readiness
scripts/wait_for_pod.sh <namespace> <pod-pattern> 300
```

### Debug Issues

```bash
# Comprehensive pod debugging
scripts/debug_pod.sh <namespace> <pod-name>

# Check operator logs
kubectl logs -n arl-system -l app=arl-operator --tail=100 -f

# Check sidecar logs in warmpool pods
kubectl logs <pod-name> -c sidecar

# Execute test commands
scripts/exec_test.sh <namespace> <pod-pattern> <command>
```

## Core Capabilities

### 0. Python Tooling with UV

All Python scripts and tools are managed with `uv`:

```bash
# Run Python scripts (uv automatically manages environment)
uv run python scripts/verify_deployment.py <args>

# Install additional packages if needed
uv add <package-name>

# Current installed packages: pyyaml
```

### 1. Custom Resource Management

#### CRD Operations

```bash
# Install CRDs via Helm
helm upgrade --install arl-operator charts/arl-operator \
  --namespace arl-system --create-namespace \
  --set crds.install=true --wait

# Or manually apply CRDs only
kubectl apply -f charts/arl-operator/crds/

# List installed CRDs
kubectl get crds | grep arl.infra.io

# View CRD definition
kubectl get crd warmpools.arl.infra.io -o yaml

# Uninstall operator and CRDs
helm uninstall arl-operator -n arl-system --wait
```

#### Working with Custom Resources

**WarmPool (Pod Pool Management):**
```bash
# Create warmpool
kubectl apply -f config/samples/warmpool.yaml

# Check status
kubectl get warmpool python-3.9-std
kubectl get warmpool python-3.9-std -o jsonpath='{.status.readyReplicas}'

# Watch pool state
kubectl get warmpool python-3.9-std -o yaml -w

# List all warmpools
kubectl get warmpools
```

**Sandbox (Agent Workspace):**
```bash
# Create sandbox (allocates pod from warmpool)
kubectl apply -f config/samples/sandbox.yaml

# Check phase (Pending -> Bound -> Ready)
kubectl get sandbox my-agent-workspace -o jsonpath='{.status.phase}'

# Get allocated pod details
kubectl get sandbox my-agent-workspace -o jsonpath='{.status.podName}'
kubectl get sandbox my-agent-workspace -o jsonpath='{.status.podIP}'

# List all sandboxes
kubectl get sandboxes
```

**Task (Code Execution):**
```bash
# Submit task for execution
kubectl apply -f config/samples/task.yaml

# Watch task state (Pending -> Running -> Succeeded/Failed)
kubectl get task my-task -o jsonpath='{.status.state}' -w

# Get execution result
kubectl get task my-task -o jsonpath='{.status.exitCode}'
kubectl get task my-task -o jsonpath='{.status.stdout}'
kubectl get task my-task -o jsonpath='{.status.stderr}'

# List all tasks
kubectl get tasks
```

### 2. Operator Management

```bash
# Deploy operator (minikube)
make deploy

# Deploy operator (standard K8s)
make k8s-deploy

# Check operator status
kubectl get deployment -n arl-system
kubectl get pods -n arl-system

# View operator logs
make logs
# Or:
kubectl logs -n arl-system -l app=arl-operator --tail=100 -f

# Restart operator
kubectl rollout restart deployment -n arl-system

# Remove operator
make undeploy  # minikube
make k8s-undeploy  # standard K8s
```

### 3. Deployment Verification

Use `scripts/verify_deployment.py` to automatically verify deployment health:

```bash
# Usage (with uv)
uv run python scripts/verify_deployment.py <deployment-name> [namespace] [timeout]

# Example
uv run python scripts/verify_deployment.py arl-operator arl-system 300
```

The script:
- Checks deployment replica status (desired vs ready vs available)
- Verifies all pods are running and healthy
- Validates container readiness
- Waits with timeout for full deployment
- Returns exit code 0 on success, 1 on failure

### 4. Pod Waiting and Readiness

Use `scripts/wait_for_pod.sh` to wait for pods to reach Running state:

```bash
# Usage
scripts/wait_for_pod.sh <namespace> <pod-pattern> <timeout-seconds>

# Example
scripts/wait_for_pod.sh default "operator" 300
```

Useful for CI/CD pipelines and sequential testing workflows.

### 5. Pod Debugging

Use `scripts/debug_pod.sh` for comprehensive debugging information:

```bash
# Usage
scripts/debug_pod.sh <namespace> <pod-name>

# Example
scripts/debug_pod.sh default my-operator-abc123
```

Collects:
- Pod status and details
- Container logs (current and previous)
- Events related to the pod
- Resource constraints and issues

### 6. Test Execution

Use `scripts/exec_test.sh` to run commands in pods for testing:

```bash
# Usage
scripts/exec_test.sh <namespace> <pod-pattern> <command>

# Examples
scripts/exec_test.sh default "sidecar" curl -f http://localhost:8080/health
scripts/exec_test.sh default "operator" ps aux
```

Automatically finds the first running pod matching the pattern.

## Testing Workflows

### Complete Operator Deployment Test (Minikube)

```bash
# 1. Start minikube
make minikube-start

# 2. Build Docker images
make docker-build

# 3. Deploy operator and CRDs
make deploy

# 4. Verify operator deployment
uv run python scripts/verify_deployment.py arl-operator arl-system 300

# 5. Deploy sample resources
make deploy-samples

# 6. Verify WarmPool creates pods
kubectl get pods -l warmpool=python-3.9-std
kubectl get warmpool python-3.9-std -o jsonpath='{.status.readyReplicas}'

# 7. Verify Sandbox allocation
kubectl get sandbox my-agent-workspace -o jsonpath='{.status.phase}'

# 8. Check Task execution
kubectl get task my-task -o jsonpath='{.status.state}'
kubectl get task my-task -o yaml

# 9. Check operator logs for issues
make logs

# 10. Cleanup
make undeploy
```

### Complete Operator Deployment Test (Standard K8s)

```bash
# 1. Build and push images
REGISTRY=your-registry.com make k8s-build-push

# 2. Deploy operator and CRDs
make k8s-deploy

# 3. Verify operator deployment
uv run python scripts/verify_deployment.py arl-operator arl-system 300

# 4. Deploy sample resources (ensure image registry matches)
kubectl apply -f config/samples/

# 5. Verify resources
kubectl get warmpools,sandboxes,tasks

# 6. Check operator logs
kubectl logs -n arl-system -l app=arl-operator --tail=100

# 7. Cleanup
make k8s-undeploy
```

### CRD Testing Workflow

```bash
# 1. Validate CRD YAML before applying
kubectl apply --dry-run=client -f charts/arl-operator/crds/arl.infra.io_warmpools.yaml
kubectl apply --dry-run=server -f charts/arl-operator/crds/arl.infra.io_warmpools.yaml

# 2. Install operator with CRDs via Helm
helm upgrade --install arl-operator charts/arl-operator \
  --namespace arl-system --create-namespace --set crds.install=true --wait

# 3. Verify CRD registration
kubectl get crds | grep arl.infra.io

# 4. Test with invalid resource (should fail validation)
kubectl apply -f invalid-resource.yaml

# 5. Apply valid resource
kubectl apply -f config/samples/warmpool.yaml

# 6. Watch status updates from operator
kubectl get warmpool python-3.9-std -o yaml -w

# 7. Check operator reconciliation
kubectl logs -n arl-system -l app=arl-operator | grep "Reconciling WarmPool"

# 8. Delete resource and check cleanup
kubectl delete warmpool python-3.9-std
kubectl get pods  # Should see pods being terminated
```

### Sidecar Communication Testing

```bash
# 1. Get pod from warmpool
POD_NAME=$(kubectl get pods -l warmpool=python-3.9-std -o jsonpath='{.items[0].metadata.name}')

# 2. Check sidecar is running
kubectl logs $POD_NAME -c sidecar

# 3. Port-forward to sidecar
kubectl port-forward $POD_NAME 8080:8080 &

# 4. Test sidecar API
curl -X POST http://localhost:8080/files -d '{"path":"/tmp/test.txt","content":"hello"}'
curl -X POST http://localhost:8080/execute -d '{"command":["cat","/tmp/test.txt"]}'

# 5. Or test from within cluster
scripts/exec_test.sh default $POD_NAME curl http://localhost:8080/health
```

### State Transition Testing

```bash
# Test Sandbox lifecycle
# 1. Watch sandbox state transitions
kubectl get sandbox test-sandbox -o jsonpath='{.status.phase}' -w &

# 2. Create sandbox
kubectl apply -f - <<EOF
apiVersion: arl.infra.io/v1alpha1
kind: Sandbox
metadata:
  name: test-sandbox
spec:
  poolRef: python-3.9-std
  keepAlive: true
EOF

# Expected phases: Pending -> Bound -> Ready

# Test Task lifecycle
# 1. Watch task state transitions
kubectl get task test-task -o jsonpath='{.status.state}' -w &

# 2. Submit task
kubectl apply -f - <<EOF
apiVersion: arl.infra.io/v1alpha1
kind: Task
metadata:
  name: test-task
spec:
  sandboxRef: test-sandbox
  timeout: 30s
  steps:
    - name: test_step
      type: Command
      command: ["echo", "testing"]
EOF

# Expected states: Pending -> Running -> Succeeded

# 3. Verify result
kubectl get task test-task -o jsonpath='{.status.stdout}'
```

### Standard Deployment Test

```bash
# 1. Validate YAML
kubectl apply --dry-run=client -f deployment.yaml

# 2. Deploy
kubectl apply -f deployment.yaml

# 3. Verify deployment
uv run python scripts/verify_deployment.py my-app default 300

# 4. Test functionality
scripts/exec_test.sh default my-app curl http://localhost:8080/health

# 5. Check logs if issues
scripts/debug_pod.sh default <pod-name>
```

### Operator Testing Workflow (Legacy - use CRD Testing Workflow above)

```bash
# 1. Deploy operator
kubectl apply -f config/operator/deployment.yaml

# 2. Wait for operator readiness
scripts/wait_for_pod.sh default "operator" 120

# 3. Deploy custom resource
kubectl apply -f config/samples/resource.yaml

# 4. Watch CR status
kubectl get <resource-type> <name> -o jsonpath='{.status.phase}' -w

# 5. Verify operator logs
kubectl logs -l app=operator --tail=50 -f

# 6. Test state transitions
kubectl get <resource-type> <name> -o yaml
```

### Integration Testing

For end-to-end integration tests:

```bash
# Create test namespace
kubectl create namespace test-integration

# Deploy all components
kubectl apply -f manifests/ -n test-integration

# Wait for all pods ready
kubectl wait --for=condition=ready pod --all -n test-integration --timeout=300s

# Run functional tests
scripts/exec_test.sh test-integration app "curl -f http://api-service/test"

# Cleanup
kubectl delete namespace test-integration
```

## Advanced Testing Patterns

For detailed testing patterns, see [references/testing_patterns.md](references/testing_patterns.md):

- Pre-deployment validation strategies
- Custom Resource (CRD) testing
- State transition verification
- Performance and load testing
- Network connectivity testing
- Troubleshooting checklist

## Kubectl Command Reference

For comprehensive kubectl usage, see [references/kubectl_reference.md](references/kubectl_reference.md):

- Essential commands (get, describe, logs, exec)
- Status and monitoring (rollout, top, events)
- Advanced operations (wait, patch, scale)
- Debugging techniques
- Output formatting (jsonpath, custom columns)
- Useful command combinations

## Common Scenarios

### Working with Custom Resources

#### Create and Monitor WarmPool

```bash
# Create warmpool
cat <<EOF | kubectl apply -f -
apiVersion: arl.infra.io/v1alpha1
kind: WarmPool
metadata:
  name: my-pool
spec:
  replicas: 5
  template:
    spec:
      containers:
        - name: executor
          image: python:3.9-slim
          command: ["/bin/sh", "-c", "sleep infinity"]
EOF

# Monitor pool status
watch kubectl get warmpool my-pool -o custom-columns=NAME:.metadata.name,READY:.status.readyReplicas,ALLOCATED:.status.allocatedReplicas

# List pods created by warmpool
kubectl get pods -l warmpool=my-pool

# Scale warmpool
kubectl patch warmpool my-pool --type='merge' -p '{"spec":{"replicas":10}}'
```

#### Debug Sandbox Allocation Issues

```bash
# Check sandbox is stuck in Pending
kubectl get sandbox my-sandbox

# Check if warmpool has available pods
kubectl get warmpool <pool-name> -o jsonpath='{.status.readyReplicas}'

# Check operator logs for allocation errors
kubectl logs -n arl-system -l app=arl-operator | grep "Sandbox.*my-sandbox"

# Describe sandbox for events
kubectl describe sandbox my-sandbox

# Check if pod exists but not bound
kubectl get pods -l sandbox=my-sandbox
```

#### Debug Task Execution Failures

```bash
# Get task status
kubectl get task my-task -o yaml

# Check exit code and error output
kubectl get task my-task -o jsonpath='{.status.exitCode}'
kubectl get task my-task -o jsonpath='{.status.stderr}'

# Check sandbox pod logs
SANDBOX_NAME=$(kubectl get task my-task -o jsonpath='{.spec.sandboxRef}')
POD_NAME=$(kubectl get sandbox $SANDBOX_NAME -o jsonpath='{.status.podName}')
kubectl logs $POD_NAME -c sidecar

# Check operator logs
kubectl logs -n arl-system -l app=arl-operator | grep "Task.*my-task"
```

### Verify HTTP Service

```bash
# Port-forward
kubectl port-forward service/<service-name> 8080:80 &

# Test endpoint
curl -f http://localhost:8080/health

# Or test from within cluster
scripts/exec_test.sh default <pod-pattern> curl -f http://<service-name>/health
```

### Check Configuration

```bash
# Verify environment variables
kubectl exec <pod-name> -- env | grep <VAR_NAME>

# Check mounted config
kubectl exec <pod-name> -- cat /etc/config/config.yaml

# Verify secrets mounted
kubectl exec <pod-name> -- ls -la /etc/secrets/
```

### Monitor Resources

```bash
# Pod resource usage
kubectl top pods -n <namespace>

# Watch deployment updates
kubectl get deployment <name> -n <namespace> -w

# Stream logs
kubectl logs -f <pod-name> -n <namespace>
```

### Troubleshooting Checklist

When deployments fail:

1. Check pod status: `kubectl get pods -n <namespace>`
2. Run debug script: `scripts/debug_pod.sh <namespace> <pod-name>`
3. Review events: `kubectl get events -n <namespace> --sort-by='.lastTimestamp'`
4. Check resource usage: `kubectl top pods -n <namespace>`
5. Verify configuration: Check env vars, configmaps, secrets
6. Test connectivity: Network policies, service discovery
7. Review RBAC: Service account permissions

**Operator-specific troubleshooting:**

1. Check operator is running: `kubectl get pods -n arl-system`
2. Review operator logs: `make logs` or `kubectl logs -n arl-system -l app=arl-operator`
3. Verify CRDs are installed: `kubectl get crds | grep arl.infra.io`
4. Check custom resource status: `kubectl get <resource-type> <name> -o yaml`
5. Check operator reconciliation: `kubectl logs -n arl-system -l app=arl-operator | grep "Reconciling"`
6. Verify RBAC for operator: `kubectl get clusterrole,clusterrolebinding | grep arl`
7. Check sidecar logs in pods: `kubectl logs <pod-name> -c sidecar`
8. Test sidecar API: Port-forward and curl endpoints
9. Verify warmpool has ready pods: `kubectl get warmpool <name> -o jsonpath='{.status.readyReplicas}'`
10. Check for resource conflicts: Multiple controllers, duplicate resources

## Project-Specific Notes

### Testing Environment Cleanup

**IMPORTANT: Always clean up test resources before running tests:**

```bash
# Delete all existing tasks (they don't auto-delete)
kubectl delete tasks --all

# Delete test sandboxes
kubectl delete sandboxes --all

# Optional: Restart warmpool to get fresh pods
kubectl delete pods -l app=arl-sandbox
# Operator will recreate pods automatically

# Verify cleanup
kubectl get tasks,sandboxes,pods
```

**Why cleanup is important:**
- Tasks remain in terminal state (Succeeded/Failed) and accumulate
- Old task status may interfere with new test results
- Sandbox allocations may exhaust warmpool capacity
- File changes in pods persist across tasks if using same sandbox

### FilePatch Path Handling

**CRITICAL: FilePatch paths are relative to workDir (`/workspace` by default):**

```yaml
# CORRECT - relative path
steps:
  - name: create_script
    type: FilePatch
    path: hello.py           # Creates /workspace/hello.py
    content: "print('hello')"

# CORRECT - absolute path (creates outside workspace)
steps:
  - name: create_script
    type: FilePatch
    path: /tmp/test.py       # Creates /tmp/test.py
    content: "print('hello')"

# WRONG - this will create /workspace/workspace/hello.py
steps:
  - name: create_script
    type: FilePatch
    path: /workspace/hello.py  # DON'T use full workspace path
    content: "print('hello')"
```

### WarmPool Image Registry

**Remember to use correct image registry:**

```yaml
# For local testing with private registry
spec:
  template:
    spec:
      containers:
        - name: sidecar
          image: 10.10.10.240/library/arl-sidecar:latest
          imagePullPolicy: IfNotPresent  # Important for local registry
```

The operator automatically updates images when deployed via `make k8s-run`.

### Task Step Behavior

**Important task execution characteristics:**

1. **Steps execute sequentially** - if one fails, remaining steps are skipped
2. **FilePatch creates files** but doesn't execute them
3. **Command steps need explicit paths** - use full path or workDir
4. **Environment variables** are only set for specific steps, not globally
5. **WorkDir defaults to `/workspace`** - change with `workDir` field

```yaml
# Example showing proper step sequencing
steps:
  - name: create_script
    type: FilePatch
    path: test.py
    content: |
      import os
      print(f"Current dir: {os.getcwd()}")
  
  - name: run_script
    type: Command
    command: ["python", "/workspace/test.py"]  # Use full path
    env:
      MY_VAR: "value"  # Only available in this step
```

### Rebuilding After Code Changes

**After modifying operator or sidecar code:**

```bash
# 1. Format and check code
make fmt && make vet

# 2. Rebuild and push images
make docker-build docker-push

# 3. Restart operator to use new image
kubectl rollout restart deployment -n arl-system arl-operator

# 4. Delete old warmpool pods to get new sidecar
kubectl delete pods -l app=arl-sandbox

# 5. Verify new images are running
kubectl get pods -o jsonpath='{.items[*].spec.containers[*].image}' | tr ' ' '\n' | grep arl
```

## Best Practices

### Use Validation Before Apply

Always validate YAML before deploying:

```bash
kubectl apply --dry-run=client -f <file.yaml>
kubectl apply --dry-run=server -f <file.yaml>
```

For custom resources, this also validates against CRD schema.

### Wait for Readiness

Don't assume immediate readiness. Use wait commands:

```bash
kubectl rollout status deployment/<name>
kubectl wait --for=condition=ready pod/<name> --timeout=300s

# For custom resources, watch status field
kubectl get warmpool <name> -o jsonpath='{.status.phase}' -w
```

### Operator Development Best Practices

1. **Always update CRDs after modifying types:**
   ```bash
   make generate  # Update generated code
   make manifests # Update CRD YAML
   ```

2. **Test locally before deploying:**
   ```bash
   # Run operator locally
   go run cmd/operator/main.go
   ```

3. **Use namespace for operator isolation:**
   - Operator runs in `arl-system` namespace
   - Resources can be in any namespace
   
4. **Monitor operator reconciliation loops:**
   ```bash
   kubectl logs -n arl-system -l app=arl-operator -f | grep "Reconciling"
   ```

5. **Version CRDs properly:**
   - Current version: `v1alpha1`
   - Plan migration path for breaking changes

### Python Scripting with UV

1. **Always use `uv run` for scripts:**
   ```bash
   uv run python scripts/verify_deployment.py <args>
   ```

2. **Add dependencies as needed:**
   ```bash
   uv add <package-name>
   ```

3. **Don't install packages globally** - let uv manage the environment

### Use Labels for Testing

Label test resources for easy cleanup:

```bash
kubectl apply -f test.yaml -l test-run=abc123
kubectl delete all -l test-run=abc123
```

### Capture State for Debugging

When reporting issues, capture full state:

```bash
kubectl get all -n <namespace> -o yaml > state.yaml
scripts/debug_pod.sh <namespace> <pod-name> > debug.log

# For operator issues, capture CRD state
kubectl get warmpools,sandboxes,tasks -A -o yaml > custom-resources.yaml
kubectl logs -n arl-system -l app=arl-operator > operator.log
```

## Project-Specific Workflows

### Makefile Commands Reference

**Development:**
- `make fmt` - Format Go code
- `make vet` - Run Go vet
- `make tidy` - Run go mod tidy
- `make build` - Build operator and sidecar binaries

**Docker (Minikube):**
- `make docker-build` - Build images for minikube
- `make docker-build-operator` - Build operator image only
- `make docker-build-sidecar` - Build sidecar image only

**Docker (Standard K8s):**
- `make docker-build-k8s` - Build and tag for registry
- `make docker-push` - Push images to registry
- `make k8s-build-push` - Build and push in one command

**Minikube:**
- `make minikube-start` - Start minikube cluster
- `make minikube-stop` - Stop minikube
- `make minikube-delete` - Delete minikube cluster

**Deployment (Minikube):**
- `helm upgrade --install arl-operator charts/arl-operator --namespace arl-system --create-namespace --set crds.install=true --wait` - Deploy operator with CRDs
- `kubectl apply -f config/samples/` - Deploy sample resources
- `helm uninstall arl-operator -n arl-system --wait` - Remove operator and CRDs
- `kubectl apply -f charts/arl-operator/crds/` - Install CRDs only

**Deployment (Standard K8s):**
- `helm upgrade --install arl-operator charts/arl-operator --namespace arl-system --create-namespace --set crds.install=true --set image.repository=<registry>/arl-operator --set sidecar.image.repository=<registry>/arl-sidecar --wait` - Deploy to K8s cluster
- `helm uninstall arl-operator -n arl-system --wait` - Remove from K8s cluster

**Testing:**
- `make test-integration` - Run integration tests
- `make logs` - Show operator logs

**All-in-one:**
- `make quickstart` - Start minikube and deploy with samples
- `make quickstart-dev` - Start in dev mode with auto-reload

### Custom Resource API

**WarmPool Spec:**
```yaml
spec:
  replicas: 3              # Number of idle pods to maintain
  template:                # Standard pod template
    spec:
      containers:
        - name: executor
          image: python:3.9-slim
```

**WarmPool Status:**
```yaml
status:
  readyReplicas: 3         # Number of ready idle pods
  allocatedReplicas: 2     # Number of allocated pods
  phase: Ready             # Overall pool status
```

**Sandbox Spec:**
```yaml
spec:
  poolRef: python-3.9-std  # WarmPool to allocate from
  keepAlive: true          # Keep pod after tasks complete
  resources:               # Optional resource overrides
    limits:
      memory: "1Gi"
```

**Sandbox Status:**
```yaml
status:
  phase: Ready             # Pending | Bound | Ready | Failed
  podName: pool-pod-123    # Allocated pod name
  podIP: 10.244.1.5        # Pod IP address
  workDir: /workspace      # Working directory path
```

**Task Spec:**
```yaml
spec:
  sandboxRef: my-sandbox   # Target sandbox
  timeout: 30s             # Maximum execution time
  steps:
    - name: write_file
      type: FilePatch
      content: "print('hello')"
    - name: run_code
      type: Command
      command: ["python", "test.py"]
```

**Task Status:**
```yaml
status:
  state: Succeeded         # Pending | Running | Succeeded | Failed
  exitCode: 0              # Command exit code
  stdout: "hello\n"        # Standard output
  stderr: ""               # Standard error
  duration: 1.2s           # Execution time
```

## Resources

### scripts/

Automation scripts for common K8s operations:

- `verify_deployment.py` - Automated deployment health verification
- `wait_for_pod.sh` - Wait for pod readiness with timeout
- `debug_pod.sh` - Comprehensive pod debugging information
- `exec_test.sh` - Execute test commands in pods

### references/

Detailed documentation and patterns:

- `testing_patterns.md` - Comprehensive testing workflows and patterns
- `kubectl_reference.md` - Complete kubectl command reference and examples
