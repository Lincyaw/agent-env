---
name: arl-cli
description: |
  Guide for using the `arl` CLI to inspect, debug, and manage the ARL (Agentic RL) runtime: experiments, warm pools, sessions, files, snapshots, replay, private containers, logs, and metrics. Use this skill whenever the user asks about checking pool status, viewing experiment sessions, creating or debugging sessions, transferring files, restoring snapshots, replaying trajectories, streaming logs, exporting trajectories, waiting for pool readiness, running commands in private sidecar containers, or any operational task involving the ARL system. Also trigger when the user mentions `arl` CLI commands, asks "how do I see what's running", "show me the pools", "check experiment X", "debug this session", "看看 pool 状态", "查看实验", "导出 trajectory", or wants to combine multiple arl operations into a workflow.
---

# ARL CLI

`arl` is the command-line tool for the ARL runtime. It talks to the Gateway REST
API, which allocates sandbox-backed sessions and reaches sidecars inside pods.

Use this skill to choose the right CLI workflow and inspect the focused
reference needed for the task.

## Setup

```bash
make build-cli

export ARL_GATEWAY_URL=http://localhost:8080
export ARL_API_KEY=your-token
export ARL_FORMAT=json
```

Use `--format json` for scripts and automation. Do not use JSON format with
`arl session download <id> <remote> -`, because stdout is reserved for raw file
bytes.

Agent-facing introspection:

```bash
arl --dump-schema
```

## Choose a Reference

- Command syntax, flags, env vars, and exit codes: read `references/commands.md`.
- Operational playbooks and best practices: read `references/workflows.md`.
- Python SDK parity or implementation mapping: use the `arl-python-sdk` skill.

## Core Workflow

1. Confirm `ARL_GATEWAY_URL` and auth context before mutating pools or
   sessions.
2. Start with `arl status`, `arl pool list --format wide`, and
   `arl session list --format wide` when diagnosing runtime state.
3. Use experiments for benchmark/training runs that need grouping and cleanup.
4. Use one-off sessions or pool exec for ad hoc debugging.
5. Capture snapshot IDs and trajectory JSONL when a run may need replay or
   training data export.
6. Clean up temporary sessions, experiments, and pools after debugging.

## Verification

For CLI code changes, run focused Go checks:

```bash
go test ./cmd/arl/... ./pkg/gateway/...
go test ./...
```
