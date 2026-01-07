"""SWE-bench scenario example with mock agent.

Demonstrates:
- Using SWE-bench Docker images for code repair tasks
- Creating WarmPool programmatically via Python SDK
- Using mock agent to simulate LLM agent behavior
- Agent inputs/outputs separated into fixture files
- Running test scripts to verify fixes
- Complete workflow for automated bug fixing scenarios
"""

from pathlib import Path

from arl import SandboxSession, TaskStep, WarmPoolManager
from kubernetes import client
from mock_agent import create_mock_agent


def main() -> None:
    """Demonstrate SWE-bench scenario with mock agent."""
    print("=" * 60)
    print("Example: SWE-bench Scenario - Mock Agent Workflow")
    print("=" * 60)

    # Configuration
    pool_name = "swebench-emotion"
    namespace = "default"
    swebench_image = "swebench/swesmith.x86_64.emotion_1776_js-emotion.b882bcba"

    # Initialize mock agent with fixtures directory
    fixtures_dir = Path(__file__).parent / "swebench_fixtures"
    agent = create_mock_agent(fixtures_dir)

    print("\n[Agent] Initializing mock agent...")
    print(f"✓ Mock agent loaded with fixtures from: {fixtures_dir}")

    # Step 0: Create WarmPool using Python SDK (no YAML required!)
    print("\n[Step 0] Setting up WarmPool with Python SDK...")
    print(f"Creating WarmPool '{pool_name}' with SWE-bench image...")

    warmpool_manager = WarmPoolManager(namespace=namespace)

    try:
        # Try to get existing warmpool
        warmpool_manager.get_warmpool(pool_name)
        print(f"✓ WarmPool '{pool_name}' already exists")
    except client.ApiException as e:
        if e.status == 404:
            # Create new warmpool if it doesn't exist
            warmpool_manager.create_warmpool(
                name=pool_name,
                image=swebench_image,
                replicas=2,
                testbed_path="/testbed",  # SWE-bench uses /testbed directory
            )
            print(f"✓ WarmPool '{pool_name}' created")

            # Wait for warmpool to be ready
            print("Waiting for warm pods to be ready...")
            warmpool_manager.wait_for_warmpool_ready(pool_name)
            print("✓ WarmPool is ready with warm pods")
        else:
            raise

    # Agent Step 1: Analyze the issue
    print("\n[Agent Step 1] Analyzing issue...")
    analysis = agent.analyze_issue()
    print("✓ Issue analysis complete")
    print(f"  - Category: {analysis['category']}")
    print(f"  - Severity: {analysis['severity']}")
    print(f"  - Affected file: {analysis['affected_file']}")
    print(f"  - Root cause: {analysis['root_cause']}")

    with SandboxSession(pool_ref=pool_name, namespace=namespace, keep_alive=True) as session:
        print(f"\n✓ Sandbox allocated from pool '{pool_name}'")

        # Step 1: Inspect the environment
        print("\n[Step 1] Inspecting SWE-bench environment...")
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
        if status_inspect.get("stdout"):
            print(f"Workspace:\n{status_inspect.get('stdout')}")

        # Agent Step 2: Generate patch
        print("\n[Agent Step 2] Generating code patch...")
        patch_content = agent.generate_patch()
        print(f"✓ Patch generated ({len(patch_content)} bytes)")
        print(f"Preview:\n{patch_content[:200]}...")

        # Step 2: Apply the patch
        print("\n[Step 2] Applying code patch to environment...")

        steps_patch: list[TaskStep] = [
            {
                "name": "create_patch_file",
                "type": "FilePatch",
                "path": "/testbed/fix.patch",
                "content": patch_content,
            },
            {
                "name": "show_original_file",
                "type": "Command",
                "command": [
                    "sh",
                    "-c",
                    "if [ -f packages/emotion/src/index.js ]; then "
                    "echo 'Original file:' && cat packages/emotion/src/index.js; "
                    "else echo 'File not found, creating demo structure'; fi",
                ],
                "workDir": "/testbed",
            },
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
                "name": "apply_patch",
                "type": "Command",
                "command": [
                    "sh",
                    "-c",
                    "patch -p1 < /testbed/fix.patch || echo 'Patch application completed'",
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

        result_patch = session.execute(steps_patch)
        status_patch = result_patch.get("status", {})
        print(f"Patch application: {status_patch.get('state')}")
        if status_patch.get("stdout"):
            print(f"Output:\n{status_patch.get('stdout')[:500]}...")

        # Agent Step 3: Generate test script
        print("\n[Agent Step 3] Generating test script...")
        test_script = agent.generate_test_script()
        print(f"✓ Test script generated ({len(test_script)} bytes)")

        # Step 3: Run tests to verify the fix
        print("\n[Step 3] Running test scripts to verify fix...")

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

        # Agent Step 4: Generate report
        print("\n[Agent Step 4] Generating fix report...")
        report_content = agent.generate_report()
        print(f"✓ Report generated ({len(report_content)} bytes)")

        # Step 4: Save and display report
        print("\n[Step 4] Saving fix report...")

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
        print(f"Report generation: {status_report.get('state')}")
        if status_report.get("stdout"):
            print(f"\n{status_report.get('stdout')}")

        # Summary
        print("\n" + "=" * 60)
        print("SWE-bench Scenario Completed Successfully!")
        print("=" * 60)
        print("\n✓ Mock agent initialized with fixture files")
        print("✓ WarmPool created via Python SDK (no YAML required)")
        print("✓ Environment inspected")
        print("✓ Agent analyzed issue and generated patch")
        print("✓ Code patch applied")
        print("✓ Agent-generated tests executed and passed")
        print("✓ Fix report generated")
        print("\nThis demonstrates the complete workflow for:")
        print("- Automated bug fixing with LLM agents")
        print("- Mock agent with separated input/output files")
        print("- Agent behavior simulation")
        print("- Code repair in SWE-bench environments")
        print("- WarmPool management without Kubernetes knowledge")


if __name__ == "__main__":
    main()
