"""Executor container execution example.

Demonstrates:
- Executing commands in the executor container (instead of sidecar)
- Using executor-specific tools and environment
- Mixed execution: some steps in sidecar, some in executor
- When to use executor vs sidecar
"""

from arl import SandboxSession, TaskStep


def main() -> None:
    """Run commands in executor container."""
    print("=" * 60)
    print("Example: Executor Container Execution")
    print("=" * 60)

    with SandboxSession(pool_ref="python-39-std", namespace="default") as session:
        # Mixed execution: sidecar for file ops, executor for commands
        steps: list[TaskStep] = [
            # Step 1: Create a Python script (runs in sidecar by default)
            {
                "name": "create_script",
                "type": "FilePatch",
                "path": "/workspace/hello.py",
                "content": """#!/usr/bin/env python3
import sys
import platform

print(f"Hello from Python {sys.version}")
print(f"Platform: {platform.platform()}")
print(f"Architecture: {platform.machine()}")
""",
            },
            # Step 2: Run in SIDECAR (default, faster ~1-5ms)
            {
                "name": "run_in_sidecar",
                "type": "Command",
                "command": ["python3", "/workspace/hello.py"],
                # No "container" field = runs in sidecar (default)
            },
            # Step 3: Run in EXECUTOR (slower ~10-50ms, but has executor's environment)
            {
                "name": "run_in_executor",
                "type": "Command",
                "command": ["python3", "/workspace/hello.py"],
                "container": "executor",  # Explicitly run in executor container
            },
            # Step 4: Use executor-specific commands
            # (commands that might not exist in sidecar)
            {
                "name": "check_executor_tools",
                "type": "Command",
                "command": ["sh", "-c", "which python3 pip git && python3 --version"],
                "container": "executor",
            },
            # Step 5: Install package in executor (if needed)
            {
                "name": "install_package",
                "type": "Command",
                "command": ["pip", "install", "requests"],
                "container": "executor",
                "env": {"PIP_DISABLE_PIP_VERSION_CHECK": "1"},
            },
            # Step 6: Use the installed package
            {
                "name": "use_package",
                "type": "Command",
                "command": [
                    "python3",
                    "-c",
                    "import requests; print(f'requests version: {requests.__version__}')",
                ],
                "container": "executor",
            },
        ]

        result = session.execute(steps)

        # Print results
        status = result.get("status", {}) if result else {}
        print(f"\n✓ Task State: {status.get('state')}")

        # Print individual step results
        step_results = status.get("steps", [])
        if step_results:
            print("\n" + "=" * 60)
            print("Step Results:")
            print("=" * 60)
            for step in step_results:
                name = step.get("name", "unknown")
                exit_code = step.get("exitCode", -1)
                stdout = step.get("stdout", "").strip()
                stderr = step.get("stderr", "").strip()

                print(f"\n[{name}] (exit code: {exit_code})")
                if stdout:
                    print(f"stdout:\n{stdout}")
                if stderr:
                    print(f"stderr:\n{stderr}")


def usage_guide() -> None:
    """Print usage guide for executor container."""
    print("\n" + "=" * 60)
    print("When to use executor container:")
    print("=" * 60)
    print("""
✅ Use executor container when:
  - Need executor-specific tools (npm, pip, cargo, etc.)
  - Need executor's environment variables
  - Installing packages or dependencies
  - Running build commands
  - Debugging in actual execution environment

⚠️  Use sidecar (default) when:
  - Performance is critical (10x faster)
  - High-frequency operations
  - File operations (always use sidecar)
  - Simple commands available in both containers

Performance comparison:
  - Sidecar (gRPC):     1-5ms latency
  - Executor (kubectl): 10-50ms latency

Example:
  # Fast: runs in sidecar
  {"name": "list", "type": "Command", "command": ["ls", "-la"]}

  # Slower but has executor tools: runs in executor
  {"name": "build", "type": "Command", "command": ["npm", "build"],
   "container": "executor"}
""")


if __name__ == "__main__":
    main()
    usage_guide()
