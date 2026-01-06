"""Working directory example.

Demonstrates:
- Setting custom working directory for commands
- Creating subdirectories
- Working with relative paths
"""

from arl_client.session import SandboxSession


def main():
    """Execute commands with custom working directory."""
    print("=" * 60)
    print("Example: Working Directory")
    print("=" * 60)

    with SandboxSession(pool_ref="python-39-std", namespace="default") as session:
        result = session.execute(
            [
                # Create subdirectory structure
                {
                    "name": "create_structure",
                    "type": "Command",
                    "command": ["mkdir", "-p", "/workspace/project/src"],
                },
                # Create file in subdirectory
                {
                    "name": "create_file",
                    "type": "FilePatch",
                    "path": "/workspace/project/src/main.py",
                    "content": 'print("Hello from src!")',
                },
                # Run command with custom working directory
                {
                    "name": "run_from_workdir",
                    "type": "Command",
                    "command": ["python3", "main.py"],
                    "workDir": "/workspace/project/src",
                },
                # List files using relative path
                {
                    "name": "list_files",
                    "type": "Command",
                    "command": ["ls", "-la"],
                    "workDir": "/workspace/project",
                },
            ]
        )

        status = result.get("status", {})
        print(f"\n✓ Task State: {status.get('state')}")
        print(f"✓ Output:\n{status.get('stdout')}")


if __name__ == "__main__":
    main()
