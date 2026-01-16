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

### Prerequisites

Ensure you have a WarmPool created. You can create one programmatically:

```python
from arl import WarmPoolManager

# Create WarmPool (one-time setup)
warmpool_mgr = WarmPoolManager(namespace="default")
warmpool_mgr.create_warmpool(
    name="python-39-std",
    image="python:3.9-slim",
    replicas=2  # Number of pre-warmed pods
)
warmpool_mgr.wait_for_warmpool_ready("python-39-std")
print("✓ WarmPool ready!")
```

### Basic Usage

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
    print(f"Task State: {status.get('state')}")
    print(f"Output: {status.get('stdout')}")
```

## Manual Lifecycle Management

For long-running operations or sandbox reuse:

```python
from arl import SandboxSession

session = SandboxSession(pool_ref="python-39-std", namespace="default", keep_alive=True)

try:
    session.create_sandbox()
    print("✓ Sandbox allocated")
    
    # Task 1: Initialize workspace
    result1 = session.execute([
        {"name": "init", "type": "Command", "command": ["mkdir", "-p", "/workspace"]}
    ])
    
    # Task 2: Reuses same sandbox (fast!)
    result2 = session.execute([
        {"name": "work", "type": "Command", "command": ["ls", "/workspace"]}
    ])
    
finally:
    session.delete_sandbox()
    print("✓ Sandbox cleaned up")
```

## WarmPool Management

WarmPools pre-create pods to eliminate cold-start delays:

```python
from arl import WarmPoolManager

warmpool_mgr = WarmPoolManager(namespace="default")

# Create a new pool
warmpool_mgr.create_warmpool(
    name="python-39-std",
    image="python:3.9-slim",
    sidecar_image="your-registry/arl-sidecar:latest",  # Optional
    replicas=3,
    resources={  # Optional
        "requests": {"cpu": "500m", "memory": "512Mi"},
        "limits": {"cpu": "1", "memory": "1Gi"}
    }
)

# Wait for readiness
warmpool_mgr.wait_for_warmpool_ready("python-39-std", timeout=300)

# List all pools
pools = warmpool_mgr.list_warmpools()
for pool in pools:
    print(f"Pool: {pool['metadata']['name']}, Status: {pool['status']['phase']}")

# Delete a pool
warmpool_mgr.delete_warmpool("python-39-std")
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
