# API Reference

Complete API reference for the ARL Python SDK.

## Module: arl

### SandboxSession

```python
class SandboxSession:
    """High-level API for sandbox management and execution via Gateway."""
```

#### Constructor

```python
def __init__(
    self,
    pool_ref: str,
    namespace: str = "default",
    gateway_url: str = "http://localhost:8080",
    keep_alive: bool = False,
    timeout: float = 300.0,
    idle_timeout_seconds: int | None = None,
) -> None
```

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `pool_ref` | `str` | Required | Name of the WarmPool to allocate from |
| `namespace` | `str` | `"default"` | Kubernetes namespace |
| `gateway_url` | `str` | `"http://localhost:8080"` | URL of the Gateway API |
| `keep_alive` | `bool` | `False` | Keep sandbox after context manager exit |
| `timeout` | `float` | `300.0` | HTTP request timeout in seconds |
| `idle_timeout_seconds` | `int \| None` | `None` | Auto-delete sandbox after idle time (defaults to 1800s when keep_alive=True) |

**Example:**

```python
from arl import SandboxSession

session = SandboxSession(
    pool_ref="python-pool",
    namespace="default",
    gateway_url="http://localhost:8080",
    keep_alive=True,
    timeout=60.0,
)
```

#### attach (classmethod)

```python
@classmethod
def attach(
    cls,
    session_id: str,
    gateway_url: str = "http://localhost:8080",
    timeout: float = 300.0,
    keep_alive: bool = True,
) -> SandboxSession
```

Attach to an existing session by session ID.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `session_id` | `str` | Required | The session ID to attach to |
| `gateway_url` | `str` | `"http://localhost:8080"` | Gateway base URL |
| `timeout` | `float` | `300.0` | HTTP request timeout |
| `keep_alive` | `bool` | `True` | If False, context manager exit deletes the session |

**Example:**

```python
session = SandboxSession.attach("gw-12345", gateway_url="http://localhost:8080")
result = session.execute([{"name": "ls", "command": ["ls"]}])
session.delete_sandbox()
```

#### Context Manager

```python
def __enter__(self) -> "SandboxSession"
def __exit__(self, exc_type, exc_val, exc_tb) -> None
```

Automatically creates a sandbox on enter. On exit, deletes the sandbox unless `keep_alive=True`.

**Example:**

```python
with SandboxSession(
    pool_ref="python-pool",
    gateway_url="http://localhost:8080",
) as session:
    result = session.execute([...])
# Sandbox is automatically deleted
```

#### create_sandbox

```python
def create_sandbox(self) -> SessionInfo
```

Create a new session (sandbox) via the Gateway.

**Returns:** `SessionInfo` with sandbox details (session ID, pod IP, pod name, etc.)

**Example:**

```python
session = SandboxSession(
    pool_ref="python-pool",
    gateway_url="http://localhost:8080",
    keep_alive=True,
)
info = session.create_sandbox()
print(f"Session ID: {info.id}, Pod IP: {info.pod_ip}")
# ... use sandbox
session.delete_sandbox()
```

#### delete_sandbox

```python
def delete_sandbox(self) -> None
```

Delete the session and its underlying sandbox.

#### execute

```python
def execute(
    self,
    steps: list[dict[str, Any]],
    trace_id: str | None = None,
) -> ExecuteResponse
```

Execute steps in the sandbox. Returns synchronously.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `steps` | `list[dict]` | Required | List of step definitions |
| `trace_id` | `str \| None` | `None` | Optional trace ID for distributed tracing |

**Returns:** `ExecuteResponse` with per-step results, snapshot IDs, and durations.

**Example:**

```python
result = session.execute([
    {"name": "step1", "command": ["echo", "hello"]},
])
print(result.results[0].output.stdout)  # "hello\n"
print(result.results[0].snapshot_id)     # git snapshot ID
print(result.total_duration_ms)          # total execution time in ms
```

#### restore

```python
def restore(self, snapshot_id: str) -> None
```

Restore workspace to a previous step's snapshot. Each step execution automatically creates a snapshot.

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `snapshot_id` | `str` | Snapshot ID (git commit SHA) from a step result |

**Example:**

```python
r1 = session.execute([{"name": "step1", "command": ["echo", "first"]}])
snap = r1.results[0].snapshot_id

r2 = session.execute([{"name": "step2", "command": ["rm", "-rf", "/workspace/*"]}])

# Rollback to state after step1
session.restore(snap)
```

#### get_history

```python
def get_history(self) -> list[StepResult]
```

Get complete execution history for this session.

**Returns:** List of `StepResult` with output, snapshot IDs, and durations.

#### export_trajectory

```python
def export_trajectory(self) -> str
```

Export execution history as JSONL trajectory (for RL/SFT training).

**Returns:** JSONL string, one entry per step.

#### list_tools

```python
def list_tools(self) -> ToolsRegistry
```

List all available tools in the sandbox (reads `/opt/arl/tools/registry.json`).

**Returns:** `ToolsRegistry` with all tool manifests.

#### call_tool

```python
def call_tool(
    self,
    tool_name: str,
    params: dict[str, object] | None = None,
) -> ToolResult
```

Call a tool by name with JSON parameters.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `tool_name` | `str` | Required | Name of the tool (must exist in registry) |
| `params` | `dict \| None` | `None` | Parameters dict (passed as JSON stdin) |

**Returns:** `ToolResult` with parsed JSON output, exit code, and stderr.

---

### GatewayClient

Low-level HTTP client for the ARL Gateway API. Used internally by `SandboxSession`.

```python
class GatewayClient:
    """HTTP client for the ARL Gateway API."""
```

#### Constructor

```python
def __init__(self, base_url: str = "http://localhost:8080", timeout: float = 300.0) -> None
```

**Methods:**

| Method | Description |
|--------|-------------|
| `create_session(pool_ref, namespace, idle_timeout_seconds)` | Create a new session |
| `get_session(session_id)` | Get session info |
| `delete_session(session_id)` | Delete a session |
| `execute(session_id, steps, trace_id)` | Execute steps |
| `restore(session_id, snapshot_id)` | Restore to a snapshot |
| `get_history(session_id)` | Get execution history |
| `get_trajectory(session_id)` | Export trajectory as JSONL |
| `create_pool(name, namespace, image, replicas, tools)` | Create a WarmPool |
| `get_pool(name, namespace)` | Get pool status |
| `delete_pool(name, namespace)` | Delete a pool |
| `health()` | Health check |

### WarmPoolManager

Manage WarmPool resources programmatically.

```python
class WarmPoolManager:
    """Manage WarmPool resources."""
```

#### Constructor

```python
def __init__(self, namespace: str = "default") -> None
```

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `namespace` | `str` | `"default"` | Kubernetes namespace |

#### create_warmpool

```python
def create_warmpool(
    self,
    name: str,
    image: str,
    sidecar_image: str,
    replicas: int = 1,
    command: list[str] | None = None,
) -> None
```

Create a new WarmPool.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `name` | `str` | Required | WarmPool name |
| `image` | `str` | Required | Executor container image |
| `sidecar_image` | `str` | Required | Sidecar container image |
| `replicas` | `int` | `1` | Number of pods to maintain |
| `command` | `list[str] \| None` | `None` | Container command |

**Example:**

```python
manager = WarmPoolManager(namespace="default")
manager.create_warmpool(
    name="python-pool",
    image="python:3.9-slim",
    sidecar_image="arl-sidecar:latest",
    replicas=3,
)
```

#### get_warmpool

```python
def get_warmpool(self, name: str) -> dict
```

Get WarmPool details.

#### wait_for_warmpool_ready

```python
def wait_for_warmpool_ready(
    self,
    name: str,
    timeout: int = 300,
) -> None
```

Wait for WarmPool to be ready.

**Raises:** `TimeoutError` if pool is not ready within timeout.

#### delete_warmpool

```python
def delete_warmpool(self, name: str) -> None
```

Delete a WarmPool.

---

## Execution Steps

Steps are passed to `session.execute()` as a list of dictionaries. Each step specifies a command to run.

```python
{
    "name": str,           # Required: Step name (unique within batch)
    "command": list[str],  # Required: Command and arguments
    "env": dict[str, str], # Optional: Environment variables
    "workDir": str,        # Optional: Working directory (default: /workspace)
    "timeout": int,        # Optional: Timeout in seconds
}
```

**Example:**

```python
{
    "name": "run_python",
    "command": ["python", "-c", "print('hello')"],
    "env": {"PYTHONPATH": "/workspace/lib"},
    "workDir": "/workspace",
}
```

---

## Result Types

### ExecuteResponse

Returned by `session.execute()`.

```python
class ExecuteResponse:
    session_id: str              # Session ID
    results: list[StepResult]    # Per-step results
    total_duration_ms: int       # Total execution time in ms
```

### StepResult

Individual step result within an `ExecuteResponse`.

```python
class StepResult:
    index: int              # Zero-based step index
    name: str               # Step name
    output: StepOutput      # Command output
    snapshot_id: str        # Git snapshot ID for restore
    duration_ms: int        # Step execution time in ms
    timestamp: datetime     # Execution timestamp
```

### StepOutput

Command output for a step.

```python
class StepOutput:
    stdout: str       # Standard output
    stderr: str       # Standard error
    exit_code: int    # Exit code (0 = success)
```

### SessionInfo

Returned by `create_sandbox()` and `attach()`.

```python
class SessionInfo:
    id: str               # Session ID
    sandbox_name: str     # Sandbox CRD name
    namespace: str        # Kubernetes namespace
    pool_ref: str         # WarmPool name
    pod_ip: str           # Pod IP address
    pod_name: str         # Pod name
    created_at: datetime  # Creation timestamp
```

### PoolInfo

Returned by `GatewayClient.get_pool()`.

```python
class PoolInfo:
    name: str                       # WarmPool name
    namespace: str                  # Kubernetes namespace
    replicas: int                   # Desired pod count
    ready_replicas: int             # Ready idle pods
    allocated_replicas: int         # Allocated pods
    conditions: list[PoolCondition] # Status conditions
```

### ToolResult

Returned by `session.call_tool()`.

```python
class ToolResult:
    raw_output: str            # Raw stdout string
    parsed: dict[str, object]  # Parsed JSON output
    exit_code: int             # Exit code
    stderr: str                # Standard error
```

---

## Exceptions

### GatewayError

Raised for Gateway API errors.

```python
class GatewayError(Exception):
    status_code: int  # HTTP status code
    error: str        # Error message
    detail: str       # Additional detail
```

**Example:**

```python
from arl import GatewayError

try:
    result = session.execute([...])
except GatewayError as e:
    if e.status_code == 404:
        print("Session or WarmPool not found")
    else:
        print(f"Gateway error: {e.status_code} - {e.error}")
```

### PoolNotReadyError

Raised when a WarmPool cannot become ready.

```python
class PoolNotReadyError(Exception):
    pool_name: str          # Pool name
    conditions: list        # Pool conditions
```

### TimeoutError

Raised when operations exceed their timeout.

```python
try:
    result = session.execute([...])
except TimeoutError:
    print("Execution timed out")
```
