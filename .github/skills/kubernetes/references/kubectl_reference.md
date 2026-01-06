# Kubectl Command Reference

## Essential Commands

### Get Resources

```bash
# Get all resources
kubectl get all -n <namespace>

# Get specific resource type
kubectl get pods -n <namespace>
kubectl get deployments -n <namespace>
kubectl get services -n <namespace>
kubectl get configmaps -n <namespace>
kubectl get secrets -n <namespace>

# Get with labels
kubectl get pods -l app=myapp -n <namespace>

# Get with output formats
kubectl get pod <name> -o yaml
kubectl get pod <name> -o json
kubectl get pod <name> -o wide
kubectl get pod <name> -o jsonpath='{.status.phase}'

# Watch resources
kubectl get pods -w -n <namespace>
```

### Describe Resources

```bash
# Detailed information
kubectl describe pod <name> -n <namespace>
kubectl describe deployment <name> -n <namespace>
kubectl describe node <name>

# Events are shown at the bottom of describe output
```

### Logs

```bash
# Get pod logs
kubectl logs <pod-name> -n <namespace>

# Specific container in multi-container pod
kubectl logs <pod-name> -c <container-name> -n <namespace>

# Follow logs
kubectl logs -f <pod-name> -n <namespace>

# Previous container logs (if restarted)
kubectl logs <pod-name> --previous -n <namespace>

# Last N lines
kubectl logs <pod-name> --tail=100 -n <namespace>

# All pods with label
kubectl logs -l app=myapp -n <namespace> --all-containers=true
```

### Execute Commands

```bash
# Execute command in pod
kubectl exec <pod-name> -n <namespace> -- <command>

# Interactive shell
kubectl exec -it <pod-name> -n <namespace> -- /bin/bash
kubectl exec -it <pod-name> -n <namespace> -- /bin/sh

# Specific container
kubectl exec <pod-name> -c <container-name> -n <namespace> -- <command>
```

### Apply and Create

```bash
# Apply from file
kubectl apply -f <file.yaml>
kubectl apply -f <directory>/

# Apply with dry-run
kubectl apply --dry-run=client -f <file.yaml>
kubectl apply --dry-run=server -f <file.yaml>

# Create from command
kubectl create deployment <name> --image=<image>
kubectl create service clusterip <name> --tcp=80:8080
kubectl create configmap <name> --from-file=<path>
kubectl create secret generic <name> --from-literal=key=value
```

### Delete Resources

```bash
# Delete from file
kubectl delete -f <file.yaml>

# Delete by name
kubectl delete pod <name> -n <namespace>
kubectl delete deployment <name> -n <namespace>

# Delete with label selector
kubectl delete pods -l app=myapp -n <namespace>

# Force delete stuck pod
kubectl delete pod <name> -n <namespace> --force --grace-period=0
```

### Port Forwarding

```bash
# Forward local port to pod port
kubectl port-forward <pod-name> 8080:80 -n <namespace>

# Forward to service
kubectl port-forward service/<service-name> 8080:80 -n <namespace>

# Background port forward
kubectl port-forward <pod-name> 8080:80 -n <namespace> &
```

### Copy Files

```bash
# Copy from pod to local
kubectl cp <namespace>/<pod-name>:/path/to/file ./local-file

# Copy from local to pod
kubectl cp ./local-file <namespace>/<pod-name>:/path/to/file

# Specific container
kubectl cp <namespace>/<pod-name>:/path/to/file ./local-file -c <container-name>
```

## Status and Monitoring

### Rollout Management

```bash
# Check rollout status
kubectl rollout status deployment/<name> -n <namespace>

# Rollout history
kubectl rollout history deployment/<name> -n <namespace>

# Rollback to previous version
kubectl rollout undo deployment/<name> -n <namespace>

# Rollback to specific revision
kubectl rollout undo deployment/<name> --to-revision=2 -n <namespace>

# Pause/Resume rollout
kubectl rollout pause deployment/<name> -n <namespace>
kubectl rollout resume deployment/<name> -n <namespace>
```

### Resource Usage

```bash
# Pod resource usage
kubectl top pods -n <namespace>
kubectl top pods -n <namespace> --containers

# Node resource usage
kubectl top nodes

# Sort by CPU
kubectl top pods -n <namespace> --sort-by=cpu

# Sort by memory
kubectl top pods -n <namespace> --sort-by=memory
```

### Events

```bash
# Get all events
kubectl get events -n <namespace>

# Sort by timestamp
kubectl get events -n <namespace> --sort-by='.lastTimestamp'

# Watch events
kubectl get events -w -n <namespace>

# Filter events for specific object
kubectl get events -n <namespace> --field-selector involvedObject.name=<pod-name>
```

## Advanced Operations

### Wait Operations

```bash
# Wait for condition
kubectl wait --for=condition=ready pod/<name> -n <namespace> --timeout=300s
kubectl wait --for=condition=available deployment/<name> -n <namespace> --timeout=300s

# Wait for deletion
kubectl wait --for=delete pod/<name> -n <namespace> --timeout=60s
```

### Patch Resources

```bash
# Strategic merge patch
kubectl patch deployment <name> -n <namespace> -p '{"spec":{"replicas":3}}'

# JSON patch
kubectl patch deployment <name> -n <namespace> --type=json -p='[{"op": "replace", "path": "/spec/replicas", "value":3}]'

# Patch from file
kubectl patch deployment <name> -n <namespace> --patch-file patch.yaml
```

### Scale

```bash
# Scale deployment
kubectl scale deployment <name> --replicas=3 -n <namespace>

# Autoscale
kubectl autoscale deployment <name> --min=2 --max=10 --cpu-percent=80 -n <namespace>
```

### Labels and Annotations

```bash
# Add label
kubectl label pod <name> env=prod -n <namespace>

# Remove label
kubectl label pod <name> env- -n <namespace>

# Add annotation
kubectl annotate pod <name> description="my pod" -n <namespace>

# Overwrite existing
kubectl label pod <name> env=staging --overwrite -n <namespace>
```

## Context and Configuration

### Context Management

```bash
# View current context
kubectl config current-context

# List all contexts
kubectl config get-contexts

# Switch context
kubectl config use-context <context-name>

# Set default namespace
kubectl config set-context --current --namespace=<namespace>
```

### Cluster Info

```bash
# Cluster information
kubectl cluster-info

# Cluster version
kubectl version

# API resources
kubectl api-resources

# API versions
kubectl api-versions
```

## Debugging Commands

### Ephemeral Debug Container (K8s 1.23+)

```bash
# Create debug container in running pod
kubectl debug <pod-name> -it --image=busybox -n <namespace>

# Create copy of pod for debugging
kubectl debug <pod-name> -it --image=<debug-image> --copy-to=<new-name> -n <namespace>

# Debug node
kubectl debug node/<node-name> -it --image=ubuntu
```

### Network Debugging

```bash
# Test DNS
kubectl run -it --rm debug --image=busybox --restart=Never -- nslookup kubernetes.default

# Test connectivity
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- curl http://<service>

# Network policy testing
kubectl run -it --rm debug --image=nicolaka/netshoot --restart=Never -- bash
```

## Output Formatting

### JSONPath Examples

```bash
# Get pod IPs
kubectl get pods -o jsonpath='{.items[*].status.podIP}'

# Get pod names and status
kubectl get pods -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.status.phase}{"\n"}{end}'

# Get image versions
kubectl get pods -o jsonpath='{.items[*].spec.containers[*].image}'

# Get node names
kubectl get nodes -o jsonpath='{.items[*].metadata.name}'
```

### Go Template

```bash
# Custom columns
kubectl get pods -o custom-columns=NAME:.metadata.name,STATUS:.status.phase,IP:.status.podIP

# With go template
kubectl get pods -o go-template='{{range .items}}{{.metadata.name}}{{"\n"}}{{end}}'
```

## Useful Combinations

```bash
# Find pods not running
kubectl get pods -A --field-selector=status.phase!=Running

# Get pod on specific node
kubectl get pods -o wide | grep <node-name>

# Delete all pods with label
kubectl delete pods -l app=myapp -n <namespace>

# Restart deployment (recreate pods)
kubectl rollout restart deployment/<name> -n <namespace>

# Get all images in use
kubectl get pods -A -o jsonpath='{range .items[*]}{.spec.containers[*].image}{"\n"}{end}' | sort -u

# Check pod resource requests/limits
kubectl get pods -o custom-columns=NAME:.metadata.name,CPU-REQUEST:.spec.containers[*].resources.requests.cpu,MEM-REQUEST:.spec.containers[*].resources.requests.memory
```
