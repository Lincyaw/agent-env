# ARL Python SDK Usage and Recovery

## Creation Policy

- `SandboxSession(image=...)`: gateway ensures an image/profile-backed pool if
  no matching pool exists, then allocates a session from that pool.
- `SandboxSession(profile=...)` without image: gateway expects a matching
  existing pool; it cannot infer an executor image.
- `ManagedSession`: gateway derives a stable `managed-...` pool from
  namespace/profile/image, creates it if needed, and tags the session with
  experiment metadata.
- Warm capacity is preferred. If no warm sandbox is available, gateway policy
  decides whether to allow cold start or queue/reject.

## Error and Recovery Semantics

- Python transport/API failures surface as `GatewayError` or transport
  exceptions from `httpx`.
- Non-streaming execute uses operation IDs; a client timeout can be followed by
  operation-status lookup instead of blindly retrying.
- Non-zero command exit codes are part of step output; they are not necessarily
  SDK exceptions.
- If gateway detects that a session lost its `SandboxClaim`/pod binding, it
  tombstones the session as runtime-lost and closes stale sidecar connections.
- Deleting a warm pool removes pool-bound `SandboxClaim` objects before
  deleting the pool/template.

## Change Guidance

- Add or change Python-facing models in `types.py`, then export them from
  `__init__.py` if they are public.
- Add high-level session ergonomics in `session.py`; keep raw HTTP behavior in
  `gateway_client.py`.
- Keep gateway behavior authoritative. If a capability needs lifecycle,
  admission, recovery, or Kubernetes changes, update `pkg/gateway` first and
  make the SDK a thin wrapper.
- Keep CLI and SDK behavior aligned for shared concepts such as sessions,
  pools, logs, files, restore/replay, and experiments.
- Prefer tests near the changed layer: SDK unit tests in `sdk/python/arl/tests`,
  gateway tests in `pkg/gateway`, smoke tests in `examples/python` when a real
  gateway is needed.

## Verification

Use focused checks for SDK-only changes:

```bash
uv run pytest sdk/python/arl/tests
uv run ruff check sdk/python/arl/arl examples/python
uv run mypy sdk/python/arl/arl examples/python
```

Use full repository checks when changing gateway-backed behavior:

```bash
go test ./...
make check
```

When a gateway is available, run the Python smoke flow from `examples/python`
against the target namespace and image.
