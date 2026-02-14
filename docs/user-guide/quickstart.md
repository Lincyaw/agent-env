# Quick Start Tutorial

This tutorial walks you through using the ARL Python SDK from installation to execution.

## Prerequisites

- Python 3.9+
- Access to an ARL-Infra cluster with the Gateway running
- Gateway URL (e.g., `http://localhost:8080`)

## Step 1: Install the SDK

```bash
pip install arl-env
```

## Step 2: Verify Setup

```bash
# Check ARL-Infra is deployed
kubectl get warmpools
```

You should see at least one WarmPool. If not, ask your administrator to create one.

## Step 3: Your First Execution

Create a file called `hello.py`:

```python
from arl import SandboxSession

# Connect to a sandbox via the Gateway
with SandboxSession(
    pool_ref="python-pool",
    namespace="default",
    gateway_url="http://localhost:8080",
) as session:
    # Execute a simple command
    result = session.execute([
        {"name": "hello", "command": ["echo", "Hello from ARL!"]},
    ])

    # Print the output
    print(result.results[0].output.stdout)
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

with SandboxSession(
    pool_ref="python-pool",
    namespace="default",
    gateway_url="http://localhost:8080",
) as session:
    result = session.execute([
        # Step 1: Write a Python script
        {
            "name": "write_script",
            "command": ["bash", "-c", """
cat > /workspace/calculate.py << 'PYEOF'
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
PYEOF
"""],
        },
        # Step 2: Execute the script
        {"name": "run_script", "command": ["python", "/workspace/calculate.py"]},
    ])

    print(result.results[-1].output.stdout)
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

with SandboxSession(
    pool_ref="python-pool",
    namespace="default",
    gateway_url="http://localhost:8080",
) as session:
    result = session.execute([
        {
            "name": "env_demo",
            "command": ["python", "-c", """
import os

name = os.environ.get("USER_NAME", "Guest")
mode = os.environ.get("MODE", "default")

print(f"Hello, {name}!")
print(f"Running in {mode} mode")
"""],
        },
    ])

    print(result.results[0].output.stdout)
```

## Step 6: Persistent Sandbox

For multiple related executions, reuse the same sandbox:

```python
from arl import SandboxSession

# Create a persistent session
session = SandboxSession(
    pool_ref="python-pool",
    namespace="default",
    gateway_url="http://localhost:8080",
    keep_alive=True,  # Keep sandbox between executions
)

try:
    session.create_sandbox()

    # Execution 1: Initialize a counter file
    session.execute([
        {"name": "init", "command": ["bash", "-c", "echo 0 > /workspace/counter.txt"]},
    ])

    # Execution 2: Increment counter
    result = session.execute([
        {
            "name": "increment",
            "command": ["sh", "-c",
                "count=$(cat /workspace/counter.txt); "
                "count=$((count + 1)); "
                "echo $count > /workspace/counter.txt; "
                "echo Counter: $count"
            ],
        },
    ])
    print(result.results[0].output.stdout)  # Counter: 1

    # Execution 3: Increment again (state persists!)
    result = session.execute([
        {
            "name": "increment",
            "command": ["sh", "-c",
                "count=$(cat /workspace/counter.txt); "
                "count=$((count + 1)); "
                "echo $count > /workspace/counter.txt; "
                "echo Counter: $count"
            ],
        },
    ])
    print(result.results[0].output.stdout)  # Counter: 2

finally:
    session.delete_sandbox()
```

## Step 7: Error Handling

Handle errors gracefully:

```python
from arl import SandboxSession

with SandboxSession(
    pool_ref="python-pool",
    namespace="default",
    gateway_url="http://localhost:8080",
) as session:
    result = session.execute([
        {
            "name": "failing_command",
            "command": ["python", "-c", "raise ValueError('Something went wrong!')"],
        },
    ])

    step_result = result.results[0]

    if step_result.output.exit_code == 0:
        print(f"Success: {step_result.output.stdout}")
    else:
        print(f"Failed with exit code: {step_result.output.exit_code}")
        print(f"Error output: {step_result.output.stderr}")
```

## Summary

You've learned how to:

- [x] Install the ARL SDK
- [x] Execute simple commands
- [x] Write and execute code files
- [x] Use environment variables
- [x] Maintain persistent sandboxes
- [x] Handle errors

## Next Steps

- [Python SDK Guide](python-sdk.md) - Detailed SDK documentation
- [API Reference](api-reference.md) - Complete API documentation
- [Examples](examples.md) - More advanced examples
