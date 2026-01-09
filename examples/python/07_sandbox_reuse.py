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

"""Sandbox reuse example (Host-like behavior).

Demonstrates:
- Keeping sandbox alive between tasks
- Serial task execution in same environment
- State persistence across tasks
- Manual sandbox lifecycle management
"""

from arl import SandboxSession


def main() -> None:
    print("=" * 60)
    print("Example: Sandbox Reuse (Host-like)")
    print("=" * 60)

    # Create session with keep_alive=True
    session = SandboxSession(pool_ref="python-39-std", namespace="default", keep_alive=True)

    try:
        # Manually create sandbox
        session.create_sandbox()
        print("\n✓ Sandbox created\n")

        # Task 1: Initialize state
        print("Task 1: Creating state file...")
        result1 = session.execute(
            [
                {
                    "name": "init_state",
                    "type": "FilePatch",
                    "path": "/workspace/state.txt",
                    "content": "initialized",
                },
                {
                    "name": "create_counter",
                    "type": "FilePatch",
                    "path": "/workspace/counter.txt",
                    "content": "0",
                },
            ]
        )
        print(f"  State: {result1.get('status', {}).get('state')}\n")

        # Task 2: Update state
        print("Task 2: Updating counter...")
        result2 = session.execute(
            [
                {
                    "name": "increment",
                    "type": "Command",
                    "command": ["sh", "-c", "echo '1' > /workspace/counter.txt"],
                },
                {
                    "name": "read_state",
                    "type": "Command",
                    "command": [
                        "cat",
                        "/workspace/state.txt",
                        "/workspace/counter.txt",
                    ],
                },
            ]
        )
        status2 = result2.get("status", {})
        print(f"  State: {status2.get('state')}")
        print(f"  Output: {status2.get('stdout')}\n")

        # Task 3: Verify persistence
        print("Task 3: Verifying state persistence...")
        result3 = session.execute(
            [
                {
                    "name": "verify",
                    "type": "Command",
                    "command": [
                        "sh",
                        "-c",
                        (
                            "echo 'State:' && cat /workspace/state.txt && "
                            "echo 'Counter:' && cat /workspace/counter.txt"
                        ),
                    ],
                }
            ]
        )
        status3 = result3.get("status", {})
        print(f"  State: {status3.get('state')}")
        print(f"  Output:\n{status3.get('stdout')}\n")

        # Task 4: Process data
        print("Task 4: Processing data...")
        result4 = session.execute(
            [
                {
                    "name": "process",
                    "type": "Command",
                    "command": [
                        "sh",
                        "-c",
                        "echo 'Processing complete!' >> /workspace/state.txt",
                    ],
                },
                {
                    "name": "final_state",
                    "type": "Command",
                    "command": ["cat", "/workspace/state.txt"],
                },
            ]
        )
        status4 = result4.get("status", {})
        print(f"  State: {status4.get('state')}")
        print(f"  Final state: {status4.get('stdout')}\n")

        print("✓ All tasks completed successfully!")
        print("✓ State persisted across 4 separate task executions")

    finally:
        # Clean up
        session.delete_sandbox()
        print("\n✓ Sandbox deleted")


if __name__ == "__main__":
    main()
