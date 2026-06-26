---
name: arl-cli
description: |
  Guide for using the `arl` CLI to inspect, debug, and manage the ARL (Agentic RL) runtime — experiments, warm pools, sessions, logs, and metrics. Use this skill whenever the user asks about checking pool status, viewing experiment sessions, debugging pods, streaming logs, executing commands in a sandbox, exporting trajectories, or any operational task involving the ARL system. Also trigger when the user mentions `arl` CLI commands, asks "how do I see what's running", "show me the pools", "check experiment X", "debug this session", "看看 pool 状态", "查看实验", "导出 trajectory", or wants to combine multiple arl operations into a workflow. Even if the user doesn't say "arl" explicitly, trigger when the question is about inspecting or controlling ARL runtime resources.
---

# ARL CLI

`arl` is the command-line tool for the ARL runtime. It talks to the Gateway REST API (and through it, to sidecar gRPC in every pod) to give you a single pane of glass over experiments, pools, sessions, and logs.

## Setup

```bash
# Build
make build-cli          # produces bin/arl

# Configure (pick one)
export ARL_GATEWAY_URL=http://localhost:8080   # or your gateway address
export ARL_API_KEY=your-admin-token            # required if auth is enabled
export ARL_NAMESPACE=default                   # default namespace for pool operations

# Or pass per-command
arl --gateway-url http://gw:8080 --api-key xxx pool list
```

Global flags available on every command:

| Flag | Short | Env var | Default | Purpose |
|------|-------|---------|---------|---------|
| `--gateway-url` | `-g` | `ARL_GATEWAY_URL` | `http://localhost:8080` | Gateway base URL |
| `--api-key` | `-k` | `ARL_API_KEY` | (none) | Bearer token for auth |
| `--namespace` | `-n` | `ARL_NAMESPACE` | `default` | K8s namespace |
| `--output` | `-o` | — | `table` | Output format: `table`, `json`, `wide` |

## Command Reference

### Experiments

Experiments group managed sessions that share an image and auto-scaled pool.

```bash
arl exp list                           # list all experiments with session counts
arl exp sessions <experiment-id>       # list sessions under an experiment
arl exp stats <experiment-id>          # show summary (session count, pool, namespace)
arl exp delete <experiment-id> --force # delete ALL sessions + release pool capacity
```

### Pools

Pools are WarmPool CRDs — pre-warmed pod groups ready for instant session allocation.

```bash
arl pool list                          # list pools in current namespace
arl pool list -A                       # list across ALL namespaces
arl pool list -o wide                  # include image name, namespace, age
arl pool get <name>                    # detailed view: replicas, conditions, image
arl pool create <name> --image python:3.11 --replicas 3
arl pool scale <name> --replicas 10
arl pool delete <name> --force

# Execute a command inside a pool pod (creates temp session, runs, cleans up)
arl pool exec <name> -- python -c "import torch; print(torch.cuda.is_available())"
arl pool exec <name> -- ls /workspace
arl pool exec <name> -- nvidia-smi

# Stream logs from ALL pods in the pool
arl pool logs <name>                   # last 100 lines
arl pool logs <name> -f                # follow mode (like tail -f)
arl pool logs <name> --tail 50 -f      # last 50 then follow
```

### Sessions

Sessions are allocated pod slots with execution history and snapshot/restore.

```bash
arl session list                               # all active sessions
arl session list --pool my-pool                # filter by pool
arl session list --experiment exp-42           # filter by experiment
arl session list -o wide                       # include pod IP, namespace
arl session get <id>                           # session detail (pod, pool, age)
arl session delete <id>                        # terminate and release pod

# Execution
arl session exec <id> -- echo hello            # run a command
arl session shell <id>                         # interactive terminal (WebSocket)

# History and trajectory
arl session history <id>                       # step table (index, name, exit, duration)
arl session history <id> -v                    # include stdout/stderr output
arl session trajectory <id>                    # JSONL to stdout
arl session trajectory <id> -f out.jsonl       # write to file

# Logs
arl session logs <id>                          # sidecar ring buffer (last 100)
arl session logs <id> -f                       # follow mode
```

### Diagnostics

```bash
arl status                 # gateway health + session/pool/experiment counts
arl metrics                # ARL-prefixed Prometheus metrics
arl metrics --filter pool  # filter by substring
arl metrics --raw          # full Prometheus exposition format
arl config                 # show current CLI configuration
```

## Common Workflows

### 1. "What's running right now?"

Quick overview of the entire system state:

```bash
arl status
arl pool list -o wide
arl session list -o wide
```

### 2. Debug a failing experiment

When sessions in an experiment are failing:

```bash
# See which sessions exist and their pods
arl exp sessions exp-42

# Check the pool health (conditions, ready vs allocated)
arl pool get <pool-name-from-above>

# Look at execution history for a specific session
arl session history <session-id> -v

# Stream live logs from the session's sidecar
arl session logs <session-id> -f

# Or stream logs from ALL pods in the pool at once
arl pool logs <pool-name> -f
```

### 3. Pre-warm an image (replicas=0)

When `replicas=0`, the controller creates a lightweight pre-pull pod to cache the
image on one node without running actual warm pods. When a session is requested,
the pool scales up and the ImageLocality scheduler routes the pod to the node that
already has the cached image — near-instant startup.

```bash
# Pre-warm: create a pool with replicas=0 (image-only, no running pods)
arl pool create my-pool --image my-registry/my-image:latest --replicas 0

# Check that the pre-pull pod ran (image is cached)
arl pool get my-pool            # readyReplicas=0, no active pods, image cached

# Later, scale up when needed
arl pool scale my-pool --replicas 5
```

This is the **default behavior** — `DefaultPoolReplicas=0` and
`ManagedPoolInitialReplicas=0`. All new pools (including managed pools created
via `POST /v1/managed/sessions`) start with replicas=0 and pre-pull only.
The pool auto-scales when a session is requested.

### 4. Quick-test a container image

Before using an image in a large experiment, validate it works:

```bash
# Create a pool with 1 replica for testing
arl pool create test-pool --image my-registry/my-image:latest --replicas 1

# Wait for it to be ready, then test
arl pool get test-pool          # check conditions
arl pool exec test-pool -- python -c "import mylib; print('ok')"
arl pool exec test-pool -- cat /etc/os-release

# Clean up
arl pool delete test-pool --force
```

### 5. Export training trajectories

After an experiment completes, export all session trajectories for SFT/RL:

```bash
# List sessions
arl exp sessions exp-42 -o json | jq -r '.[].id' > session_ids.txt

# Export each trajectory
mkdir -p trajectories/
while read sid; do
  arl session trajectory "$sid" -f "trajectories/${sid}.jsonl"
done < session_ids.txt
```

### 6. Monitor pool utilization

Watch pool metrics for capacity planning:

```bash
# One-shot utilization
arl pool list -o wide

# Prometheus metrics for deeper analysis
arl metrics --filter pool_utilization
arl metrics --filter pod_schedule_seconds
arl metrics --filter image_pull
```

### 7. Scale up before a big run

Pre-scale pools before launching many sessions:

```bash
arl pool scale training-pool --replicas 20
arl pool get training-pool    # watch ready count climb
```

### 8. Clean up after experiments

```bash
# Delete a single experiment (all its sessions)
arl exp delete exp-old --force

# Or find and delete idle pools
arl pool list -o json | jq -r '.[] | select(.allocatedReplicas == 0) | .name'
```

## JSON Output and Scripting

Every command supports `-o json` for machine-readable output. Combine with `jq` for powerful one-liners:

```bash
# Count sessions per experiment
arl exp list -o json | jq '.[] | "\(.experimentId): \(.sessionCount) sessions"'

# Find pools with no ready replicas (possible issue)
arl pool list -o json | jq '.[] | select(.readyReplicas == 0)'

# Get all pod IPs for a pool
arl session list --pool my-pool -o json | jq -r '.[].podIP'

# Total allocated pods across all pools
arl pool list -o json | jq '[.[].allocatedReplicas] | add'
```

## HTTP Proxy (mihomo)

The cluster runs an in-cluster mihomo (Clash Meta) proxy at `mihomo.arl.svc:7890`
so sandbox pods can access GitHub, PyPI, npm, etc. The operator automatically
injects `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY` env vars into all warm pool
containers (except sidecar) when `proxy.url` is set in Helm values.

```bash
# Verify proxy is running
kubectl --context=arl get deploy mihomo -n arl

# Test connectivity
kubectl --context=arl run test-proxy --rm -i --restart=Never -n arl \
  --image=pair-diag-cn-guangzhou.cr.volces.com/pair/redis:8-alpine \
  --env="https_proxy=http://mihomo.arl.svc:7890" \
  -- sh -c 'wget -q -O /dev/null https://github.com && echo OK || echo FAIL'

# Update proxy config (static Clash YAML)
kubectl --context=arl create configmap mihomo-config -n arl \
  --from-file=config.yaml=/path/to/config.yaml --dry-run=client -o yaml | \
  kubectl --context=arl apply -f -
kubectl --context=arl rollout restart deploy/mihomo -n arl
```

Helm values controlling proxy injection:
```yaml
proxy:
  url: "http://mihomo.arl.svc:7890"    # injected into all warm pool pods
  noProxy: ""                           # defaults to K8s internal ranges
```

## Tips

- `arl pool exec` is the fastest way to test something in a pool — it handles session lifecycle automatically
- `arl session shell` gives you a full interactive terminal if you need to poke around
- Use `-o wide` for extra columns (image, namespace, pod IP) without switching to JSON
- Logs stream from the sidecar's ring buffer (last 2000 lines), not from K8s — works without kubeconfig
- `arl exp` aliases to `arl experiment`, `arl session` aliases to `arl sess`
- Default `replicas=0` means new pools only pre-pull the image; pods are created on-demand when sessions are requested
- The mihomo proxy config is a static Clash YAML in ConfigMap `mihomo-config`; update it and restart the deployment to change proxy nodes
