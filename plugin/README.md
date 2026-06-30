# ARL Agent Plugin

This directory contains the ARL agent-facing plugin. Install it to give Claude
Code or Codex task-specific ARL skills for sessions, warm pools, experiments,
files, replay, trajectories, logs, and Python SDK work.

## Install for Claude Code

Add the ARL plugin marketplace, then install the `arl` plugin from it:

```text
/plugin marketplace add Lincyaw/agent-env
/plugin install arl@arl
/reload-plugins
```

Claude Code reads `.claude-plugin/plugin.json`, `skills/`, and `commands/`.
Before cutting a release tag, sync the source manifest versions so Claude Code
can detect marketplace updates:

```bash
python3 plugin/scripts/set_plugin_version.py 0.15.7
```

## Install for Codex

Codex uses `.codex-plugin/plugin.json` and skill folders. The installer clones
this repository, regenerates command compatibility skills, and installs ARL
skills into `~/.codex/skills`:

```bash
curl -fsSL https://raw.githubusercontent.com/Lincyaw/agent-env/main/plugin/install.sh | bash
```

Install a specific branch or tag:

```bash
curl -fsSL https://raw.githubusercontent.com/Lincyaw/agent-env/main/plugin/install.sh | bash -s -- v0.15.6
```

Useful overrides:

```bash
curl -fsSL https://raw.githubusercontent.com/Lincyaw/agent-env/main/plugin/install.sh \
  | CODEX_SKILLS_DIR=/tmp/codex-skills \
    ARL_REPO_DIR=/tmp/arl-plugin \
    ARL_OVERWRITE_EXISTING=1 \
    bash -s -- main
```

## Included Skills

- `arl-cli`: operational CLI workflows for experiments, pools, sessions, logs,
  files, replay, trajectories, and gateway status.
- `arl-python-sdk`: Python SDK feature map and implementation guidance.
- Generated Codex command-wrapper skills from `commands/*.md`, produced during
  Codex install.

Detailed command/API notes live under each skill's `references/` directory so
the loaded skill body stays small.

## Local Development

Regenerate Codex command-wrapper skills:

```bash
cd plugin
python3 scripts/build_codex_compat_skills.py \
  --repo-root . \
  --out-dir .codex-generated-skills \
  --clean
```

Install locally:

```bash
make install-codex-skills
```

Dry run or custom destination:

```bash
cd plugin
./install-codex-skills.sh --dry-run
./install-codex-skills.sh --skills-dir /tmp/codex-skills
```
