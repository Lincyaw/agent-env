"""Basic task execution example.

Demonstrates:
- Creating a sandbox from a warm pool
- Executing multiple serial steps
- Getting task results
"""

from arl_client.session import SandboxSession


def main():
    """Execute basic serial steps."""
    print("=" * 60)
    print("Example: Basic Serial Execution")
    print("=" * 60)

    # Using context manager (automatically cleans up)
    with SandboxSession(pool_ref="python-39-std", namespace="default") as session:
        # Execute serial steps
        result = session.execute(
            [
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
        )

        # Check results
        status = result.get("status", {})
        print(f"\n✓ Task State: {status.get('state')}")
        print(f"✓ Exit Code: {status.get('exitCode')}")
        print(f"✓ Output:\n{status.get('stdout')}")

        if status.get("stderr"):
            print(f"✓ Errors: {status.get('stderr')}")


if __name__ == "__main__":
    main()
