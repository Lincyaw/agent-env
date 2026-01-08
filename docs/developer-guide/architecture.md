# Architecture

This document describes the architecture of ARL-Infra, including its components, interactions, and design decisions.

## Overview

ARL-Infra consists of two main planes:

- **Control Plane**: The ARL Operator that manages resources and orchestrates execution
- **Data Plane**: Warm pool pods with sidecar agents for code execution

```mermaid
graph TB
    subgraph "User Layer"
        User[User/AI Agent]
        PythonSDK[Python SDK]
        User --> PythonSDK
        User --> |kubectl apply| K8sAPI[Kubernetes API]
        PythonSDK --> |kubernetes client| K8sAPI
    end

    subgraph "Kubernetes Control Plane"
        K8sAPI
        
        subgraph "ARL Operator"
            Operator[Operator Main]
            
            subgraph "Controllers"
                WPCtrl[WarmPool Controller]
                SBCtrl[Sandbox Controller]
                TaskCtrl[Task Controller]
                TTLCtrl[TTL Controller]
            end
            
            subgraph "Webhooks"
                WPWebhook[WarmPool Validator]
                SBWebhook[Sandbox Validator]
                TaskWebhook[Task Validator]
            end
            
            Operator --> WPCtrl
            Operator --> SBCtrl
            Operator --> TaskCtrl
            Operator --> TTLCtrl
            Operator --> WPWebhook
            Operator --> SBWebhook
            Operator --> TaskWebhook
        end
        
        K8sAPI --> |validate| WPWebhook
        K8sAPI --> |validate| SBWebhook
        K8sAPI --> |validate| TaskWebhook
        K8sAPI --> |watch/update| WPCtrl
        K8sAPI --> |watch/update| SBCtrl
        K8sAPI --> |watch/update| TaskCtrl
        K8sAPI --> |watch/delete| TTLCtrl
    end

    subgraph "Kubernetes Data Plane"
        subgraph "Custom Resources"
            WP[WarmPool CRD]
            SB[Sandbox CRD]
            Task[Task CRD]
        end
        
        subgraph "Warm Pool Pods"
            Pod1[Pod 1 - Ready]
            Pod2[Pod 2 - Allocated]
            Pod3[Pod 3 - Ready]
        end
    end

    WPCtrl --> |create/manage| Pod1
    WPCtrl --> |create/manage| Pod2
    WPCtrl --> |create/manage| Pod3
    
    SBCtrl --> |allocate| Pod2
    SBCtrl --> |read| WP
    
    TaskCtrl --> |gRPC| Pod2
    TaskCtrl --> |read| SB
    
    Task --> |references| SB
    SB --> |references| WP
```

## Core Components

### ARL Operator

The operator is the central control component running in the `arl-system` namespace.

| Component | Responsibility |
|-----------|----------------|
| **WarmPool Controller** | Maintains pod pools, ensures desired replica count |
| **Sandbox Controller** | Allocates pods from pools, manages sandbox lifecycle |
| **Task Controller** | Executes tasks via gRPC calls to sidecars |
| **TTL Controller** | Cleans up completed tasks and idle sandboxes |
| **Webhooks** | Validates CRD resources before creation |

### Sidecar Agent

Each pod in the warm pool includes a sidecar container that:

- Runs a gRPC server on port 50051
- Handles file operations (create, update, delete)
- Executes commands in the executor container
- Returns stdout, stderr, and exit codes

```mermaid
graph LR
    subgraph "Pod"
        direction TB
        Sidecar[Sidecar Container<br/>gRPC Server :50051]
        Executor[Executor Container<br/>User Code]
        Volume[(Shared Volume<br/>/workspace)]
        
        Sidecar -.-> Volume
        Executor -.-> Volume
    end
    
    TaskCtrl[Task Controller] --> |gRPC| Sidecar
```

### Custom Resources

#### WarmPool

Defines a pool of pre-created pods.

```yaml
apiVersion: arl.infra.io/v1alpha1
kind: WarmPool
metadata:
  name: python-pool
spec:
  replicas: 3           # Number of pods to maintain
  template:             # Pod template
    spec:
      containers:
        - name: executor
          image: python:3.9-slim
```

#### Sandbox

Represents an allocated workspace.

```yaml
apiVersion: arl.infra.io/v1alpha1
kind: Sandbox
metadata:
  name: my-sandbox
spec:
  poolRef: python-pool  # Which pool to allocate from
  keepAlive: true       # Keep for multiple tasks
```

#### Task

A unit of work to execute.

```yaml
apiVersion: arl.infra.io/v1alpha1
kind: Task
metadata:
  name: my-task
spec:
  sandboxRef: my-sandbox
  timeout: 30s
  steps:
    - name: run
      type: Command
      command: ["python", "-c", "print('hello')"]
```

## Interaction Flow

### Task Execution Flow

```mermaid
sequenceDiagram
    participant User
    participant API as Kubernetes API
    participant WPC as WarmPool Controller
    participant SBC as Sandbox Controller
    participant TC as Task Controller
    participant Pod as Pod (Sidecar)

    Note over User,Pod: Phase 1: Create Pod Pool
    User->>API: Create WarmPool
    API->>WPC: Watch WarmPool
    WPC->>API: Create Pods
    Note over Pod: Pods Ready

    Note over User,Pod: Phase 2: Allocate Sandbox
    User->>API: Create Sandbox
    API->>SBC: Watch Sandbox
    SBC->>API: Query WarmPool
    SBC->>API: Allocate Pod (update labels)
    Note over Pod: Pod Allocated
    SBC->>API: Update Sandbox Status (Ready)

    Note over User,Pod: Phase 3: Execute Task
    User->>API: Create Task
    API->>TC: Watch Task
    TC->>API: Get Sandbox (pod IP)
    TC->>Pod: gRPC: UpdateFiles
    Pod-->>TC: Success
    TC->>Pod: gRPC: Execute
    Pod-->>TC: stdout, stderr, exitCode
    TC->>API: Update Task Status (Succeeded)
    User->>API: Get Task Status
    API-->>User: Results
```

### Data Flow

```mermaid
flowchart LR
    subgraph "Input"
        YAML[YAML/Python SDK]
    end
    
    subgraph "Kubernetes Resources"
        WP[WarmPool]
        SB[Sandbox]
        Task[Task]
    end
    
    subgraph "Pod Execution"
        File[FilePatch]
        Cmd[Command]
    end
    
    subgraph "Output"
        Status[Task.Status<br/>stdout/stderr/exitCode]
    end
    
    YAML --> WP
    WP --> |provides| SB
    YAML --> SB
    SB --> |binds Pod| Task
    YAML --> Task
    Task --> File
    Task --> Cmd
    File --> Status
    Cmd --> Status
```

## Design Decisions

### Why Warm Pools?

| Approach | Latency | Resource Usage | Isolation |
|----------|---------|----------------|-----------|
| Create pod per task | 5-30s | Low | High |
| Shared long-running pod | <100ms | Medium | Low |
| **Warm pool** | <100ms | Medium | High |

Warm pools provide the best balance of low latency and strong isolation.

### Why Sidecar Architecture?

- **Separation of concerns**: Sidecar handles orchestration, executor runs user code
- **Language agnostic**: Any container image can be used as executor
- **Security**: Sidecar has limited permissions, user code is sandboxed
- **Observability**: Sidecar can collect metrics and logs

### Why CRD-based API?

- **Kubernetes-native**: Uses familiar kubectl commands
- **Declarative**: Desired state is explicitly defined
- **Extensible**: Easy to add new resource types
- **Auditable**: All changes tracked by Kubernetes

## Directory Structure

```
agent-env/
├── api/                    # CRD type definitions
│   └── v1alpha1/
├── cmd/
│   ├── operator/          # Operator entrypoint
│   └── sidecar/           # Sidecar entrypoint
├── config/
│   └── crd/               # Generated CRD manifests
├── pkg/
│   ├── controllers/       # Reconciliation logic
│   ├── pb/                # Generated protobuf code
│   └── webhooks/          # Validation webhooks
├── proto/                 # Protocol buffer definitions
├── sdk/python/            # Python SDK
└── charts/                # Helm charts
```
