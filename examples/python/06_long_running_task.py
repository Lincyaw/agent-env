# Copyright 2024 ARL-Infra Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Long-running task example.

Demonstrates:
- Running tasks with longer execution time
- Setting timeouts
- Tracking execution duration
- Progress output
"""

from arl import SandboxSession


def main() -> None:
    """Execute a long-running task."""
    print("=" * 60)
    print("Example: Long-Running Task")
    print("=" * 60)

    with SandboxSession(pool_ref="python-39-std", namespace="default", timeout=60) as session:
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
        )

        status = result.get("status", {})
        print(f"\n✓ Task State: {status.get('state')}")
        print(f"✓ Duration: {status.get('duration')}")
        print(f"✓ Output:\n{status.get('stdout')}")


if __name__ == "__main__":
    main()
