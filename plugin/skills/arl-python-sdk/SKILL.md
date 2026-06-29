---
name: arl-python-sdk
description: |
  Guide for using the `arl-env` Python SDK (`from arl import ...`) to create and attach ARL sandbox sessions, execute commands, stream output, transfer files, restore snapshots, replay history, export trajectories, manage managed experiments, create and scale warm pools, open interactive shells, and call the Gateway REST API from Python. Use this skill whenever the user asks how to write Python code against ARL, use the ARL SDK, run sandbox commands programmatically, manage ARL experiments from Python, upload/download files through the SDK, use ManagedSession, SandboxSession, WarmPoolManager, GatewayClient, InteractiveShellClient, or says "arl python sdk", "Python 里怎么用 arl", "用 Python 创建 session", "SDK 导出 trajectory", or wants SDK examples instead of CLI commands.
---

# ARL Python SDK

The Python package is `arl-env`; the import package is `arl`.

```bash
uv add arl-env
# or
pip install arl-env

# Interactive shell support only:
uv add "arl-env[shell]"
# or
pip install "arl-env[shell]"
```

The SDK talks to the ARL Gateway HTTP/WebSocket API. It does not call
Kubernetes directly.

## Setup

Default gateway URL is `http://localhost:8080`. Pass `gateway_url` explicitly
when using a port-forward, remote gateway, or test endpoint.

```python
from arl import SandboxSession

with SandboxSession(
    image="python:3.12",
    profile="default",
    namespace="arl",
    gateway_url="http://127.0.0.1:8080",
) as session:
    result = session.execute([
        {"name": "hello", "command": ["python", "-c", "print('ok')"]},
    ])
    print(result.results[0].output.stdout)
```

If gateway auth is enabled, use `ARL_API_KEY` or pass `api_key`. For static
bearer auth, constructor `api_key` and `ARL_API_KEY` are equivalent. For OIDC
client-credentials flows, pass `auth=SsoTokenAuth(...)` to `GatewayClient`.

```bash
export ARL_API_KEY=your-token
```

```python
from arl import SandboxSession

session = SandboxSession(
    image="python:3.12",
    gateway_url="http://127.0.0.1:8080",
    api_key="your-token",
)
```

## Choose the API

| Use case | API |
| --- | --- |
| Create one sandbox, run commands, move files, export trajectory | `SandboxSession` |
| Reuse or inspect an existing session ID | `SandboxSession.attach(...)` |
| Group many sessions under an experiment and let the server manage pools | `ManagedSession` |
| Create, list, wait for, scale, log, or delete warm pools | `WarmPoolManager` |
| Use a low-level REST endpoint or admin listing/cleanup | `GatewayClient` |
| Drive a WebSocket shell / PTY | `InteractiveShellClient` |

Prefer `ManagedSession` for benchmark/training runs that need experiment-level
cleanup. Prefer `SandboxSession` for ad hoc execution or when an existing pool
already exists.

## SandboxSession Lifecycle

Use a context manager for temporary sessions. It creates the sandbox on entry
and deletes it on exit.

```python
from arl import SandboxSession

with SandboxSession(image="python:3.12", profile="default") as session:
    result = session.execute([
        {"name": "pwd", "command": ["pwd"]},
        {"name": "list", "command": ["ls", "-la", "/workspace"]},
    ])
    for step in result.results:
        print(step.name, step.output.exit_code, step.output.stdout)
```

For multi-step workflows that must survive across Python scopes, set
`keep_alive=True` and clean up explicitly.

```python
from arl import SandboxSession

session = SandboxSession(
    image="python:3.12",
    profile="default",
    keep_alive=True,
    idle_timeout_seconds=1800,
)

try:
    info = session.create_sandbox()
    print(info.id)
    session.execute([
        {"name": "init", "command": ["sh", "-c", "echo one > /workspace/value.txt"]},
    ])
finally:
    session.delete_sandbox()
    session.close()
```

`keep_alive=True` with a context manager intentionally does not delete the
sandbox on exit. Use `delete_sandbox()` when finished.

Attach to an existing session without creating a new sandbox:

```python
from arl import SandboxSession

session = SandboxSession.attach("gw-12345", gateway_url="http://127.0.0.1:8080")
try:
    result = session.execute([{"name": "whoami", "command": ["whoami"]}])
finally:
    session.close()
```

## Execute Commands

`execute()` takes a list of step dictionaries. Each step normally has `name`
and `command`; optional fields include `env`, `workDir`, and `timeout`.

```python
result = session.execute([
    {
        "name": "train",
        "command": ["sh", "-c", "python train.py > out.log 2>&1"],
        "env": {"PYTHONUNBUFFERED": "1"},
        "workDir": "/workspace",
        "timeout": 600,
    },
])

step = result.results[0]
print(step.output.exit_code)
print(step.output.stdout)
print(step.output.stderr)
print(step.snapshot_id)
```

Use `["sh", "-c", "..."]` when the command needs shell features such as pipes,
redirection, variable expansion, or `&&`. Otherwise pass argv directly.

Streaming output is available by passing `on_output`; the SDK requests the
Gateway SSE endpoint when a callback is provided.

```python
def on_output(stdout: str, stderr: str) -> None:
    if stdout:
        print(stdout, end="")
    if stderr:
        print(stderr, end="")

session.execute(
    [{"name": "loop", "command": ["sh", "-c", "for i in 1 2 3; do echo $i; sleep 1; done"]}],
    on_output=on_output,
)
```

For long idempotent operations, pass `operation_id`. If the HTTP request times
out while the gateway continues running the operation, catch
`GatewayOperationTimeout` and query `GatewayClient.get_execute_operation(...)`.

## Files

SDK file paths are workspace-relative. Avoid leading slashes unless deliberately
targeting gateway-side normalized behavior.

```python
session.upload_file("data/input.txt", "hello\n")
payload = session.download_file("data/input.txt")  # bytes

session.upload_path("local.bin", "data/local.bin")
session.download_path("data/local.bin", "out/local.bin")

for chunk in session.iter_download("data/big.bin"):
    process(chunk)
```

Pass `sha256` to `upload_file` or `upload_path` when the caller needs integrity
verification.

## History, Restore, Replay, and Trajectory

Every executed step records history and a snapshot ID. Use the snapshot ID from
a `StepResult` for restore.

```python
r1 = session.execute([
    {"name": "write", "command": ["sh", "-c", "echo one > /workspace/x"]},
])
snapshot_id = r1.results[0].snapshot_id

session.execute([
    {"name": "change", "command": ["sh", "-c", "echo two > /workspace/x"]},
])
session.restore(snapshot_id)

history = session.get_history()
trajectory_jsonl = session.export_trajectory()
```

Replay copies recorded steps from another session into the current session:

```python
target = SandboxSession(image="python:3.12", keep_alive=True)
try:
    target.create_sandbox()
    target.replay_from(source_session_id="gw-source", up_to_step=3)
finally:
    target.delete_sandbox()
    target.close()
```

## Managed Experiments

`ManagedSession` creates or reuses a server-side managed pool for an image and
groups sessions by `experiment_id`.

```python
from arl import GatewayClient, ManagedSession, ResourceRequirements

with ManagedSession(
    image="python:3.12",
    experiment_id="exp-1",
    namespace="arl",
    profile="default",
    gateway_url="http://127.0.0.1:8080",
    resources=ResourceRequirements(
        requests={"cpu": "500m", "memory": "512Mi"},
        limits={"cpu": "1", "memory": "1Gi"},
    ),
) as session:
    result = session.execute([
        {"name": "hello", "command": ["python", "-c", "print('ok')"]},
    ])
    print(result.results[0].output.stdout)

with GatewayClient(base_url="http://127.0.0.1:8080") as client:
    sessions = client.list_experiment_sessions("exp-1")
    deleted = client.delete_experiment("exp-1")
```

Pool creation, managed sessions, global session listing, and experiment cleanup
may require an admin key when gateway auth is enabled.

## Warm Pools

Use `WarmPoolManager` for explicit pool lifecycle management.

```python
from arl import ResourceRequirements, WarmPoolManager

with WarmPoolManager(namespace="arl", gateway_url="http://127.0.0.1:8080") as manager:
    manager.create_warmpool(
        name="python-pool",
        image="python:3.12",
        profile="default",
        replicas=2,
        resources=ResourceRequirements(
            requests={"cpu": "500m", "memory": "512Mi"},
            limits={"cpu": "1", "memory": "1Gi"},
        ),
    )
    info = manager.wait_for_ready("python-pool", min_ready=1)
    print(info.ready_replicas)
    manager.scale_warmpool("python-pool", replicas=3)
```

Read logs with `manager.get_logs(name, tail=100)` or stream with
`manager.iter_logs(name, follow=True, tail=50)`.

## GatewayClient

Use `GatewayClient` when the high-level classes do not expose the exact
operation needed.

```python
from arl import GatewayClient

with GatewayClient(base_url="http://127.0.0.1:8080") as client:
    assert client.health()
    sessions = client.list_sessions()
    pools = client.list_pools(namespace="arl")
    experiments = client.list_experiments()
```

Important methods:

| Area | Methods |
| --- | --- |
| Sessions | `create_session`, `get_session`, `delete_session`, `list_sessions` |
| Execution | `execute`, `get_execute_operation` |
| Files | `upload_file`, `download_file`, `upload_path`, `download_path`, `iter_download_file` |
| Replay | `restore`, `replay_from`, `get_history`, `get_trajectory` |
| Logs | `iter_session_logs`, `list_session_logs`, `iter_pool_logs`, `list_pool_logs` |
| Pools | `create_pool`, `list_pools`, `get_pool`, `scale_pool`, `delete_pool` |
| Experiments | `create_managed_session`, `list_experiment_sessions`, `list_experiments`, `delete_experiment` |

Catch `GatewayError` to inspect gateway API failures:

```python
from arl import GatewayClient, GatewayError

with GatewayClient(base_url="http://127.0.0.1:8080") as client:
    try:
        client.get_session("missing-session-id")
    except GatewayError as exc:
        print(exc.status_code, exc.error, exc.detail)
```

Non-zero command exit codes are returned in `StepResult.output.exit_code`; they
do not necessarily raise `GatewayError`.

## Interactive Shell

Install the optional shell dependency before using this API.

```python
from arl import InteractiveShellClient

shell = InteractiveShellClient(gateway_url="http://127.0.0.1:8080")
try:
    shell.connect(session.session_id)
    shell.send_resize(cols=120, rows=40)
    shell.send_input("pwd\n")
    print(shell.read_output(timeout=2.0))
    shell.send_signal("SIGINT")
finally:
    shell.close()
```

Use `read_message()` instead of `read_output()` when the caller needs `exit` or
`error` events.

## Config, Tools, and Resources

Resource quantities follow Kubernetes format:

```python
from arl import ResourceRequirements

resources = ResourceRequirements(
    requests={"cpu": "500m", "memory": "512Mi", "ephemeral-storage": "2Gi"},
    limits={"cpu": "2", "memory": "2Gi"},
)
```

`ConfigEnvSpec` can pass environment variables, ConfigMaps, and Secrets to pool
or managed-session creation when the deployed gateway supports that payload.

```python
from arl import ConfigEnvSpec

config_env = ConfigEnvSpec(vars={"HTTP_PROXY": "http://proxy:7890"})
```

`list_tools()` and `call_tool()` operate inside the executor image by reading
`/opt/arl/tools/registry.json` and executing matching tool files. Use them only
with images that already contain the ARL tool registry, or with a deployment
that provisions tools into the executor.

## Verification

For this repository, use the runnable smoke suite when a gateway is available:

```bash
cd examples/python
uv run python test_arl_sdk.py \
  --gateway-url http://127.0.0.1:8080 \
  --namespace arl \
  --pool-image busybox:latest
```

Run SDK unit checks from the repository root with:

```bash
uv run pytest sdk/python/arl/tests
```

Or from the SDK package directory:

```bash
cd sdk/python/arl
uv run pytest tests
```

## Best Practices

- Use `uv` for repository-local Python work.
- Pass `gateway_url`, `namespace`, and `api_key` explicitly in examples and automation.
- Prefer `ManagedSession` plus `GatewayClient.delete_experiment()` for grouped training or benchmark runs.
- Prefer context managers for temporary `SandboxSession`, `WarmPoolManager`, and `GatewayClient` objects.
- Close attached or keep-alive sessions with `close()`; delete owned persistent sessions with `delete_sandbox()`.
- Check `StepResult.output.exit_code` for command failure; do not rely on exceptions for failed commands.
- Use `workDir` in raw step dictionaries, not Python field name `work_dir`.
- Use workspace-relative file paths for uploads and downloads.
- Capture `snapshot_id` values whenever rollback or replay matters.
