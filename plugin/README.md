# ARL Agent Plugin

This directory is the agent-facing ARL plugin source.

## Claude Code

Claude Code uses `.claude-plugin/plugin.json`, `skills/`, and `commands/` directly.

## Codex

Codex uses `.codex-plugin/plugin.json` and `skills/`. Claude Code slash commands do not have a
native Codex equivalent, so convert them into compatibility skills before installing or testing:

After this plugin has been pushed to GitHub, install or update it in one command:

```bash
curl -fsSL https://raw.githubusercontent.com/Lincyaw/agent-env/main/plugin/install.sh | bash
```

To install a specific branch or tag:

```bash
curl -fsSL https://raw.githubusercontent.com/Lincyaw/agent-env/main/plugin/install.sh | bash -s -- my-feature-branch
```

For forks or private mirrors, override the clone URL:

```bash
curl -fsSL https://raw.githubusercontent.com/Lincyaw/agent-env/main/plugin/install.sh \
  | ARL_REPO_URL=git@github.com:your-org/agent-env.git bash
```

The remote installer clones/updates the repo under `~/.codex/repos/arl`, regenerates command
compatibility skills, and installs them into `~/.codex/skills`.

Useful environment overrides:

```bash
curl -fsSL https://raw.githubusercontent.com/Lincyaw/agent-env/main/plugin/install.sh \
  | CODEX_SKILLS_DIR=/tmp/codex-skills \
    ARL_REPO_DIR=/tmp/arl-plugin \
    ARL_OVERWRITE_EXISTING=1 \
    bash -s -- main
```

For local development, regenerate the compatibility skills directly:

```bash
cd plugin
python3 scripts/build_codex_compat_skills.py \
  --repo-root . \
  --out-dir .codex-generated-skills \
  --clean
```

The generated `.codex-generated-skills/` directory is ignored by git because `commands/*.md` remain
the source of truth. Copy both `skills/` and `.codex-generated-skills/` into a Codex skills location
when doing a manual install.

To do that automatically:

```bash
make install-codex-skills
```

The installer refreshes generated skills, then copies both `skills/` and `.codex-generated-skills/`
into `~/.codex/skills`. It writes `.arl-managed.json` markers so later installs can update ARL-owned
skills without clobbering unrelated same-named skills.

Useful options:

```bash
cd plugin
./install-codex-skills.sh --dry-run
./install-codex-skills.sh --skills-dir /tmp/codex-skills
CODEX_SKILLS_DIR=/tmp/codex-skills make install-codex-skills
./install-codex-skills.sh --force
```
