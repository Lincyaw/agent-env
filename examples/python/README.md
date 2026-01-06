# ARL Python SDK Examples

This directory contains examples demonstrating various features of the ARL Python SDK.

## Prerequisites

1. Install the ARL Python SDK:
   ```bash
   cd sdk/python/arl-client
   pip install -e .
   ```

2. Ensure you have access to a Kubernetes cluster with ARL infrastructure deployed
3. Configure kubectl to access your cluster

## Examples

### Basic Usage (`basic_usage.py`)

Simple example showing how to:
- Create a sandbox using a context manager
- Execute a task with file and command steps
- Read task output

```bash
python basic_usage.py
```

### Batch Tasks (`batch_tasks.py`)

Demonstrates parallel task execution:
- Run multiple tasks concurrently
- Use ThreadPoolExecutor for parallelism
- Collect and display results

```bash
python batch_tasks.py
```

### Error Handling (`error_handling.py`)

Shows how to handle various error scenarios:
- Task execution failures
- Timeouts
- Invalid pool references
- Retry logic

```bash
python error_handling.py
```

## Common Patterns

### Using Context Manager (Recommended)

```python
from arl_client.session import SandboxSession

with SandboxSession("python-3.9-std", namespace="default") as session:
    result = session.execute([...])
    # Sandbox automatically cleaned up on exit
```

### Manual Management

```python
session = SandboxSession("python-3.9-std", keep_alive=True)
session.create_sandbox()
try:
    result = session.execute([...])
finally:
    session.delete_sandbox()
```

### Task Steps

**FilePatch Step:**
```python
{
    "name": "write_file",
    "type": "FilePatch",
    "path": "/workspace/script.py",
    "content": "print('Hello')"
}
```

**Command Step:**
```python
{
    "name": "run_script",
    "type": "Command",
    "command": ["python", "/workspace/script.py"],
    "workDir": "/workspace",
    "env": {"DEBUG": "true"}
}
```

## Troubleshooting

- **Connection errors**: Ensure kubectl is configured and you can access the cluster
- **Pool not found**: Verify the WarmPool exists with `kubectl get warmpools`
- **Timeout errors**: Increase the timeout parameter or check sandbox/task status manually
