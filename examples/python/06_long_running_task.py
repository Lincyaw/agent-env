"""Long-running task example.

Demonstrates:
- Running tasks with longer execution time
- Setting timeouts
- Tracking execution duration
- Progress output
"""

from arl_client.session import SandboxSession


def main():
    """Execute a long-running task."""
    print("=" * 60)
    print("Example: Long-Running Task")
    print("=" * 60)

    with SandboxSession(pool_ref="python-39-std", namespace="default") as session:
        result = session.execute(
            [
                # Create a script that takes time to execute
                {
                    "name": "create_task",
                    "type": "FilePatch",
                    "path": "/workspace/long_task.py",
                    "content": """#!/usr/bin/env python3
import time
import sys

print("Starting long-running task...")
print(f"Python version: {sys.version}")

for i in range(5):
    print(f"Step {i+1}/5: Processing...")
    time.sleep(1)

print("\\nTask completed successfully!")
print("Total execution time: ~5 seconds")
""",
                },
                # Execute the task with timeout
                {
                    "name": "run_task",
                    "type": "Command",
                    "command": ["python3", "/workspace/long_task.py"],
                },
            ],
            timeout="60s",  # Allow up to 60 seconds
        )

        status = result.get("status", {})
        print(f"\n✓ Task State: {status.get('state')}")
        print(f"✓ Duration: {status.get('duration')}")
        print(f"✓ Output:\n{status.get('stdout')}")


if __name__ == "__main__":
    main()
