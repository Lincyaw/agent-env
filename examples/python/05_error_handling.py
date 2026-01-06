"""Error handling example.

Demonstrates:
- Detecting task failures
- Getting exit codes
- Handling exceptions
- Retry logic
"""

from arl import SandboxSession


def example_task_failure() -> None:
    """Handle task execution failure."""
    print("\n" + "=" * 60)
    print("Example 1: Task Failure")
    print("=" * 60)

    with SandboxSession(pool_ref="python-39-std", namespace="default") as session:
        # Execute a task that will fail
        result = session.execute(
            [
                {
                    "name": "failing_command",
                    "type": "Command",
                    "command": ["sh", "-c", "echo 'Starting...'; exit 42"],
                }
            ]
        )

        status = result.get("status", {})
        print(f"\n✓ Task State: {status.get('state')}")
        print(f"✓ Exit Code: {status.get('exitCode')}")
        print(f"✓ Output: {status.get('stdout')}")

        if status.get("state") == "Failed":
            print("✓ Task failed as expected")


def example_invalid_command() -> None:
    """Handle invalid command."""
    print("\n" + "=" * 60)
    print("Example 2: Invalid Command")
    print("=" * 60)

    with SandboxSession(pool_ref="python-39-std", namespace="default") as session:
        result = session.execute(
            [
                {
                    "name": "invalid_command",
                    "type": "Command",
                    "command": ["nonexistent_command", "--help"],
                }
            ]
        )

        status = result.get("status", {})
        print(f"\n✓ Task State: {status.get('state')}")
        print(f"✓ Exit Code: {status.get('exitCode')}")
        if status.get("stderr"):
            print(f"✓ Error: {status.get('stderr')[:200]}")


def example_retry_logic() -> None:
    """Demonstrate retry logic for failed tasks."""
    print("\n" + "=" * 60)
    print("Example 3: Retry Logic")
    print("=" * 60)

    max_retries = 3

    with SandboxSession(pool_ref="python-39-std", namespace="default") as session:
        for attempt in range(max_retries):
            print(f"\nAttempt {attempt + 1}/{max_retries}")

            result = session.execute(
                [
                    {
                        "name": "flaky_task",
                        "type": "Command",
                        "command": [
                            "sh",
                            "-c",
                            f"[ {attempt} -eq 2 ] && echo 'Success!' || (echo 'Failed'; exit 1)",
                        ],
                    }
                ]
            )

            status = result.get("status", {})
            if status.get("state") == "Succeeded":
                print("✓ Task succeeded!")
                print(f"✓ Output: {status.get('stdout')}")
                break
            else:
                print(f"✗ Task failed with exit code: {status.get('exitCode')}")
                if attempt == max_retries - 1:
                    print("✗ Max retries reached")


def main() -> None:
    """Run all error handling examples."""
    print("=" * 60)
    print("Error Handling Examples")
    print("=" * 60)

    example_task_failure()
    example_invalid_command()
    example_retry_logic()


if __name__ == "__main__":
    main()
