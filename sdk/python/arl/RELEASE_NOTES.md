# arl-env Python SDK 0.18.0 Release Notes

Release date: 2026-07-03

This release aligns the Python SDK, gateway images, CLI artifacts, and Helm
chart on version `0.18.0`. It makes WarmPool lifecycle semantics explicit and
keeps session allocation on the WarmPool path: requests queue, scale the selected
WarmPool up, and wait for ready capacity before claim allocation.

## Highlights

- `arl pool delete`, `GatewayClient.delete_pool()`, and
  `WarmPoolManager.delete_warmpool()` now drain and stop a pool instead of
  deleting the WarmPool/template objects.
- Added explicit physical deletion through `arl pool destroy`,
  `GatewayClient.destroy_pool()`, and `WarmPoolManager.destroy_warmpool()`.
- Session creation no longer exposes cold-start or bypass controls. Capacity
  shortages are handled by scaling the selected WarmPool and waiting.
- Managed pool cleanup now stops unused pools without deleting their WarmPool or
  template objects.
- `PoolInfo.state` reports running, draining, or stopped lifecycle state.
- Warm capacity accounting now treats `SandboxWarmPool.status.readyReplicas` as
  idle capacity.

## Compatibility

This release changes pool deletion semantics. Existing cleanup code that calls
`delete_pool()` or `delete_warmpool()` now leaves the WarmPool/template objects
in place with replicas set to zero. Use the new destroy helpers only when the
objects should be physically removed.

The `allowColdStart` / `allow_cold_start` request knob and `arl session create
--allow-cold-start` CLI flag are removed. Session allocation always goes through
a matching WarmPool.

## Testing

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
