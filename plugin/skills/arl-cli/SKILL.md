---
name: arl-cli
description: |
  Guide for using the `arl` CLI to inspect, debug, and manage the ARL (Agentic RL) runtime: experiments, warm pools, sessions, files, snapshots, replay, logs, and metrics. Use this skill whenever the user asks about checking pool status, viewing experiment sessions, creating or debugging sessions, transferring files, restoring snapshots, replaying trajectories, streaming logs, exporting trajectories, or any operational task involving the ARL system. Also trigger when the user mentions `arl` CLI commands, asks "how do I see what's running", "show me the pools", "check experiment X", "debug this session", "看看 pool 状态", "查看实验", "导出 trajectory", or wants to combine multiple arl operations into a workflow.
---

# ARL CLI

`arl` is the command-line tool for the ARL runtime. It talks to the Gateway REST
API, which allocates sandbox-backed sessions and reaches sidecars inside pods.

## Setup

```bash
make build-cli          # produces bin/arl

export ARL_GATEWAY_URL=http://localhost:8080
export ARL_API_KEY=your-token
export ARL_NAMESPACE=default
export ARL_FORMAT=json        # optional: table, json, or wide
```

Global flags:

| Flag | Short | Env var | Default | Purpose |
| --- | --- | --- | --- | --- |
| `--gateway-url` | `-g` | `ARL_GATEWAY_URL` | `http://localhost:8080` | Gateway base URL |
| `--api-key-file` | none | none | empty | Read bearer token from a file |
| `--api-key` | `-k` | `ARL_API_KEY` | empty | Bearer token; prefer env or file for automation |
| `--namespace` | `-n` | `ARL_NAMESPACE` | `default` | Namespace for session, pool, and experiment creation |
| `--format` | none | `ARL_FORMAT` / `ARL_OUTPUT_FORMAT` | `table` | `table`, `json`, or `wide` |
| `--output` | `-o` | none | same as `--format` | Legacy alias for `--format` |
| `--no-color` | none | `NO_COLOR` | false | Disable ANSI color |

Use `--format json` for scripts and automation. Do not use JSON format with
`arl session download <id> <remote> -`, because stdout is reserved for the file
bytes.

Agent-facing introspection:

```bash
arl --dump-schema
```

Exit codes:

| Code | Meaning |
| --- | --- |
| `0` | Success |
| `1` | Generic error |
| `2` | Argument or syntax error |
| `3` | Resource not found |
| `4` | Authentication or permission failure |
| `5` | Conflict or already exists |
| `6` | User cancelled or interrupted |
| `7` | Missing dependency or environment error |

## Commands

### Experiments

```bash
arl exp create <experiment-id> --image python:3.12 --sessions 1
arl exp create <experiment-id> --image python:3.12 --profile gpu --sessions 4 \
  --workspace-dir /workspace --idle-timeout 1800 --max-lifetime 7200
arl exp list
arl exp sessions <experiment-id>
arl exp stats <experiment-id>
arl exp delete <experiment-id> --force
```

Use experiments when sessions should be grouped for later listing, statistics,
trajectory export, or bulk deletion.

### Pools

```bash
arl pool list
arl pool list -A
arl pool list --format wide
arl pool get <name>
arl pool create <name> --image python:3.12 --profile <profile> --replicas 2
arl pool create <name> --image python:3.12 --workspace-dir /workspace
arl pool scale <name> --replicas 5
arl pool delete <name> --force

# Creates a temporary session, runs the command, then deletes the session.
arl pool exec <name> -- python -c "print('ok')"

arl pool logs <name>
arl pool logs <name> --tail 50 -f
```

Current CLI defaults `arl pool create` to `--replicas 2`. Gateway-created
image-backed pools use one replica unless the caller provides another value.

### Sessions

```bash
arl session create --image python:3.12
arl session create --profile default
arl session create --image python:3.12 --profile cpu \
  --idle-timeout 1800 --max-lifetime 7200

arl session list
arl session list --profile <profile>
arl session list --experiment <experiment-id>
arl session list --format wide
arl session get <id>
arl session delete <id>

arl session exec <id> -- echo hello
arl session shell <id>

arl session upload <id> ./local.txt data/local.txt
arl session upload <id> ./local.txt data/local.txt --verify
arl session upload <id> - data/stdin.txt --sha256 <expected-sha256>
arl session download <id> data/local.txt ./local-copy.txt
arl session download <id> data/local.txt -

arl session restore <id> <snapshot-id>
arl session replay <target-id> --source <source-id>
arl session replay <target-id> --source <source-id> --up-to-step 3

arl session history <id>
arl session history <id> -v
arl session trajectory <id>
arl session trajectory <id> -f out.jsonl

arl session logs <id>
arl session logs <id> --tail 50 -f
```

File paths are workspace-relative. Upload with `--verify` when the local input
is a file and you want the CLI to compute SHA256 and ask the gateway to verify
the write. When uploading from stdin, pass `--sha256` yourself if verification
is required.

`restore` uses snapshot IDs returned by `session exec` results. `replay` copies
recorded steps from a source session into an existing target session; use
`--up-to-step` to stop after a specific zero-based step index.

There is no current `arl session list --pool` flag. Filter by `--profile` or
`--experiment`, or use `--format json` with `jq`.

### Diagnostics

```bash
arl status
arl metrics
arl metrics --filter pool
arl metrics --raw
arl config
```

The gateway exposes metrics on the internal port. If `arl metrics` fails
against the public gateway URL, port-forward the metrics service and point
`--gateway-url` at that internal endpoint.

## Common Workflows

### Runtime Snapshot

```bash
arl status
arl pool list --format wide
arl session list --format wide
arl exp list
```

### Debug a Session

```bash
arl session get <session-id>
arl session history <session-id> -v
arl session logs <session-id> --tail 50
```

Look for non-zero step exit codes, stderr, missing snapshot IDs, and
sidecar/executor connection errors.

### Debug a Pool

```bash
arl pool get <pool-name>
arl pool logs <pool-name> --tail 50
kubectl get sandboxwarmpools,sandboxclaims,sandboxes -A
```

Check conditions for failing pods, image pull errors, zero ready replicas, or
allocated replicas consuming all warm capacity.

### Test an Image

```bash
arl pool create test-pool --image my-registry/my-image:latest --replicas 1
arl pool get test-pool
arl pool exec test-pool -- sh -c "uname -a && pwd"
arl pool delete test-pool --force
```

### Transfer Files

```bash
arl session upload <session-id> ./input.json data/input.json --verify
arl session exec <session-id> -- python train.py data/input.json
arl session download <session-id> outputs/result.json ./result.json
```

Use `-` for stdin/stdout only when the surrounding script expects raw bytes.

### Restore and Replay

```bash
arl session exec <session-id> -- python step.py
arl session history <session-id> -v
arl session restore <session-id> <snapshot-id>

arl session create --image python:3.12 --format json
arl session replay <target-session-id> --source <source-session-id> --up-to-step 3
```

Restore is for returning one session to a known snapshot. Replay is for
reconstructing a sequence of recorded steps in another session.

### Export Trajectories

```bash
arl exp sessions exp-42 --format json | jq -r '.[].id' > session_ids.txt
mkdir -p trajectories/exp-42
while read sid; do
  arl session trajectory "$sid" -f "trajectories/exp-42/${sid}.jsonl"
done < session_ids.txt
```

## Best Practices

- Prefer `arl exp create` for benchmark or training runs that need grouping and cleanup; prefer `arl session create` for ad hoc debugging.
- Use `--format json` for scripts, but keep binary downloads on raw stdout.
- Capture `snapshot_id` values from exec results if a run may need rollback or replay.
- Pass workspace-relative file paths to upload/download; avoid absolute paths unless the gateway-side behavior is intentional.
- Use `--verify` or `--sha256` for file uploads where corruption would invalidate an experiment.
- Clean up debug sessions and test pools with `arl session delete`, `arl exp delete --force`, and `arl pool delete --force`.
- Pool management, global session listing, and managed session creation require an admin key when auth is enabled.
- User keys can create/delete owned sessions, execute commands, transfer files, restore/replay, open shells, and read owned history/trajectory.
- Gateway auth is enabled by default in Helm. Set `auth.apiKeys` or explicitly set `auth.enabled=false` only for trusted local deployments.
