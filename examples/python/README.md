# Python Examples

This directory contains runnable examples for the current `arl-env` SDK.

## Files

| File | Purpose |
| --- | --- |
| `test_arl_sdk.py` | Integration smoke suite for gateway health, pool lifecycle, execution, file transfer, restore, replay, logs, shell, keep-alive attach, managed sessions, and optional observability endpoints. |
| `shell.py` | Interactive terminal client for a sandbox session over the gateway WebSocket shell endpoint. |
| `bench_gateway.py` | Gateway benchmark helper. |

The older numbered examples (`01_basic_execution.py`, `run_all_examples.py`,
and similar names) are not present in this checkout.

## Run the Integration Suite

Start a gateway port-forward first:

```bash
kubectl -n arl port-forward svc/agent-env-gateway 8080:8080
```

Then run:

```bash
cd examples/python
uv run python test_arl_sdk.py \
  --gateway-url http://127.0.0.1:8080 \
  --namespace arl \
  --pool-image busybox:latest
```

Useful options:

```bash
uv run python test_arl_sdk.py --verbose
uv run python test_arl_sdk.py --skip-cleanup
uv run python test_arl_sdk.py --metrics-url http://127.0.0.1:9091
```

If gateway auth is enabled, set `ARL_API_KEY` before running the examples.

## Interactive Shell

```bash
cd examples/python
uv run python shell.py \
  --gateway-url http://127.0.0.1:8080 \
  --namespace arl \
  --pool numpy
```

The shell example ensures a pool exists, creates a session, then connects to
`/v1/sessions/{id}/shell`.

## Execution Model

Commands execute in the executor container, which is the requested user image.
The sidecar exposes the gRPC/WebSocket control plane and forwards work to the
executor-agent over a Unix socket. There is no supported `container` step field
for choosing sidecar versus executor execution in the current gateway API.
