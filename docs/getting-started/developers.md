# For Developers

This guide is for developers deploying or modifying `agent-env`.

## Prerequisites

- Kubernetes cluster
- `kubectl`
- Helm
- Docker
- Go 1.26+
- Python with `uv`
- `agent-sandbox` CRDs and controller installed

Check the sandbox CRDs:

```bash
kubectl api-resources | grep -i sandbox
```

## Local Checks

```bash
go test ./...
uv run ruff check sdk/python/arl/arl examples/python scripts
uv run mypy sdk/python/arl/arl
helm lint charts/agent-env
```

## Deployment

For local or CI deployment, build the three runtime images:

```bash
docker build -f Dockerfile.gateway -t arl-gateway:dev .
docker build -f Dockerfile.sidecar -t arl-sidecar:dev .
docker build -f Dockerfile.executor-agent -t arl-executor-agent:dev .
```

Then install the Helm chart with matching tags.

## Debugging

```bash
kubectl get deploy,pod,svc -n arl
kubectl logs -n arl -l app.kubernetes.io/component=gateway --tail=100
kubectl get sandboxwarmpools,sandboxclaims,sandboxes -A
```
