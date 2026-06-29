# API Reference

This page documents the current hand-written Python SDK surface. It is based on
the code in `sdk/python/arl/arl`.

## SandboxSession

High-level session manager backed by the Gateway REST API.

```python
SandboxSession(
    image: str | None = None,
    *,
    profile: str | None = "default",
    namespace: str = "default",
    gateway_url: str = "http://localhost:8080",
    keep_alive: bool = False,
    timeout: float = 300.0,
    idle_timeout_seconds: int | None = None,
    max_lifetime_seconds: int | None = None,
    api_key: str | None = None,
)
```

Parameters:

| Parameter | Description |
| --- | --- |
| `image` | Executor image. When set, the gateway ensures a matching sandbox-backed pool exists. |
| `profile` | Pool selection profile. Use `None` or `""` only when selecting purely by image. |
| `namespace` | Kubernetes namespace for pool/session resources. |
| `gateway_url` | Gateway base URL. |
| `keep_alive` | Keep the session after context manager exit. |
| `timeout` | HTTP request timeout in seconds. |
| `idle_timeout_seconds` | Per-session idle timeout override. With `keep_alive=True`, defaults to 1800 seconds if omitted. |
| `max_lifetime_seconds` | Per-session max lifetime override. |
| `api_key` | Bearer API key. Defaults to `ARL_API_KEY`. |

### Lifecycle

```python
info = session.create_sandbox()
session.delete_sandbox()
session.close()
```

`SandboxSession` is a context manager. It creates a session on enter and deletes
it on exit unless `keep_alive=True`.

```python
with SandboxSession(image="python:3.12", profile="python-pool") as session:
    result = session.execute([
        {"name": "hello", "command": ["echo", "hello"]},
    ])
```

Attach to an existing session without creating a new sandbox:

```python
session = SandboxSession.attach(
    "gw-12345",
    gateway_url="http://localhost:8080",
    keep_alive=True,
)
```

### Execution

```python
result = session.execute(
    steps: list[dict[str, object]],
    trace_id: str | None = None,
    on_output: Callable[[str, str], None] | None = None,
)
```

Step dictionaries are serialized directly to the gateway:

```python
{
    "name": "run",
    "command": ["python", "-c", "print('hello')"],
    "env": {"MODE": "test"},
    "workDir": "/workspace",
}
```

The Python `StepRequest` model also has a `timeout` field, but the current Go
gateway request type does not consume per-step timeouts. Use the SDK HTTP
`timeout` constructor argument for client-side request timeouts.

Streaming output:

```python
def on_output(stdout: str, stderr: str) -> None:
    print(stdout or stderr, end="")

result = session.execute(
    [{"name": "stream", "command": ["sh", "-c", "echo start; sleep 1; echo end"]}],
    on_output=on_output,
)
```

### Files

All file paths are relative to the session workspace.

```python
session.upload_file(path, content, sha256=None)
data = session.download_file(path)
session.upload_path(local_path, remote_path, sha256=None)
session.download_path(remote_path, local_path)
for chunk in session.iter_download(path, chunk_size=1024 * 1024):
    ...
```

### History, Restore, and Trajectory

```python
history = session.get_history()
jsonl = session.export_trajectory()
session.restore(snapshot_id)
```

Snapshot IDs are step-index strings produced by the gateway. Restore allocates a
new runtime from the same pool and replays history up to the requested step.

### Tools

```python
registry = session.list_tools()
result = session.call_tool("tool-name", {"arg": "value"})
```

These helpers read and execute files under `/opt/arl/tools` in the executor
container. Current sandbox-backed pool creation rejects `tools` provisioning
requests, so these methods only work when the executor image already includes
the registry and tool files.

## ManagedSession

`ManagedSession` is a `SandboxSession` variant that creates sessions through
`POST /v1/managed/sessions` and groups them by experiment ID.

```python
ManagedSession(
    image: str,
    experiment_id: str,
    namespace: str = "default",
    gateway_url: str = "http://localhost:8080",
    timeout: float = 300.0,
    resources: ResourceRequirements | None = None,
    tools: ToolsSpec | None = None,
    workspace_dir: str = "/workspace",
    idle_timeout_seconds: int | None = None,
    max_lifetime_seconds: int | None = None,
    config_env: ConfigEnvSpec | dict[str, object] | None = None,
    profile: str = "default",
    api_key: str | None = None,
)
```

Current gateway code accepts `resources`, `workspace_dir`, timeout overrides,
image, profile, namespace, and experiment ID. It rejects managed-session
`tools` and `config_env` payloads for sandbox-backed pools.

Example:

```python
from arl import ManagedSession

with ManagedSession(
    image="python:3.12",
    experiment_id="exp-1",
    gateway_url="http://localhost:8080",
) as session:
    result = session.execute([
        {"name": "hello", "command": ["echo", "hello"]},
    ])
```

## GatewayClient

Low-level HTTP client.

```python
GatewayClient(
    base_url: str = "http://localhost:8080",
    timeout: float = 300.0,
    api_key: str | None = None,
    auth: httpx.Auth | None = None,
)
```

Important methods:

| Method | Description |
| --- | --- |
| `create_session(...)` | Create a regular session. |
| `get_session(session_id)` | Fetch session metadata. |
| `list_sessions()` | List active sessions. |
| `delete_session(session_id)` | Delete and release a session. |
| `execute(session_id, steps, trace_id=None, on_output=None)` | Execute steps, preferring SSE streaming. |
| `upload_file(...)`, `download_file(...)` | Transfer one workspace file. |
| `upload_path(...)`, `download_path(...)`, `iter_download_file(...)` | Stream local/workspace files. |
| `replay_from(session_id, source_session_id, up_to_step=None)` | Replay another session's history into a target session. |
| `restore(session_id, snapshot_id)` | Restore a session to a previous step index. |
| `get_history(session_id)` | Get step history. |
| `get_trajectory(session_id)` | Get JSONL trajectory. |
| `iter_session_logs(...)`, `list_session_logs(...)` | Stream or collect session logs. |
| `create_pool(...)`, `list_pools(...)`, `get_pool(...)`, `scale_pool(...)`, `delete_pool(...)` | Manage SandboxWarmPools. |
| `iter_pool_logs(...)`, `list_pool_logs(...)` | Stream or collect pool logs. |
| `create_managed_session(...)` | Create a managed experiment session. |
| `list_experiments()` | List managed experiment summaries. |
| `list_experiment_sessions(experiment_id)` | List active sessions for an experiment. |
| `delete_experiment(experiment_id)` | Delete all sessions for an experiment and return the count. |
| `health()` | Check `/healthz`. |

## WarmPoolManager

Convenience wrapper around `GatewayClient` pool APIs.

```python
WarmPoolManager(
    namespace: str = "default",
    gateway_url: str = "http://localhost:8080",
    timeout: float = 300.0,
    api_key: str | None = None,
)
```

Methods:

```python
manager.create_warmpool(
    name: str,
    image: str,
    replicas: int = 2,
    profile: str = "default",
    tools: ToolsSpec | None = None,
    resources: ResourceRequirements | None = None,
    workspace_dir: str = "/workspace",
    config_env: ConfigEnvSpec | dict[str, object] | None = None,
)
info = manager.get_warmpool(name)
infos = manager.list_warmpools()
info = manager.wait_for_ready(name, timeout=300.0, poll_interval=5.0, min_ready=1)
info = manager.scale_warmpool(name, replicas, resources=None)
logs = manager.get_logs(name, tail=100)
manager.delete_warmpool(name)
```

Current gateway code rejects `tools`, `config_env`, and resource updates during
`scale_warmpool`. Provide resources at pool creation time.

## InteractiveShellClient

WebSocket client for `/v1/sessions/{id}/shell`.

```python
client = InteractiveShellClient(
    gateway_url="http://localhost:8080",
    api_key=None,
)
client.connect(session_id)
client.send_input("ls -la\n")
msg = client.read_message(timeout=1.0)
client.send_signal("SIGINT")
client.send_resize(cols=120, rows=40)
client.close()
```

Requires the `websockets` package.

## Types

### ExecuteResponse

```python
class ExecuteResponse:
    session_id: str
    results: list[StepResult]
    total_duration_ms: int
```

### StepResult

```python
class StepResult:
    index: int
    name: str
    output: StepOutput
    snapshot_id: str
    duration_ms: int
    timestamp: datetime | None
```

### StepOutput

```python
class StepOutput:
    stdout: str
    stderr: str
    exit_code: int
```

### SessionInfo

```python
class SessionInfo:
    id: str
    sandbox_name: str
    namespace: str
    image: str
    profile: str
    pod_ip: str
    pod_name: str
    created_at: datetime | None
```

### PoolInfo

```python
class PoolInfo:
    name: str
    namespace: str
    image: str
    profile: str
    replicas: int
    ready_replicas: int
    allocated_replicas: int
    conditions: list[PoolCondition]
```

### ResourceRequirements

```python
ResourceRequirements(
    requests={"cpu": "500m", "memory": "512Mi"},
    limits={"cpu": "1", "memory": "1Gi"},
)
```

### ToolResult

```python
class ToolResult:
    raw_output: str
    parsed: dict[str, object]
    exit_code: int
    stderr: str
```

## Exceptions

- `GatewayError`: gateway HTTP error with `status_code`, `error`, and `detail`.
- `PoolNotReadyError`: pool has failing pods or cannot become ready.
- `TimeoutError`: raised by wait helpers or underlying HTTP clients when timeouts expire.
