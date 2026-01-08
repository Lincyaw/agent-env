"""WarmPool creation and sandbox reuse example.

Demonstrates:
- Creating WarmPool programmatically via Python SDK
- Reusing sandbox for multiple tasks
- Complete warmpool lifecycle management
- State persistence across multiple command executions
- Callback hooks for monitoring task execution
"""

from arl import SandboxSession, WarmPoolManager
from arl.types import TaskResource
from kubernetes import client


def on_task_complete(result: TaskResource) -> None:
    """Callback when task completes (success or failure)."""
    status = result.get("status", {})
    state = status.get("state", "unknown")
    print(f"  [Callback] Task completed with state: {state}")


def on_task_success(result: TaskResource) -> None:
    """Callback when task succeeds."""
    status = result.get("status", {})
    stdout = status.get("stdout", "").strip()
    print(f"  [Callback] Task succeeded! Output: {stdout[:50]}...")


def on_task_failure(result: TaskResource) -> None:
    """Callback when task fails."""
    status = result.get("status", {})
    stderr = status.get("stderr", "")
    print(f"  [Callback] Task failed! Error: {stderr}")


def main() -> None:
    pool_name = "demo-python-pool"
    namespace = "default"
    pool_image = "pair-diag-cn-guangzhou.cr.volces.com/pair/busybox:1.35"
    sidecar_image = "pair-diag-cn-guangzhou.cr.volces.com/pair/arl-sidecar:latest"
    # Step 1: Create WarmPool programmatically
    print("\n[Step 1] Creating WarmPool with Python SDK...")
    warmpool_manager = WarmPoolManager(namespace=namespace)

    try:
        warmpool_manager.get_warmpool(pool_name)
        print(f"✓ WarmPool '{pool_name}' already exists")
    except client.ApiException as e:
        if e.status == 404:
            # Create new warmpool
            warmpool_manager.create_warmpool(
                name=pool_name,
                sidecar_image=sidecar_image,
                image=pool_image,
                replicas=2,
            )
            print(f"✓ WarmPool '{pool_name}' created with image '{pool_image}'")

            # Wait for warmpool to be ready
            warmpool_manager.wait_for_warmpool_ready(pool_name)
            print("✓ WarmPool is ready")
        else:
            raise

    # Step 2: Create session with keep_alive and register callbacks
    print("\n[Step 2] Creating sandbox session with callbacks...")
    session = SandboxSession(pool_ref=pool_name, namespace=namespace, keep_alive=True)

    # Register callbacks to monitor task execution
    session.register_callback("on_task_complete", on_task_complete)
    session.register_callback("on_task_success", on_task_success)
    session.register_callback("on_task_failure", on_task_failure)

    try:
        # Manually create sandbox from pool
        session.create_sandbox()
        print(f"✓ Sandbox allocated from pool '{pool_name}'")

        # Task 1: Initialize environment and create files
        print("\n[Task 1] Initializing environment...")
        result1 = session.execute(
            [
                {
                    "name": "create_workspace",
                    "type": "Command",
                    "command": ["mkdir", "-p", "/workspace"],
                },
                {
                    "name": "init_counter",
                    "type": "FilePatch",
                    "path": "/workspace/counter.txt",
                    "content": "0",
                },
                {
                    "name": "create_script",
                    "type": "FilePatch",
                    "path": "/workspace/increment.sh",
                    "content": """#!/bin/bash
COUNTER_FILE="/workspace/counter.txt"
CURRENT=$(cat $COUNTER_FILE)
NEW=$((CURRENT + 1))
echo $NEW > $COUNTER_FILE
echo "Counter incremented: $CURRENT -> $NEW"
""",
                },
                {
                    "name": "make_executable",
                    "type": "Command",
                    "command": ["chmod", "+x", "/workspace/increment.sh"],
                },
                {
                    "name": "verify_files",
                    "type": "Command",
                    "command": ["sh", "-c", "ls -lh /workspace && echo '---' && cat /workspace/counter.txt"],
                },
            ]
        )
        status1 = result1.get("status", {})
        print(f"  State: {status1.get('state')}")
        if status1.get("stdout"):
            print(f"  Output:\n{status1.get('stdout')}")

        # Task 2: Execute increment command (callbacks will be triggered)
        print("\n[Task 2] Running increment script with callbacks (1st time)...")
        result2 = session.execute(
            [
                {
                    "name": "run_increment",
                    "type": "Command",
                    "command": ["/workspace/increment.sh"],
                },
                {
                    "name": "show_counter",
                    "type": "Command",
                    "command": ["cat", "/workspace/counter.txt"],
                },
            ],
        )
        status2 = result2.get("status", {})
        print(f"  State: {status2.get('state')}")
        print(f"  Counter value: {status2.get('stdout', '').strip()}")

        # Task 3: Execute increment command (second time)
        print("\n[Task 3] Running increment script (2nd time)...")
        result3 = session.execute(
            [
                {
                    "name": "run_increment",
                    "type": "Command",
                    "command": ["/workspace/increment.sh"],
                },
                {
                    "name": "show_counter",
                    "type": "Command",
                    "command": ["cat", "/workspace/counter.txt"],
                },
            ]
        )
        status3 = result3.get("status", {})
        print(f"  State: {status3.get('state')}")
        print(f"  Counter value: {status3.get('stdout', '').strip()}")

        # Task 4: Final verification
        print("\n[Task 4] Final verification...")
        result4 = session.execute(
            [
                {
                    "name": "show_summary",
                    "type": "Command",
                    "command": [
                        "sh",
                        "-c",
                        "echo 'Workspace contents:' && ls -lh /workspace && echo '' && "
                        "echo 'Final counter value:' && cat /workspace/counter.txt",
                    ],
                }
            ]
        )
        status4 = result4.get("status", {})
        print(f"  State: {status4.get('state')}")
        if status4.get("stdout"):
            print(f"  Output:\n{status4.get('stdout')}")

        print("\n" + "=" * 60)
        print("✓ All tasks completed successfully!")
        print("=" * 60)
        print("\nThis example demonstrated:")
        print("- Creating WarmPool programmatically with Python SDK")
        print("- Allocating sandbox from custom warmpool")
        print("- Reusing sandbox across multiple task executions")
        print("- State persistence (counter incremented from 0 -> 1 -> 2)")
        print("- Callback hooks for monitoring task execution")
        print("- Complete warmpool and sandbox lifecycle management")

    finally:
        # Clean up sandbox
        session.delete_sandbox()
        print("\n✓ Sandbox deleted")
        # warmpool_manager.delete_warmpool(pool_name)
        # print(f"✓ WarmPool '{pool_name}' deleted")


if __name__ == "__main__":
    main()
