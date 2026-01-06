# ARL Wrapper

High-level Python wrapper for the ARL (Agent Runtime Layer) client providing simplified sandbox session management.

## Features

- **Context Manager Support**: Automatic sandbox lifecycle management
- **Type-Safe API**: Full type hints with Pydantic models
- **Kubernetes Integration**: Direct CRD interaction
- **gRPC Streaming**: Real-time command output streaming
- **Interactive Shell**: Bidirectional shell sessions via gRPC
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
    print(result["status"]["stdout"])
```

## Streaming Execution (gRPC)

For real-time output streaming, use direct gRPC communication:

```python
from arl import SandboxSession

with SandboxSession(pool_ref="python-39-std") as session:
    # Stream command output in real-time
    for log in session.execute_stream(["python", "-c", "print('hello')"]):
        print(log.stdout, end="")
        if log.done:
            print(f"Exit code: {log.exit_code}")
```

## Interactive Shell

Start an interactive shell session:

```python
from arl import SandboxSession

with SandboxSession(pool_ref="python-39-std") as session:
    with session.interactive_shell() as shell:
        shell.send_data("echo hello\n")
        for output in shell.read_output():
            print(output.data, end="")
            if output.closed:
                break
        
        # Send Ctrl+C if needed
        shell.send_signal("SIGINT")
```

## Direct Sidecar Client

For low-level gRPC access to the sidecar:

```python
from arl import SidecarClient

with SidecarClient("10.0.0.1:9090") as client:
    # Update files
    client.update_files("/workspace", {"script.py": "print('hello')"})
    
    # Execute with streaming
    for log in client.execute_stream(["python", "script.py"]):
        print(log.stdout, end="")
    
    # Reset workspace
    client.reset()
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
- **SidecarClient**: Low-level gRPC client for direct sidecar communication
- **ShellSession**: Bidirectional streaming shell sessions

The SDK supports two execution modes:
1. **CRD-based** (`execute()`): Creates Task CRD, operator handles execution
2. **Direct gRPC** (`execute_stream()`, `execute_direct()`): Communicates directly with sidecar

## Development

This package provides a clean separation between auto-generated API client code and custom business logic.

- Auto-generated client: `arl-client` package (CRD models)
- Auto-generated gRPC: `arl.pb` package (protobuf stubs)
- Custom wrapper: `arl` package (this package)

This architecture ensures custom code is never overwritten during API regeneration.
