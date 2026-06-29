# arl-env Python SDK

High-level Python SDK for the `agent-env` Gateway API. The SDK creates sandbox
sessions, runs commands, streams output, transfers files, opens interactive
shells, and manages SandboxWarmPool resources through the gateway.

## Installation

```bash
pip install arl-env
# or
uv add arl-env
```

Interactive shell support needs the optional dependency:

```bash
pip install "arl-env[shell]"
```

## Authentication

If gateway authentication is enabled, provide a bearer API key through the
environment or constructor:

```bash
export ARL_API_KEY="your-api-key"
```

```python
from arl import SandboxSession

session = SandboxSession(
    image="python:3.12",
    profile="python-pool",
    gateway_url="http://localhost:8080",
    api_key="your-api-key",
)
```

## Basic Usage

```python
from arl import SandboxSession

with SandboxSession(
    image="python:3.12",
    profile="python-pool",
    namespace="default",
    gateway_url="http://localhost:8080",
) as session:
    result = session.execute([
        {"name": "hello", "command": ["echo", "Hello, World!"]},
    ])
    print(result.results[0].output.stdout)
```

Commands run in the executor container, which uses the requested image. The
sidecar only exposes the gRPC control plane and proxies execution to the
executor-agent over a Unix socket.

## Persistent Sessions

Use `keep_alive=True` when several operations should share the same workspace.
Always delete the session when finished.

```python
from arl import SandboxSession

session = SandboxSession(
    image="python:3.12",
    profile="python-pool",
    gateway_url="http://localhost:8080",
    keep_alive=True,
)

try:
    session.create_sandbox()
    session.execute([
        {"name": "init", "command": ["sh", "-c", "echo 0 > /workspace/count.txt"]},
    ])
    result = session.execute([
        {"name": "read", "command": ["cat", "/workspace/count.txt"]},
    ])
    print(result.results[0].output.stdout)
finally:
    session.delete_sandbox()
    session.close()
```

Attach to an existing session:

```python
from arl import SandboxSession

session = SandboxSession.attach("gw-12345", gateway_url="http://localhost:8080")
result = session.execute([{"name": "pwd", "command": ["pwd"]}])
session.close()
```

## Streaming Output

`execute()` uses the gateway SSE endpoint when available. Pass `on_output` to
receive stdout/stderr chunks while the step is still running.

```python
def on_output(stdout: str, stderr: str) -> None:
    if stdout:
        print(stdout, end="")
    if stderr:
        print(stderr, end="")

result = session.execute(
    [{"name": "loop", "command": ["sh", "-c", "for i in 1 2 3; do echo $i; sleep 1; done"]}],
    on_output=on_output,
)
```

## File Transfer

Paths are relative to the session workspace.

```python
session.upload_file("input.txt", "hello\n")
data = session.download_file("input.txt")

session.upload_path("local.bin", "data/local.bin")
session.download_path("data/local.bin", "out/local.bin")
```

## History, Restore, and Trajectory

Each executed step is recorded in session history. Snapshot IDs are step-index
strings used by the gateway's replay-based restore implementation.

```python
r1 = session.execute([{"name": "write", "command": ["sh", "-c", "echo one > /workspace/x"]}])
snapshot_id = r1.results[0].snapshot_id

session.execute([{"name": "change", "command": ["sh", "-c", "echo two > /workspace/x"]}])
session.restore(snapshot_id)

history = session.get_history()
jsonl = session.export_trajectory()
```

## WarmPool Management

`WarmPoolManager` uses the gateway pool endpoints. Pool creation is an admin
operation when gateway auth is enabled.

```python
from arl import ResourceRequirements, WarmPoolManager

manager = WarmPoolManager(namespace="default", gateway_url="http://localhost:8080")
manager.create_warmpool(
    name="python-pool",
    image="python:3.12",
    profile="python-pool",
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

Current sandbox-backed pools reject `tools` and `config_env` provisioning
requests. `list_tools()` and `call_tool()` only work when the executor image
already contains `/opt/arl/tools/registry.json` and matching tool files.

## Managed Sessions

`ManagedSession` creates or reuses a server-side managed pool for an image and
groups sessions by experiment ID.

```python
from arl import ManagedSession

with ManagedSession(
    image="python:3.12",
    experiment_id="exp-1",
    profile="default",
    gateway_url="http://localhost:8080",
) as session:
    result = session.execute([
        {"name": "hello", "command": ["python", "-c", "print('ok')"]},
    ])
    print(result.results[0].output.stdout)
```

Clean up an experiment:

```python
from arl import GatewayClient

client = GatewayClient(base_url="http://localhost:8080")
deleted = client.delete_experiment("exp-1")
```

## Core Classes

- `SandboxSession`: session lifecycle, execute, replay, restore, files, logs, history, trajectory.
- `ManagedSession`: image + experiment session flow with server-side pool creation.
- `GatewayClient`: low-level HTTP client for all public gateway REST endpoints.
- `WarmPoolManager`: pool create/list/get/wait/scale/logs/delete helpers.
- `InteractiveShellClient`: WebSocket shell client.
