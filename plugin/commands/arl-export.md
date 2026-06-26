---
name: arl-export
description: Export trajectories from an experiment for SFT/RL training
args: experiment_id
---

Export all session trajectories from experiment `$ARGS` as JSONL files for downstream training.

```bash
EXPERIMENT_ID="$ARGS"

# List sessions
arl exp sessions "$EXPERIMENT_ID" -o json
```

Parse the session IDs from the JSON output, then for each session:

```bash
mkdir -p "trajectories/${EXPERIMENT_ID}"
arl session trajectory "<session-id>" -f "trajectories/${EXPERIMENT_ID}/<session-id>.jsonl"
```

After export, report:
- Number of sessions exported
- Total trajectory files and combined size
- Path to the output directory

If the experiment has no sessions, check if it exists at all with `arl exp list` and suggest alternatives.
