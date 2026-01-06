"""Streaming execution example using gRPC.

Demonstrates:
- Real-time output streaming via gRPC
- Direct sidecar communication (bypassing Task CRD)
- Processing output as it arrives
"""

from arl import SandboxSession


def main() -> None:
    """Execute command with streaming output."""
    print("=" * 60)
    print("Example: Streaming Execution (gRPC)")
    print("=" * 60)

    with SandboxSession(pool_ref="python-39-std", namespace="default") as session:
        print("\n--- Streaming 'ls -la /workspace' ---")
        # Use execute_stream for real-time output
        for log in session.execute_stream(["ls", "-la", "/workspace"]):
            if log.stdout:
                print(f"[stdout] {log.stdout}", end="")
            if log.stderr:
                print(f"[stderr] {log.stderr}", end="")
            if log.done:
                print(f"\n✓ Command completed with exit code: {log.exit_code}")

        print("\n--- Streaming Python script execution ---")
        # First create a script that produces output over time
        session.sidecar_client.update_files(
            base_path="/workspace",
            files={
                "countdown.py": """import time
import sys

for i in range(5, 0, -1):
    print(f"Countdown: {i}")
    sys.stdout.flush()
    time.sleep(0.5)

print("Liftoff!")
"""
            },
        )

        # Stream the execution
        for log in session.execute_stream(
            ["python3", "/workspace/countdown.py"],
            working_dir="/workspace",
        ):
            if log.stdout:
                print(f"  {log.stdout}", end="")
            if log.done:
                print(f"✓ Script completed with exit code: {log.exit_code}")


if __name__ == "__main__":
    main()
