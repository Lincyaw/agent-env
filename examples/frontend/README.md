# Interactive Shell - Frontend Integration Guide

This guide shows how to integrate ARL interactive shells into your web frontend.

## Architecture

```
┌─────────────┐     WebSocket      ┌──────────────┐     K8s Stream     ┌─────────────┐
│   Browser   │ ◄─────────────────► │ Shell Server │ ◄─────────────────► │ Pod/Container│
│  (xterm.js) │                     │  (FastAPI)   │                     │  (executor)  │
└─────────────┘                     └──────────────┘                     └─────────────┘
```

## Quick Start

### 1. Start the WebSocket Server

**Option A: FastAPI Server (Recommended)**

```bash
cd sdk/python/arl
pip install fastapi uvicorn websockets kubernetes

# Start server
python -m arl.shell_server
# Server runs on http://localhost:8000
```

**Option B: Standalone WebSocket Server**

```bash
pip install websockets kubernetes

# Start server
python -m arl.interactive_shell
# Server runs on ws://localhost:8765
```

### 2. Open the Frontend

```bash
# Serve the HTML file
cd examples/frontend
python -m http.server 8080

# Open in browser
open http://localhost:8080/interactive_shell.html
```

### 3. Connect to a Sandbox

1. Create a sandbox first:
   ```python
   from arl import SandboxSession

   session = SandboxSession(pool_ref="my-pool", namespace="default", keep_alive=True)
   session.create_sandbox()
   print(f"Pod name: {session.get_sandbox()['status']['podName']}")
   ```

2. In the web UI:
   - Enter the pod name
   - Select container (executor or sidecar)
   - Click "Connect"

## API Reference

### WebSocket Protocol

**Client → Server Messages:**

```json
// Send input to shell
{
  "type": "input",
  "data": "ls -la\n"
}

// Resize terminal
{
  "type": "resize",
  "rows": 24,
  "cols": 80
}
```

**Server → Client Messages:**

```json
// Connection established
{
  "type": "connected",
  "pod_name": "session-xxx",
  "container": "executor"
}

// Shell output
{
  "type": "output",
  "data": "output text"
}

// Error occurred
{
  "type": "error",
  "message": "error message"
}

// Connection closed
{
  "type": "closed"
}
```

### FastAPI Endpoints

**WebSocket Endpoint:**
```
ws://localhost:8000/ws/shell/{namespace}/{pod_name}?container=executor
```

**REST Endpoints:**
```
GET /health
GET /api/sandboxes/{namespace}
```

## Frontend Integration

### Using xterm.js

```javascript
import { Terminal } from 'xterm';
import { FitAddon } from 'xterm-addon-fit';

// Create terminal
const term = new Terminal();
const fitAddon = new FitAddon();
term.loadAddon(fitAddon);
term.open(document.getElementById('terminal'));
fitAddon.fit();

// Connect to WebSocket
const ws = new WebSocket('ws://localhost:8000/ws/shell/default/my-pod?container=executor');

// Forward terminal input to WebSocket
term.onData(data => {
  ws.send(JSON.stringify({ type: 'input', data }));
});

// Forward WebSocket output to terminal
ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  if (msg.type === 'output') {
    term.write(msg.data);
  }
};
```

### React Example

```jsx
import React, { useEffect, useRef } from 'react';
import { Terminal } from 'xterm';
import { FitAddon } from 'xterm-addon-fit';
import 'xterm/css/xterm.css';

function InteractiveShell({ podName, namespace, container }) {
  const terminalRef = useRef(null);
  const wsRef = useRef(null);

  useEffect(() => {
    const term = new Terminal();
    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.open(terminalRef.current);
    fitAddon.fit();

    const ws = new WebSocket(
      `ws://localhost:8000/ws/shell/${namespace}/${podName}?container=${container}`
    );

    term.onData(data => {
      ws.send(JSON.stringify({ type: 'input', data }));
    });

    ws.onmessage = (event) => {
      const msg = JSON.parse(event.data);
      if (msg.type === 'output') {
        term.write(msg.data);
      }
    };

    wsRef.current = ws;

    return () => {
      ws.close();
      term.dispose();
    };
  }, [podName, namespace, container]);

  return <div ref={terminalRef} style={{ height: '600px' }} />;
}
```

## Production Deployment

### 1. Configure CORS

```python
# shell_server.py
app.add_middleware(
    CORSMiddleware,
    allow_origins=["https://your-frontend.com"],  # Specific origins
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)
```

### 2. Add Authentication

```python
from fastapi import WebSocket, HTTPException, Depends
from fastapi.security import HTTPBearer

security = HTTPBearer()

async def verify_token(token: str):
    # Implement your token verification
    pass

@app.websocket("/ws/shell/{namespace}/{pod_name}")
async def websocket_shell(
    websocket: WebSocket,
    namespace: str,
    pod_name: str,
    token: str = Depends(security)
):
    await verify_token(token)
    # ... rest of the code
```

### 3. Use HTTPS/WSS

```bash
# Use nginx or similar for SSL termination
# Configure to proxy WebSocket connections
```

### 4. Deploy with Docker

```dockerfile
FROM python:3.11-slim

WORKDIR /app
COPY sdk/python/arl /app/arl
RUN pip install fastapi uvicorn websockets kubernetes

CMD ["python", "-m", "arl.shell_server"]
```

## Troubleshooting

### Connection Fails

1. Check server is running:
   ```bash
   curl http://localhost:8000/health
   ```

2. Check Kubernetes access:
   ```bash
   kubectl get pods -n default
   ```

3. Check pod exists:
   ```bash
   kubectl get pod <pod-name> -n default
   ```

### Terminal Not Responding

1. Check WebSocket connection in browser console
2. Verify container has `/bin/bash` or `/bin/sh`
3. Check pod logs for errors

### CORS Errors

Configure CORS in `shell_server.py` to allow your frontend origin.

## Security Considerations

⚠️ **Important Security Notes:**

1. **Authentication**: Always require authentication in production
2. **Authorization**: Verify users can only access their own sandboxes
3. **Rate Limiting**: Implement rate limiting to prevent abuse
4. **Audit Logging**: Log all shell sessions for security audits
5. **Network Policies**: Use Kubernetes network policies to restrict access
6. **SSL/TLS**: Always use WSS (WebSocket Secure) in production

## Examples

See:
- `examples/frontend/interactive_shell.html` - Complete frontend example
- `sdk/python/arl/arl/shell_server.py` - FastAPI server
- `sdk/python/arl/arl/interactive_shell.py` - Standalone WebSocket server
