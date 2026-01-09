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

"""Basic task execution example.

Demonstrates:
- Creating a sandbox from a warm pool
- Executing multiple serial steps
- Getting task results
"""

from arl import SandboxSession, TaskStep


def main() -> None:
    """Execute basic serial steps."""
    print("=" * 60)
    print("Example: Basic Serial Execution")
    print("=" * 60)

    # Using context manager (automatically cleans up)
    with SandboxSession(pool_ref="python-39-std", namespace="default") as session:
        # Define task steps with proper type annotations
        steps: list[TaskStep] = [
            {
                "name": "step1_create_file",
                "type": "FilePatch",
                "path": "/workspace/config.txt",
                "content": "API_KEY=test123\nDEBUG=true",
            },
            {
                "name": "step2_read_file",
                "type": "Command",
                "command": ["cat", "/workspace/config.txt"],
            },
            {
                "name": "step3_count_lines",
                "type": "Command",
                "command": ["sh", "-c", "cat /workspace/config.txt | wc -l"],
            },
        ]

        # Execute serial steps
        result = session.execute(steps)

        # Check results
        status = result.get("status", {})
        print(f"\n✓ Task State: {status.get('state')}")
        print(f"✓ Exit Code: {status.get('exitCode')}")
        print(f"✓ Output:\n{status.get('stdout')}")

        if status.get("stderr"):
            print(f"✓ Errors: {status.get('stderr')}")


if __name__ == "__main__":
    main()
