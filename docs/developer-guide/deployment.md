# Deployment

The supported runtime stack is:

```text
agent-sandbox-controller
agent-env gateway
Redis       optional session persistence
ClickHouse  optional trajectory storage
Prometheus  optional metrics collection
Grafana     optional dashboards
Tinyauth    optional ingress authentication
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

## Tinyauth Ingress Authentication

The chart can deploy [Tinyauth](https://github.com/tinyauthapp/tinyauth) as an
optional edge-auth service. This is useful for browser access through an
Ingress. SDK/CLI callers can still use gateway API keys.

Create a bcrypt user entry with the Tinyauth CLI:

```bash
docker run -it --rm ghcr.io/tinyauthapp/tinyauth:v5 user create --interactive
```

Then enable Tinyauth, expose both hosts, and configure the gateway to trust only
headers from your ingress controller/pod CIDR:

```bash
helm upgrade --install agent-env charts/agent-env \
  -n arl --create-namespace \
  --set tinyauth.enabled=true \
  --set tinyauth.appURL=https://auth.example.com \
  --set-string 'tinyauth.users=user:$2a$...' \
  --set tinyauth.ingress.enabled=true \
  --set tinyauth.ingress.className=nginx \
  --set tinyauth.ingress.host=auth.example.com \
  --set gateway.ingress.enabled=true \
  --set gateway.ingress.className=nginx \
  --set gateway.ingress.hosts[0].host=agent-env.example.com \
  --set gateway.ingress.tinyauth.enabled=true \
  --set auth.forwardHeaders.enabled=true \
  --set auth.forwardHeaders.trustedProxies=10.0.0.0/8
```

For HTTPS deployments, set `tinyauth.secureCookie=true`. To allow selected
Tinyauth users to call gateway admin endpoints, set
`auth.forwardHeaders.adminUsers=alice,bob`. Keep `auth.enabled=true`; disabling
it makes the gateway trust every request that can reach the Service directly.
If browser clients use the WebSocket shell endpoint, also set
`auth.allowedOrigins` to the public gateway host.

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
