# ARL Python SDK Examples

Examples demonstrating how to manage Kubernetes sandbox environments and execute tasks using the ARL Python SDK.

## Quick Start

### 1. Install SDK

```bash
pip install arl-env
```

### 2. Configure Kubernetes Access

Ensure you have access to a Kubernetes cluster with a properly configured `kubeconfig`:

```bash
# Verify cluster access
kubectl cluster-info
```

### 3. Run the Comprehensive Example

```bash
cd examples/python
uv run python 09_multiple_feature.py
```

## Comprehensive Example: 09_multiple_feature.py

This is a complete example demonstrating the core features of the SDK:

### Feature Overview

| Feature             | Description                                           |
| ------------------- | ----------------------------------------------------- |
| WarmPool Management | Create and manage warm pools via Python SDK           |
| Sandbox Session     | Allocate sandboxes from warm pool with reuse support  |
| Task Execution      | Execute commands and file operations                  |
| State Persistence   | Maintain state across tasks (e.g., counter increment) |
| Callback Hooks      | Monitor task execution status                         |

### Code Walkthrough

#### Step 1: Create WarmPool

```python
from arl import WarmPoolManager
from kubernetes import client

warmpool_manager = WarmPoolManager(namespace="default")

try:
    warmpool_manager.get_warmpool(pool_name)
    print(f"WarmPool '{pool_name}' already exists")
except client.ApiException as e:
    if e.status == 404:
        # Create new WarmPool
        warmpool_manager.create_warmpool(
            name=pool_name,
            sidecar_image=sidecar_image,
            image=pool_image,
            replicas=2,
        )
        # Wait for WarmPool to be ready
        warmpool_manager.wait_for_warmpool_ready(pool_name)
```

#### Step 2: Create Session and Register Callbacks

```python
from arl import SandboxSession

# Callback functions
def on_task_complete(result):
    state = result.get("status", {}).get("state", "unknown")
    print(f"Task completed with state: {state}")

def on_task_success(result):
    stdout = result.get("status", {}).get("stdout", "")
    print(f"Task succeeded! Output: {stdout[:50]}...")

def on_task_failure(result):
    stderr = result.get("status", {}).get("stderr", "")
    print(f"Task failed! Error: {stderr}")

# Create session (keep_alive=True enables sandbox reuse)
session = SandboxSession(pool_ref=pool_name, namespace="default", keep_alive=True)

# Register callbacks
session.register_callback("on_task_complete", on_task_complete)
session.register_callback("on_task_success", on_task_success)
session.register_callback("on_task_failure", on_task_failure)
```

#### Step 3: Execute Multi-Step Tasks

```python
# Allocate sandbox from warm pool
session.create_sandbox()

# Task 1: Initialize environment
result = session.execute([
    {
        "name": "create_workspace",
        "type": "Command",
        "command": ["mkdir", "-p", "/workspace"],
    },
    {
        "name": "init_counter",
        "type": "FilePatch",
        "path": "/workspace/counter.txt",
        "content": "0",
    },
    {
        "name": "create_script",
        "type": "FilePatch",
        "path": "/workspace/increment.sh",
        "content": """#!/bin/bash
COUNTER_FILE="/workspace/counter.txt"
CURRENT=$(cat $COUNTER_FILE)
NEW=$((CURRENT + 1))
echo $NEW > $COUNTER_FILE
echo "Counter incremented: $CURRENT -> $NEW"
""",
    },
])

# Task 2-3: Execute script multiple times, state persists in sandbox
result = session.execute([
    {"name": "run", "type": "Command", "command": ["/workspace/increment.sh"]},
])
# Counter increments with each execution: 0 -> 1 -> 2
```

#### Step 4: Cleanup Resources

```python
# Delete sandbox
session.delete_sandbox()

# Optional: Delete WarmPool
# warmpool_manager.delete_warmpool(pool_name)
```

### Expected Output

```
[Step 1] Creating WarmPool with Python SDK...
✓ WarmPool 'demo-python-pool' created
✓ WarmPool is ready

[Step 2] Creating sandbox session with callbacks...
✓ Sandbox allocated from pool 'demo-python-pool'

[Task 1] Initializing environment...
  State: Succeeded

[Task 2] Running increment script (1st time)...
  [Callback] Task completed with state: Succeeded
  [Callback] Task succeeded! Output: Counter incremented: 0 -> 1...
  Counter value: 1

[Task 3] Running increment script (2nd time)...
  Counter value: 2

✓ All tasks completed successfully!
```

## Other Examples

| Example                     | Description                                          |
| --------------------------- | ---------------------------------------------------- |
| 01_basic_execution.py       | Basic: Create sandbox, execute commands, read output |
| 02_multi_step_pipeline.py   | Multi-step data processing pipeline                  |
| 03_environment_variables.py | Custom environment variables                         |
| 04_working_directory.py     | Working directory management                         |
| 05_error_handling.py        | Error handling and retries                           |
| 06_long_running_task.py     | Long-running tasks with timeouts                     |
| 07_sandbox_reuse.py         | Sandbox reuse                                        |
| 08_callback_hooks.py        | Callback hooks                                       |

## API Reference

### Task Step Types

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

### Result Handling

```python
task = session.execute([...])

status = task["status"]
state = status["state"]           # "Succeeded" or "Failed"
stdout = status.get("stdout", "") # Standard output
stderr = status.get("stderr", "") # Standard error
exit_code = status.get("exitCode", 0)
```

### Callback Events

| Event              | Trigger                                   |
| ------------------ | ----------------------------------------- |
| `on_task_complete` | After task completes (success or failure) |
| `on_task_success`  | Only when task succeeds                   |
| `on_task_failure`  | Only when task fails                      |
