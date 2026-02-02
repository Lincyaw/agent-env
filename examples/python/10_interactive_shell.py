"""Example: Using interactive shell with Python SDK.

This demonstrates how to create a sandbox and use an interactive shell.
"""

import time

from arl import SandboxSession


def main():
    """Create a sandbox and provide an interactive shell."""
    print("=" * 60)
    print("Interactive Shell Example")
    print("=" * 60)

    # Step 1: Create a sandbox
    print("\n1. Creating sandbox...")
    session = SandboxSession(
        pool_ref="csc4001-pool",
        namespace="default",
        keep_alive=True,  # Keep sandbox alive for interactive use
    )
    session.create_sandbox()

    # Step 2: Create interactive shell
    print("\n2. Creating interactive shell...")
    shell = session.create_interactive_shell(container="executor")

    print(f"✓ Sandbox created: {session.sandbox_name}")
    print("✓ Interactive shell connected")
    print("\n" + "=" * 60)
    print("Interactive Shell Ready")
    print("=" * 60)
    print("Type commands and press Enter. Type 'exit' to quit.")
    print("=" * 60 + "\n")

    try:
        while shell.is_open():
            # Read user input
            try:
                user_input = input("$ ")
            except EOFError:
                break

            # Check for exit command
            if user_input.strip().lower() in ["exit", "quit"]:
                break

            # Send input to shell
            shell.send_input_sync(user_input + "\n")

            # Wait for command to execute and read output
            time.sleep(0.1)

            # Read all available output
            all_output = ""
            for _ in range(10):  # Try multiple times
                output = shell.read_output_sync(timeout=0.2)
                if output:
                    all_output += output
                    print(output, end="", flush=True)
                else:
                    # No more output
                    break
                time.sleep(0.1)

            # If no output at all, print a newline
            if not all_output:
                print()

    except KeyboardInterrupt:
        print("\n\nInterrupted by user")
    finally:
        print("\n\nCleaning up...")
        shell.close_sync()
        session.delete_sandbox()
        print("✓ Sandbox deleted")


if __name__ == "__main__":
    main()
