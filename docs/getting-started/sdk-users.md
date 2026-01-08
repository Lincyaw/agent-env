# Getting Started for SDK Users

This guide is for users who want to use the Python SDK to execute code in ARL-Infra sandboxed environments. You don't need to deploy or manage the infrastructure - just use the SDK!

## Prerequisites

| Requirement | Description |
|-------------|-------------|
| Python | 3.9 or higher |
| Kubernetes access | Valid `kubeconfig` with cluster access |
| ARL-Infra deployed | The operator must be running on the cluster |

!!! note "Don't have ARL-Infra deployed?"
    Ask your cluster administrator to deploy ARL-Infra, or follow the [Developer Guide](developers.md) if you need to set it up yourself.

## Installation

Install the ARL SDK using pip:

```bash
pip install arl-env
```

Or using uv:

```bash
uv add arl-env
```

## Quick Start

### 1. Verify Cluster Access

```bash
# Ensure you can access the cluster
kubectl cluster-info

# Check if ARL-Infra is deployed
kubectl get crds | grep arl.infra.io
```

### 2. Basic Usage

```python
from arl import SandboxSession

# Create a session (automatically allocates a sandbox)
with SandboxSession(pool_ref="python-pool", namespace="default") as session:
    # Execute a command
    result = session.execute([
        {
            "name": "hello",
            "type": "Command",
            "command": ["echo", "Hello, World!"],
        }
    ])
    
    # Get the output
    print(result["status"]["stdout"])  # "Hello, World!"
```

### 3. Multi-Step Tasks

```python
from arl import SandboxSession

with SandboxSession(pool_ref="python-pool", namespace="default") as session:
    result = session.execute([
        # Step 1: Create a Python file
        {
            "name": "write_script",
            "type": "FilePatch",
            "path": "/workspace/hello.py",
            "content": "print('Hello from Python!')",
        },
        # Step 2: Execute the file
        {
            "name": "run_script",
            "type": "Command",
            "command": ["python", "/workspace/hello.py"],
        },
    ])
    
    print(result["status"]["stdout"])  # "Hello from Python!"
```

## What's Next?

Continue learning with these resources:

1. **[Python SDK Guide](../user-guide/python-sdk.md)** - Detailed SDK documentation
2. **[Quick Start Tutorial](../user-guide/quickstart.md)** - Step-by-step tutorial
3. **[API Reference](../user-guide/api-reference.md)** - Complete API documentation
4. **[Examples](../user-guide/examples.md)** - More example code

## Key Concepts

| Concept | Description |
|---------|-------------|
| **WarmPool** | Pre-created pods managed by admin |
| **Sandbox** | Your allocated workspace |
| **Task** | Unit of work (commands, file operations) |
| **SandboxSession** | High-level Python API |

## Common Use Cases

=== "Code Execution"

    ```python
    result = session.execute([
        {
            "name": "run",
            "type": "Command",
            "command": ["python", "-c", "print(1+1)"],
        }
    ])
    ```

=== "File Operations"

    ```python
    result = session.execute([
        {
            "name": "write",
            "type": "FilePatch",
            "path": "/workspace/data.json",
            "content": '{"key": "value"}',
        }
    ])
    ```

=== "Multi-Step Pipeline"

    ```python
    result = session.execute([
        {"name": "install", "type": "Command", "command": ["pip", "install", "numpy"]},
        {"name": "write", "type": "FilePatch", "path": "/workspace/calc.py", "content": "import numpy; print(numpy.mean([1,2,3]))"},
        {"name": "run", "type": "Command", "command": ["python", "/workspace/calc.py"]},
    ])
    ```

## Troubleshooting

| Issue | Solution |
|-------|----------|
| `No warm pool found` | Check if the pool exists: `kubectl get warmpools` |
| `Connection refused` | Verify kubeconfig: `kubectl cluster-info` |
| `Timeout` | Increase timeout in task or check pod status |
