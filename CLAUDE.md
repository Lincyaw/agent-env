# CLAUDE.md

Gateway platform for Agentic RL sandboxes, backed by `sigs.k8s.io/agent-sandbox`.

## Commands

```bash
make build              # Build all Go binaries
go test ./...           # Run Go unit tests
make check              # fmt + vet + tidy + ruff + mypy
make generate           # Proto codegen
make arch-check         # Validate architecture docs
helm dependency build charts/agent-env
helm lint charts/agent-env --set auth.enabled=false --set grafana.adminPassword=test-grafana-password

# Build and push images to Docker Hub, then deploy:
TAG=$(git rev-parse --short HEAD)-$(date +%Y%m%d%H%M%S)
skaffold build --default-repo docker.io/opspai --platform linux/amd64 \
  --tag "$TAG" --file-output /tmp/agent-env-builds.json
# Render with desired Helm values. Use global.imageRegistry to switch
# all image prefixes (e.g. to a China-mainland mirror):
skaffold render --build-artifacts /tmp/agent-env-builds.json \
  --default-repo docker.io/opspai --platform linux/amd64 --tag "$TAG" \
  --namespace arl \
  --set global.imageRegistry="" \
  --set auth.enabled=false \
  --set grafana.adminPassword="$GRAFANA_ADMIN_PASSWORD" \
  --set agentSandbox.enabled=true \
  --set agentSandbox.image.repository=docker.io/opspai/agent-sandbox-controller \
  --set agentSandbox.image.tag="$TAG" \
  --set agentSandbox.controller.extensions=true \
  --set build.enabled=false \
  --set registry.enabled=false \
  --set checkpoint.enabled=false \
  -o /tmp/agent-env.yaml
kubectl --context <context> apply --server-side=true --force-conflicts \
  -f /tmp/agent-env.yaml
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
cmd/{gateway,sidecar,executor-agent,image-locality-scheduler}/  # Entry points
proto/agent.proto       # gRPC service definition
sdk/python/arl/         # Python SDK (ManagedSession, SandboxSession, GatewayClient)
charts/agent-env/    # Helm chart
```

## Lifecycle

SandboxWarmPool creates warm Sandboxes -> SandboxClaim binds one Sandbox to a session -> Gateway forwards execution to the sidecar gRPC endpoint. Managed sessions (`POST /v1/managed/sessions`) auto-create sandbox-backed pools.

## Code Style

- **Go 1.26.0**: English only. `make check` before commit. No test files unless requested.
- **Python 3.10+**: Modern type hints, Pydantic models, no `Any`. `make check` before commit.
- Comments only where logic isn't self-evident.

## Architecture Change Rules

After modifying components or interfaces:
1. Check `architecture/propagation-rules.yaml` for affected components
2. Run required actions (`make proto-go`, Helm lint, SDK checks, etc.)
3. Update `architecture/{components,dependencies,propagation-rules}.yaml` if needed
4. Validate with `make arch-check`

## Clusters

| Cluster | Context | Gateway URL | Notes |
| --- | --- | --- | --- |
| HK | `hk` | `http://150.5.144.30:8080` | Production |

## Key Helm Values

| Value | Default | Purpose |
| --- | --- | --- |
| `global.imageRegistry` | `""` | Override registry prefix for ALL images (e.g. `pair-cn-guangzhou.cr.volces.com`) |
| `build.enabled` | `false` | Enable Kaniko image build API (`POST /v1/build`) |
| `registry.enabled` | `false` | Deploy in-cluster container registry (distribution/registry:2) |
| `checkpoint.enabled` | `false` | Persistent per-step checkpoint tars for fork; forces Recreate deploy strategy and auto-configures fsGroup |

When `checkpoint.enabled=true`, the gateway deployment switches to Recreate strategy (no rolling update) because the checkpoint PVC is ReadWriteMany and multiple writers would corrupt state. The template auto-sets `fsGroup` on the pod security context.

## Deployment Tips

- **Skaffold profiles**: Keep deployment profiles minimal. Prefer the base config plus `--default-repo`, `--tag`, `--namespace`, and `--kube-context` over hard-coded `k8s`/`prod` profiles.
- **Skaffold values**: Current `skaffold run` does not accept Helm `--set`. When deployment needs values such as `auth.enabled=false` or `agentSandbox.enabled=true`, use `skaffold build`, `skaffold render --set ...`, then `kubectl apply`.
- **Helm dependencies**: `agent-sandbox` is an OCI chart dependency. Run `helm dependency build charts/agent-env` before lint/render. Commit `Chart.lock`; do not commit vendored `charts/*.tgz` packages.
- **agent-sandbox chart**: Do not use `file://../../agent-sandbox/helm` in `charts/agent-env/Chart.yaml`. Package and publish the sandbox chart to OCI, then reference the OCI repository.
- **agent-sandbox namespace**: Bundled installs should keep the controller in `agent-sandbox-system`; the upstream CRDs currently reference that namespace for conversion webhooks.
- **agent-sandbox image**: The controller image is not built by the `agent-env` Skaffold config. Build/push it from the `agent-sandbox` repo, sync it to the same target registry as the gateway images, and set `agentSandbox.image.repository/tag`. Do not leave bundled installs pointing at `registry.k8s.io`.
- **agent-sandbox extensions**: Keep `agentSandbox.controller.extensions=true`; the gateway uses the extension CRDs (`SandboxClaim`, `SandboxWarmPool`, `SandboxTemplate`).
- **Registry path**: The runtime image set is `arl-gateway`, `arl-sidecar`, `arl-executor-agent`, `arl-image-locality-scheduler`, plus `agent-sandbox-controller` when bundling sandbox.
- **Image tags**: Use one immutable tag for all images in a deployment. Avoid reusing a tag after a failed push; create a fresh tag to avoid registry-side partial state.
- **Registry mirror**: `pair-cn-guangzhou.cr.volces.com` (and `pair-cn-shanghai.cr.volces.com`) are Docker Hub pull-through mirrors — replace the `docker.io` prefix and the image syncs automatically (`docker.io/opspai/arl-gateway:v0.19.9` → `pair-cn-guangzhou.cr.volces.com/opspai/arl-gateway:v0.19.9`). No manual `crane copy` for Docker Hub images. Always push to `docker.io/opspai` first, then use `global.imageRegistry` to switch the mirror prefix at render time.
- **global.imageRegistry**: Set this Helm value to switch ALL image prefixes in one shot. Default is empty (uses `docker.io`). Example: `--set global.imageRegistry=pair-cn-guangzhou.cr.volces.com` for China-mainland clusters.
- **Release flow**: Pushing a `v*` git tag triggers the Publish Images workflow, which builds all four runtime images to `docker.io/opspai`. In-cluster manifests then reference them through the mirror prefix.
- **Sidecar image**: Keep the sidecar runtime minimal. The sidecar is a static Go server; shell, Python, and tools belong in the executor/user image, not the sidecar image.
- **sing-box**: In-cluster proxy subchart (`charts/sing-box`). Sandbox pods get `HTTP_PROXY` injected automatically when `proxy.url` is set.
- **replicas=0**: Default. Pre-pulls image only; pods are created on demand when sessions arrive.
- **Stale pools after image update**: After pushing a new image tag, delete existing `SandboxWarmPool` and `SandboxTemplate` resources that reference the old tag. Warm sandboxes retain the previous image until their pool is recreated.
- **Post-deploy checks**: Verify CRDs and controllers first, then gateway health:
  ```bash
  kubectl --context <context> get crd | grep -i sandbox
  kubectl --context <context> -n agent-sandbox-system rollout status deploy/agent-sandbox-controller
  kubectl --context <context> -n arl rollout status deploy/agent-env-image-locality-scheduler
  kubectl --context <context> -n arl rollout status deploy/agent-env-gateway
  kubectl --context <context> -n arl port-forward svc/agent-env-gateway 18080:8080
  uv run python examples/python/test_arl_sdk.py --gateway-url http://127.0.0.1:18080
  ```

## Reference Files

- `pkg/gateway/router.go` — all REST API endpoints
- `pkg/config/config.go` — all environment variables
- `plugin/skills/` — agent-facing operational and SDK guidance
