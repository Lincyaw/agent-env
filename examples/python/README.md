# ARL Python SDK Examples

Examples demonstrating ARL Python SDK features.

## Prerequisites

1. Install dependencies:
   ```bash
   cd examples/python
   uv sync
   ```

2. Access to Kubernetes cluster with ARL infrastructure
3. Create a WarmPool named `python-39-std`

## Running Examples

```bash
cd examples/python

# Individual examples
uv run python 01_basic_execution.py
uv run python 02_multi_step_pipeline.py
uv run python 03_environment_variables.py
uv run python 04_working_directory.py
uv run python 05_error_handling.py
uv run python 06_long_running_task.py
uv run python 07_sandbox_reuse.py

# Run all examples
uv run python run_all_examples.py
```

## Examples

- **01_basic_execution.py**: Create sandbox, execute steps, read output
- **02_multi_step_pipeline.py**: Data processing pipeline with multiple steps
- **03_environment_variables.py**: Custom environment variables
- **04_working_directory.py**: Working directory management
- **05_error_handling.py**: Error handling and retries
- **06_long_running_task.py**: Long-running tasks with timeouts
- **07_sandbox_reuse.py**: Sandbox reuse for serial tasks

## Common Patterns

### Context Manager (Recommended)

```python
from arl import SandboxSession

with SandboxSession("python-3.9-std", namespace="default") as session:
    result = session.execute([...])
```

### Task Steps (via Kubernetes CRD)

```python
# FilePatch: Create/modify files
{
    "name": "write_file",
    "type": "FilePatch",
    "path": "/workspace/script.py",
    "content": "print('Hello')"
}

# Command: Execute commands
{
    "name": "run_script",
    "type": "Command",
    "command": ["python", "/workspace/script.py"],
    "workDir": "/workspace",
    "env": {"DEBUG": "true"}
}
```

### Streaming Execution (Direct gRPC)

For real-time output streaming, use the direct gRPC methods:

```python
from arl import SandboxSession

with SandboxSession("python-3.9-std") as session:
    # Stream execution output in real-time
    for log in session.execute_stream(["python", "-c", "print('hello')"]):
        print(log.stdout, end="")
        if log.done:
            print(f"Exit code: {log.exit_code}")
```

### Interactive Shell

For interactive shell sessions:

```python
from arl import SandboxSession

with SandboxSession("python-3.9-std") as session:
    with session.interactive_shell() as shell:
        # Run commands interactively
        shell.send_data("echo hello\n")
        for output in shell.read_output():
            print(output.data, end="")
            if output.closed:
                break
        
        # Send Ctrl+C
        shell.send_signal("SIGINT")
```

### Direct Sidecar Client

For low-level sidecar access:

```python
from arl import SidecarClient

# Connect directly to sidecar (requires pod IP)
with SidecarClient("10.0.0.1:9090") as client:
    # Update files
    client.update_files("/workspace", {"test.py": "print('hello')"})
    
    # Execute with streaming
    for log in client.execute_stream(["python", "test.py"]):
        print(log.stdout, end="")
    
    # Reset workspace
    client.reset()
```
