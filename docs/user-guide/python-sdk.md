# Python SDK

The ARL Python SDK provides a high-level API for interacting with ARL-Infra sandboxed environments.

## Installation

### From PyPI

```bash
pip install arl-env
```

Or using uv:

```bash
uv add arl-env
```

### From Source

```bash
git clone https://github.com/Lincyaw/agent-env.git
cd agent-env/sdk/python/arl
pip install -e .
```

## Prerequisites

Before using the SDK, ensure:

1. **Kubernetes access**: Valid `kubeconfig` file with cluster access
2. **ARL-Infra deployed**: The operator must be running on the cluster
3. **WarmPool available**: At least one WarmPool must exist

Verify your setup:

```bash
# Check cluster access
kubectl cluster-info

# Check ARL-Infra is deployed
kubectl get crds | grep arl.infra.io

# Check available WarmPools
kubectl get warmpools
```

## Quick Start

### Basic Usage with Context Manager

```python
from arl import SandboxSession

# Using context manager (recommended)
with SandboxSession(pool_ref="python-pool", namespace="default") as session:
    result = session.execute([
        {
            "name": "hello",
            "type": "Command",
            "command": ["echo", "Hello, World!"],
        }
    ])
    
    # Access results
    print(result["status"]["stdout"])  # "Hello, World!"
```

### Manual Lifecycle Management

For long-running operations or sandbox reuse:

```python
from arl import SandboxSession

session = SandboxSession(
    pool_ref="python-pool",
    namespace="default",
    keep_alive=True  # Keep sandbox for multiple tasks
)

try:
    # Create sandbox
    session.create_sandbox()
    
    # Execute multiple tasks in the same sandbox
    result1 = session.execute([...])
    result2 = session.execute([...])
    
finally:
    # Clean up
    session.delete_sandbox()
```

## Core Classes

### SandboxSession

The main class for interacting with sandboxes.

```python
from arl import SandboxSession

session = SandboxSession(
    pool_ref="python-pool",     # WarmPool to allocate from
    namespace="default",        # Kubernetes namespace
    keep_alive=False,           # Delete sandbox after task (default)
    timeout="30s",              # Default task timeout
)
```

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `pool_ref` | str | Required | Name of the WarmPool |
| `namespace` | str | `"default"` | Kubernetes namespace |
| `keep_alive` | bool | `False` | Keep sandbox after task completion |
| `timeout` | str | `"30s"` | Default timeout for tasks |

**Methods:**

| Method | Description |
|--------|-------------|
| `create_sandbox()` | Allocate a sandbox from the pool |
| `delete_sandbox()` | Release the sandbox |
| `execute(steps)` | Execute steps in the sandbox |
| `register_callback(event, fn)` | Register a callback function |

### WarmPoolManager

Manage WarmPool resources programmatically.

```python
from arl import WarmPoolManager

manager = WarmPoolManager(namespace="default")

# Create a WarmPool
manager.create_warmpool(
    name="my-pool",
    image="python:3.9-slim",
    sidecar_image="arl-sidecar:latest",
    replicas=3,
)

# Wait for pool to be ready
manager.wait_for_warmpool_ready("my-pool")

# Get WarmPool status
pool = manager.get_warmpool("my-pool")

# Delete WarmPool
manager.delete_warmpool("my-pool")
```

## Task Steps

### Command Step

Execute a command in the sandbox:

```python
{
    "name": "run_script",
    "type": "Command",
    "command": ["python", "script.py"],
    "workDir": "/workspace",      # Optional: working directory
    "env": {"DEBUG": "true"},     # Optional: environment variables
}
```

### FilePatch Step

Create or update a file:

```python
{
    "name": "write_file",
    "type": "FilePatch",
    "path": "/workspace/script.py",
    "content": "print('Hello!')",
}
```

## Result Handling

Task results are returned as dictionaries:

```python
result = session.execute([...])

# Access status
status = result["status"]
state = status["state"]           # "Succeeded" or "Failed"
stdout = status.get("stdout", "") # Standard output
stderr = status.get("stderr", "") # Standard error
exit_code = status.get("exitCode", 0)

# Check success
if state == "Succeeded":
    print(f"Output: {stdout}")
else:
    print(f"Error: {stderr}")
```

## Callbacks

Register callbacks for task events:

```python
def on_complete(result):
    print(f"Task completed: {result['status']['state']}")

def on_success(result):
    print(f"Success! Output: {result['status']['stdout']}")

def on_failure(result):
    print(f"Failed! Error: {result['status']['stderr']}")

session = SandboxSession(pool_ref="python-pool")
session.register_callback("on_task_complete", on_complete)
session.register_callback("on_task_success", on_success)
session.register_callback("on_task_failure", on_failure)
```

**Available events:**

| Event | Trigger |
|-------|---------|
| `on_task_complete` | After task completes (success or failure) |
| `on_task_success` | Only when task succeeds |
| `on_task_failure` | Only when task fails |

## Error Handling

```python
from kubernetes import client as k8s_client

try:
    with SandboxSession(pool_ref="python-pool") as session:
        result = session.execute([...])
        
except k8s_client.ApiException as e:
    if e.status == 404:
        print("WarmPool not found")
    else:
        print(f"Kubernetes API error: {e}")
        
except TimeoutError:
    print("Task timed out")
    
except Exception as e:
    print(f"Unexpected error: {e}")
```

## Best Practices

### 1. Use Context Managers

```python
# Good: Resources are automatically cleaned up
with SandboxSession(pool_ref="python-pool") as session:
    result = session.execute([...])

# Avoid: Manual cleanup required
session = SandboxSession(pool_ref="python-pool")
session.create_sandbox()
# ... if exception occurs, sandbox may not be cleaned up
session.delete_sandbox()
```

### 2. Reuse Sandboxes for Related Tasks

```python
# Good: Single sandbox for related tasks
session = SandboxSession(pool_ref="python-pool", keep_alive=True)
try:
    session.create_sandbox()
    
    # Setup
    session.execute([{"name": "install", "type": "Command", "command": ["pip", "install", "numpy"]}])
    
    # Multiple executions share the environment
    for i in range(10):
        session.execute([{"name": f"run_{i}", "type": "Command", "command": ["python", "script.py"]}])
        
finally:
    session.delete_sandbox()
```

### 3. Handle Timeouts Appropriately

```python
# Set appropriate timeouts for long-running tasks
result = session.execute([
    {
        "name": "long_task",
        "type": "Command",
        "command": ["python", "long_running_script.py"],
    }
], timeout="5m")  # 5 minutes
```

### 4. Check Results

```python
result = session.execute([...])

if result["status"]["state"] != "Succeeded":
    stderr = result["status"].get("stderr", "")
    raise RuntimeError(f"Task failed: {stderr}")
```

## Next Steps

- [Quick Start Tutorial](quickstart.md) - Step-by-step tutorial
- [API Reference](api-reference.md) - Complete API documentation
- [Examples](examples.md) - More code examples
