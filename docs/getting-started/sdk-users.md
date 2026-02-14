# Getting Started for SDK Users

This guide is for users who want to use the Python SDK to execute code in ARL-Infra sandboxed environments. You don't need to deploy or manage the infrastructure - just use the SDK!

## Prerequisites

| Requirement | Description |
|-------------|-------------|
| Python | 3.9 or higher |
| ARL-Infra deployed | The operator and Gateway must be running on the cluster |
| Gateway URL | The URL of the ARL Gateway API |

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

### 1. Basic Usage

```python
from arl import SandboxSession

# Create a session (automatically allocates a sandbox)
with SandboxSession(
    pool_ref="python-pool",
    namespace="default",
    gateway_url="http://localhost:8080",
) as session:
    # Execute a command
    result = session.execute([
        {"name": "hello", "command": ["echo", "Hello, World!"]},
    ])

    # Get the output
    print(result.results[0].output.stdout)  # "Hello, World!"
```

### 2. Multi-Step Execution

```python
from arl import SandboxSession

with SandboxSession(
    pool_ref="python-pool",
    namespace="default",
    gateway_url="http://localhost:8080",
) as session:
    result = session.execute([
        # Step 1: Create a Python file
        {"name": "write_script", "command": ["bash", "-c", "cat > /workspace/hello.py << 'PYEOF'\nprint('Hello from Python!')\nPYEOF"]},
        # Step 2: Execute the file
        {"name": "run_script", "command": ["python", "/workspace/hello.py"]},
    ])

    print(result.results[-1].output.stdout)  # "Hello from Python!"
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
| **Gateway** | REST API that routes execution requests to sandbox pods via gRPC |
| **SandboxSession** | High-level Python API |

## Common Use Cases

=== "Code Execution"

    ```python
    result = session.execute([
        {"name": "run", "command": ["python", "-c", "print(1+1)"]},
    ])
    print(result.results[0].output.stdout)
    ```

=== "Install & Run"

    ```python
    result = session.execute([
        {"name": "install", "command": ["pip", "install", "numpy"]},
        {"name": "run", "command": ["python", "-c", "import numpy; print(numpy.mean([1,2,3]))"]},
    ])
    print(result.results[-1].output.stdout)
    ```

=== "Multi-Step Pipeline"

    ```python
    result = session.execute([
        {"name": "install", "command": ["pip", "install", "numpy"]},
        {"name": "write", "command": ["bash", "-c", "cat > /workspace/calc.py << 'EOF'\nimport numpy\nprint(numpy.mean([1,2,3]))\nEOF"]},
        {"name": "run", "command": ["python", "/workspace/calc.py"]},
    ])
    print(result.results[-1].output.stdout)
    ```

## Troubleshooting

| Issue | Solution |
|-------|----------|
| `No warm pool found` | Check if the pool exists: `kubectl get warmpools` |
| `Connection refused` | Verify the Gateway URL is correct and the Gateway is running |
| `Timeout` | Increase timeout on SandboxSession or check pod status |
