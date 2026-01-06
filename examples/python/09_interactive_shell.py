"""Interactive shell example using gRPC bidirectional streaming.

Demonstrates:
- Opening an interactive shell session
- Sending commands to the shell
- Receiving real-time output
- Sending signals (Ctrl+C)
"""

from arl import SandboxSession


def main() -> None:
    """Demonstrate interactive shell usage."""
    print("=" * 60)
    print("Example: Interactive Shell (gRPC Bidirectional Streaming)")
    print("=" * 60)

    with SandboxSession(pool_ref="python-39-std", namespace="default") as session:
        print("\n--- Starting interactive shell session ---")

        # Start an interactive shell
        with session.interactive_shell() as shell:
            # Send a simple command
            print("\n[Sending] echo 'Hello from interactive shell!'")
            shell.send_data("echo 'Hello from interactive shell!'\n")

            # Read some output (with timeout handling)
            import time

            start = time.time()
            output_collected = []
            while time.time() - start < 2:  # 2 second timeout
                try:
                    for output in shell.read_output():
                        if output.data:
                            output_collected.append(output.data)
                            print(f"[Output] {output.data}", end="")
                        if output.closed:
                            break
                except Exception:
                    break
                if output_collected:
                    break
                time.sleep(0.1)

            # Send another command
            print("\n[Sending] pwd")
            shell.send_data("pwd\n")

            start = time.time()
            while time.time() - start < 2:
                try:
                    for output in shell.read_output():
                        if output.data:
                            print(f"[Output] {output.data}", end="")
                        if output.closed:
                            break
                except Exception:
                    break
                time.sleep(0.1)

            # Demonstrate signal sending (Ctrl+C simulation)
            print("\n[Info] Shell session supports sending signals like SIGINT (Ctrl+C)")

        print("\n✓ Shell session closed")


if __name__ == "__main__":
    main()
