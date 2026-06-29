# Installation

`agent-env` requires an existing Kubernetes cluster and an installed
`agent-sandbox` controller.

## Prerequisites

Install these local tools:

- Go 1.26+
- Docker
- `kubectl`
- Helm
- Python and `uv`

Install project dependencies:

```bash
uv sync --all-groups
go mod download
```

## Install agent-sandbox

Install `agent-sandbox` from the sibling repository or your package source.
After installation, these resources should exist:

```bash
kubectl api-resources | grep -i sandbox
kubectl get deploy -A | grep agent-sandbox
```

Expected CRDs include:

- `sandboxtemplates.extensions.agents.x-k8s.io`
- `sandboxwarmpools.extensions.agents.x-k8s.io`
- `sandboxclaims.extensions.agents.x-k8s.io`
- `sandboxes.agents.x-k8s.io`

## Install agent-env

Build or provide these images:

```bash
docker build -f Dockerfile.gateway -t arl-gateway:dev .
docker build -f Dockerfile.sidecar -t arl-sidecar:dev .
docker build -f Dockerfile.executor-agent -t arl-executor-agent:dev .
```

Install the chart:

```bash
helm upgrade --install agent-env charts/agent-env \
  -n arl --create-namespace \
  --set auth.enabled=false \
  --set gateway.image.tag=dev \
  --set sidecar.image.tag=dev \
  --set executorAgent.image.tag=dev \
  --set image.injectedPullPolicy=IfNotPresent
```

For production, keep authentication enabled and provide API keys, or enable the
Tinyauth ingress authentication path documented in
`docs/developer-guide/deployment.md`.
