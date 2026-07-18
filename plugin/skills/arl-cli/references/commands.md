# ARL CLI Command Reference

## Global Flags

| Flag | Short | Env var | Default | Purpose |
| --- | --- | --- | --- | --- |
| `--gateway-url` | `-g` | `ARL_GATEWAY_URL` | `http://localhost:8080` | Gateway base URL |
| `--api-key-file` | none | none | empty | Read bearer token from a file |
| `--api-key` | `-k` | `ARL_API_KEY` | empty | Bearer token; prefer env or file for automation |
| `--format` | none | `ARL_FORMAT` / `ARL_OUTPUT_FORMAT` | `table` | `table`, `json`, or `wide` |
| `--output` | `-o` | none | same as `--format` | Legacy alias for `--format` |
| `--no-color` | none | `NO_COLOR` | false | Disable ANSI color |

## Exit Codes

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

## Experiments

```bash
arl exp create <experiment-id> --image python:3.12 --sessions 1
arl exp create <experiment-id> --image python:3.12 --profile gpu --sessions 4 \
  --idle-timeout 1800
arl exp create <experiment-id> --image python:3.12 --wait-timeout 10m
arl exp list
arl exp sessions <experiment-id>
arl exp stats <experiment-id>
arl exp delete <experiment-id> --force
```

Use experiments when sessions should be grouped for later listing, statistics,
trajectory export, or bulk deletion.

## Pools

```bash
arl pool list
arl pool list --format wide
# Include stopped historical pools; default list is active-only.
arl pool list --all
arl pool get <name>
arl pool create <name> --image python:3.12 --profile <profile> --replicas 2
arl pool create <name> --image python:3.12 --replicas 1
arl pool create <name> --image python:3.12 --replicas 3 --wait --min-ready 2 --timeout 5m
arl pool scale <name> --replicas 5
arl pool scale <name> --replicas 10 --wait --timeout 10m
arl pool wait <name>
arl pool wait <name> --min-ready 3 --timeout 5m
# Drains sessions/claims and scales the WarmPool to zero.
arl pool delete <name> --force
# Physically deletes the WarmPool and its owned template.
arl pool destroy <name> --force

# Creates a temporary session, runs the command, then deletes the session.
arl pool exec <name> -- python -c "print('ok')"

arl pool logs <name>
arl pool logs <name> --tail 50 -f
```

`pool create` and `pool scale` accept `--wait` to block until `--min-ready`
sandboxes are ready (default: target replicas). `--timeout` defaults to 10m.
`pool wait` is the standalone equivalent for an already-existing pool.

Current CLI defaults `arl pool create` to `--replicas 2`. Gateway-created
image-backed pools use one replica unless the caller provides another value.

## Sessions

```bash
arl session create --image python:3.12
arl session create --profile default
arl session create --image python:3.12 --profile cpu \
  --idle-timeout 1800
arl session create --image python:3.12 --wait-timeout 10m

arl session list
arl session list --profile <profile>
arl session list --experiment <experiment-id>
arl session list --status active --limit 100 --cursor <session-id>
arl session list --format wide
arl session get <id>
arl session delete <id>

arl session exec <id> -- echo hello
arl session exec-container <id> <container> -- python -c "print('ok')"
arl session exec-container <id> <container> -- ls /data \
  --workdir /app --timeout 30 --env KEY=VALUE
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

All file paths are absolute (the workspace concept has been removed). Upload
uses `POST /v1/sessions/{id}/upload-file` with the `X-ARL-Path` header for
the remote path and raw bytes in the request body. Download uses
`POST /v1/sessions/{id}/download-file` with a JSON body `{"path": "/abs/path"}`.
Upload with `--verify` when the local input is a file and you want the CLI to
compute SHA256 and ask the gateway to verify the write. When uploading from
stdin, pass `--sha256` yourself if verification is required.

`restore` uses snapshot IDs returned by `session exec` results. `replay` copies
recorded steps from a source session into an existing target session; use
`--up-to-step` to stop after a specific zero-based step index.

`exec-container` runs a command in a private sidecar container rather than the
main sandbox. Accepts `--workdir`, `--timeout` (seconds), and `--env KEY=VALUE`
(repeatable).

`session create`, `exp create`, and `pool exec` accept `--wait-timeout`. The
default `0` waits until allocation is ready or the caller cancels; a value like
`10m` caps each session allocation wait and cleans up request-created resources
on timeout.

There is no current `arl session list --pool` flag. Filter by `--profile` or
`--experiment`, or use `--format json` with `jq`.

## Private Containers

`session create`, `pool create`, and `exp create` accept private container
flags for multi-container sandboxes:

```bash
arl session create --image python:3.12 \
  --private-container '{"name":"redis","image":"redis:7"}'
arl pool create my-pool --image python:3.12 \
  --private-container '{"name":"db","image":"postgres:16","mountWorkspace":true}' \
  --private-container '{"name":"cache","image":"redis:7"}'
arl exp create exp-1 --image python:3.12 --sessions 2 \
  --private-containers-file containers.json
```

`--private-container` takes an inline JSON object (repeatable).
`--private-containers-file` reads a JSON object or array from a file.

Private container spec fields: `name`, `image` (required), `mountWorkspace`,
`workspaceMountPath`, `workspaceAccess`, `command`, `args`, `env`,
`resources`, `imagePullPolicy`.

## Build API

`POST /v1/build` (admin role). Builds a container image via Kaniko. Requires
`build.enabled=true` in Helm values and a checkpoint PVC for context staging.

Request: multipart form with fields `image` (target ref), `context`
(tar.gz build context), optional `build_args` (JSON), `timeout` (seconds),
`cache` (true/false). Response: JSON with `image`, `digest`, `status`, `log`.

## Network Policy

`PATCH /v1/sessions/{id}/network-policy` (session owner). Updates the
network policy for a running session.

Request: JSON body with policy fields (e.g. `{"denyInternet": true}` or
`{"egressAllowCIDRs": ["10.0.0.0/8"]}`). Applies immediately to the
session's sandbox pod.

## Diagnostics

```bash
arl status
arl metrics
arl metrics --filter pool
arl metrics --raw
arl config
```

`arl status` uses the compact `/v1/summary` endpoint instead of downloading
full session, pool, and experiment lists.

The gateway exposes metrics on the internal port. If `arl metrics` fails
against the public gateway URL, port-forward the metrics service and point
`--gateway-url` at that internal endpoint.
