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
4. (Optional) For SWE-bench example: WarmPool is created automatically via Python SDK!

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
uv run python 08_swebench_scenario.py  # Creates WarmPool automatically!
uv run python 08_swebench_scenario_mock.py  # With mock agent
uv run python 09_swebench_callbacks.py  # With callback hooks

# Run all examples
uv run python run_all_examples.py
```

## Examples

### Task CRD-Based Execution

All examples use the Kubernetes Task CRD for execution, which works from anywhere with cluster access:

- **01_basic_execution.py**: Create sandbox, execute steps, read output
- **02_multi_step_pipeline.py**: Data processing pipeline with multiple steps
- **03_environment_variables.py**: Custom environment variables
- **04_working_directory.py**: Working directory management
- **05_error_handling.py**: Error handling and retries
- **06_long_running_task.py**: Long-running tasks with timeouts
- **07_sandbox_reuse.py**: Sandbox reuse for serial tasks
- **08_swebench_scenario.py**: SWE-bench automated bug fixing workflow with WarmPool created via Python SDK (see [SWEBENCH_README.md](SWEBENCH_README.md))
- **08_swebench_scenario_mock.py**: SWE-bench with mock agent and separated fixture files
- **09_swebench_callbacks.py**: SWE-bench with callback hooks for automatic test execution

### Mock Agent

The `mock_agent.py` module provides a simple mock agent for simulating LLM behavior:

```python
from mock_agent import create_mock_agent

# Initialize mock agent
agent = create_mock_agent("swebench_fixtures")

# Simulate agent workflow
analysis = agent.analyze_issue()
patch = agent.generate_patch()
test_script = agent.generate_test_script()
report = agent.generate_report()
```

Agent inputs/outputs are stored in `swebench_fixtures/` directory for easy modification and testing.

### Callback System

The SDK supports callbacks for running scripts or functions after task completion:

```python
from arl import SandboxSession

session = SandboxSession(pool_ref="my-pool")

# Register callbacks for different events
def on_success(result):
    print(f"Task succeeded: {result['status']['state']}")

session.register_callback("on_task_success", on_success)

# Execute with automatic callback script execution
result = session.execute_with_callback(
    steps=[...],
    callback_script="/testbed/run_tests.sh"
)
```

**Supported callback events:**
- `on_task_complete` - Triggered after any task completes
- `on_task_success` - Triggered only when task succeeds
- `on_task_failure` - Triggered only when task fails

## Common Patterns

### Context Manager (Recommended)

```python
from arl import SandboxSession

with SandboxSession("python-3.9-std", namespace="default") as session:
    result = session.execute([...])
```

### WarmPool Management (Python SDK)

```python
from arl import WarmPoolManager

# Create a WarmPool
manager = WarmPoolManager(namespace="default")
manager.create_warmpool(
    name="my-pool",
    image="python:3.9-slim",
    replicas=2
)

# Wait for it to be ready
manager.wait_for_warmpool_ready("my-pool")

# Check status
warmpool = manager.get_warmpool("my-pool")
print(warmpool["status"])

# Delete when done
manager.delete_warmpool("my-pool")
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

### Result Handling

```python
# Execute returns the completed task object
task = session.execute([...])

# Check execution status
status = task["status"]
state = status["state"]  # "Succeeded" or "Failed"

# Access step results
for step_result in status.get("steps", []):
    print(f"Step: {step_result['name']}")
    print(f"Exit Code: {step_result['exitCode']}")
    print(f"Stdout: {step_result['stdout']}")
    print(f"Stderr: {step_result['stderr']}")
```
