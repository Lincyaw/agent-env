# CLAUDE.md

K8s Operator for Agentic RL — warm pod pools + sidecar injection for ultra-low latency code execution.

## Commands

```bash
make build              # Build all Go binaries
make check              # fmt + vet + tidy + ruff + mypy
make generate           # Proto, CRDs, deepcopy
make arch-check         # Validate architecture docs
skaffold run --profile=k8s  # Deploy
```

Python: use `uv` exclusively (`uv run`, `uv add`). SDK: `make build-sdk`.

## Directory Structure

```
api/v1alpha1/           # WarmPool CRD types
pkg/controller/         # WarmPoolController (LRU scale-down)
pkg/gateway/            # REST API + SessionStore + PoolManager
pkg/execagent/          # Executor agent (Unix socket inside container)
pkg/sidecar/            # Sidecar gRPC server
pkg/scheduler/          # Image-locality scheduling (Rendezvous hashing)
pkg/client/             # gRPC client for sidecar
pkg/interfaces/         # Shared interfaces (SidecarClient, AuditWriter)
pkg/metrics/            # Prometheus metrics
pkg/audit/              # ClickHouse audit logging
cmd/{operator,gateway,sidecar,executor-agent}/  # Entry points
proto/agent.proto       # gRPC service definition
sdk/python/arl/         # Python SDK (ManagedSession, SandboxSession, GatewayClient)
charts/arl-operator/    # Helm chart
```

## Lifecycle

WarmPool creates N warm pods -> PodAllocator assigns pod to session -> Gateway forwards execution to sidecar gRPC. Managed sessions (`POST /v1/managed/sessions`) auto-create/scale pools.

## Code Style

- **Go 1.25.0**: English only. `make check` before commit. No test files unless requested.
- **Python 3.10+**: Modern type hints, Pydantic models, no `Any`. `make check` before commit.
- Comments only where logic isn't self-evident. Chinese OK in docs.

## Architecture Change Rules

After modifying components or interfaces:
1. Check `architecture/propagation-rules.yaml` for affected components
2. Run required actions (`make manifests`, `make proto-go`, etc.)
3. Update `architecture/{components,dependencies,propagation-rules}.yaml` if needed
4. Validate with `make arch-check`
5. Update `docs/` if the change affects user-facing behavior, APIs, CRDs, or deployment config

## Docs

- `docs/developer-guide/session-state.md` — SessionStore, Redis setup, deployment patterns
- `pkg/gateway/router.go` — all REST API endpoints
- `pkg/config/config.go` — all environment variables
- Site: https://lincyaw.github.io/agent-env/
