# CRD Reference

Complete reference for ARL-Infra Custom Resource Definitions.

## WarmPool

A WarmPool maintains a pool of pre-created pods ready for instant allocation.

### Spec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `replicas` | integer | Yes | Number of pods to maintain in the pool |
| `template` | PodTemplateSpec | Yes | Pod template for pool pods |

### Status

| Field | Type | Description |
|-------|------|-------------|
| `ready` | integer | Number of ready (unallocated) pods |
| `allocated` | integer | Number of allocated pods |
| `total` | integer | Total number of pods |
| `phase` | string | Current phase (Pending, Ready, Scaling) |
| `conditions` | []Condition | Detailed status conditions |

### Example

```yaml
apiVersion: arl.infra.io/v1alpha1
kind: WarmPool
metadata:
  name: python-pool
  namespace: default
spec:
  replicas: 3
  template:
    spec:
      containers:
        - name: executor
          image: python:3.9-slim
          command: ["sleep", "infinity"]
          resources:
            requests:
              memory: "256Mi"
              cpu: "100m"
            limits:
              memory: "512Mi"
              cpu: "500m"
          volumeMounts:
            - name: workspace
              mountPath: /workspace
      volumes:
        - name: workspace
          emptyDir: {}
```

### Pod Labels

Pods created by WarmPool have these labels:

| Label | Value | Description |
|-------|-------|-------------|
| `arl.infra.io/warmpool` | Pool name | Identifies the owning pool |
| `arl.infra.io/pod-state` | `ready` or `allocated` | Current pod state |
| `arl.infra.io/sandbox` | Sandbox name | Set when allocated |

---

## Sandbox

A Sandbox represents an allocated workspace - a pod bound from a WarmPool.

### Spec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `poolRef` | string | Yes | Name of the WarmPool to allocate from |
| `keepAlive` | boolean | No | Keep sandbox after task completion (default: false) |
| `ttlSecondsAfterFinished` | integer | No | Auto-delete after this many seconds of idle time |

### Status

| Field | Type | Description |
|-------|------|-------------|
| `phase` | string | Current phase (Pending, Ready, Bound, Released) |
| `podName` | string | Name of the allocated pod |
| `podIP` | string | IP address of the allocated pod |
| `allocatedAt` | timestamp | When the pod was allocated |
| `conditions` | []Condition | Detailed status conditions |

### Phases

| Phase | Description |
|-------|-------------|
| `Pending` | Waiting for pod allocation |
| `Ready` | Pod allocated and ready for tasks |
| `Bound` | Currently executing a task |
| `Released` | Sandbox released (pod returned to pool or deleted) |

### Example

```yaml
apiVersion: arl.infra.io/v1alpha1
kind: Sandbox
metadata:
  name: my-workspace
  namespace: default
spec:
  poolRef: python-pool
  keepAlive: true
  ttlSecondsAfterFinished: 3600  # Auto-delete after 1 hour idle
```

---

## Task

A Task is a unit of work executed in a Sandbox.

### Spec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `sandboxRef` | string | Yes | Name of the Sandbox to execute in |
| `timeout` | duration | No | Maximum execution time (default: 30s) |
| `steps` | []Step | Yes | List of steps to execute |

### Step

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Step name |
| `type` | string | Yes | Step type: `Command` or `FilePatch` |
| `command` | []string | For Command | Command and arguments to execute |
| `path` | string | For FilePatch | File path to create/update |
| `content` | string | For FilePatch | File content |
| `workDir` | string | No | Working directory for commands |
| `env` | map[string]string | No | Environment variables |

### Status

| Field | Type | Description |
|-------|------|-------------|
| `state` | string | Current state (Pending, Running, Succeeded, Failed) |
| `stdout` | string | Standard output from the last command |
| `stderr` | string | Standard error from the last command |
| `exitCode` | integer | Exit code from the last command |
| `startedAt` | timestamp | When execution started |
| `completedAt` | timestamp | When execution completed |
| `steps` | []StepStatus | Status of each step |

### States

| State | Description |
|-------|-------------|
| `Pending` | Waiting to be executed |
| `Running` | Currently executing |
| `Succeeded` | All steps completed successfully |
| `Failed` | One or more steps failed |

### Example: Command Step

```yaml
apiVersion: arl.infra.io/v1alpha1
kind: Task
metadata:
  name: run-python
  namespace: default
spec:
  sandboxRef: my-workspace
  timeout: 60s
  steps:
    - name: execute
      type: Command
      command: ["python", "-c", "print('Hello, World!')"]
      workDir: /workspace
      env:
        DEBUG: "true"
        PYTHONPATH: "/workspace/lib"
```

### Example: FilePatch Step

```yaml
apiVersion: arl.infra.io/v1alpha1
kind: Task
metadata:
  name: write-and-run
  namespace: default
spec:
  sandboxRef: my-workspace
  timeout: 30s
  steps:
    - name: write-script
      type: FilePatch
      path: /workspace/script.py
      content: |
        import os
        print(f"Hello from {os.getcwd()}")
        print("Environment:", os.environ.get("MY_VAR", "not set"))
    
    - name: run-script
      type: Command
      command: ["python", "/workspace/script.py"]
      env:
        MY_VAR: "custom-value"
```

### Example: Multi-Step Pipeline

```yaml
apiVersion: arl.infra.io/v1alpha1
kind: Task
metadata:
  name: data-pipeline
spec:
  sandboxRef: my-workspace
  timeout: 120s
  steps:
    - name: setup
      type: Command
      command: ["pip", "install", "pandas", "numpy"]
    
    - name: create-data
      type: FilePatch
      path: /workspace/data.csv
      content: |
        name,value
        a,1
        b,2
        c,3
    
    - name: create-script
      type: FilePatch
      path: /workspace/process.py
      content: |
        import pandas as pd
        df = pd.read_csv('/workspace/data.csv')
        print(f"Sum: {df['value'].sum()}")
    
    - name: run
      type: Command
      command: ["python", "/workspace/process.py"]
```

---

## Common Patterns

### Reusable Sandbox

For executing multiple tasks in the same environment:

```yaml
# Create a persistent sandbox
apiVersion: arl.infra.io/v1alpha1
kind: Sandbox
metadata:
  name: persistent-workspace
spec:
  poolRef: python-pool
  keepAlive: true
---
# Task 1
apiVersion: arl.infra.io/v1alpha1
kind: Task
metadata:
  name: task-1
spec:
  sandboxRef: persistent-workspace
  steps:
    - name: init
      type: FilePatch
      path: /workspace/state.txt
      content: "initialized"
---
# Task 2 (uses same sandbox)
apiVersion: arl.infra.io/v1alpha1
kind: Task
metadata:
  name: task-2
spec:
  sandboxRef: persistent-workspace
  steps:
    - name: read
      type: Command
      command: ["cat", "/workspace/state.txt"]
```

### Auto-Cleanup

For temporary workspaces that auto-delete:

```yaml
apiVersion: arl.infra.io/v1alpha1
kind: Sandbox
metadata:
  name: temp-workspace
spec:
  poolRef: python-pool
  keepAlive: false
  ttlSecondsAfterFinished: 300  # Delete 5 minutes after last task
```

### Custom Container Image

For specialized environments:

```yaml
apiVersion: arl.infra.io/v1alpha1
kind: WarmPool
metadata:
  name: ml-pool
spec:
  replicas: 2
  template:
    spec:
      containers:
        - name: executor
          image: pytorch/pytorch:2.0.0-cuda11.7-cudnn8-runtime
          command: ["sleep", "infinity"]
          resources:
            limits:
              nvidia.com/gpu: 1
          volumeMounts:
            - name: workspace
              mountPath: /workspace
      volumes:
        - name: workspace
          emptyDir: {}
```
