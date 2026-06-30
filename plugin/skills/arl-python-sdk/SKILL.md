---
name: arl-python-sdk
description: |
  Conceptual map for maintaining or using the `arl-env` Python SDK (`from arl import ...`). Use this skill when the user asks about ARL Python SDK capabilities, how SDK features map to gateway/runtime code, where to implement a SDK change, or how Python users should interact with ARL sessions, managed experiments, warm pools, files, replay, trajectories, logs, auth, or interactive shells. Prefer this as a feature/file guide, not a detailed API tutorial.
---

# ARL Python SDK

The Python package is `arl-env`; the import package is `arl`. The SDK is a
thin client over the ARL Gateway HTTP/WebSocket API. It does not call
Kubernetes directly.

Use this skill as a routing layer. Load only the reference needed for the task:

- Public SDK surfaces, feature ownership, runtime model, and repository index:
  read `references/implementation-map.md`.
- Usage semantics, recovery behavior, and change verification: read
  `references/usage-and-recovery.md`.
- CLI parity or operational workflows: use the `arl-cli` skill.

## Core Guidance

1. Keep gateway behavior authoritative. If a capability needs lifecycle,
   admission, recovery, or Kubernetes changes, update `pkg/gateway` first and
   make the SDK a thin wrapper.
2. Add or change Python-facing models in `types.py`, then export public symbols
   from `__init__.py`.
3. Put high-level session ergonomics in `session.py`; keep raw HTTP behavior in
   `gateway_client.py`.
4. Keep CLI and SDK behavior aligned for shared concepts such as sessions,
   pools, logs, files, restore/replay, and experiments.

## Focused Verification

For SDK-only changes:

```bash
uv run pytest sdk/python/arl/tests
uv run ruff check sdk/python/arl/arl examples/python
uv run mypy sdk/python/arl/arl examples/python
```

For gateway-backed behavior:

```bash
go test ./...
make check
```
