#!/bin/sh
set -e

# ARL Devbox Entrypoint
# Configures SSH, git, and workspace from ARL_DEVBOX_* environment variables
# injected by the gateway when mode=devbox.

setup_ssh() {
    if [ -z "$ARL_DEVBOX_SSH_PUBLIC_KEYS" ]; then
        return
    fi

    # Install openssh-server if not present
    if ! command -v sshd >/dev/null 2>&1; then
        if command -v apt-get >/dev/null 2>&1; then
            apt-get update -qq && apt-get install -y -qq openssh-server >/dev/null 2>&1
        elif command -v apk >/dev/null 2>&1; then
            apk add --no-cache openssh-server >/dev/null 2>&1
        elif command -v yum >/dev/null 2>&1; then
            yum install -y openssh-server >/dev/null 2>&1
        fi
    fi

    # Configure SSH
    mkdir -p /root/.ssh
    chmod 700 /root/.ssh
    printf '%s\n' "$ARL_DEVBOX_SSH_PUBLIC_KEYS" > /root/.ssh/authorized_keys
    chmod 600 /root/.ssh/authorized_keys

    # Generate host keys if missing
    mkdir -p /etc/ssh
    if [ ! -f /etc/ssh/ssh_host_ed25519_key ]; then
        ssh-keygen -t ed25519 -f /etc/ssh/ssh_host_ed25519_key -N "" -q
    fi
    if [ ! -f /etc/ssh/ssh_host_rsa_key ]; then
        ssh-keygen -t rsa -b 4096 -f /etc/ssh/ssh_host_rsa_key -N "" -q
    fi

    # Ensure sshd config allows key auth
    mkdir -p /run/sshd
    cat > /etc/ssh/sshd_config.d/devbox.conf 2>/dev/null <<'EOF' || true
PermitRootLogin prohibit-password
PubkeyAuthentication yes
PasswordAuthentication no
EOF

    # Start sshd in background
    if command -v sshd >/dev/null 2>&1; then
        /usr/sbin/sshd -e 2>&1 &
        echo "[devbox] sshd started on port 22"
    fi
}

setup_git() {
    if [ -n "$ARL_DEVBOX_GIT_USER_NAME" ]; then
        git config --global user.name "$ARL_DEVBOX_GIT_USER_NAME"
    fi
    if [ -n "$ARL_DEVBOX_GIT_USER_EMAIL" ]; then
        git config --global user.email "$ARL_DEVBOX_GIT_USER_EMAIL"
    fi
}

main() {
    setup_ssh
    setup_git

    # Execute the original command or sleep forever
    if [ $# -gt 0 ]; then
        exec "$@"
    else
        echo "[devbox] ready"
        exec sleep infinity
    fi
}

main "$@"
