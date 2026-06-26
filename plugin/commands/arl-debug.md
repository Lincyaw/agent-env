---
name: arl-debug
description: Debug a failing ARL experiment or session — checks pool health, session history, and logs
args: target
---

The user wants to debug an ARL issue. The `$ARGS` may be an experiment ID, session ID, or pool name — figure out which one based on the format:

- Starts with `gw-` → session ID
- Contains only alphanumeric, dash, underscore, dot → could be experiment ID or pool name; try both

## Debugging flow

### If session ID:
```bash
arl session get "$TARGET"
arl session history "$TARGET" -v
arl session logs "$TARGET" --tail 50
```
Look at the execution history for non-zero exit codes, error output in stderr, and sidecar logs for connectivity issues.

### If experiment ID:
```bash
arl exp sessions "$TARGET"
arl exp stats "$TARGET"
```
Then pick the pool from the output and check its health:
```bash
arl pool get <pool-name>
```
If there are sessions, check the most recent one's history:
```bash
arl session history <most-recent-session-id> -v
arl session logs <most-recent-session-id> --tail 50
```

### If pool name:
```bash
arl pool get "$TARGET"
arl pool logs "$TARGET" --tail 50
```
Check conditions for `PodsFailing`, `Ready=False`, or `ConfigEnvReady=False`.

## What to report

Summarize:
1. Is the pool healthy? (ready replicas, conditions)
2. Are sessions executing successfully? (exit codes, error output)
3. Any sidecar-level errors? (from logs)
4. Recommended action (scale up, fix image, check config, etc.)
