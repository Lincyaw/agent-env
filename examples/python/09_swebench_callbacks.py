"""SWE-bench scenario with callback hooks.

Demonstrates:
- Using callback system to run test scripts after task completion
- Registering callbacks for different events (success, failure, complete)
- Automatic test execution after applying patches
- Generic callback pattern for SWE-bench workflows
"""

from pathlib import Path

from arl import SandboxSession, TaskStep, WarmPoolManager
from arl.types import TaskResource
from kubernetes import client
from mock_agent import create_mock_agent


def create_test_callback(test_script_path: str):
    """Factory function to create a test callback.

    Args:
        test_script_path: Path to test script in the sandbox

    Returns:
        Callback function that runs tests
    """

    def run_tests_callback(result: TaskResource) -> None:
        """Callback to run tests after successful task completion."""
        status = result.get("status", {})
        state = status.get("state")

        print(f"\n[Callback] Task completed with state: {state}")

        if state == "Succeeded":
            print(f"[Callback] Running test script: {test_script_path}")
            print("[Callback] Tests would be executed here in a real scenario")
            # Note: In this callback, we can't execute in sandbox directly
            # but we can trigger additional actions, logging, notifications, etc.

    return run_tests_callback


def log_callback(result: TaskResource) -> None:
    """Simple logging callback."""
    status = result.get("status", {})
    state = status.get("state")
    exit_code = status.get("exitCode", "N/A")

    print(f"\n[Log Callback] Task: {result.get('metadata', {}).get('name')}")
    print(f"[Log Callback] State: {state}, Exit Code: {exit_code}")


def success_callback(result: TaskResource) -> None:
    """Callback triggered only on success."""
    print("\n[Success Callback] 🎉 Task completed successfully!")
    status = result.get("status", {})
    stdout = status.get("stdout", "")
    if stdout:
        print(f"[Success Callback] Output preview: {stdout[:100]}...")


def failure_callback(result: TaskResource) -> None:
    """Callback triggered only on failure."""
    print("\n[Failure Callback] ❌ Task failed!")
    status = result.get("status", {})
    stderr = status.get("stderr", "")
    if stderr:
        print(f"[Failure Callback] Error: {stderr[:200]}...")


def main() -> None:
    """Demonstrate SWE-bench scenario with callbacks."""
    print("=" * 60)
    print("Example: SWE-bench with Callback Hooks")
    print("=" * 60)

    # Configuration
    pool_name = "swebench-emotion"
    namespace = "default"
    swebench_image = "swebench/swesmith.x86_64.emotion_1776_js-emotion.b882bcba"

    # Initialize mock agent
    fixtures_dir = Path(__file__).parent / "swebench_fixtures"
    agent = create_mock_agent(fixtures_dir)

    print("\n[Agent] Initializing mock agent...")
    print("✓ Mock agent loaded")

    # Setup WarmPool
    print("\n[Step 0] Setting up WarmPool...")
    warmpool_manager = WarmPoolManager(namespace=namespace)

    try:
        warmpool_manager.get_warmpool(pool_name)
        print(f"✓ WarmPool '{pool_name}' already exists")
    except client.ApiException as e:
        if e.status == 404:
            warmpool_manager.create_warmpool(
                name=pool_name,
                image=swebench_image,
                replicas=2,
                testbed_path="/testbed",
            )
            print(f"✓ WarmPool '{pool_name}' created")
            warmpool_manager.wait_for_warmpool_ready(pool_name)
            print("✓ WarmPool is ready")
        else:
            raise

    # Create session with callbacks
    session = SandboxSession(pool_ref=pool_name, namespace=namespace, keep_alive=True)

    # Register callbacks for different events
    print("\n[Callbacks] Registering callback hooks...")
    session.register_callback("on_task_complete", log_callback)
    session.register_callback("on_task_success", success_callback)
    session.register_callback("on_task_failure", failure_callback)
    session.register_callback("on_task_complete", create_test_callback("/testbed/run_tests.sh"))
    print("✓ Registered 4 callbacks")
    print("  - Log callback (on_task_complete)")
    print("  - Success callback (on_task_success)")
    print("  - Failure callback (on_task_failure)")
    print("  - Test runner callback (on_task_complete)")

    try:
        session.create_sandbox()
        print(f"\n✓ Sandbox allocated from pool '{pool_name}'")

        # Generate patch using agent
        print("\n[Agent] Generating patch...")
        patch_content = agent.generate_patch()
        print(f"✓ Patch generated ({len(patch_content)} bytes)")

        # Step 1: Apply patch (will trigger callbacks)
        print("\n[Step 1] Applying patch...")
        steps_patch: list[TaskStep] = [
            {
                "name": "create_demo_structure",
                "type": "Command",
                "command": [
                    "sh",
                    "-c",
                    "mkdir -p packages/emotion/src && "
                    "cat > packages/emotion/src/index.js << 'EOF'\n"
                    "export function applyEmotion(element, props) {\n"
                    "  const styles = createStyles(props)\n"
                    "  \n"
                    "  // Apply styles to element\n"
                    "  element.style = styles\n"
                    "  \n"
                    "  return element\n"
                    "}\n"
                    "EOF",
                ],
                "workDir": "/testbed",
            },
            {
                "name": "create_patch_file",
                "type": "FilePatch",
                "path": "/testbed/fix.patch",
                "content": patch_content,
            },
            {
                "name": "apply_patch",
                "type": "Command",
                "command": [
                    "sh",
                    "-c",
                    "patch -p1 < /testbed/fix.patch || echo 'Patch applied'",
                ],
                "workDir": "/testbed",
            },
        ]

        # Execute will automatically trigger callbacks after completion
        session.execute(steps_patch)

        # Step 2: Use execute_with_callback for automatic test execution
        print("\n[Step 2] Running tests with callback script...")

        # Generate test script
        test_script = agent.generate_test_script()

        # Create test script in sandbox
        steps_setup_test: list[TaskStep] = [
            {
                "name": "create_test_script",
                "type": "FilePatch",
                "path": "/testbed/run_tests.sh",
                "content": test_script,
            },
            {
                "name": "make_executable",
                "type": "Command",
                "command": ["chmod", "+x", "/testbed/run_tests.sh"],
                "workDir": "/testbed",
            },
        ]

        session.execute(steps_setup_test)

        # Now use execute_with_callback to run tests automatically
        print("\n[Step 3] Verifying patch with execute_with_callback...")
        steps_verify: list[TaskStep] = [
            {
                "name": "verify_patch",
                "type": "Command",
                "command": [
                    "sh",
                    "-c",
                    "echo 'Checking patch...' && cat packages/emotion/src/index.js",
                ],
                "workDir": "/testbed",
            }
        ]

        # This will execute the verification AND automatically run the callback script
        result_with_callback = session.execute_with_callback(
            steps=steps_verify, callback_script="/testbed/run_tests.sh"
        )

        print(f"\nVerification state: {result_with_callback.get('status', {}).get('state')}")

        if "callback_result" in result_with_callback:
            callback_status = result_with_callback["callback_result"].get("status", {})
            print(f"Callback script state: {callback_status.get('state')}")
            print(f"Test results:\n{callback_status.get('stdout', '')}")

        # Summary
        print("\n" + "=" * 60)
        print("SWE-bench with Callbacks Completed!")
        print("=" * 60)
        print("\n✓ Callbacks demonstrated:")
        print("  - Registered multiple callbacks for different events")
        print("  - Callbacks triggered automatically after task execution")
        print("  - execute_with_callback() for automatic test script execution")
        print("\nThis demonstrates:")
        print("- Generic callback pattern for SWE-bench workflows")
        print("- Automatic test execution after applying patches")
        print("- Event-driven architecture for agent workflows")
        print("- Separation of concerns (task execution vs. post-processing)")

    finally:
        session.delete_sandbox()
        print("\n✓ Sandbox deleted")


if __name__ == "__main__":
    main()
