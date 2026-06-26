# CLAUDE.md

K8s Operator for Agentic RL — warm pod pools + sidecar injection for ultra-low latency code execution.

## Commands

```bash
make build              # Build all Go binaries
make check              # fmt + vet + tidy + ruff + mypy
make generate           # Proto, CRDs, deepcopy
make arch-check         # Validate architecture docs
skaffold run --profile=k8s  # Deploy (dev)

# Prod deploy (arl cluster) — use --default-repo to pick registry:
skaffold run --profile=prod --kube-context=arl \
  --default-repo=pair-diag-cn-guangzhou.cr.volces.com/pair
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

## Deployment Tips

- **Registry**: `pair-diag-cn-guangzhou.cr.volces.com/pair/` is the private registry for arl cluster images. `pair-cn-shanghai.cr.volces.com/` is a Docker Hub mirror (read-only). Push custom images to Docker Hub under `opspai/` or directly to the guangzhou registry.
- **`--default-repo`**: Prod skaffold profile uses short image names (`arl-operator`). Pass `--default-repo=<registry>` to control where images are pushed and pulled. Never hardcode registry in skaffold build artifacts.
- **Docker Hub rate limit**: The `opspai` Docker Hub account hits rate limits quickly. Prefer pushing to `pair-diag-cn-guangzhou.cr.volces.com/pair/` directly.
- **Git push**: Use HTTPS remote + `GIT_SSH_COMMAND="ssh -i ~/.ssh/id_ed25519 -o IdentitiesOnly=yes"` for pushing as Lincyaw. Default SSH key (`id_ed25519_boxi`) maps to BoxiYu.
- **Mihomo proxy**: In-cluster Clash proxy at `mihomo.arl.svc:7890` for GitHub/PyPI/npm access. Config is a static ConfigMap (`mihomo-config`); MMDB is provided via `pair-diag-cn-guangzhou.cr.volces.com/pair/mihomo-mmdb:latest` init container. Update config: `kubectl create configmap mihomo-config -n arl --from-file=config.yaml=... --dry-run=client -o yaml | kubectl apply -f -` then restart.
- **Pool replicas=0**: Default. Controller creates a pre-pull pod to cache the image; pods are created on-demand when sessions are requested. ImageLocality scheduler routes to nodes with cached images.
- **K8s contexts**: `arl` = prod cluster. Current context may differ — always pass `--kube-context=arl` for prod operations.

## Docs

- `docs/developer-guide/session-state.md` — SessionStore, Redis setup, deployment patterns
- `pkg/gateway/router.go` — all REST API endpoints
- `pkg/config/config.go` — all environment variables
- Site: https://lincyaw.github.io/agent-env/
