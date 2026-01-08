# Quick Start Tutorial

This tutorial walks you through using the ARL Python SDK from installation to execution.

## Prerequisites

- Python 3.9+
- Access to a Kubernetes cluster with ARL-Infra deployed
- Valid `kubeconfig` file

## Step 1: Install the SDK

```bash
pip install arl-env
```

## Step 2: Verify Cluster Access

```bash
# Check cluster connectivity
kubectl cluster-info

# Verify ARL-Infra is deployed
kubectl get warmpools
```

You should see at least one WarmPool. If not, ask your administrator to create one.

## Step 3: Your First Task

Create a file called `hello.py`:

```python
from arl import SandboxSession

# Connect to a sandbox
with SandboxSession(pool_ref="python-pool", namespace="default") as session:
    # Execute a simple command
    result = session.execute([
        {
            "name": "hello",
            "type": "Command",
            "command": ["echo", "Hello from ARL!"],
        }
    ])
    
    # Print the output
    print(result["status"]["stdout"])
```

Run it:

```bash
python hello.py
```

Expected output:

```
Hello from ARL!
```

## Step 4: Write and Execute Code

Let's write a Python script and execute it:

```python
from arl import SandboxSession

with SandboxSession(pool_ref="python-pool", namespace="default") as session:
    result = session.execute([
        # Step 1: Write a Python script
        {
            "name": "write_script",
            "type": "FilePatch",
            "path": "/workspace/calculate.py",
            "content": """
import math

def calculate_circle(radius):
    area = math.pi * radius ** 2
    circumference = 2 * math.pi * radius
    return area, circumference

radius = 5
area, circumference = calculate_circle(radius)
print(f"Circle with radius {radius}:")
print(f"  Area: {area:.2f}")
print(f"  Circumference: {circumference:.2f}")
""",
        },
        # Step 2: Execute the script
        {
            "name": "run_script",
            "type": "Command",
            "command": ["python", "/workspace/calculate.py"],
        },
    ])
    
    print(result["status"]["stdout"])
```

Expected output:

```
Circle with radius 5:
  Area: 78.54
  Circumference: 31.42
```

## Step 5: Environment Variables

Pass environment variables to your commands:

```python
from arl import SandboxSession

with SandboxSession(pool_ref="python-pool", namespace="default") as session:
    result = session.execute([
        {
            "name": "write_script",
            "type": "FilePatch",
            "path": "/workspace/env_demo.py",
            "content": """
import os

name = os.environ.get("USER_NAME", "Guest")
mode = os.environ.get("MODE", "default")

print(f"Hello, {name}!")
print(f"Running in {mode} mode")
""",
        },
        {
            "name": "run_script",
            "type": "Command",
            "command": ["python", "/workspace/env_demo.py"],
            "env": {
                "USER_NAME": "Alice",
                "MODE": "production",
            },
        },
    ])
    
    print(result["status"]["stdout"])
```

Expected output:

```
Hello, Alice!
Running in production mode
```

## Step 6: Persistent Sandbox

For multiple related tasks, reuse the same sandbox:

```python
from arl import SandboxSession

# Create a persistent session
session = SandboxSession(
    pool_ref="python-pool",
    namespace="default",
    keep_alive=True  # Keep sandbox between tasks
)

try:
    session.create_sandbox()
    
    # Task 1: Initialize a counter file
    session.execute([
        {
            "name": "init",
            "type": "FilePatch",
            "path": "/workspace/counter.txt",
            "content": "0",
        }
    ])
    
    # Task 2: Increment counter
    result = session.execute([
        {
            "name": "increment",
            "type": "Command",
            "command": ["sh", "-c", 
                "count=$(cat /workspace/counter.txt); "
                "count=$((count + 1)); "
                "echo $count > /workspace/counter.txt; "
                "echo Counter: $count"
            ],
        }
    ])
    print(result["status"]["stdout"])  # Counter: 1
    
    # Task 3: Increment again (state persists!)
    result = session.execute([
        {
            "name": "increment",
            "type": "Command",
            "command": ["sh", "-c", 
                "count=$(cat /workspace/counter.txt); "
                "count=$((count + 1)); "
                "echo $count > /workspace/counter.txt; "
                "echo Counter: $count"
            ],
        }
    ])
    print(result["status"]["stdout"])  # Counter: 2
    
finally:
    session.delete_sandbox()
```

## Step 7: Error Handling

Handle errors gracefully:

```python
from arl import SandboxSession

with SandboxSession(pool_ref="python-pool", namespace="default") as session:
    result = session.execute([
        {
            "name": "failing_command",
            "type": "Command",
            "command": ["python", "-c", "raise ValueError('Something went wrong!')"],
        }
    ])
    
    status = result["status"]
    
    if status["state"] == "Succeeded":
        print(f"Success: {status['stdout']}")
    else:
        print(f"Failed with exit code: {status.get('exitCode', 'unknown')}")
        print(f"Error output: {status.get('stderr', 'no error output')}")
```

## Step 8: Using Callbacks

Monitor task execution with callbacks:

```python
from arl import SandboxSession

def on_complete(result):
    state = result["status"]["state"]
    print(f"[Callback] Task completed with state: {state}")

def on_success(result):
    stdout = result["status"].get("stdout", "")
    print(f"[Callback] Success! Output: {stdout[:50]}...")

def on_failure(result):
    stderr = result["status"].get("stderr", "")
    print(f"[Callback] Failed! Error: {stderr[:50]}...")

# Create session and register callbacks
session = SandboxSession(pool_ref="python-pool", namespace="default")
session.register_callback("on_task_complete", on_complete)
session.register_callback("on_task_success", on_success)
session.register_callback("on_task_failure", on_failure)

with session:
    # This will trigger on_complete and on_success
    result = session.execute([
        {
            "name": "success_task",
            "type": "Command",
            "command": ["echo", "This will succeed"],
        }
    ])
```

## Summary

You've learned how to:

- [x] Install the ARL SDK
- [x] Execute simple commands
- [x] Write and execute code files
- [x] Use environment variables
- [x] Maintain persistent sandboxes
- [x] Handle errors
- [x] Use callbacks

## Next Steps

- [Python SDK Guide](python-sdk.md) - Detailed SDK documentation
- [API Reference](api-reference.md) - Complete API documentation
- [Examples](examples.md) - More advanced examples
