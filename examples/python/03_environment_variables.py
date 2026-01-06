"""Environment variables example.

Demonstrates:
- Setting custom environment variables
- Accessing environment in commands
- Combining system and custom variables
"""

from arl import SandboxSession, TaskStep


def main() -> None:
    """Use custom environment variables in commands."""
    print("=" * 60)
    print("Example: Environment Variables")
    print("=" * 60)

    with SandboxSession(pool_ref="python-39-std", namespace="default") as session:
        steps: list[TaskStep] = [
                {
                    "name": "test_env",
                    "type": "Command",
                    "command": [
                        "sh",
                        "-c",
                        "echo USER=$USER; echo HOME=$HOME; echo MY_VAR=$MY_VAR; echo API_KEY=$API_KEY",
                    ],
                    "env": {
                        "MY_VAR": "custom_value",
                        "API_KEY": "secret_key_123",
                    },
                }
            ]

        result = session.execute(steps)

        status = result.get("status", {}) if result else {}
        print(f"\n✓ Task State: {status.get('state')}")
        print(f"✓ Environment Variables:\n{status.get('stdout')}")


if __name__ == "__main__":
    main()
