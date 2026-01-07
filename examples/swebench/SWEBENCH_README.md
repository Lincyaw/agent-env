# SWE-bench Example

Complete demonstration of using ARL for SWE-bench (Software Engineering Benchmark) scenarios, where LLM agents automatically fix bugs in real-world codebases.

## Overview

Shows end-to-end workflow for automated bug fixing:
1. **WarmPool Setup** - Create warm pools via Python SDK (no YAML!)
2. **Issue Analysis** - Mock agent analyzes bug reports
3. **Environment Inspection** - Examine codebase structure
4. **Patch Application** - Apply LLM-generated fixes
5. **Test Execution** - Run test suites to verify fixes
6. **Result Validation** - Generate fix reports

## Quick Start

```bash
# Basic usage with mock agent
python swebench_example.py

# With callback hooks for event-driven workflow
python swebench_example.py --use-callbacks

# Without mock agent (inline fixtures)
python swebench_example.py --no-mock-agent
```

## SWE-bench Image

Uses `swebench/swesmith.x86_64.emotion_1776_js-emotion.b882bcba` - a SWE-bench instance for the [emotion](https://github.com/emotion-js/emotion) JavaScript library with a reproducible bug and test suite

## Prerequisites

- Kubernetes cluster with ARL operator deployed
- Python SDK: `cd examples/python && uv sync`

## What It Does

### Mock Agent Workflow (default)
- **Analyzes issue** from fixture files
- **Generates patch** to fix the bug
- **Creates test script** to verify fix
- **Generates report** documenting the fix

### Core Workflow
1. **WarmPool Setup** - Creates/reuses SWE-bench pool automatically
2. **Environment Inspection** - Checks testbed structure and repo state
3. **Patch Application** - Applies code fix to target files
4. **Test Execution** - Runs test suite and reports results
5. **Report Generation** - Documents issue, fix, and verification

### Optional: Callbacks (--use-callbacks)
- Event-driven hooks for task completion
- Automatic test execution after patches
- Success/failure handlers

## Features Demonstrated

- **No Kubernetes knowledge required** - Python SDK handles all infrastructure
- **Mock agent simulation** - Test LLM agent workflows without actual LLM calls
- **Event-driven callbacks** - Hook into task lifecycle events
- **Fixture-based testing** - Separate test data from code logic
- **WarmPool management** - Fast pod startup with pre-warmed environments

## Use Cases

- **Automated Bug Fixing** - LLM agents that generate and test code fixes
- **CI/CD Integration** - Automated testing of proposed fixes
- **Research Benchmarking** - Evaluating LLM code repair capabilities
- **Code Repair Systems** - Building automated software maintenance tools

## Architecture

```python
# WarmPool setup (programmatic, no YAML!)
manager = WarmPoolManager(namespace="default")
manager.create_warmpool(
    name="swebench-emotion",
    image="swebench/swesmith.x86_64.emotion_1776_js-emotion.b882bcba",
    replicas=2,
    testbed_path="/testbed"
)

# Mock agent simulation
agent = create_mock_agent(fixtures_dir)
analysis = agent.analyze_issue()    # From fixture files
patch = agent.generate_patch()
test_script = agent.generate_test_script()
report = agent.generate_report()

# Sandbox execution with callback hooks
with SandboxSession(pool_ref="swebench-emotion") as session:
    session.register_callback("on_task_success", success_callback)
    session.execute(steps)
```

## Files

- **swebench_example.py** - Unified example with all features
- **mock_agent.py** - Mock LLM agent implementation
- **swebench_fixtures/** - Test data (patches, scripts, reports)

## Related Examples

- [examples/python/](../python/) - Basic ARL SDK usage examples
- [01_basic_execution.py](../python/01_basic_execution.py) - Basic task execution
- [07_sandbox_reuse.py](../python/07_sandbox_reuse.py) - Sandbox reuse patterns

## References

- [SWE-bench Paper](https://www.swebench.com/)
- [SWE-bench Repository](https://github.com/princeton-nlp/SWE-bench)
- [Emotion Library](https://github.com/emotion-js/emotion)
