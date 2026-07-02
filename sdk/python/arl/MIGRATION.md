# arl-env Python SDK Migration Guide

This guide covers migration from the Python SDK `0.14.1` API to `0.15.6`.

## Upgrade Checklist

1. Replace `pool_ref` session creation with `image` and, when needed,
   `profile`.
2. Replace `SessionInfo.pool_ref` reads with `SessionInfo.profile` or
   `SessionInfo.image`.
3. Remove `namespace` arguments from SDK and CLI calls; the gateway uses its
   configured scope.
4. Audit calls to `GatewayClient.create_session()` and use keyword arguments.
5. Update file upload code that passed `encoding`.
6. Remove `ManagedSession` scaling hint arguments.
7. Remove `keep_alive` arguments and use manual lifecycle management for
   detachable sessions.
8. Add operation recovery handling for long non-streaming executions.
9. Update replay response handling from dict access to typed attributes.
10. Update examples, smoke tests, and docs that mention sidecar execution or the
   old gateway service name.

## Session Creation

The old SDK selected a warm pool with `pool_ref`. The new SDK asks the gateway
for a sandbox using an executor `image` and an optional pool-selection
`profile`.

`profile` is not a fixed enum. It is a string key used by the gateway to match
sessions to pools. It does not define CPU, memory, GPU, node placement, or
scheduling behavior by itself. Those behaviors come from how the selected pool
was created.

Gateway behavior:

- Empty profile values are normalized to `default`.
- With `image`, the gateway creates or reuses a scoped image-backed pool for
  the `(profile, image)` combination.
- With `profile` only, the gateway selects an existing scoped pool with that
  profile. The selected pool determines the image.
- With both `image` and `profile`, both must match.
- With several matching pools, the gateway chooses the pool with the most
  available warm capacity.

Use custom profile values only when the target pools were created with the same
profile. A value such as `gpu` only means "select the pools labeled gpu"; it
does not request GPU resources unless those pools were created with GPU
resources.

Before:

```python
from arl import SandboxSession

session = SandboxSession(
    pool_ref="python-pool",
    gateway_url="http://localhost:8080",
)
```

After:

```python
from arl import SandboxSession

session = SandboxSession(
    image="python:3.12",
    profile="python-pool",
    gateway_url="http://localhost:8080",
)
```

If the default profile is acceptable, do not pass `profile`:

```python
session = SandboxSession(
    image="python:3.12",
    gateway_url="http://localhost:8080",
)
```

If the pool already exists and the gateway only needs a profile lookup, the
image can be omitted:

```python
session = SandboxSession(
    profile="python-pool",
)
```

Do not migrate by passing the pool name positionally:

```python
# Wrong: "python-pool" is interpreted as image in 0.15.6.
GatewayClient().create_session("python-pool")
```

Use explicit keywords:

```python
GatewayClient().create_session(
    image="python:3.12",
    profile="python-pool",
)
```

## Namespace Handling

The gateway is scoped to one Kubernetes namespace through its deployment
configuration. SDK and CLI callers should not pass or configure a session
namespace.

Before this migration, the Python SDK defaulted to `namespace="default"`. That
could fail against a gateway deployed in a namespace such as `arl`, because the
client was explicitly asking for the wrong namespace. The SDK and CLI now omit
the field and do not expose it in creation APIs.

If old code used `namespace` only to point at a deployment, delete that
argument. Select the deployment with `gateway_url` or with your
`kubectl port-forward -n <gateway-namespace> ...` command.

If a response still contains `namespace`, treat it as diagnostic metadata. The
SDK no longer requires that field to be present.

## Session Metadata

`SessionInfo.pool_ref` is no longer part of the public model.

Before:

```python
print(session.session_info.pool_ref)
```

After:

```python
info = session.session_info
if info is not None:
    print(info.profile)
    print(info.image)
```

New session metadata fields:

- `status`
- `deleted_at`
- `deletion_reason`

Use these fields when reporting missing or expired sessions. A gateway can keep
tombstone metadata after a session is deleted or loses its runtime.

## WarmPool Creation

WarmPool creation can include a `profile`. Use the same profile value when
creating sessions that should select that pool.

Before:

```python
manager.create_warmpool(
    name="python-pool",
    image="python:3.12",
    replicas=2,
)
```

After:

```python
manager.create_warmpool(
    name="python-pool",
    image="python:3.12",
    profile="python-pool",
    replicas=2,
)
```

If you omit the pool profile, the gateway treats the pool as `default` during
selection. The CLI uses the pool name as the default `--profile` for
`arl pool create`; the Python SDK currently defaults `profile` to `default`.

The profile is best treated as a pool family name. Examples:

```text
default
cpu
gpu
large-memory
python-pool
```

Keep the name stable and avoid encoding one-off session IDs in it; high-cardinality
profiles reduce pool reuse.

For deployments that use image-locality scheduling hints:

```python
manager.create_warmpool(
    name="python-pool",
    image="python:3.12",
    profile="python-pool",
    image_locality=True,
)
```

`WarmPoolManager` also accepts `api_key` when gateway auth is enabled.

## Managed Sessions

`ManagedSession` now follows the same `image/profile` model. The client-side
scaling hints `max_replicas`, `min_replicas`, and `scale_up_step` were removed.

Before:

```python
from arl import ManagedSession

session = ManagedSession(
    image="python:3.12",
    experiment_id="exp-1",
    max_replicas=8,
    min_replicas=1,
    scale_up_step=2,
)
```

After:

```python
from arl import ManagedSession

session = ManagedSession(
    image="python:3.12",
    experiment_id="exp-1",
)
```

If an application depended on those scaling hints, move that policy to gateway
configuration or pool management instead of passing it through the SDK.

## Detach and Reattach

`keep_alive` was removed because the gateway `keepAlive` request field had no
server-side behavior and the SDK flag only controlled context-manager cleanup.

Before:

```python
with SandboxSession(image="python:3.12", keep_alive=True) as session:
    session.execute([{"name": "write", "command": ["sh", "-c", "echo ok > /workspace/x"]}])
    session_id = session.session_id
```

After:

```python
session = SandboxSession(image="python:3.12")
session.create_sandbox()
session.execute([{"name": "write", "command": ["sh", "-c", "echo ok > /workspace/x"]}])
session_id = session.session_id
session.close()  # detach without deleting the remote session

attached = SandboxSession.attach(session_id)
try:
    attached.execute([{"name": "read", "command": ["cat", "/workspace/x"]}])
finally:
    attached.delete_sandbox()
    attached.close()
```

Use `idle_timeout_seconds` or `max_lifetime_seconds` when you need explicit
server-side lifetime limits.

## Execute and Timeout Recovery

Non-streaming `execute()` now sends an `operationID`. If the HTTP request times
out, the gateway may still complete the operation.

Before, most callers retried the same command after a timeout:

```python
result = session.execute([
    {"name": "long", "command": ["sh", "-c", "sleep 60; echo done"]},
])
```

After, catch `GatewayOperationTimeout` and query the original operation:

```python
from arl import GatewayClient, GatewayOperationTimeout

client = GatewayClient(base_url="http://localhost:8080", timeout=5.0)
session = client.create_session(image="python:3.12")

try:
    result = client.execute(
        session.id,
        [{"name": "long", "command": ["sh", "-c", "sleep 60; echo done"]}],
    )
except GatewayOperationTimeout as exc:
    operation = client.get_execute_operation(session.id, exc.operation_id)
    if operation.result is None:
        raise RuntimeError(f"operation still {operation.status}") from exc
    result = operation.result
```

To stream partial stdout and stderr, keep passing `on_output`. Streaming calls
still use the SSE endpoint:

```python
session.execute(
    [{"name": "loop", "command": ["sh", "-c", "for i in 1 2 3; do echo $i; sleep 1; done"]}],
    on_output=lambda stdout, stderr: print(stdout or stderr, end=""),
)
```

## File Transfer

The file upload endpoint changed from a JSON payload with an `encoding` field to
a binary `PUT` request.

Before:

```python
client.upload_file(
    session_id,
    path="data.bin",
    content=encoded,
    encoding="base64",
)
```

After:

```python
client.upload_file(
    session_id,
    path="data.bin",
    content=b"raw bytes",
)
```

Use path helpers for local files:

```python
session.upload_path("local.bin", "data/local.bin")
session.download_path("data/local.bin", "out/local.bin")
```

For large downloads:

```python
with open("out.bin", "wb") as target:
    for chunk in client.iter_download_file(session_id, "data/remote.bin"):
        target.write(chunk)
```

## Logs

Session and pool logs are available as typed NDJSON entries.

```python
for entry in session.iter_logs(tail=50):
    print(entry.timestamp, entry.level, entry.message)

for entry in manager.iter_logs("python-pool", follow=True):
    print(entry.pod_name, entry.message)
```

## Replay Response

Replay now returns a typed `ReplayResponse` instead of an untyped dictionary.

Before:

```python
resp = client.replay_from(target_id, source_id)
print(resp.get("stepsReplayed", 0))
```

After:

```python
resp = client.replay_from(target_id, source_id)
print(resp.steps_replayed)
print(resp.errors)
```

Step results and history entries also expose the gateway-recorded `input`
payload when it is returned by the server.

## List Endpoints

Use the new list helpers instead of shelling out to Kubernetes for common
gateway state:

```python
client.list_sessions()
client.list_pools()
client.list_experiments()
manager.list_warmpools()
```

## Execution Model Text

Update docs and examples that describe sidecar versus executor execution. In
the sandbox-backed gateway, commands run in the executor container. The sidecar
exposes the control plane and forwards execution to the executor agent over a
Unix socket. There is no supported `container` step field for choosing sidecar
versus executor execution.

Also update port-forward snippets to use the current gateway service name:

```bash
kubectl -n arl port-forward svc/agent-env-gateway 8080:8080
```

## Verification

Run SDK unit tests:

```bash
uv run pytest sdk/python/arl/tests
```

Run formatting and typing checks:

```bash
uv run ruff check sdk/python/arl/arl examples/python
uv run mypy sdk/python/arl/arl examples/python
```

When a gateway is available, run the smoke suite:

```bash
uv run python examples/python/test_arl_sdk.py \
  --gateway-url http://127.0.0.1:8080 \
  --pool-image busybox:latest
```
