"""Basic example of using ARL Python SDK.

This example shows how to:
1. Create a sandbox from a warm pool
2. Execute a simple task
3. Clean up resources
"""

from arl_client.session import SandboxSession


def main():
    """Run basic example."""
    # Using context manager (automatically cleans up)
    with SandboxSession(
        pool_ref="python-3.9-std",
        namespace="default"
    ) as session:
        # Execute a simple task
        result = session.execute([
            {
                "name": "write_script",
                "type": "FilePatch",
                "path": "/workspace/hello.py",
                "content": "print('Hello from ARL!')"
            },
            {
                "name": "run_script",
                "type": "Command",
                "command": ["python", "/workspace/hello.py"]
            }
        ])
        
        # Check results
        status = result.get("status", {})
        print(f"Task State: {status.get('state')}")
        print(f"Exit Code: {status.get('exitCode')}")
        print(f"Output: {status.get('stdout')}")
        
        if status.get("stderr"):
            print(f"Errors: {status.get('stderr')}")


if __name__ == "__main__":
    main()
