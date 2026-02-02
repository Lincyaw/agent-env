# Python Examples

This directory contains examples demonstrating various features of the ARL Python SDK.

## Running Examples

```bash
# Run individual example
cd examples/python
uv run python 01_basic_execution.py

# Run all examples
uv run python run_all_examples.py
```

## Examples

### Basic Features

1. **01_basic_execution.py** - Basic command execution
2. **02_multi_step_pipeline.py** - Multi-step workflows
3. **03_environment_variables.py** - Environment variables
4. **04_working_directory.py** - Working directories
5. **05_error_handling.py** - Error handling
6. **06_long_running_task.py** - Long-running tasks
7. **07_sandbox_reuse.py** - Sandbox reuse
8. **08_callback_hooks.py** - Callback hooks

### Advanced Features

9. **09_executor_container.py** - Executor container execution ⭐ **NEW**
   - Mixed execution modes (sidecar + executor)
   - Using executor-specific tools (pip, npm, etc.)
   - Performance comparison

## Executor Container Execution

Commands can be executed in either:
- **Sidecar (default)**: Fast (1-5ms)
- **Executor**: Slower (10-50ms), but has executor tools

```python
result = session.execute([
    # Fast: sidecar
    {"name": "list", "type": "Command", "command": ["ls", "-la"]},
    
    # Has executor tools
    {"name": "install", "type": "Command",
     "command": ["pip", "install", "requests"],
     "container": "executor"},
])
```

## More Information

- [Full Documentation](https://lincyaw.github.io/agent-env/)
- [IMPLEMENTATION_SUMMARY.md](../../IMPLEMENTATION_SUMMARY.md)
- [TEST_REPORT.md](../../TEST_REPORT.md)
