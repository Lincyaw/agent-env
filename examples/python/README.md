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
from arl.session import SandboxSession

with SandboxSession("python-3.9-std", namespace="default") as session:
    result = session.execute([...])
```

### Task Steps

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
