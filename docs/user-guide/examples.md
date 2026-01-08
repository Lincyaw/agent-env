# Examples

This page contains practical examples for common use cases with the ARL Python SDK.

## Basic Examples

### Execute a Simple Command

```python
from arl import SandboxSession

with SandboxSession(pool_ref="python-pool", namespace="default") as session:
    result = session.execute([
        {
            "name": "hello",
            "type": "Command",
            "command": ["echo", "Hello, World!"],
        }
    ])
    print(result["status"]["stdout"])
```

### Write and Run a Python Script

```python
from arl import SandboxSession

with SandboxSession(pool_ref="python-pool", namespace="default") as session:
    result = session.execute([
        {
            "name": "write_script",
            "type": "FilePatch",
            "path": "/workspace/script.py",
            "content": """
def greet(name):
    return f"Hello, {name}!"

print(greet("ARL"))
""",
        },
        {
            "name": "run_script",
            "type": "Command",
            "command": ["python", "/workspace/script.py"],
        },
    ])
    print(result["status"]["stdout"])  # Hello, ARL!
```

## Data Processing

### Process CSV Data

```python
from arl import SandboxSession

with SandboxSession(pool_ref="python-pool", namespace="default") as session:
    result = session.execute([
        # Install pandas
        {
            "name": "install",
            "type": "Command",
            "command": ["pip", "install", "pandas", "-q"],
        },
        # Create sample data
        {
            "name": "create_data",
            "type": "FilePatch",
            "path": "/workspace/data.csv",
            "content": """name,age,city
Alice,30,New York
Bob,25,San Francisco
Charlie,35,Chicago
Diana,28,Boston
""",
        },
        # Process data
        {
            "name": "process",
            "type": "FilePatch",
            "path": "/workspace/process.py",
            "content": """
import pandas as pd

df = pd.read_csv('/workspace/data.csv')
print(f"Total records: {len(df)}")
print(f"Average age: {df['age'].mean():.1f}")
print(f"Cities: {', '.join(df['city'].unique())}")
""",
        },
        {
            "name": "run",
            "type": "Command",
            "command": ["python", "/workspace/process.py"],
        },
    ])
    print(result["status"]["stdout"])
```

### JSON Processing

```python
from arl import SandboxSession
import json

data = {
    "users": [
        {"id": 1, "name": "Alice", "active": True},
        {"id": 2, "name": "Bob", "active": False},
        {"id": 3, "name": "Charlie", "active": True},
    ]
}

with SandboxSession(pool_ref="python-pool", namespace="default") as session:
    result = session.execute([
        {
            "name": "write_data",
            "type": "FilePatch",
            "path": "/workspace/users.json",
            "content": json.dumps(data, indent=2),
        },
        {
            "name": "process",
            "type": "FilePatch",
            "path": "/workspace/filter.py",
            "content": """
import json

with open('/workspace/users.json') as f:
    data = json.load(f)

active_users = [u for u in data['users'] if u['active']]
print(f"Active users: {[u['name'] for u in active_users]}")
""",
        },
        {
            "name": "run",
            "type": "Command",
            "command": ["python", "/workspace/filter.py"],
        },
    ])
    print(result["status"]["stdout"])  # Active users: ['Alice', 'Charlie']
```

## Machine Learning

### Train a Simple Model

```python
from arl import SandboxSession

with SandboxSession(pool_ref="python-pool", namespace="default") as session:
    result = session.execute([
        # Install dependencies
        {
            "name": "install",
            "type": "Command",
            "command": ["pip", "install", "scikit-learn", "numpy", "-q"],
        },
        # Create training script
        {
            "name": "write_script",
            "type": "FilePatch",
            "path": "/workspace/train.py",
            "content": """
from sklearn.datasets import load_iris
from sklearn.model_selection import train_test_split
from sklearn.ensemble import RandomForestClassifier
from sklearn.metrics import accuracy_score

# Load data
iris = load_iris()
X_train, X_test, y_train, y_test = train_test_split(
    iris.data, iris.target, test_size=0.2, random_state=42
)

# Train model
model = RandomForestClassifier(n_estimators=10, random_state=42)
model.fit(X_train, y_train)

# Evaluate
predictions = model.predict(X_test)
accuracy = accuracy_score(y_test, predictions)
print(f"Model accuracy: {accuracy:.2%}")
""",
        },
        {
            "name": "train",
            "type": "Command",
            "command": ["python", "/workspace/train.py"],
        },
    ], timeout="120s")  # ML tasks may need more time
    print(result["status"]["stdout"])
```

## Stateful Operations

### Maintain State Across Tasks

```python
from arl import SandboxSession

session = SandboxSession(
    pool_ref="python-pool",
    namespace="default",
    keep_alive=True  # Important: keep sandbox for state
)

try:
    session.create_sandbox()
    
    # Initialize state
    session.execute([
        {
            "name": "init",
            "type": "FilePatch",
            "path": "/workspace/state.json",
            "content": '{"counter": 0, "history": []}',
        },
    ])
    
    # Increment counter multiple times
    for i in range(3):
        result = session.execute([
            {
                "name": "update",
                "type": "FilePatch",
                "path": "/workspace/update.py",
                "content": """
import json

with open('/workspace/state.json') as f:
    state = json.load(f)

state['counter'] += 1
state['history'].append(f"Action {state['counter']}")

with open('/workspace/state.json', 'w') as f:
    json.dump(state, f, indent=2)

print(f"Counter: {state['counter']}")
print(f"History: {state['history']}")
""",
            },
            {
                "name": "run",
                "type": "Command",
                "command": ["python", "/workspace/update.py"],
            },
        ])
        print(f"Iteration {i+1}:")
        print(result["status"]["stdout"])
        print()
        
finally:
    session.delete_sandbox()
```

## Error Handling

### Graceful Error Recovery

```python
from arl import SandboxSession
from kubernetes import client

def execute_with_retry(session, steps, max_retries=3):
    """Execute steps with retry logic."""
    for attempt in range(max_retries):
        try:
            result = session.execute(steps)
            if result["status"]["state"] == "Succeeded":
                return result
            else:
                print(f"Attempt {attempt + 1} failed: {result['status'].get('stderr', '')}")
        except client.ApiException as e:
            print(f"API error on attempt {attempt + 1}: {e.reason}")
        except TimeoutError:
            print(f"Timeout on attempt {attempt + 1}")
    
    raise RuntimeError(f"Failed after {max_retries} attempts")

with SandboxSession(pool_ref="python-pool", namespace="default") as session:
    result = execute_with_retry(session, [
        {"name": "run", "type": "Command", "command": ["python", "-c", "print('OK')"]}
    ])
    print(result["status"]["stdout"])
```

### Comprehensive Error Handling

```python
from arl import SandboxSession
from kubernetes import client

with SandboxSession(pool_ref="python-pool", namespace="default") as session:
    result = session.execute([
        {
            "name": "risky_operation",
            "type": "Command",
            "command": ["python", "-c", """
import sys
import random

# Simulate occasional failures
if random.random() < 0.5:
    print("Operation succeeded!", file=sys.stdout)
else:
    print("Something went wrong!", file=sys.stderr)
    sys.exit(1)
"""],
        },
    ])
    
    status = result["status"]
    
    if status["state"] == "Succeeded":
        print(f"✓ Success: {status['stdout'].strip()}")
    else:
        print(f"✗ Failed (exit code: {status.get('exitCode', 'unknown')})")
        print(f"  Error: {status.get('stderr', 'no error message').strip()}")
```

## Using Callbacks

### Progress Monitoring

```python
from arl import SandboxSession
import datetime

def log_completion(result):
    timestamp = datetime.datetime.now().strftime("%H:%M:%S")
    state = result["status"]["state"]
    print(f"[{timestamp}] Task completed: {state}")

def log_success(result):
    stdout = result["status"].get("stdout", "").strip()
    if stdout:
        print(f"  Output: {stdout[:100]}...")

def log_failure(result):
    stderr = result["status"].get("stderr", "").strip()
    exit_code = result["status"].get("exitCode", "?")
    print(f"  Error (exit {exit_code}): {stderr[:100]}...")

session = SandboxSession(pool_ref="python-pool", namespace="default")
session.register_callback("on_task_complete", log_completion)
session.register_callback("on_task_success", log_success)
session.register_callback("on_task_failure", log_failure)

with session:
    # Run multiple tasks
    for i in range(3):
        print(f"\n--- Task {i+1} ---")
        session.execute([
            {"name": f"task_{i}", "type": "Command", "command": ["echo", f"Task {i+1} output"]}
        ])
```

## WarmPool Management

### Create and Manage WarmPool

```python
from arl import WarmPoolManager, SandboxSession
from kubernetes import client

manager = WarmPoolManager(namespace="default")

# Check if pool exists, create if not
pool_name = "my-python-pool"
try:
    manager.get_warmpool(pool_name)
    print(f"WarmPool '{pool_name}' already exists")
except client.ApiException as e:
    if e.status == 404:
        print(f"Creating WarmPool '{pool_name}'...")
        manager.create_warmpool(
            name=pool_name,
            image="python:3.9-slim",
            sidecar_image="arl-sidecar:latest",
            replicas=2,
        )
        manager.wait_for_warmpool_ready(pool_name, timeout=120)
        print(f"WarmPool '{pool_name}' is ready")
    else:
        raise

# Use the pool
with SandboxSession(pool_ref=pool_name, namespace="default") as session:
    result = session.execute([
        {"name": "test", "type": "Command", "command": ["python", "--version"]}
    ])
    print(f"Python version: {result['status']['stdout'].strip()}")

# Optionally delete pool when done
# manager.delete_warmpool(pool_name)
```

## Advanced Patterns

### Pipeline Pattern

```python
from arl import SandboxSession

def run_pipeline(session, stages):
    """Execute a pipeline of stages, stopping on first failure."""
    results = []
    
    for i, stage in enumerate(stages):
        print(f"Running stage {i+1}: {stage['name']}")
        result = session.execute(stage["steps"])
        results.append(result)
        
        if result["status"]["state"] != "Succeeded":
            print(f"Pipeline failed at stage {i+1}")
            return results, False
    
    return results, True

with SandboxSession(pool_ref="python-pool", namespace="default") as session:
    pipeline = [
        {
            "name": "Setup",
            "steps": [
                {"name": "mkdir", "type": "Command", "command": ["mkdir", "-p", "/workspace/output"]},
            ]
        },
        {
            "name": "Generate",
            "steps": [
                {"name": "write", "type": "FilePatch", "path": "/workspace/data.txt", "content": "line1\nline2\nline3"},
            ]
        },
        {
            "name": "Process",
            "steps": [
                {"name": "count", "type": "Command", "command": ["wc", "-l", "/workspace/data.txt"]},
            ]
        },
    ]
    
    results, success = run_pipeline(session, pipeline)
    print(f"\nPipeline {'succeeded' if success else 'failed'}")
    if success:
        print(f"Final output: {results[-1]['status']['stdout'].strip()}")
```

## More Examples

For more examples, see the [examples/python](https://github.com/Lincyaw/agent-env/tree/main/examples/python) directory in the repository:

| Example | Description |
|---------|-------------|
| `01_basic_execution.py` | Basic command execution |
| `02_multi_step_pipeline.py` | Multi-step data processing |
| `03_environment_variables.py` | Using environment variables |
| `04_working_directory.py` | Working directory management |
| `05_error_handling.py` | Error handling patterns |
| `06_long_running_task.py` | Long-running tasks with timeouts |
| `07_sandbox_reuse.py` | Reusing sandboxes |
| `08_callback_hooks.py` | Using callback hooks |
| `09_multiple_feature.py` | Comprehensive feature demo |
