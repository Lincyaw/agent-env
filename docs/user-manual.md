# ARL-Infra Operator 用户手册

## 📋 什么是 ARL-Infra？

ARL-Infra 是一个 Kubernetes Operator，为 AI Agent 提供**超低延迟的代码执行环境**。

---

## 🏗️ 系统架构

### 整体架构图

```mermaid
graph TB
    subgraph "用户层"
        User[👤 用户/AI Agent]
        PythonSDK[🐍 Python SDK]
        User --> PythonSDK
        User --> |kubectl apply| K8sAPI[Kubernetes API]
        PythonSDK --> |kubernetes client| K8sAPI
    end

    subgraph "Kubernetes Control Plane"
        K8sAPI
        
        subgraph "ARL Operator"
            Operator[🎮 Operator Main]
            
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
            WP[📦 WarmPool CRD<br/>定义 Pod 池配置]
            SB[🏠 Sandbox CRD<br/>工作空间]
            Task[⚡ Task CRD<br/>执行任务]
        end
        
        subgraph "Warm Pool Pods"
            Pod1[Pod 1<br/>🔵 Ready]
            Pod2[Pod 2<br/>🟢 Allocated]
            Pod3[Pod 3<br/>🔵 Ready]
        end
        
        subgraph "Pod 2 详细视图"
            direction LR
            Executor[Executor Container<br/>python:3.9-slim<br/>执行用户代码]
            Sidecar[Sidecar Container<br/>gRPC Server<br/>:50051]
            Workspace[(Workspace<br/>/workspace<br/>共享卷)]
            
            Executor -.共享.-> Workspace
            Sidecar -.共享.-> Workspace
        end
    end

    WPCtrl --> |create/manage| Pod1
    WPCtrl --> |create/manage| Pod2
    WPCtrl --> |create/manage| Pod3
    
    SBCtrl --> |allocate| Pod2
    SBCtrl --> |read| WP
    
    TaskCtrl --> |gRPC: UpdateFiles/Execute| Sidecar
    TaskCtrl --> |read| SB
    
    Task --> |references| SB
    SB --> |references| WP
    
    TTLCtrl --> |cleanup| Task
    TTLCtrl --> |cleanup idle| SB

    style User fill:#e1f5ff
    style PythonSDK fill:#4a90e2
    style Operator fill:#ff9800
    style WPCtrl fill:#ffeb3b
    style SBCtrl fill:#ffeb3b
    style TaskCtrl fill:#ffeb3b
    style TTLCtrl fill:#ffeb3b
    style WP fill:#8bc34a
    style SB fill:#8bc34a
    style Task fill:#8bc34a
    style Pod2 fill:#f48fb1
    style Executor fill:#ce93d8
    style Sidecar fill:#ce93d8
```

### 核心组件说明

| 组件 | 类型 | 职责 |
|------|------|------|
| **Operator** | 控制器管理器 | 启动和管理所有 Controller 和 Webhook |
| **WarmPool Controller** | 控制器 | 维护 Pod 池，确保有足够的空闲 Pod |
| **Sandbox Controller** | 控制器 | 从 Pool 分配 Pod，管理 Sandbox 生命周期 |
| **Task Controller** | 控制器 | 通过 gRPC 调用 Sidecar 执行任务 |
| **TTL Controller** | 控制器 | 清理完成的 Task 和空闲的 Sandbox |
| **Sidecar** | gRPC 服务器 | 在 Pod 中执行文件操作和命令 |
| **Executor** | 用户容器 | 实际运行用户代码的容器 |

### 交互流程

```mermaid
sequenceDiagram
    participant User as 👤 用户
    participant API as Kubernetes API
    participant WPC as WarmPool Controller
    participant SBC as Sandbox Controller
    participant TC as Task Controller
    participant Pod as Pod (Sidecar)

    Note over User,Pod: 1️⃣ 创建 Pod 池
    User->>API: kubectl apply -f warmpool.yaml
    API->>WPC: Watch WarmPool 变化
    WPC->>API: 创建 3 个 Pod
    Note over Pod: 🔵 Pod Ready

    Note over User,Pod: 2️⃣ 创建沙箱
    User->>API: kubectl apply -f sandbox.yaml
    API->>SBC: Watch Sandbox 变化
    SBC->>API: 查询 WarmPool
    SBC->>API: 分配 Pod，更新标签
    Note over Pod: 🟢 Pod Allocated
    SBC->>API: 更新 Sandbox.Status (Phase=Ready)

    Note over User,Pod: 3️⃣ 执行任务
    User->>API: kubectl apply -f task.yaml
    API->>TC: Watch Task 变化
    TC->>API: 查询 Sandbox 获取 Pod IP
    TC->>Pod: gRPC: UpdateFiles(/workspace/hello.py)
    Pod-->>TC: Success
    TC->>Pod: gRPC: Execute(python hello.py)
    Pod-->>TC: stdout, stderr, exitCode
    TC->>API: 更新 Task.Status (State=Succeeded)
    User->>API: kubectl get task hello-task -o jsonpath='{.status.stdout}'
    API-->>User: "Hello from ARL!"
```

### 数据流

```mermaid
flowchart LR
    subgraph "用户输入"
        YAML[📄 YAML/Python SDK]
    end
    
    subgraph "Kubernetes 资源"
        WP[WarmPool]
        SB[Sandbox]
        Task[Task]
    end
    
    subgraph "Pod 执行"
        File[📝 FilePatch<br/>写入文件]
        Cmd[⚙️ Command<br/>执行命令]
    end
    
    subgraph "输出结果"
        Status[Task.Status<br/>stdout/stderr/exitCode]
    end
    
    YAML --> WP
    WP --> |提供| SB
    YAML --> SB
    SB --> |绑定 Pod| Task
    YAML --> Task
    Task --> File
    Task --> Cmd
    File --> Status
    Cmd --> Status
    
    style YAML fill:#e3f2fd
    style WP fill:#c8e6c9
    style SB fill:#fff9c4
    style Task fill:#ffccbc
    style Status fill:#f8bbd0
```

---

## 🎯 核心概念

使用 ARL-Infra 需要理解三种资源，它们按顺序协同工作：

### 1. WarmPool（Pod 池）
预先创建一组 Pod，等待分配使用。

### 2. Sandbox（沙箱）
从 Pool 中分配一个 Pod，作为你的工作空间。

### 3. Task（任务）
在 Sandbox 中执行具体的代码和命令。

**简单理解：**
```
WarmPool = 停车场（预留车位）
Sandbox  = 你租的车位
Task     = 停车和取车的操作
```

---

## 🚀 快速开始

### 第一步：创建 Pod 池

```yaml
# warmpool.yaml
apiVersion: arl.infra.io/v1alpha1
kind: WarmPool
metadata:
  name: python-pool
spec:
  replicas: 3                    # 保持 3 个空闲 Pod
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

---

### 第二步：创建沙箱

```yaml
# sandbox.yaml
apiVersion: arl.infra.io/v1alpha1
kind: Sandbox
metadata:
  name: my-workspace
spec:
  poolRef: python-pool           # 使用哪个 Pool
  keepAlive: true                # 保持沙箱用于多次任务
```

```bash
kubectl apply -f sandbox.yaml

# 等待沙箱就绪
kubectl get sandbox my-workspace -w
# 等待 PHASE 变为 Ready
```

---

### 第三步：提交任务

```yaml
# task.yaml
apiVersion: arl.infra.io/v1alpha1
kind: Task
metadata:
  name: hello-task
spec:
  sandboxRef: my-workspace       # 在哪个沙箱执行
  timeout: 30s
  steps:
    # 步骤 1: 写入 Python 文件
    - name: write-code
      type: FilePatch
      path: /workspace/hello.py
      content: |
        print("Hello from ARL!")
        print("Task executed successfully")
    
    # 步骤 2: 执行 Python 文件
    - name: run-code
      type: Command
      command: ["python", "/workspace/hello.py"]
```

```bash
kubectl apply -f task.yaml
```

---

### 第四步：查看结果

```bash
# 1. 查看任务状态
kubectl get task hello-task

# 2. 查看输出结果
kubectl get task hello-task -o jsonpath='{.status.stdout}'

# 3. 查看错误信息（如果有）
kubectl get task hello-task -o jsonpath='{.status.stderr}'

# 4. 查看退出码
kubectl get task hello-task -o jsonpath='{.status.exitCode}'

# 5. 查看完整状态
kubectl describe task hello-task
```

**预期输出：**
```
Hello from ARL!
Task executed successfully
```
