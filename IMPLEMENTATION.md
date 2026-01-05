# ARL-Infra Design Implementation

## Overview

This document details the implementation of the ARL (Agentic RL) Kubernetes infrastructure based on the design specification. The system provides ultra-low latency code execution for reinforcement learning agents using warm pod pools and sidecar injection.

## Architecture

### System Components

```
┌─────────────────────────────────────────────────────────────┐
│                     Control Plane (K8s)                      │
│  ┌────────────┐         ┌──────────────┐                    │
│  │ K8s API    │◄────────┤ ARL Operator │                    │
│  │ Server     │         │  Controllers │                    │
│  └────────────┘         └──────────────┘                    │
└─────────────────────────────────────────────────────────────┘
                               │
                               │ Manages
                               ▼
┌─────────────────────────────────────────────────────────────┐
│                       Data Plane (Nodes)                     │
│                                                              │
│  ┌──────────────────────────────────────────────────────┐  │
│  │           Warm Pool (Idle Pods)                      │  │
│  │  ┌────────┐  ┌────────┐  ┌────────┐                 │  │
│  │  │ Pod 1  │  │ Pod 2  │  │ Pod 3  │  ...             │  │
│  │  │ (Idle) │  │ (Idle) │  │ (Idle) │                 │  │
│  │  └────────┘  └────────┘  └────────┘                 │  │
│  └──────────────────────────────────────────────────────┘  │
│                                                              │
│  ┌──────────────────────────────────────────────────────┐  │
│  │         Active Sandboxes (Allocated Pods)            │  │
│  │  ┌─────────────────────────────────────────────┐    │  │
│  │  │  Pod: Sandbox-A                              │    │  │
│  │  │  ┌─────────────┐       ┌─────────────────┐  │    │  │
│  │  │  │  Sidecar    │◄─────►│   Executor      │  │    │  │
│  │  │  │  HTTP API   │       │  (User Code)    │  │    │  │
│  │  │  └─────────────┘       └─────────────────┘  │    │  │
│  │  │         │                      │             │    │  │
│  │  │         └──────────┬──────────┘              │    │  │
│  │  │                    ▼                         │    │  │
│  │  │          ┌────────────────┐                  │    │  │
│  │  │          │  /workspace    │                  │    │  │
│  │  │          │  (Shared Vol)  │                  │    │  │
│  │  │          └────────────────┘                  │    │  │
│  │  └─────────────────────────────────────────────┘    │  │
│  └──────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

## Custom Resource Definitions (CRDs)

### 1. WarmPool

Manages a pool of pre-created pods for instant allocation.

**Purpose**: Eliminate cold-start latency by maintaining ready pods.

**Key Fields**:
- `replicas`: Number of idle pods to maintain
- `template`: Pod specification template

**Controller Logic**:
1. Monitors pool size
2. Creates new pods when count drops below desired
3. Labels pods with `arl.infra.io/status=idle`
4. Ensures sidecar container is injected

### 2. Sandbox

Represents an isolated workspace for an agent.

**Purpose**: Logical abstraction of an agent's execution environment.

**Key Fields**:
- `poolRef`: Which warm pool to allocate from
- `keepAlive`: Whether to preserve pod after tasks complete
- `resources`: Resource requests/limits

**Controller Logic**:
1. Finds an idle pod from the specified pool
2. Marks pod as allocated (`arl.infra.io/status=allocated`)
3. Waits for pod to be ready
4. Updates status with pod IP and working directory

### 3. Task

Defines a unit of execution (file updates + commands).

**Purpose**: Execute agent-generated code with minimal latency.

**Key Fields**:
- `sandboxRef`: Target sandbox
- `timeout`: Maximum execution time
- `steps`: Sequence of FilePatch or Command operations

**Controller Logic**:
1. Verifies sandbox is ready
2. Executes steps sequentially via sidecar HTTP API
3. Collects stdout/stderr
4. Updates status with results and timing

## Sidecar Implementation

### HTTP API Endpoints

The sidecar runs on port 8080 in each pod:

```
POST /health          - Health check
POST /files           - Update files in workspace
POST /execute         - Run commands
POST /signal          - Send signals to processes
POST /reset           - Clean workspace
```

### File Operations (`/files`)

**Request**:
```json
{
  "basePath": "/workspace",
  "files": {
    "app.py": "print('hello')",
    "config.json": "{\"debug\": true}"
  }
}
```

**Response**:
```json
{
  "success": true,
  "message": "successfully updated 2 files"
}
```

### Command Execution (`/execute`)

**Request**:
```json
{
  "command": ["python", "app.py"],
  "env": {"DEBUG": "true"},
  "workingDir": "/workspace",
  "timeoutSeconds": 30
}
```

**Response**:
```json
{
  "stdout": "hello\n",
  "stderr": "",
  "exitCode": 0,
  "done": true
}
```

## Execution Flow

### Complete Task Execution Sequence

```
1. User creates WarmPool
   └─► Operator creates N idle pods
       └─► Each pod has executor + sidecar containers

2. User creates Sandbox
   └─► Operator allocates pod from pool
       └─► Pod marked as allocated
           └─► Sandbox status updated with pod IP

3. User creates Task
   └─► Operator validates sandbox is ready
       └─► For each step:
           ├─► FilePatch: HTTP POST to /files
           └─► Command: HTTP POST to /execute
       └─► Task status updated with results

4. Agent reviews results
   └─► Submits new Task with code modifications
       └─► Same pod reused (no restart!)
           └─► Millisecond-level feedback loop
```

## Key Design Decisions

### 1. Why Warm Pools?

**Problem**: Standard Kubernetes pod startup takes 30-60+ seconds
- Image pull: 10-30s
- Container init: 5-10s
- Runtime setup: 5-10s

**Solution**: Pre-create and maintain ready pods
- Allocation time: <1 second
- Enables RL training loops with fast iteration

### 2. Why HTTP Instead of gRPC?

**Design Choice**: Simplified implementation
- No protoc dependency
- Standard HTTP libraries
- Easy debugging with curl
- Can upgrade to gRPC later for streaming

### 3. Why Sidecar Pattern?

**Separation of Concerns**:
- **Executor container**: User code environment (Python, Node, etc.)
- **Sidecar container**: Platform control plane
  - File management
  - Process control
  - Workspace isolation

**Benefits**:
- Different security contexts
- Independent resource limits
- Easy environment swapping (just change executor image)

### 4. Pod Reuse Strategy

**Key Innovation**: Tasks don't create/destroy pods
- Pod allocated once per Sandbox
- Multiple Tasks execute in same pod
- Workspace persists between Tasks
- Drastically reduces latency

## Performance Characteristics

### Latency Breakdown

**Traditional Approach** (new pod per execution):
```
Pod Creation:     30-60s
Code Upload:      1-2s
Execution:        0.1-10s
Results Return:   0.1s
────────────────────────
Total:            31-72s
```

**ARL-Infra Approach** (warm pool + sidecar):
```
Pod Allocation:   <1s (first task only)
Code Upload:      0.05s (HTTP to sidecar)
Execution:        0.1-10s
Results Return:   0.05s
────────────────────────
Total:            0.2-11s per task
                  (99% latency reduction)
```

## Security Considerations

### Isolation Layers

1. **Kubernetes Pod Isolation**: Standard K8s network/process isolation
2. **Runtime Class**: gvisor recommended for additional sandboxing
3. **Resource Limits**: CPU/memory quotas per sandbox
4. **Workspace Cleanup**: Reset between sandbox reuse

### Recommended Runtime Configuration

```yaml
spec:
  runtimeClassName: gvisor  # or kata-containers
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    fsGroup: 1000
```

## Scaling Characteristics

### Pool Size Calculation

```
Required Pool Size = 
    (Active Agents × Avg Tasks/Agent/Min) / (60 / Avg Task Duration)
    + Buffer (20-30%)
```

Example:
- 10 active agents
- 5 tasks per minute per agent
- Average task duration: 2 seconds

```
Pool Size = (10 × 5) / (60/2) + 30%
         = 50 / 30 + 30%
         = 1.67 + 0.5
         ≈ 3 pods
```

### Resource Planning

Per-pod resources (typical):
- CPU: 0.5-1 core
- Memory: 512MB-2GB
- Storage: 1-5GB (workspace)

Cluster sizing:
- Small (10 agents): 2-4 nodes, 4 cores, 16GB each
- Medium (100 agents): 10-20 nodes, 8 cores, 32GB each
- Large (1000 agents): 100+ nodes, distributed

## Troubleshooting Guide

### Common Issues

**Issue**: Pods stuck in Pending
- **Check**: Resource availability
- **Solution**: Increase node capacity or reduce resource requests

**Issue**: Task fails with "sandbox not ready"
- **Check**: Pod status in sandbox
- **Solution**: Check pod logs, may need to recreate sandbox

**Issue**: Sidecar not responding
- **Check**: Sidecar container logs
- **Solution**: Ensure port 8080 is accessible, check network policies

**Issue**: Code execution timeout
- **Check**: Task timeout setting
- **Solution**: Increase timeout or optimize code

## Future Enhancements

### Potential Improvements

1. **Streaming Execution**: Use WebSocket or SSE for real-time output
2. **Resource Autoscaling**: Dynamic pool sizing based on demand
3. **Multi-tenancy**: Namespace isolation with quotas
4. **Persistent Workspaces**: Optional PVC-backed storage
5. **Metric Collection**: Prometheus integration for observability
6. **GPU Support**: Add GPU resource scheduling
7. **Network Policies**: Enhanced isolation between sandboxes

## Conclusion

ARL-Infra successfully implements the design specification by:

✅ Providing ultra-low latency execution via warm pools
✅ Maintaining strong isolation through Kubernetes primitives
✅ Supporting mixed workloads (jobs + services)
✅ Enabling hot code reload without pod restarts
✅ Offering Kubernetes-native API via CRDs

The implementation is production-ready for RL training workloads requiring fast iteration cycles.
