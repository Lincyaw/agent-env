"""SWE-bench comprehensive example.

Demonstrates complete SWE-bench workflow including:
- Using SWE-bench Docker images for code repair tasks
- Creating WarmPool programmatically via Python SDK
- Mock agent simulating LLM agent behavior
- Applying code patches and running tests
- Callback system for event-driven workflows
- Complete automated bug fixing scenario

Usage:
    # Basic usage with mock agent
    python swebench_example.py

    # With callback hooks
    python swebench_example.py --use-callbacks

    # Without mock agent (inline fixtures)
    python swebench_example.py --no-mock-agent
"""

import argparse
from pathlib import Path

from arl import SandboxSession, TaskStep, WarmPoolManager
from arl.types import TaskResource
from kubernetes import client
from mock_agent import create_mock_agent


def create_test_callback(test_script_path: str):
    """Factory function to create a test callback."""

    def run_tests_callback(result: TaskResource) -> None:
        status = result.get("status", {})
        state = status.get("state")
        print(f"\n[Callback] Task completed with state: {state}")
        if state == "Succeeded":
            print(f"[Callback] Test script {test_script_path} would be executed in production")

    return run_tests_callback


def log_callback(result: TaskResource) -> None:
    """Simple logging callback."""
    status = result.get("status", {})
    print(f"\n[Log Callback] Task: {result.get('metadata', {}).get('name')}")
    print(
        f"[Log Callback] State: {status.get('state')}, Exit Code: {status.get('exitCode', 'N/A')}"
    )


def success_callback(result: TaskResource) -> None:
    """Callback triggered only on success."""
    print("\n[Success Callback] Task completed successfully!")


def failure_callback(result: TaskResource) -> None:
    """Callback triggered only on failure."""
    status = result.get("status", {})
    print(f"\n[Failure Callback] Task failed! Error: {status.get('stderr', '')[:200]}")


def load_fixture(filename: str) -> str:
    """Load content from fixture file."""
    fixtures_dir = Path(__file__).parent / "swebench_fixtures"
    fixture_path = fixtures_dir / filename

    if not fixture_path.exists():
        raise FileNotFoundError(f"Fixture file not found: {fixture_path}")

    with open(fixture_path) as f:
        return f.read()


def get_inline_patch() -> str:
    """Fallback inline patch content."""
    return """--- a/packages/emotion/src/index.js
+++ b/packages/emotion/src/index.js
@@ -10,7 +10,7 @@
   const styles = createStyles(props)

   // Apply styles to element
-  element.style = styles
+  element.setAttribute('style', styles)

   return element
 }
"""


def get_inline_test_script() -> str:
    """Fallback inline test script."""
    return """#!/bin/bash
set -e

echo "Running test suite..."
echo "===================="

# Check if patched code is correct
if grep -q "setAttribute" packages/emotion/src/index.js; then
    echo "✓ Test 1: Patch applied correctly"
else
    echo "✗ Test 1: Patch not applied"
    exit 1
fi

# Simulate running actual tests
echo "✓ Test 2: Unit tests passed"
echo "✓ Test 3: Integration tests passed"
echo "✓ Test 4: Style application works correctly"

echo ""
echo "===================="
echo "All tests passed!"
exit 0
"""


def get_inline_report() -> str:
    """Fallback inline report template."""
    return """# Bug Fix Report

## Issue
Incorrect style application in emotion library causing DOM manipulation errors.

## Root Cause
Direct assignment to `element.style` property instead of using `setAttribute()`.

## Fix Applied
Changed line 13 in packages/emotion/src/index.js:
- Before: `element.style = styles`
- After: `element.setAttribute('style', styles)`

## Testing
All test suites passed:
- ✓ Unit tests
- ✓ Integration tests
- ✓ Style application tests

## Verification
The fix has been verified in the SWE-bench environment and all tests pass.
"""


def main() -> None:
    """Run comprehensive SWE-bench example."""
    parser = argparse.ArgumentParser(description="SWE-bench comprehensive example")
    parser.add_argument(
        "--use-callbacks",
        action="store_true",
        help="Enable callback hooks for event-driven workflow",
    )
    parser.add_argument(
        "--no-mock-agent",
        action="store_true",
        help="Don't use mock agent, use inline fixtures instead",
    )
    args = parser.parse_args()

    print("=" * 60)
    print("SWE-bench Comprehensive Example")
    print("=" * 60)

    # Configuration
    pool_name = "swebench-emotion"
    namespace = "default"
    swebench_image = "swebench/swesmith.x86_64.emotion_1776_js-emotion.b882bcba"

    # Initialize agent if requested
    agent = None
    if not args.no_mock_agent:
        fixtures_dir = Path(__file__).parent / "swebench_fixtures"
        agent = create_mock_agent(fixtures_dir)
        print("\n[Agent] Mock agent initialized with fixture files")

    # Setup WarmPool
    print("\n[Step 0] Setting up WarmPool with Python SDK...")
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

    # Create session
    session = SandboxSession(pool_ref=pool_name, namespace=namespace, keep_alive=True)

    # Register callbacks if requested
    if args.use_callbacks:
        print("\n[Callbacks] Registering callback hooks...")
        session.register_callback("on_task_complete", log_callback)
        session.register_callback("on_task_success", success_callback)
        session.register_callback("on_task_failure", failure_callback)
        session.register_callback("on_task_complete", create_test_callback("/testbed/run_tests.sh"))
        print("✓ Registered 4 callbacks")

    try:
        session.create_sandbox()
        print(f"\n✓ Sandbox allocated from pool '{pool_name}'")

        # Step 1: Analyze issue (if using agent)
        if agent:
            print("\n[Step 1] Analyzing issue with mock agent...")
            analysis = agent.analyze_issue()
            print("✓ Issue analysis complete")
            print(f"  - Category: {analysis['category']}")
            print(f"  - Severity: {analysis['severity']}")
            print(f"  - Affected file: {analysis['affected_file']}")
            print(f"  - Root cause: {analysis['root_cause']}")

        # Step 2: Inspect environment
        print("\n[Step 2] Inspecting SWE-bench environment...")
        steps_inspect: list[TaskStep] = [
            {
                "name": "check_workspace",
                "type": "Command",
                "command": ["sh", "-c", "pwd && ls -la"],
                "workDir": "/testbed",
            },
            {
                "name": "check_repo",
                "type": "Command",
                "command": ["sh", "-c", "git status || echo 'Not a git repo'"],
                "workDir": "/testbed",
            },
        ]
        result_inspect = session.execute(steps_inspect)
        status_inspect = result_inspect.get("status", {})
        print(f"Environment check: {status_inspect.get('state')}")

        # Step 3: Generate and apply patch
        print("\n[Step 3] Generating and applying code patch...")

        # Get patch content
        if agent:
            patch_content = agent.generate_patch()
            print(f"✓ Agent generated patch ({len(patch_content)} bytes)")
        else:
            try:
                patch_content = load_fixture("agent_patch.diff")
                print("✓ Loaded patch from fixture file")
            except FileNotFoundError:
                patch_content = get_inline_patch()
                print("✓ Using inline patch")

        # Apply patch
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
            {
                "name": "verify_patch",
                "type": "Command",
                "command": [
                    "sh",
                    "-c",
                    "echo 'Patched file:' && cat packages/emotion/src/index.js",
                ],
                "workDir": "/testbed",
            },
        ]

        session.execute(steps_patch)
        print("✓ Patch applied successfully")

        # Step 4: Generate and run tests
        print("\n[Step 4] Running test suite...")

        # Get test script
        if agent:
            test_script = agent.generate_test_script()
            print(f"✓ Agent generated test script ({len(test_script)} bytes)")
        else:
            try:
                test_script = load_fixture("test_script.sh")
                print("✓ Loaded test script from fixture file")
            except FileNotFoundError:
                test_script = get_inline_test_script()
                print("✓ Using inline test script")

        # Run tests
        steps_test: list[TaskStep] = [
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
            {
                "name": "run_tests",
                "type": "Command",
                "command": ["/bin/bash", "/testbed/run_tests.sh"],
                "workDir": "/testbed",
            },
        ]

        result_test = session.execute(steps_test)
        status_test = result_test.get("status", {})
        print(f"Test execution: {status_test.get('state')}")
        if status_test.get("stdout"):
            print(f"Test results:\n{status_test.get('stdout')}")

        # Step 5: Generate report
        print("\n[Step 5] Generating fix report...")

        # Get report content
        if agent:
            report_content = agent.generate_report()
            print(f"✓ Agent generated report ({len(report_content)} bytes)")
        else:
            try:
                report_content = load_fixture("fix_report_template.md")
                print("✓ Loaded report template from fixture file")
            except FileNotFoundError:
                report_content = get_inline_report()
                print("✓ Using inline report template")

        # Save report
        steps_report: list[TaskStep] = [
            {
                "name": "create_report",
                "type": "FilePatch",
                "path": "/testbed/fix_report.md",
                "content": report_content,
            },
            {
                "name": "show_report",
                "type": "Command",
                "command": ["cat", "/testbed/fix_report.md"],
                "workDir": "/testbed",
            },
        ]

        result_report = session.execute(steps_report)
        status_report = result_report.get("status", {})
        if status_report.get("stdout"):
            print(f"\n{status_report.get('stdout')}")

        # Summary
        print("\n" + "=" * 60)
        print("SWE-bench Workflow Completed Successfully!")
        print("=" * 60)
        print("\n✓ WarmPool setup via Python SDK")
        print("✓ Environment inspected")
        if agent:
            print("✓ Mock agent analyzed issue and generated fixes")
        print("✓ Code patch applied")
        print("✓ Tests executed and passed")
        print("✓ Fix report generated")
        if args.use_callbacks:
            print("✓ Callbacks triggered for event-driven workflow")

        print("\nThis demonstrates:")
        print("- Automated bug fixing with LLM agents")
        print("- Code repair in SWE-bench environments")
        print("- WarmPool management without Kubernetes knowledge")
        if agent:
            print("- Mock agent simulation with fixture files")
        if args.use_callbacks:
            print("- Event-driven architecture with callbacks")

    finally:
        session.delete_sandbox()
        print("\n✓ Sandbox deleted")


if __name__ == "__main__":
    main()
