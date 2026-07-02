# ARL CLI Workflows

## Runtime Snapshot

```bash
arl status
arl pool list --format wide
arl session list --format wide
arl exp list
```

## Debug a Session

```bash
arl session get <session-id>
arl session history <session-id> -v
arl session logs <session-id> --tail 50
```

Look for non-zero step exit codes, stderr, missing snapshot IDs, and
sidecar/executor connection errors.

## Debug a Pool

```bash
arl pool get <pool-name>
arl pool logs <pool-name> --tail 50
kubectl get sandboxwarmpools,sandboxclaims,sandboxes -A
```

Check conditions for failing pods, image pull errors, zero ready replicas, or
unexpected allocated replicas.

## Wait for Pool Readiness

```bash
arl pool create my-pool --image python:3.12 --replicas 3 --wait
arl pool scale my-pool --replicas 10 --wait --min-ready 5 --timeout 5m
arl pool wait my-pool --min-ready 3
```

Use `--wait` on create/scale for inline blocking. Use `arl pool wait` as a
standalone step in scripts that separate creation from readiness checks.

## Test an Image

```bash
arl pool create test-pool --image my-registry/my-image:latest --replicas 1 --wait
arl pool exec test-pool -- sh -c "uname -a && pwd"
arl pool delete test-pool --force
```

## Transfer Files

```bash
arl session upload <session-id> ./input.json data/input.json --verify
arl session exec <session-id> -- python train.py data/input.json
arl session download <session-id> outputs/result.json ./result.json
```

Use `-` for stdin/stdout only when the surrounding script expects raw bytes.

## Restore and Replay

```bash
arl session exec <session-id> -- python step.py
arl session history <session-id> -v
arl session restore <session-id> <snapshot-id>

arl session create --image python:3.12 --format json
arl session replay <target-session-id> --source <source-session-id> --up-to-step 3
```

Restore is for returning one session to a known snapshot. Replay is for
reconstructing a sequence of recorded steps in another session.

## Export Trajectories

```bash
arl exp sessions exp-42 --format json | jq -r '.[].id' > session_ids.txt
mkdir -p trajectories/exp-42
while read sid; do
  arl session trajectory "$sid" -f "trajectories/exp-42/${sid}.jsonl"
done < session_ids.txt
```

## Multi-Container Sandboxes

```bash
arl pool create ml-pool --image python:3.12 --replicas 2 --wait \
  --private-container '{"name":"redis","image":"redis:7"}' \
  --private-container '{"name":"pg","image":"postgres:16","mountWorkspace":true}'

arl session create --image python:3.12 \
  --private-containers-file containers.json

# Execute in the main sandbox
arl session exec <id> -- python train.py

# Execute in a private sidecar
arl session exec-container <id> redis -- redis-cli ping
arl session exec-container <id> pg -- psql -c "SELECT 1"
```

Use `--private-container` for inline JSON specs (repeatable) or
`--private-containers-file` to load from a JSON file. Use
`arl session exec-container` to run commands in a specific sidecar.

## Best Practices

- Prefer `arl exp create` for benchmark or training runs that need grouping and cleanup; prefer `arl session create` for ad hoc debugging.
- Use `--format json` for scripts, but keep binary downloads on raw stdout.
- Capture `snapshot_id` values from exec results if a run may need rollback or replay.
- Pass workspace-relative file paths to upload/download; avoid absolute paths unless the gateway-side behavior is intentional.
- Use `--verify` or `--sha256` for file uploads where corruption would invalidate an experiment.
- Clean up debug sessions and stop test pools with `arl session delete`, `arl exp delete --force`, and `arl pool delete --force`.
- Use `arl pool destroy --force` only when the WarmPool object and owned template should be physically removed.
- Pool management, global session listing, and managed session creation require an admin key when auth is enabled.
- User keys can create/delete owned sessions, execute commands, transfer files, restore/replay, open shells, and read owned history/trajectory.
- Gateway auth is enabled by default in Helm. Set `auth.apiKeys` or explicitly set `auth.enabled=false` only for trusted local deployments.
