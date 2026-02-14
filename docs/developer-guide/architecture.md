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
        PythonSDK --> |REST API| Gateway[Gateway]
    end

    subgraph "Kubernetes Control Plane"
        K8sAPI
        Gateway --> |kubernetes client| K8sAPI

        subgraph "ARL Operator"
            Operator[Operator Main]

            subgraph "Controllers"
                WPCtrl[WarmPool Controller]
                SBCtrl[Sandbox Controller]
            end

            subgraph "Webhooks"
                WPWebhook[WarmPool Validator]
                SBWebhook[Sandbox Validator]
            end

            Operator --> WPCtrl
            Operator --> SBCtrl
            Operator --> WPWebhook
            Operator --> SBWebhook
        end

        K8sAPI --> |validate| WPWebhook
        K8sAPI --> |validate| SBWebhook
        K8sAPI --> |watch/update| WPCtrl
        K8sAPI --> |watch/update| SBCtrl
    end

    subgraph "Kubernetes Data Plane"
        subgraph "Custom Resources"
            WP[WarmPool CRD]
            SB[Sandbox CRD]
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

    Gateway --> |gRPC| Pod2
    Gateway --> |read| SB

    SB --> |references| WP
```

## Core Components

### ARL Operator

The operator is the central control component running in the `arl-system` namespace.

| Component | Responsibility |
|-----------|----------------|
| **WarmPool Controller** | Maintains pod pools, ensures desired replica count |
| **Sandbox Controller** | Allocates pods from pools, manages sandbox lifecycle |
| **Gateway** | REST API for session management and command execution via gRPC |
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

    Gateway[Gateway] --> |gRPC| Sidecar
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
  keepAlive: true       # Keep for multiple executions
```

## Interaction Flow

### Execution Flow

```mermaid
sequenceDiagram
    participant User
    participant SDK as Python SDK
    participant GW as Gateway API
    participant API as Kubernetes API
    participant WPC as WarmPool Controller
    participant SBC as Sandbox Controller
    participant Pod as Pod (Sidecar)

    Note over User,Pod: Phase 1: Create Pod Pool
    User->>API: Create WarmPool
    API->>WPC: Watch WarmPool
    WPC->>API: Create Pods
    Note over Pod: Pods Ready

    Note over User,Pod: Phase 2: Allocate Sandbox
    User->>SDK: Create SandboxSession
    SDK->>GW: Create Sandbox
    GW->>API: Create Sandbox CRD
    API->>SBC: Watch Sandbox
    SBC->>API: Query WarmPool
    SBC->>API: Allocate Pod (update labels)
    Note over Pod: Pod Allocated
    SBC->>API: Update Sandbox Status (Ready)

    Note over User,Pod: Phase 3: Execute Commands
    User->>SDK: session.execute(steps)
    SDK->>GW: POST /execute
    GW->>API: Get Sandbox (pod IP)
    GW->>Pod: gRPC: Execute
    Pod-->>GW: stdout, stderr, exitCode
    GW-->>SDK: ExecuteResponse
    SDK-->>User: Results
```

### Data Flow

```mermaid
flowchart LR
    subgraph "Input"
        SDK[Python SDK]
    end

    subgraph "Gateway"
        GW[REST API]
    end

    subgraph "Kubernetes Resources"
        WP[WarmPool]
        SB[Sandbox]
    end

    subgraph "Pod Execution"
        Cmd[Command via gRPC]
    end

    subgraph "Output"
        Status[ExecuteResponse<br/>stdout/stderr/exitCode]
    end

    SDK --> GW
    GW --> SB
    WP --> |provides| SB
    SB --> |binds Pod| Cmd
    Cmd --> Status
    Status --> GW
    GW --> SDK
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

### Why Gateway-based Execution?

- **Low latency**: Direct gRPC calls to sidecars without creating Kubernetes resources per execution
- **Simplicity**: No Task CRD lifecycle to manage
- **Scalability**: Gateway can route and load-balance execution requests
- **Flexibility**: Supports restore, trajectory export, and tool invocation beyond simple command execution

### Why CRD-based Resource Management?

- **Kubernetes-native**: Uses familiar kubectl commands for WarmPool and Sandbox management
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
│   ├── sidecar/           # Sidecar entrypoint
│   ├── gateway/           # Gateway entrypoint
│   └── executor-agent/    # Executor agent entrypoint
├── config/
│   └── crd/               # Generated CRD manifests
├── pkg/
│   ├── controller/        # Reconciliation logic
│   ├── gateway/           # Gateway REST API
│   ├── execagent/         # Executor agent logic
│   ├── scheduler/         # Pod scheduling logic
│   ├── pb/                # Generated protobuf code
│   └── webhook/           # Validation webhooks
├── proto/                 # Protocol buffer definitions
├── sdk/python/            # Python SDK
└── charts/                # Helm charts
```
