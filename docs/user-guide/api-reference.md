# API Reference

Complete API reference for the ARL Python SDK.

## Module: arl

### SandboxSession

```python
class SandboxSession:
    """High-level API for sandbox management and task execution."""
```

#### Constructor

```python
def __init__(
    self,
    pool_ref: str,
    namespace: str = "default",
    keep_alive: bool = False,
    timeout: str = "30s",
) -> None
```

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `pool_ref` | `str` | Required | Name of the WarmPool to allocate from |
| `namespace` | `str` | `"default"` | Kubernetes namespace |
| `keep_alive` | `bool` | `False` | Keep sandbox after task completion |
| `timeout` | `str` | `"30s"` | Default timeout for tasks |

**Example:**

```python
from arl import SandboxSession

session = SandboxSession(
    pool_ref="python-pool",
    namespace="default",
    keep_alive=True,
    timeout="60s",
)
```

#### Context Manager

```python
def __enter__(self) -> "SandboxSession"
def __exit__(self, exc_type, exc_val, exc_tb) -> None
```

Automatically creates and deletes sandbox.

**Example:**

```python
with SandboxSession(pool_ref="python-pool") as session:
    result = session.execute([...])
# Sandbox is automatically deleted
```

#### create_sandbox

```python
def create_sandbox(self) -> None
```

Allocate a sandbox from the warm pool.

**Raises:**

- `kubernetes.client.ApiException`: If allocation fails

**Example:**

```python
session = SandboxSession(pool_ref="python-pool", keep_alive=True)
session.create_sandbox()
# ... use sandbox
session.delete_sandbox()
```

#### delete_sandbox

```python
def delete_sandbox(self) -> None
```

Release the sandbox and return pod to pool (or delete if not keep_alive).

**Example:**

```python
session.delete_sandbox()
```

#### execute

```python
def execute(
    self,
    steps: list[dict],
    timeout: str | None = None,
) -> dict
```

Execute steps in the sandbox.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `steps` | `list[dict]` | Required | List of step definitions |
| `timeout` | `str \| None` | `None` | Override default timeout |

**Returns:** Task result dictionary

**Example:**

```python
result = session.execute([
    {"name": "step1", "type": "Command", "command": ["echo", "hello"]},
], timeout="60s")
```

#### register_callback

```python
def register_callback(
    self,
    event: str,
    callback: Callable[[dict], None],
) -> None
```

Register a callback function for task events.

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `event` | `str` | Event name: `on_task_complete`, `on_task_success`, `on_task_failure` |
| `callback` | `Callable` | Function to call with task result |

**Example:**

```python
def my_callback(result):
    print(f"Task state: {result['status']['state']}")

session.register_callback("on_task_complete", my_callback)
```

---

### WarmPoolManager

```python
class WarmPoolManager:
    """Manage WarmPool resources."""
```

#### Constructor

```python
def __init__(self, namespace: str = "default") -> None
```

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `namespace` | `str` | `"default"` | Kubernetes namespace |

#### create_warmpool

```python
def create_warmpool(
    self,
    name: str,
    image: str,
    sidecar_image: str,
    replicas: int = 1,
    command: list[str] | None = None,
) -> None
```

Create a new WarmPool.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `name` | `str` | Required | WarmPool name |
| `image` | `str` | Required | Executor container image |
| `sidecar_image` | `str` | Required | Sidecar container image |
| `replicas` | `int` | `1` | Number of pods to maintain |
| `command` | `list[str] \| None` | `None` | Container command |

**Example:**

```python
manager = WarmPoolManager(namespace="default")
manager.create_warmpool(
    name="python-pool",
    image="python:3.9-slim",
    sidecar_image="arl-sidecar:latest",
    replicas=3,
)
```

#### get_warmpool

```python
def get_warmpool(self, name: str) -> dict
```

Get WarmPool details.

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `name` | `str` | WarmPool name |

**Returns:** WarmPool resource dictionary

**Raises:**

- `kubernetes.client.ApiException`: If not found (status 404)

#### wait_for_warmpool_ready

```python
def wait_for_warmpool_ready(
    self,
    name: str,
    timeout: int = 300,
) -> None
```

Wait for WarmPool to be ready.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `name` | `str` | Required | WarmPool name |
| `timeout` | `int` | `300` | Timeout in seconds |

**Raises:**

- `TimeoutError`: If pool is not ready within timeout

#### delete_warmpool

```python
def delete_warmpool(self, name: str) -> None
```

Delete a WarmPool.

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `name` | `str` | WarmPool name |

---

## Step Types

### Command Step

Execute a command in the sandbox.

```python
{
    "name": str,           # Required: Step name
    "type": "Command",     # Required: Step type
    "command": list[str],  # Required: Command and arguments
    "workDir": str,        # Optional: Working directory
    "env": dict[str, str], # Optional: Environment variables
}
```

**Example:**

```python
{
    "name": "run_python",
    "type": "Command",
    "command": ["python", "-c", "print('hello')"],
    "workDir": "/workspace",
    "env": {"PYTHONPATH": "/workspace/lib"},
}
```

### FilePatch Step

Create or update a file.

```python
{
    "name": str,           # Required: Step name
    "type": "FilePatch",   # Required: Step type
    "path": str,           # Required: File path
    "content": str,        # Required: File content
}
```

**Example:**

```python
{
    "name": "write_config",
    "type": "FilePatch",
    "path": "/workspace/config.yaml",
    "content": "key: value\nother: 123",
}
```

---

## Result Structure

Task execution returns a dictionary with the following structure:

```python
{
    "metadata": {
        "name": str,           # Task name
        "namespace": str,      # Namespace
        "creationTimestamp": str,
    },
    "spec": {
        "sandboxRef": str,     # Sandbox name
        "timeout": str,        # Timeout duration
        "steps": list[dict],   # Step definitions
    },
    "status": {
        "state": str,          # "Pending", "Running", "Succeeded", "Failed"
        "stdout": str,         # Standard output (last command)
        "stderr": str,         # Standard error (last command)
        "exitCode": int,       # Exit code (last command)
        "startedAt": str,      # Execution start time
        "completedAt": str,    # Execution completion time
        "steps": list[dict],   # Status of each step
    },
}
```

### Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `state` | `str` | Task state |
| `stdout` | `str` | Standard output from last command |
| `stderr` | `str` | Standard error from last command |
| `exitCode` | `int` | Exit code from last command |
| `startedAt` | `str` | ISO timestamp of execution start |
| `completedAt` | `str` | ISO timestamp of execution completion |

### State Values

| State | Description |
|-------|-------------|
| `Pending` | Task created, waiting to execute |
| `Running` | Task is currently executing |
| `Succeeded` | All steps completed successfully |
| `Failed` | One or more steps failed |

---

## Callback Events

| Event | Trigger | Signature |
|-------|---------|-----------|
| `on_task_complete` | After task completes | `fn(result: dict) -> None` |
| `on_task_success` | Task state is "Succeeded" | `fn(result: dict) -> None` |
| `on_task_failure` | Task state is "Failed" | `fn(result: dict) -> None` |

---

## Exceptions

### kubernetes.client.ApiException

Raised for Kubernetes API errors.

**Common status codes:**

| Status | Meaning |
|--------|---------|
| `404` | Resource not found |
| `403` | Permission denied |
| `409` | Conflict (resource already exists) |
| `422` | Invalid resource specification |

**Example:**

```python
from kubernetes import client

try:
    result = session.execute([...])
except client.ApiException as e:
    if e.status == 404:
        print("Sandbox or WarmPool not found")
    else:
        print(f"API error: {e.status} - {e.reason}")
```

### TimeoutError

Raised when operations exceed their timeout.

```python
try:
    result = session.execute([...], timeout="5s")
except TimeoutError:
    print("Task execution timed out")
```
