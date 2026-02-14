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

1. **ARL-Infra deployed**: The operator and Gateway must be running on the cluster
2. **Gateway URL**: You need the URL of the ARL Gateway API
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
with SandboxSession(
    pool_ref="python-pool",
    namespace="default",
    gateway_url="http://localhost:8080",
) as session:
    result = session.execute([
        {"name": "hello", "command": ["echo", "Hello, World!"]},
    ])

    # Access results
    print(result.results[0].output.stdout)  # "Hello, World!"
```

### Manual Lifecycle Management

For long-running operations or sandbox reuse:

```python
from arl import SandboxSession

session = SandboxSession(
    pool_ref="python-pool",
    namespace="default",
    gateway_url="http://localhost:8080",
    keep_alive=True,  # Keep sandbox for multiple executions
)

try:
    # Create sandbox
    session.create_sandbox()

    # Execute multiple commands in the same sandbox
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
    pool_ref="python-pool",          # WarmPool to allocate from
    namespace="default",             # Kubernetes namespace
    gateway_url="http://localhost:8080",  # Gateway API URL
    keep_alive=False,                # Delete sandbox after use (default)
    timeout="30s",                   # Default execution timeout
)
```

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `pool_ref` | str | Required | Name of the WarmPool |
| `namespace` | str | `"default"` | Kubernetes namespace |
| `gateway_url` | str | Required | URL of the Gateway API |
| `keep_alive` | bool | `False` | Keep sandbox after use |
| `timeout` | str | `"30s"` | Default timeout for executions |

**Methods:**

| Method | Description |
|--------|-------------|
| `create_sandbox()` | Allocate a sandbox from the pool |
| `delete_sandbox()` | Release the sandbox |
| `execute(steps)` | Execute steps in the sandbox |
| `restore()` | Restore sandbox to initial state |
| `export_trajectory()` | Export execution trajectory |

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

## Execution Steps

Steps are passed to `session.execute()` as a list of dictionaries. Each step specifies a command to run.

```python
{
    "name": "run_script",
    "command": ["python", "script.py"],
}
```

## Result Handling

Execution results are returned as `ExecuteResponse` objects:

```python
result = session.execute([...])

# Access individual step results
for step_result in result.results:
    stdout = step_result.output.stdout
    stderr = step_result.output.stderr
    exit_code = step_result.output.exit_code

    if exit_code == 0:
        print(f"Output: {stdout}")
    else:
        print(f"Error: {stderr}")
```

## Error Handling

```python
try:
    with SandboxSession(
        pool_ref="python-pool",
        gateway_url="http://localhost:8080",
    ) as session:
        result = session.execute([...])

except ConnectionError:
    print("Cannot connect to Gateway API")

except TimeoutError:
    print("Execution timed out")

except Exception as e:
    print(f"Unexpected error: {e}")
```

## Best Practices

### 1. Use Context Managers

```python
# Good: Resources are automatically cleaned up
with SandboxSession(
    pool_ref="python-pool",
    gateway_url="http://localhost:8080",
) as session:
    result = session.execute([...])

# Avoid: Manual cleanup required
session = SandboxSession(pool_ref="python-pool", gateway_url="http://localhost:8080")
session.create_sandbox()
# ... if exception occurs, sandbox may not be cleaned up
session.delete_sandbox()
```

### 2. Reuse Sandboxes for Related Executions

```python
# Good: Single sandbox for related executions
session = SandboxSession(
    pool_ref="python-pool",
    gateway_url="http://localhost:8080",
    keep_alive=True,
)
try:
    session.create_sandbox()

    # Setup
    session.execute([{"name": "install", "command": ["pip", "install", "numpy"]}])

    # Multiple executions share the environment
    for i in range(10):
        session.execute([{"name": f"run_{i}", "command": ["python", "script.py"]}])

finally:
    session.delete_sandbox()
```

### 3. Set Appropriate Timeouts

```python
# Set timeout on the session for long-running operations
session = SandboxSession(
    pool_ref="python-pool",
    gateway_url="http://localhost:8080",
    timeout="5m",  # 5 minutes
)
```

### 4. Check Results

```python
result = session.execute([...])

for step_result in result.results:
    if step_result.output.exit_code != 0:
        raise RuntimeError(f"Step failed: {step_result.output.stderr}")
```

## Next Steps

- [Quick Start Tutorial](quickstart.md) - Step-by-step tutorial
- [API Reference](api-reference.md) - Complete API documentation
- [Examples](examples.md) - More code examples
