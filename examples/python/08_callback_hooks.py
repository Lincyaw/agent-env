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

"""Callback hooks example.

Demonstrates:
- Registering callback functions for task events
- Using callbacks for logging and monitoring
- Chaining tasks with callbacks
- Execute with automatic callback script execution
"""

import json
import os

from arl import SandboxSession

# Enable debug mode to see full task results
DEBUG = os.getenv("DEBUG", "false").lower() == "true"


def main() -> None:
    print("=" * 60)
    print("Example: Callback Hooks")
    print("=" * 60)

    # Define callback functions
    def on_complete(result: dict) -> None:
        """Triggered when any task completes."""
        status = result.get("status", {})
        task_name = result.get("metadata", {}).get("name", "unknown")
        state = status.get("state", "unknown")
        print(f"\n[CALLBACK] Task '{task_name}' completed with state: {state}")

        if DEBUG:
            print(f"[DEBUG] Full result: {json.dumps(result, indent=2, default=str)}")

    def on_success(result: dict) -> None:
        """Triggered only when task succeeds."""
        status = result.get("status", {})
        spec = result.get("spec", {})
        steps = spec.get("steps", [])
        print(f"[CALLBACK] Task succeeded! Executed {len(steps)} steps")
        stdout = status.get("stdout", "").strip()
        if stdout:
            print(f"  Output: {stdout}")

    def on_failure(result: dict) -> None:
        """Triggered only when task fails."""
        status = result.get("status", {})
        print("[CALLBACK] Task failed!")
        exit_code = status.get("exitCode", 0)
        stderr = status.get("stderr", "").strip()
        conditions = status.get("conditions", [])

        print(f"  Exit code: {exit_code}")
        if stderr:
            print(f"  Error output: {stderr}")
        if conditions:
            for cond in conditions:
                if cond.get("status") == "False":
                    print(f"  Reason: {cond.get('message', 'Unknown')}")

    def log_execution_time(result: dict) -> None:
        """Log task execution time."""
        status = result.get("status", {})
        start = status.get("startTime")
        end = status.get("completionTime")
        if start and end:
            # Simple time calculation (in real scenario, parse ISO format)
            print(f"[CALLBACK] Task completed (start: {start}, end: {end})")

    # Create session and register callbacks
    # Note: keep_alive=True ensures sandbox persists between multiple tasks
    print("\n1. Registering callbacks...")
    session = SandboxSession(
        pool_ref="python-39-std",
        namespace="default",
        keep_alive=True  # Keep sandbox alive for multiple tasks
    )
    session.register_callback("on_task_complete", on_complete)
    session.register_callback("on_task_success", on_success)
    session.register_callback("on_task_failure", on_failure)
    session.register_callback("on_task_complete", log_execution_time)
    print("   ✓ Registered 4 callbacks")

    try:
        with session:
            # Test 1: Successful task
            print("\n2. Executing successful task...")
            result1 = session.execute(
                [
                    {
                        "name": "create_file",
                        "type": "FilePatch",
                        "path": "/workspace/data.txt",
                        "content": "Hello, callbacks!",
                    },
                    {
                        "name": "read_file",
                        "type": "Command",
                        "command": ["cat", "/workspace/data.txt"],
                    },
                ]
            )
            status1 = result1.get("status", {})
            stdout = status1.get("stdout", "").strip()
            if stdout:
                print(f"   Task output: {stdout}")
            if status1.get("state") == "Failed":
                print(f"   ⚠️  Task failed: {status1.get('stderr', 'Unknown error')}")

            # Test 2: Task with multiple steps
            print("\n3. Executing multi-step pipeline...")
            result2 = session.execute(
                [
                    {
                        "name": "create_script",
                        "type": "FilePatch",
                        "path": "/workspace/process.sh",
                        "content": "#!/bin/bash\necho 'Processing...'\nsleep 1\necho 'Done!'",
                    },
                    {
                        "name": "make_executable",
                        "type": "Command",
                        "command": ["chmod", "+x", "/workspace/process.sh"],
                    },
                    {
                        "name": "run_script",
                        "type": "Command",
                        "command": ["/bin/bash", "/workspace/process.sh"],
                    },
                ]
            )
            status2 = result2.get("status", {})
            if status2.get("state") == "Succeeded":
                stdout = status2.get("stdout", "").strip()
                if stdout:
                    print(f"   Script output: {stdout}")
            else:
                print(f"   ⚠️  Task failed: {status2.get('stderr', 'Unknown error')}")

            # Test 3: Execute with callback script
            print("\n4. Using execute_with_callback...")
            result3 = session.execute_with_callback(
                steps=[
                    {
                        "name": "create_test_script",
                        "type": "FilePatch",
                        "path": "/workspace/test.sh",
                        "content": "#!/bin/bash\necho 'Running tests...'\nexit 0",
                    },
                    {
                        "name": "setup",
                        "type": "Command",
                        "command": ["chmod", "+x", "/workspace/test.sh"],
                    },
                ],
                callback_script="/workspace/test.sh",
            )

            status3 = result3.get("status", {})
            if status3.get("state") == "Succeeded":
                # Check callback result
                if "callback_result" in result3:
                    callback_status = result3["callback_result"].get("status", {})
                    print(f"   Callback script state: {callback_status.get('state')}")
                    callback_output = callback_status.get("stdout", "").strip()
                    if callback_output:
                        print(f"   Callback output: {callback_output}")
            else:
                print(f"   ⚠️  Main task failed: {status3.get('stderr', 'Unknown error')}")

            # Test 4: Intentional failure (commented out by default)
            # print("\n5. Testing failure callback...")
            # result4 = session.execute([
            #     {
            #         "name": "failing_command",
            #         "type": "Command",
            #         "command": ["false"],
            #     }
            # ])

        print("\n" + "=" * 60)
        print("✓ All callback examples completed!")
        print("=" * 60)

    except Exception as e:
        print(f"\nError: {e}")


if __name__ == "__main__":
    main()
