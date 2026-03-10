# SSH Gateway

The ARL Gateway embeds an SSH server (Go `crypto/ssh`) that bridges native SSH sessions to existing sidecar InteractiveShell gRPC streams. This allows users to connect to sessions using any standard SSH client.

## Architecture

```
SSH Client (OpenSSH, PuTTY, Paramiko)
    ↓ SSH protocol (TCP port 2222)
Gateway SSH Server (pkg/gateway/ssh_server.go)
    ↓ Session lookup by username (sessionID)
    ↓ gRPC bidirectional stream
Sidecar (InteractiveShell RPC)
    ↓ JSON-over-Unix-socket
Executor Agent
    ↓ fork/exec
Container Shell (bash/sh)
```

## Usage

### Connecting to a Session

The SSH username is the session ID. First create a session via the REST API, then connect:

```bash
# Create a session (returns session ID like "gw-1710000000000-abcd1234")
SESSION_ID=$(curl -s -X POST http://gateway:8080/v1/sessions \
  -d '{"poolRef":"my-pool","namespace":"default"}' | jq -r .id)

# Connect via SSH
ssh -p 2222 $SESSION_ID@gateway.example.com
```

### Disable Host Key Checking

For ephemeral sessions where host key verification is impractical:

```bash
ssh -p 2222 \
  -o StrictHostKeyChecking=no \
  -o UserKnownHostsFile=/dev/null \
  $SESSION_ID@gateway.example.com
```

### Port Forwarding

Forward a local port to the pod (e.g., for Jupyter):

```bash
ssh -p 2222 -L 8888:localhost:8888 $SESSION_ID@gateway.example.com
```

### Programmatic Access (Python)

```python
import paramiko

client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect("gateway.example.com", port=2222, username=session_id, password="")

stdin, stdout, stderr = client.exec_command("ls /workspace")
print(stdout.read().decode())
client.close()
```

## Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `SSH_ENABLED` | `false` | Enable the SSH server |
| `SSH_PORT` | `2222` | TCP port for SSH |
| `SSH_HOST_KEY_PATH` | `/etc/arl/ssh_host_key` | Path to Ed25519 host key file |
| `SSH_PASSWORD` | (empty) | Optional static password; if empty, any password accepted as long as session exists |

### Helm Values

```yaml
ssh:
  enabled: true
  port: 2222
  hostKeyPath: "/etc/arl/ssh_host_key"
  password: ""  # Optional, stored in K8s Secret
```

## Host Key Management

- On first startup, the SSH server **auto-generates an Ed25519 key pair** and saves it to `SSH_HOST_KEY_PATH`
- On subsequent startups, the key is loaded from the file
- For production, pre-provision the host key via a Kubernetes Secret mount to ensure consistency across gateway restarts/replicas

### Pre-provisioning Host Key

```bash
# Generate a host key
ssh-keygen -t ed25519 -f ssh_host_key -N ""

# Create K8s secret
kubectl create secret generic arl-ssh-host-key \
  --from-file=ssh_host_key=ssh_host_key \
  -n arl-system

# Mount in gateway deployment (add to values.yaml or deployment template)
```

## Authentication

Currently supports:

- **Password callback**: Username = sessionID. The server validates that the session exists in the `SessionStore`. If `SSH_PASSWORD` is set, the provided password must match; otherwise any password is accepted.
- **No public key auth** in v1 (can be added later by extending `PublicKeyCallback` in `ssh_server.go`).

## Supported SSH Features

| Feature | Status |
|---------|--------|
| Interactive shell | Supported |
| PTY allocation (pty-req) | Supported |
| Terminal resize (window-change) | Supported |
| Signal forwarding (SIGINT, SIGTERM, SIGKILL) | Supported |
| Exit status reporting | Supported |
| exec (non-interactive command) | Not supported (returns error) |
| SFTP/SCP | Not supported |
| Port forwarding (direct-tcpip) | Not supported in v1 |

## Implementation

The SSH server is implemented in `pkg/gateway/ssh_server.go`:

- `SSHServer` struct manages the TCP listener and connection lifecycle
- Each SSH connection spawns a goroutine to handle channels
- On "session" channel with "shell" request, the handler:
  1. Looks up the session via `gw.GetSession(sessionID)`
  2. Opens a gRPC `InteractiveShell` stream to the sidecar at the session's pod IP
  3. Bridges the SSH channel ↔ gRPC stream with two goroutines (stdin and stdout)
  4. Forwards PTY resize and signal requests via the gRPC stream
  5. Sends `exit-status` to the SSH client when the gRPC stream closes
