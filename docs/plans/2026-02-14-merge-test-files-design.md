# Design: Merge Test Files with User-Friendly Output

**Date**: 2026-02-14
**Status**: Approved
**Purpose**: Merge `test_gateway_api.py` and `test_tools.py` into a single comprehensive test file that serves both as automated tests and educational examples.

## Overview

Create `test_arl_sdk.py` that combines all ARL SDK tests with beautiful, user-friendly terminal output using the `rich` library. The file should work both as a runnable test suite for CI/CD and as an interactive demo for developers learning the SDK.

## Structure

### Test Groups (executed in order)

1. **Prerequisites** - Health check, pool lifecycle (MUST pass to continue)
2. **Core Features** - Basic execution, snapshot/restore, history/trajectory
3. **Advanced Features** - Tool provisioning and calling
4. **Interactive** - WebSocket shell (optional, skipped if websockets not installed)

### File Organization

```
examples/python/
├── test_arl_sdk.py          # NEW: merged comprehensive test
├── test_interactive_shell.py  # KEEP: separate interactive tool
└── README.md                # UPDATE: reference new test file

DELETED:
├── test_gateway_api.py
└── test_tools.py
```

**Why keep test_interactive_shell.py separate**: It's an interactive exploration tool for manual use, not an automated test.

## Test Execution Flow

```python
def main():
    # 1. Setup phase
    - Parse arguments (--verbose, --gateway-url, --skip-cleanup)
    - Initialize clients (GatewayClient, WarmPoolManager)
    - Display banner with config

    # 2. Prerequisites (MUST pass to continue)
    - Health check → exit(1) if failed
    - Pool creation/wait → exit(1) if failed

    # 3. Run test suite (collect all results)
    - Basic execution
    - Snapshot & restore
    - History & trajectory
    - Tool provisioning (creates temporary pool)
    - Interactive shell (skip if websockets missing)

    # 4. Summary & cleanup
    - Display results table (name, status, duration)
    - Optional cleanup (--skip-cleanup to keep pool)
    - Exit with code 0 (all passed) or 1 (any failed)
```

### State Management

- Each test is independent using `with SandboxSession()` context manager
- Tests don't depend on each other's side effects
- Tool test creates its own temporary pool (`tools-demo-pool`) to avoid conflicts
- All tests return `(bool, float)` tuple: `(passed, duration_seconds)`

### Error Handling

- Graceful degradation: if one test fails, continue with others
- Clear error messages with actionable hints
- Distinguish between: test failure, infrastructure issue, missing dependencies

## Output Design

### Visual Components

```
╭──────────────────────────────────────────╮
│  ARL SDK Integration Tests               │
│  Gateway: http://14.103.184.145:8080    │
│  Pool: test-pool                         │
╰──────────────────────────────────────────╮

[1/7] ✓ Health Check (0.12s)
[2/7] ⏳ Creating WarmPool...
[2/7] ✓ WarmPool Management (12.5s)

╭─────────────────────────────────────────╮
│           Test Results                  │
├──────────────────┬────────┬─────────────┤
│ Test             │ Status │ Duration    │
├──────────────────┼────────┼─────────────┤
│ Health Check     │   ✓    │   0.12s     │
│ WarmPool Mgmt    │   ✓    │  12.45s     │
│ Basic Execution  │   ✓    │   2.34s     │
│ Snapshot/Restore │   ✓    │   3.21s     │
│ History Export   │   ✓    │   1.87s     │
│ Tool Calling     │   ✓    │   8.92s     │
│ Interactive Shell│  SKIP  │   0.00s     │
├──────────────────┴────────┴─────────────┤
│ 6/6 PASSED • Total: 28.91s              │
╰─────────────────────────────────────────╯
```

### Verbosity Levels

**Normal mode** (default):
- Section headers
- Test names with ✓/✗ status
- Duration for each test
- Summary table

**Verbose mode** (`--verbose`):
- All of the above, plus:
- Step-by-step execution details
- stdout/stderr snippets (truncated)
- Snapshot IDs, pod names, session IDs

### Color Scheme

- **Green**: ✓ passed tests, success messages
- **Red**: ✗ failed tests, error messages
- **Yellow**: ⏳ in progress, warnings, skipped tests
- **Blue**: section headers, info messages
- **Dim/Gray**: verbose details, timestamps

### Helpful Messages

- Health check fails → show `kubectl port-forward` command
- Pool not ready → show how to check pod logs (`kubectl logs...`)
- Websockets missing → "Install with: uv add websockets (optional)"

## Command-Line Interface

```bash
# Basic usage
uv run python examples/python/test_arl_sdk.py

# With verbose output
uv run python examples/python/test_arl_sdk.py --verbose

# Custom gateway
uv run python examples/python/test_arl_sdk.py --gateway-url http://localhost:8080

# Skip cleanup (keep pool for inspection)
uv run python examples/python/test_arl_sdk.py --skip-cleanup
```

### Arguments

```python
--verbose, -v         Show detailed output for each test step
--gateway-url URL     Gateway URL (default: http://14.103.184.145:8080)
--pool-name NAME      WarmPool name (default: test-pool)
--skip-cleanup        Skip pool cleanup after tests
```

## Dependencies

### New Dependency

Add to `sdk/python/arl/pyproject.toml`:
```toml
dependencies = [
    # ... existing ...
    "rich>=13.0.0",  # Beautiful terminal output
]
```

### Optional Dependencies

- `websockets` - Already optional, needed for interactive shell test

## Implementation Checklist

- [ ] Create `test_arl_sdk.py` with rich output formatting
- [ ] Implement all test functions from both source files
- [ ] Add command-line argument parsing
- [ ] Create summary table and progress indicators
- [ ] Add helpful error messages with hints
- [ ] Update `examples/python/README.md`
- [ ] Add `rich` dependency to pyproject.toml
- [ ] Delete `test_gateway_api.py` and `test_tools.py`
- [ ] Test the merged file end-to-end

## Success Criteria

- Single command runs all tests with clear visual feedback
- Works as both automated test suite and educational example
- Output is beautiful and easy to understand
- Helpful error messages guide users to solutions
- Both normal and verbose modes provide appropriate detail
- All existing test coverage is preserved
