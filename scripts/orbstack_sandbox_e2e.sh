#!/usr/bin/env bash
set -euo pipefail

KUBE_CONTEXT="${KUBE_CONTEXT:-orbstack}"
NAMESPACE="${NAMESPACE:-agent-env-sandbox-port-test}"
LOCAL_PORT="${LOCAL_PORT:-18080}"
INTERNAL_LOCAL_PORT="${INTERNAL_LOCAL_PORT:-19091}"
GRPC_AUTH_TOKEN="${GRPC_AUTH_TOKEN:-sandbox-port-token}"
PERF_SESSIONS="${PERF_SESSIONS:-8}"
PERF_CONCURRENCY="${PERF_CONCURRENCY:-2}"
BUILD_IMAGES="${BUILD_IMAGES:-true}"
CLEANUP="${CLEANUP:-false}"
RUN_ID="${RUN_ID:-$(date +%Y%m%d%H%M%S)}"
TAG="${TAG:-sandbox-port-test-${RUN_ID}}"
POOL_NAME="${POOL_NAME:-arl-sandbox-e2e-${RUN_ID}}"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PF_LOG="${TMPDIR:-/tmp}/agent-env-sandbox-e2e-port-forward-${RUN_ID}.log"
INTERNAL_PF_LOG="${TMPDIR:-/tmp}/agent-env-sandbox-e2e-internal-port-forward-${RUN_ID}.log"
PF_PID=""
INTERNAL_PF_PID=""

cleanup() {
  if [[ -n "${PF_PID}" ]] && kill -0 "${PF_PID}" >/dev/null 2>&1; then
    kill "${PF_PID}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${INTERNAL_PF_PID}" ]] && kill -0 "${INTERNAL_PF_PID}" >/dev/null 2>&1; then
    kill "${INTERNAL_PF_PID}" >/dev/null 2>&1 || true
  fi
  if [[ "${CLEANUP}" == "true" ]]; then
    kubectl --context "${KUBE_CONTEXT}" delete namespace "${NAMESPACE}" --ignore-not-found
  fi
}
trap cleanup EXIT

node_arch="$(kubectl --context "${KUBE_CONTEXT}" get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}')"
platform="linux/${node_arch}"

kubectl --context "${KUBE_CONTEXT}" get crd sandboxclaims.extensions.agents.x-k8s.io >/dev/null
kubectl --context "${KUBE_CONTEXT}" -n agent-sandbox-system rollout status deploy/agent-sandbox-controller --timeout=120s

if [[ "${BUILD_IMAGES}" == "true" ]]; then
  docker build --platform "${platform}" -t "arl-gateway:${TAG}" -f "${ROOT_DIR}/Dockerfile.gateway" "${ROOT_DIR}"
  docker build --platform "${platform}" -t "arl-sidecar:${TAG}" -f "${ROOT_DIR}/Dockerfile.sidecar" "${ROOT_DIR}"
  docker build --platform "${platform}" -t "arl-executor-agent:${TAG}" -f "${ROOT_DIR}/Dockerfile.executor-agent" "${ROOT_DIR}"
fi

kubectl --context "${KUBE_CONTEXT}" create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl --context "${KUBE_CONTEXT}" apply -f -

cat <<YAML | kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" apply -f -
apiVersion: v1
kind: ServiceAccount
metadata:
  name: arl-gateway
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: arl-gateway
rules:
  - apiGroups: [""]
    resources: ["pods", "secrets"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["agents.x-k8s.io"]
    resources: ["sandboxes"]
    verbs: ["get", "list", "watch", "patch", "delete"]
  - apiGroups: ["extensions.agents.x-k8s.io"]
    resources: ["sandboxclaims", "sandboxtemplates", "sandboxwarmpools"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["extensions.agents.x-k8s.io"]
    resources: ["sandboxclaims/status", "sandboxtemplates/status", "sandboxwarmpools/status"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: arl-gateway
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: arl-gateway
subjects:
  - kind: ServiceAccount
    name: arl-gateway
    namespace: ${NAMESPACE}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: arl-gateway
spec:
  replicas: 1
  selector:
    matchLabels:
      app: arl-gateway
  template:
    metadata:
      labels:
        app: arl-gateway
    spec:
      serviceAccountName: arl-gateway
      containers:
        - name: gateway
          image: arl-gateway:${TAG}
          imagePullPolicy: IfNotPresent
          args: ["--port=8080", "--sidecar-grpc-port=9090"]
          env:
            - name: AUTH_ENABLED
              value: "false"
            - name: GRPC_AUTH_TOKEN
              value: "${GRPC_AUTH_TOKEN}"
            - name: SIDECAR_IMAGE
              value: "arl-sidecar:${TAG}"
            - name: EXECUTOR_AGENT_IMAGE
              value: "arl-executor-agent:${TAG}"
            - name: IMAGE_PULL_POLICY
              value: "IfNotPresent"
            - name: INTERNAL_PORT
              value: "9091"
          ports:
            - name: http
              containerPort: 8080
            - name: internal
              containerPort: 9091
          readinessProbe:
            httpGet:
              path: /healthz
              port: http
            periodSeconds: 2
            failureThreshold: 30
          livenessProbe:
            httpGet:
              path: /healthz
              port: http
            periodSeconds: 10
            failureThreshold: 3
---
apiVersion: v1
kind: Service
metadata:
  name: arl-gateway
spec:
  selector:
    app: arl-gateway
  ports:
    - name: http
      port: 8080
      targetPort: http
    - name: internal
      port: 9091
      targetPort: internal
YAML

kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" rollout status deploy/arl-gateway --timeout=180s

kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" port-forward svc/arl-gateway "${LOCAL_PORT}:8080" >"${PF_LOG}" 2>&1 &
PF_PID="$!"
kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" port-forward svc/arl-gateway "${INTERNAL_LOCAL_PORT}:9091" >"${INTERNAL_PF_LOG}" 2>&1 &
INTERNAL_PF_PID="$!"

python3 - <<PY
import sys
import time
import urllib.request

urls = [
    ("gateway", "http://127.0.0.1:${LOCAL_PORT}/healthz", "${PF_LOG}"),
    ("internal gateway", "http://127.0.0.1:${INTERNAL_LOCAL_PORT}/healthz", "${INTERNAL_PF_LOG}"),
]
deadline = time.time() + 30
for name, url, log in urls:
    ready = False
    while time.time() < deadline:
        try:
            with urllib.request.urlopen(url, timeout=2) as resp:
                if resp.status == 200:
                    ready = True
                    break
        except Exception:
            time.sleep(0.5)
    if not ready:
        print(f"{name} port-forward did not become ready; log={log}", file=sys.stderr)
        sys.exit(1)
PY

python3 "${ROOT_DIR}/scripts/sandbox_gateway_e2e.py" \
  --gateway-url "http://127.0.0.1:${LOCAL_PORT}" \
  --metrics-url "http://127.0.0.1:${INTERNAL_LOCAL_PORT}" \
  --namespace "${NAMESPACE}" \
  --pool "${POOL_NAME}" \
  --sessions "${PERF_SESSIONS}" \
  --concurrency "${PERF_CONCURRENCY}"
