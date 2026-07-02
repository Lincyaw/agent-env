# arl-env Python SDK 0.17.0 Release Notes

Release date: 2026-07-02

This release aligns the Python SDK, gateway images, CLI artifacts, and Helm
chart on version `0.17.0`. It includes the sandbox runtime lifecycle cleanup
fixes deployed to `arl2`, including automatic cleanup of managed pools and
templates after allocation failures, runtime loss, and image pull failures.

## Highlights

- Managed image-backed pools are cleaned when session allocation fails.
- Runtime-loss cleanup now removes unused managed pools and templates.
- `ImagePullBackOff` and `ErrImagePull` failures do not leave managed
  pool/template/claim/sandbox/pod resources behind.
- Session creation now uses `image` and `profile`.
- Non-streaming `execute()` calls are idempotent through `operationID`.
- `GatewayOperationTimeout` exposes the operation ID after an HTTP timeout.
- File transfer uses binary streaming and path-based file endpoints.
- Session and pool log endpoints are available as typed NDJSON iterators.
- Replay and experiment-delete responses have typed SDK models.
- Step results now preserve the original gateway-recorded `input` payload.
- WarmPool creation supports `profile`, `image_locality`, and API-key auth.
- Managed sessions use the same `image/profile` selection model.
- The SDK exposes list endpoints for sessions, pools, and managed experiments.
- Public models now include deletion metadata, log entries, experiment
  summaries, operation status, and upload checksums.

## Compatibility

This release has breaking API changes for code that creates sessions with
`pool_ref` or reads `SessionInfo.pool_ref`.

`profile` is a gateway pool-selection key. It is not a fixed enum and it does
not define resources by itself. Empty profile values are normalized to
`default`. A profile such as `gpu` or `large-memory` only has that meaning if
matching pools were created with GPU or large-memory resources.

Session selection is based on the gateway-scoped pool set, normalized profile,
and optional image:

- `SandboxSession(image="python:3.12")` uses the `default` profile and creates
  or reuses a pool for that image/profile pair.
- `SandboxSession(profile="gpu")` selects an existing `gpu` profile pool; the
  selected pool determines the image.
- `SandboxSession(image="python:3.12", profile="gpu")` requires both the image
  and profile to match.

When several pools match, the gateway chooses the pool with the most available
warm capacity.

`namespace` is no longer part of normal SDK or CLI usage. The gateway is scoped
to a single Kubernetes namespace at deployment time, so callers do not choose
where a session is created. Response models tolerate the field being absent; if
the gateway still returns it, treat it as diagnostic metadata.

Old code:

```python
from arl import SandboxSession

with SandboxSession(pool_ref="python-pool") as session:
    result = session.execute([
        {"name": "hello", "command": ["echo", "hello"]},
    ])
```

New code:

```python
from arl import SandboxSession

with SandboxSession(
    image="python:3.12",
    profile="python-pool",
) as session:
    result = session.execute([
        {"name": "hello", "command": ["echo", "hello"]},
    ])
```

For the default profile, omit `profile`:

```python
session = SandboxSession(image="python:3.12")
```

If the target pool already exists and the gateway can resolve it by profile,
the image can be omitted:

```python
session = SandboxSession(profile="python-pool")
```

Use keyword arguments when calling low-level APIs. A positional string passed to
`GatewayClient.create_session()` is now interpreted as `image`, not as the old
pool reference.

## New APIs

### Operation Recovery

Non-streaming execution now includes an operation ID. If the HTTP request times
out, the command may still be running on the gateway. Catch
`GatewayOperationTimeout` and query the operation status instead of blindly
retrying the step.

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
    result = operation.result
```

Use `SandboxSession.execute()` for the common case and `GatewayClient` when an
application needs explicit operation recovery.

### File Transfer

File upload now sends bytes to the path-based endpoint. Text, bytes, binary
file objects, and byte iterators are supported.

```python
session.upload_file("input.txt", "hello\n")
session.upload_path("local.bin", "data/local.bin")
session.download_path("data/local.bin", "out/local.bin")
```

The upload response includes an optional `sha256` field when returned by the
gateway.

### Logs

Session and pool logs can be consumed as typed entries.

```python
for entry in session.iter_logs(tail=100):
    print(entry.level, entry.message)

for entry in manager.iter_logs("python-pool", follow=True):
    print(entry.pod_name, entry.message)
```

### Pool and Experiment Listing

The low-level client now exposes list endpoints:

```python
client.list_sessions()
client.list_pools()
client.list_experiments()
```

## Changed Behavior

- `execute()` only requests SSE streaming when `on_output` is provided.
  Otherwise it uses the JSON endpoint with an operation ID.
- `restore()` snapshots are step-index strings in the sandbox-backed gateway
  implementation.
- `SessionInfo` now reports `image`, `profile`, `status`, `deleted_at`, and
  `deletion_reason`.
- `PoolInfo` now reports `image` and `profile`.
- `StepResult` now includes `input` when the gateway returns it.
- `GatewayClient.replay_from()` and `SandboxSession.replay_from()` now return
  `ReplayResponse` instead of an untyped dict.
- `GatewayClient.delete_experiment_info()` returns the full delete response;
  `delete_experiment()` still returns the deleted-session count and now raises
  `GatewayError` if the gateway reports a partial cleanup error in the response
  body.
- `SandboxSession(keep_alive=...)`, `SandboxSession.attach(..., keep_alive=...)`,
  and the unused gateway `keepAlive` request field were removed. Use manual
  lifecycle management to detach from a session without deleting it.
- Python SDK and CLI session/pool creation no longer expose or send
  `namespace`; the gateway uses its deployment scope.
- `WarmPoolManager.create_warmpool()` should usually pass a profile that
  matches the profile used by sessions.
- `ManagedSession` no longer accepts the client-side pool scaling hints
  `max_replicas`, `min_replicas`, or `scale_up_step`.

## Testing

The release adds SDK unit tests for gateway route coverage, list endpoint
parsing, NDJSON log parsing, image locality payloads, operation IDs, and timeout
recovery.

Run focused SDK checks with:

```bash
uv run pytest sdk/python/arl/tests
uv run ruff check sdk/python/arl/arl examples/python
uv run mypy sdk/python/arl/arl examples/python
```

Run the gateway smoke suite when a deployed gateway is available:

```bash
uv run python examples/python/test_arl_sdk.py \
  --gateway-url http://127.0.0.1:8080 \
  --pool-image busybox:latest
```
