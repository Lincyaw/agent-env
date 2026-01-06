"""Error handling example.

This example shows how to handle various error scenarios:
1. Task execution failures
2. Timeout handling
3. Sandbox allocation failures
"""

from arl_client.session import SandboxSession


def example_task_failure():
    """Handle task execution failure."""
    print("==> Example: Task Failure Handling")
    
    with SandboxSession("python-3.9-std", namespace="default") as session:
        # Execute a task that will fail
        result = session.execute([
            {
                "name": "failing_command",
                "type": "Command",
                "command": ["python", "-c", "import sys; sys.exit(1)"]
            }
        ])
        
        status = result.get("status", {})
        if status.get("state") == "Failed":
            print(f"Task failed as expected")
            print(f"Exit Code: {status.get('exitCode')}")
            print(f"Error Output: {status.get('stderr')}")
        else:
            print("Unexpected success")


def example_timeout():
    """Handle task timeout."""
    print("\n==> Example: Timeout Handling")
    
    with SandboxSession("python-3.9-std", namespace="default") as session:
        try:
            # Execute a task with short timeout
            result = session.execute(
                steps=[
                    {
                        "name": "slow_task",
                        "type": "Command",
                        "command": ["sleep", "60"]
                    }
                ],
                timeout="5s"  # Task will timeout
            )
            
            status = result.get("status", {})
            print(f"Task State: {status.get('state')}")
            
        except TimeoutError as e:
            print(f"Task timed out as expected: {e}")


def example_invalid_pool():
    """Handle sandbox allocation failure."""
    print("\n==> Example: Invalid Pool Reference")
    
    try:
        with SandboxSession("non-existent-pool", namespace="default") as session:
            pass
    except RuntimeError as e:
        print(f"Failed to create sandbox as expected: {e}")


def example_retry_logic():
    """Demonstrate retry logic for failed tasks."""
    print("\n==> Example: Retry Logic")
    
    max_retries = 3
    
    with SandboxSession("python-3.9-std", namespace="default") as session:
        for attempt in range(max_retries):
            print(f"Attempt {attempt + 1}/{max_retries}")
            
            result = session.execute([
                {
                    "name": "flaky_task",
                    "type": "Command",
                    "command": ["python", "-c", "import random; import sys; sys.exit(random.choice([0, 1]))"]
                }
            ])
            
            status = result.get("status", {})
            if status.get("state") == "Succeeded":
                print("Task succeeded!")
                break
            else:
                print(f"Task failed, exit code: {status.get('exitCode')}")
                if attempt == max_retries - 1:
                    print("Max retries reached")


def main():
    """Run all error handling examples."""
    example_task_failure()
    example_timeout()
    example_invalid_pool()
    example_retry_logic()


if __name__ == "__main__":
    main()
