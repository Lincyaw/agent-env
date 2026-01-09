# Copyright 2024 ARL-Infra Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

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
