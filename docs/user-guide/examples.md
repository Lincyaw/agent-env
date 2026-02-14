# Examples

This page contains practical examples for common use cases with the ARL Python SDK.

## Basic Examples

### Execute a Simple Command

```python
from arl import SandboxSession

with SandboxSession(
    pool_ref="python-pool",
    namespace="default",
    gateway_url="http://localhost:8080",
) as session:
    result = session.execute([
        {"name": "hello", "command": ["echo", "Hello, World!"]},
    ])
    print(result.results[0].output.stdout)
```

### Write and Run a Python Script

```python
from arl import SandboxSession

with SandboxSession(
    pool_ref="python-pool",
    namespace="default",
    gateway_url="http://localhost:8080",
) as session:
    result = session.execute([
        {
            "name": "write_and_run",
            "command": ["bash", "-c", """
cat > /workspace/script.py << 'PYEOF'
def greet(name):
    return f"Hello, {name}!"

print(greet("ARL"))
PYEOF
python /workspace/script.py
"""],
        },
    ])
    print(result.results[0].output.stdout)  # Hello, ARL!
```

## Data Processing

### Process CSV Data

```python
from arl import SandboxSession

with SandboxSession(
    pool_ref="python-pool",
    namespace="default",
    gateway_url="http://localhost:8080",
) as session:
    result = session.execute([
        # Install pandas
        {"name": "install", "command": ["pip", "install", "pandas", "-q"]},
        # Create data and process it
        {
            "name": "process",
            "command": ["bash", "-c", """
cat > /workspace/data.csv << 'EOF'
name,age,city
Alice,30,New York
Bob,25,San Francisco
Charlie,35,Chicago
Diana,28,Boston
EOF

cat > /workspace/process.py << 'EOF'
import pandas as pd

df = pd.read_csv('/workspace/data.csv')
print(f"Total records: {len(df)}")
print(f"Average age: {df['age'].mean():.1f}")
print(f"Cities: {', '.join(df['city'].unique())}")
EOF

python /workspace/process.py
"""],
        },
    ])
    print(result.results[-1].output.stdout)
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

with SandboxSession(
    pool_ref="python-pool",
    namespace="default",
    gateway_url="http://localhost:8080",
) as session:
    result = session.execute([
        {
            "name": "process_json",
            "command": ["bash", "-c", f"""
cat > /workspace/users.json << 'EOF'
{json.dumps(data, indent=2)}
EOF

cat > /workspace/filter.py << 'EOF'
import json

with open('/workspace/users.json') as f:
    data = json.load(f)

active_users = [u for u in data['users'] if u['active']]
print(f"Active users: {{[u['name'] for u in active_users]}}")
EOF

python /workspace/filter.py
"""],
        },
    ])
    print(result.results[0].output.stdout)  # Active users: ['Alice', 'Charlie']
```

## Machine Learning

### Train a Simple Model

```python
from arl import SandboxSession

with SandboxSession(
    pool_ref="python-pool",
    namespace="default",
    gateway_url="http://localhost:8080",
    timeout="120s",
) as session:
    result = session.execute([
        # Install dependencies
        {"name": "install", "command": ["pip", "install", "scikit-learn", "numpy", "-q"]},
        # Create and run training script
        {
            "name": "train",
            "command": ["bash", "-c", """
cat > /workspace/train.py << 'EOF'
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
EOF

python /workspace/train.py
"""],
        },
    ])
    print(result.results[-1].output.stdout)
```

## Stateful Operations

### Maintain State Across Executions

```python
from arl import SandboxSession

session = SandboxSession(
    pool_ref="python-pool",
    namespace="default",
    gateway_url="http://localhost:8080",
    keep_alive=True,  # Important: keep sandbox for state
)

try:
    session.create_sandbox()

    # Initialize state
    session.execute([
        {
            "name": "init",
            "command": ["bash", "-c", """echo '{"counter": 0, "history": []}' > /workspace/state.json"""],
        },
    ])

    # Increment counter multiple times
    for i in range(3):
        result = session.execute([
            {
                "name": "update",
                "command": ["python", "-c", """
import json

with open('/workspace/state.json') as f:
    state = json.load(f)

state['counter'] += 1
state['history'].append(f"Action {state['counter']}")

with open('/workspace/state.json', 'w') as f:
    json.dump(state, f, indent=2)

print(f"Counter: {state['counter']}")
print(f"History: {state['history']}")
"""],
            },
        ])
        print(f"Iteration {i+1}:")
        print(result.results[0].output.stdout)
        print()

finally:
    session.delete_sandbox()
```

## Error Handling

### Graceful Error Recovery

```python
from arl import SandboxSession

def execute_with_retry(session, steps, max_retries=3):
    """Execute steps with retry logic."""
    for attempt in range(max_retries):
        try:
            result = session.execute(steps)
            if result.results[-1].output.exit_code == 0:
                return result
            else:
                print(f"Attempt {attempt + 1} failed: {result.results[-1].output.stderr}")
        except ConnectionError:
            print(f"Connection error on attempt {attempt + 1}")
        except TimeoutError:
            print(f"Timeout on attempt {attempt + 1}")

    raise RuntimeError(f"Failed after {max_retries} attempts")

with SandboxSession(
    pool_ref="python-pool",
    namespace="default",
    gateway_url="http://localhost:8080",
) as session:
    result = execute_with_retry(session, [
        {"name": "run", "command": ["python", "-c", "print('OK')"]},
    ])
    print(result.results[0].output.stdout)
```

### Comprehensive Error Handling

```python
from arl import SandboxSession

with SandboxSession(
    pool_ref="python-pool",
    namespace="default",
    gateway_url="http://localhost:8080",
) as session:
    result = session.execute([
        {
            "name": "risky_operation",
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

    step_result = result.results[0]

    if step_result.output.exit_code == 0:
        print(f"Success: {step_result.output.stdout.strip()}")
    else:
        print(f"Failed (exit code: {step_result.output.exit_code})")
        print(f"  Error: {step_result.output.stderr.strip()}")
```

## WarmPool Management

### Create and Manage WarmPool

```python
from arl import WarmPoolManager, SandboxSession

manager = WarmPoolManager(namespace="default")

# Check if pool exists, create if not
pool_name = "my-python-pool"
try:
    manager.get_warmpool(pool_name)
    print(f"WarmPool '{pool_name}' already exists")
except Exception:
    print(f"Creating WarmPool '{pool_name}'...")
    manager.create_warmpool(
        name=pool_name,
        image="python:3.9-slim",
        sidecar_image="arl-sidecar:latest",
        replicas=2,
    )
    manager.wait_for_warmpool_ready(pool_name, timeout=120)
    print(f"WarmPool '{pool_name}' is ready")

# Use the pool
with SandboxSession(
    pool_ref=pool_name,
    namespace="default",
    gateway_url="http://localhost:8080",
) as session:
    result = session.execute([
        {"name": "test", "command": ["python", "--version"]},
    ])
    print(f"Python version: {result.results[0].output.stdout.strip()}")

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

        if result.results[-1].output.exit_code != 0:
            print(f"Pipeline failed at stage {i+1}")
            return results, False

    return results, True

with SandboxSession(
    pool_ref="python-pool",
    namespace="default",
    gateway_url="http://localhost:8080",
) as session:
    pipeline = [
        {
            "name": "Setup",
            "steps": [
                {"name": "mkdir", "command": ["mkdir", "-p", "/workspace/output"]},
            ]
        },
        {
            "name": "Generate",
            "steps": [
                {"name": "write", "command": ["bash", "-c", "echo -e 'line1\nline2\nline3' > /workspace/data.txt"]},
            ]
        },
        {
            "name": "Process",
            "steps": [
                {"name": "count", "command": ["wc", "-l", "/workspace/data.txt"]},
            ]
        },
    ]

    results, success = run_pipeline(session, pipeline)
    print(f"\nPipeline {'succeeded' if success else 'failed'}")
    if success:
        print(f"Final output: {results[-1].results[0].output.stdout.strip()}")
```

## More Examples

For more examples, see the [examples/python](https://github.com/Lincyaw/agent-env/tree/main/examples/python) directory in the repository:

| Example | Description |
|---------|-------------|
| `test_arl_sdk.py` | Comprehensive SDK test |
| `bench_gateway.py` | Gateway benchmarking |
| `test_interactive_shell.py` | Interactive shell test |
