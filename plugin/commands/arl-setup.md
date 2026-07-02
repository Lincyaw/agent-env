---
name: arl-setup
description: Install and configure the arl CLI tool
---

Help the user install and configure the `arl` CLI. Detect their platform and guide accordingly.

## Step 1: Install the binary

Check if `arl` is already available:
```bash
which arl 2>/dev/null || arl --version 2>/dev/null
```

If not installed, determine the installation method:

**Option A — From source (if in the agent-env repo):**
```bash
make build-cli
# Binary at bin/arl — suggest adding to PATH or copying to /usr/local/bin
```

**Option B — From GitHub Release:**
```bash
# Detect platform
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
[ "$ARCH" = "x86_64" ] && ARCH="amd64"
[ "$ARCH" = "aarch64" ] && ARCH="arm64"

# Download latest
curl -LO "https://github.com/Lincyaw/agent-env/releases/latest/download/arl-${OS}-${ARCH}"
chmod +x "arl-${OS}-${ARCH}"
sudo mv "arl-${OS}-${ARCH}" /usr/local/bin/arl
```

## Step 2: Configure

Ask the user for their gateway URL and API key. Prefer environment variables or
a key file over passing secrets as CLI arguments:
```bash
export ARL_GATEWAY_URL=http://<gateway-address>:8080
export ARL_API_KEY=<their-api-key>
export ARL_FORMAT=json

# Or:
mkdir -p ~/.config/arl
printf '%s\n' '<their-api-key>' > ~/.config/arl/api-key
chmod 600 ~/.config/arl/api-key
arl --api-key-file ~/.config/arl/api-key status
```

Recommend adding to shell profile (`~/.bashrc`, `~/.zshrc`).

## Step 3: Verify

```bash
arl --version
arl status
```

If `arl status` shows "Gateway: OK", the setup is complete. If it fails, help diagnose (wrong URL, auth issues, gateway not running).

## Step 4: Shell completion (optional)

```bash
# Bash
arl completion bash > /etc/bash_completion.d/arl

# Zsh
arl completion zsh > "${fpath[1]}/_arl"

# Fish
arl completion fish > ~/.config/fish/completions/arl.fish
```
