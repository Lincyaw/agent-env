# Development

## Repository Layout

```text
cmd/gateway/            Gateway entrypoint
cmd/sidecar/            Sidecar entrypoint
cmd/executor-agent/     Executor-agent entrypoint
pkg/gateway/            REST API, sessions, SandboxClaim allocator
pkg/sidecar/            gRPC service inside sandbox pods
pkg/execagent/          Unix-socket command executor
pkg/client/             Gateway sidecar gRPC client
pkg/metrics/            Prometheus collectors
pkg/audit/              ClickHouse trajectory writer
sdk/python/arl/         Python SDK
charts/agent-env/    Helm chart for agent-env gateway stack
```

## Common Commands

```bash
go test ./...
go build ./cmd/gateway/
go build ./cmd/sidecar/
go build ./cmd/executor-agent/
uv run ruff check sdk/python/arl/arl examples/python scripts
uv run mypy sdk/python/arl/arl
helm lint charts/agent-env
```

Generate protobuf code after editing `proto/agent.proto`:

```bash
make proto-go
```

## Local Deployment Loop

1. Install and verify `agent-sandbox`.
2. Build `arl-gateway`, `arl-sidecar`, and `arl-executor-agent`.
3. Deploy the Helm chart.
4. Create a session through the gateway.
5. Inspect `SandboxClaim` and `Sandbox` status if allocation fails.

Useful commands:

```bash
kubectl get sandboxwarmpools,sandboxclaims,sandboxes -A
kubectl logs -n arl -l app.kubernetes.io/component=gateway --tail=100 -f
```
