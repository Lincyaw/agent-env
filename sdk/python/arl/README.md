# ARL Wrapper

High-level Python wrapper for the ARL (Agent Runtime Layer) client providing simplified sandbox session management.

## Features

- **Context Manager Support**: Automatic sandbox lifecycle management
- **Type-Safe API**: Full type hints with Pydantic models
- **Kubernetes Integration**: Direct CRD interaction
- **Error Handling**: Comprehensive error reporting and retry logic

## Installation

```bash
uv add arl-wrapper
```

## Quick Start

```python
from arl import SandboxSession

# Using context manager (recommended)
with SandboxSession(pool_ref="python-39-std", namespace="default") as session:
    result = session.execute([
        {
            "name": "hello",
            "type": "Command",
            "command": ["echo", "Hello, World!"],
        }
    ])
    
    # Access results
    status = result["status"]
    for step in status.get("steps", []):
        print(f"Step: {step['name']}")
        print(f"Exit Code: {step['exitCode']}")
        print(f"Stdout: {step['stdout']}")
```

## Manual Lifecycle Management

For long-running operations or sandbox reuse:

```python
session = SandboxSession(pool_ref="python-39-std", namespace="default", keep_alive=True)

try:
    session.create_sandbox()
    
    # Task 1
    result1 = session.execute([...])
    
    # Task 2 (reuses same sandbox)
    result2 = session.execute([...])
    
finally:
    session.delete_sandbox()
```

## Task Step Types

### Command Step

```python
{
    "name": "run_script",
    "type": "Command",
    "command": ["python", "script.py"],
    "env": {"DEBUG": "1"},  # optional
    "workDir": "/workspace",  # optional
}
```

### FilePatch Step

```python
{
    "name": "create_config",
    "type": "FilePatch",
    "path": "/workspace/config.yaml",
    "content": "key: value",
}
```

## Architecture

- **SandboxSession**: High-level API using Kubernetes CRDs for task execution
- **Task CRD**: Operator watches and executes tasks via sidecar
- **Auto-generated client**: `arl-client` package (CRD models)

Task execution flow:
1. Client creates Task CRD via Kubernetes API
2. Operator watches for new tasks
3. Operator communicates with sidecar to execute steps
4. Client polls Task status for results

This architecture ensures tasks can be executed from anywhere with cluster access.
