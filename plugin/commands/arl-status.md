---
name: arl-status
description: Quick snapshot of the ARL runtime — gateway health, pools, sessions, experiments
---

Run the following commands and present the results as a concise dashboard:

```bash
arl status
arl pool list --format wide
arl session list --format wide
arl exp list
```

If any command fails (e.g., gateway unreachable), report the error and suggest checking `ARL_GATEWAY_URL` and `ARL_API_KEY` environment variables.

Summarize key numbers: total pools, ready/allocated pods, active sessions, experiments. Flag anything that looks unhealthy (pools with 0 ready replicas, sessions stuck for a long time).
