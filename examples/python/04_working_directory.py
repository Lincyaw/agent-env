"""Working directory example.

Demonstrates:
- Setting custom working directory for commands
- Creating subdirectories
- Working with relative paths
"""

from arl import SandboxSession, TaskStep


def main() -> None:
    """Execute commands with custom working directory."""
    print("=" * 60)
    print("Example: Working Directory")
    print("=" * 60)

    with SandboxSession(pool_ref="python-39-std", namespace="default") as session:
        steps: list[TaskStep] = [
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

        result = session.execute(steps)

        status = result.get("status", {}) if result else {}
        print(f"\n✓ Task State: {status.get('state')}")
        print(f"✓ Output:\n{status.get('stdout')}")


if __name__ == "__main__":
    main()
