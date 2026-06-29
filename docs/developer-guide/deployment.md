# Deployment

The supported runtime stack is:

```text
agent-sandbox-controller
agent-env gateway
Redis       optional session persistence
ClickHouse  optional trajectory storage
Prometheus  metrics collection, enabled by chart defaults
Grafana     dashboards, enabled by chart defaults
Alertmanager optional alerting, enabled by chart defaults
```

The legacy in-tree WarmPool controller is no longer deployed.

## Helm Install

Local development:

```bash
helm upgrade --install agent-env charts/agent-env \
  -n arl --create-namespace \
  --set auth.enabled=false \
  --set image.injectedPullPolicy=IfNotPresent \
  --set gateway.image.tag=dev \
  --set sidecar.image.tag=dev \
  --set executorAgent.image.tag=dev
```

Enable Redis, ClickHouse, Prometheus, and Grafana:

```bash
helm upgrade --install agent-env charts/agent-env \
  -n arl --create-namespace \
  --set auth.enabled=false \
  --set redis.enabled=true \
  --set clickhouse.enabled=true \
  --set prometheus.enabled=true \
  --set grafana.enabled=true
```

For production, keep authentication enabled and set `auth.apiKeys` with one or
more `key:role` lines. Roles are `admin` and `user`.

## Required RBAC

The gateway needs access to agent-sandbox resources:

- `sandboxclaims`
- `sandboxtemplates`
- `sandboxwarmpools`
- `sandboxes`
- Pods and Secrets in target namespaces

The Helm chart grants this through the gateway service account.

## Verification

```bash
kubectl get deploy,pod,svc -n arl
kubectl get sandboxwarmpools,sandboxclaims,sandboxes -A
kubectl logs -n arl -l app.kubernetes.io/component=gateway --tail=100
```

Port-forward the gateway:

```bash
kubectl -n arl port-forward svc/agent-env-gateway 8080:8080
kubectl -n arl port-forward svc/agent-env-gateway-metrics 9091:9091
```

Then check:

```bash
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:9091/metrics
```

## Cleanup

```bash
helm uninstall agent-env -n arl
```
