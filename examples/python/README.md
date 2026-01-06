# ARL Python SDK Examples

This directory contains examples demonstrating various features of the ARL Python SDK.

# ARL Python SDK Examples

This directory contains examples demonstrating various features of the ARL Python SDK.

## Prerequisites

1. **Install dependencies using uv:**
   ```bash
   cd examples/python
   uv sync
   ```

2. Ensure you have access to a Kubernetes cluster with ARL infrastructure deployed
3. Configure kubectl to access your cluster
4. Create a WarmPool named `python-39-std`

## Running Examples

### Individual Examples

Each example demonstrates a specific feature and can be run independently:

```bash
cd examples/python

# Basic serial execution
uv run python 01_basic_execution.py

# Multi-step data pipeline
uv run python 02_multi_step_pipeline.py

# Environment variables
uv run python 03_environment_variables.py

# Working directory
uv run python 04_working_directory.py

# Error handling
uv run python 05_error_handling.py

# Long-running tasks
uv run python 06_long_running_task.py

# Sandbox reuse (host-like behavior)
uv run python 07_sandbox_reuse.py
```

### Run All Examples

Run all examples as tests:

```bash
cd examples/python
uv run python run_all_examples.py
```

## Examples Overview

### 01. Basic Execution (`01_basic_execution.py`)

Simple example showing how to:
- Create a sandbox using a context manager
- Execute serial steps
- Read task output

### 02. Multi-Step Pipeline (`02_multi_step_pipeline.py`)

Demonstrates data processing pipeline:
- Create data files
- Process data with Python scripts
- Pass data between steps
- Verify output

### 03. Environment Variables (`03_environment_variables.py`)

Shows environment variable usage:
- Set custom environment variables
- Access variables in commands
- Combine system and custom variables

### 04. Working Directory (`04_working_directory.py`)

Working directory management:
- Set custom working directory
- Create subdirectories
- Work with relative paths

### 05. Error Handling (`05_error_handling.py`)

Error handling scenarios:
- Task execution failures
- Invalid commands
- Retry logic

### 06. Long-Running Task (`06_long_running_task.py`)

Long-running task execution:
- Tasks with longer execution time
- Setting timeouts
- Tracking duration

### 07. Sandbox Reuse (`07_sandbox_reuse.py`)

Host-like behavior with sandbox reuse:
- Keep sandbox alive between tasks
- Serial task execution in same environment
- State persistence across tasks

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
