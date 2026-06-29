# CLAUDE.md

Gateway platform for Agentic RL sandboxes, backed by `sigs.k8s.io/agent-sandbox`.

## Commands

```bash
make build              # Build all Go binaries
make check              # fmt + vet + tidy + ruff + mypy
make generate           # Proto codegen
make arch-check         # Validate architecture docs
skaffold run --profile=k8s  # Deploy (dev)

# Prod deploy (arl cluster) — use --default-repo to pick registry:
skaffold run --profile=prod --kube-context=arl \
  --default-repo=pair-diag-cn-guangzhou.cr.volces.com/pair
```

Python: use `uv` exclusively (`uv run`, `uv add`). SDK: `make build-sdk`.

## Directory Structure

```
pkg/gateway/            # REST API + SessionStore + SandboxClaim allocator
pkg/execagent/          # Executor agent (Unix socket inside container)
pkg/sidecar/            # Sidecar gRPC server
pkg/client/             # gRPC client for sidecar
pkg/interfaces/         # Shared interfaces (SidecarClient, MetricsCollector)
pkg/metrics/            # Prometheus metrics
pkg/audit/              # ClickHouse trajectory storage
cmd/{gateway,sidecar,executor-agent}/  # Entry points
proto/agent.proto       # gRPC service definition
sdk/python/arl/         # Python SDK (ManagedSession, SandboxSession, GatewayClient)
charts/agent-env/    # Helm chart
```

## Lifecycle

SandboxWarmPool creates warm Sandboxes -> SandboxClaim binds one Sandbox to a session -> Gateway forwards execution to the sidecar gRPC endpoint. Managed sessions (`POST /v1/managed/sessions`) auto-create sandbox-backed pools.

## Code Style

- **Go 1.26.0**: English only. `make check` before commit. No test files unless requested.
- **Python 3.10+**: Modern type hints, Pydantic models, no `Any`. `make check` before commit.
- Comments only where logic isn't self-evident. Chinese OK in docs.

## Architecture Change Rules

After modifying components or interfaces:
1. Check `architecture/propagation-rules.yaml` for affected components
2. Run required actions (`make proto-go`, Helm lint, SDK checks, etc.)
3. Update `architecture/{components,dependencies,propagation-rules}.yaml` if needed
4. Validate with `make arch-check`
5. Update `docs/` if the change affects user-facing behavior, APIs, CRDs, or deployment config

## Deployment Tips

- **Prod deploy**: `skaffold run --profile=prod --kube-context=arl --default-repo=pair-diag-cn-guangzhou.cr.volces.com/pair`
- **Registry**: Push to `pair-diag-cn-guangzhou.cr.volces.com/pair/` (Docker Hub has rate limits). `pair-cn-shanghai.cr.volces.com/` is a read-only Docker Hub mirror.
- **Mihomo**: In-cluster proxy at `mihomo.arl.svc:7890`. Sandbox pods get `HTTP_PROXY` injected automatically.
- **replicas=0**: Default. Pre-pulls image only; pods created on-demand when sessions arrive.

## Docs

- `docs/developer-guide/session-state.md` — SessionStore, Redis setup, deployment patterns
- `pkg/gateway/router.go` — all REST API endpoints
- `pkg/config/config.go` — all environment variables
- Site: https://lincyaw.github.io/agent-env/
