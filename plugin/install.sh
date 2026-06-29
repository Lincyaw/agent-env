#!/usr/bin/env bash
set -euo pipefail

REPO_URL="${ARL_REPO_URL:-https://github.com/Lincyaw/agent-env.git}"
REF="${1:-${ARL_REF:-main}}"
REPO_DIR="${ARL_REPO_DIR:-$HOME/.codex/repos/arl}"
PLUGIN_DIR="$REPO_DIR/plugin"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

require_cmd git
require_cmd python3

mkdir -p "$(dirname "$REPO_DIR")"

if [ -d "$REPO_DIR/.git" ]; then
  echo "Updating ARL repo in $REPO_DIR"
  git -C "$REPO_DIR" fetch --tags origin
  git -C "$REPO_DIR" checkout "$REF"
  if git -C "$REPO_DIR" show-ref --verify --quiet "refs/remotes/origin/${REF}"; then
    if git -C "$REPO_DIR" diff --quiet && git -C "$REPO_DIR" diff --cached --quiet; then
      git -C "$REPO_DIR" pull --ff-only origin "$REF"
    else
      echo "Skipping pull because $REPO_DIR has local changes" >&2
    fi
  fi
else
  echo "Cloning ARL repo into $REPO_DIR"
  git clone "$REPO_URL" "$REPO_DIR"
  git -C "$REPO_DIR" checkout "$REF"
fi

if [ ! -f "$PLUGIN_DIR/install-codex-skills.sh" ]; then
  echo "Missing installer: $PLUGIN_DIR/install-codex-skills.sh" >&2
  exit 1
fi

bash "$PLUGIN_DIR/install-codex-skills.sh"

cat <<EOF

Done.

Installed ARL Codex skills into:
  ${CODEX_SKILLS_DIR:-$HOME/.codex/skills}

Repo checkout lives in:
  $REPO_DIR

Restart Codex if the new skills do not appear immediately.
EOF
