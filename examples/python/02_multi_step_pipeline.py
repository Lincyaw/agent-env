"""Multi-step pipeline example.

Demonstrates:
- Creating data files
- Processing data with Python scripts
- Passing data between steps
- Verifying output
"""

from arl_client.session import SandboxSession


def main():
    """Run a multi-step data processing pipeline."""
    print("=" * 60)
    print("Example: Multi-Step Pipeline")
    print("=" * 60)

    with SandboxSession(pool_ref="python-39-std", namespace="default") as session:
        # Create and execute a data processing pipeline
        result = session.execute(
            [
                # Step 1: Create input data
                {
                    "name": "create_data",
                    "type": "FilePatch",
                    "path": "/workspace/data.txt",
                    "content": "apple\nbanana\ncherry\ndate\neggplant",
                },
                # Step 2: Create processing script
                {
                    "name": "create_processor",
                    "type": "FilePatch",
                    "path": "/workspace/process.py",
                    "content": """#!/usr/bin/env python3
with open('/workspace/data.txt', 'r') as f:
    items = f.read().strip().split('\\n')

print(f"Total items: {len(items)}")
print("Items:", ', '.join(items))

# Process and save results
results = [item.upper() for item in items]
with open('/workspace/output.txt', 'w') as f:
    f.write('\\n'.join(results))

print("Processing completed!")
""",
                },
                # Step 3: Run the processor
                {
                    "name": "run_processor",
                    "type": "Command",
                    "command": ["python3", "/workspace/process.py"],
                },
                # Step 4: Verify output
                {
                    "name": "verify_output",
                    "type": "Command",
                    "command": ["cat", "/workspace/output.txt"],
                },
            ]
        )

        status = result.get("status", {})
        print(f"\n✓ Task State: {status.get('state')}")
        print(f"✓ Output:\n{status.get('stdout')}")


if __name__ == "__main__":
    main()
