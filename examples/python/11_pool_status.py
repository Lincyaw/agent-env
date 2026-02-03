"""Example: Query WarmPool status and available sandboxes.

This demonstrates how to check pool capacity before creating sandboxes.
"""

from arl import SandboxSession


def main():
    """Query pool status and create sandboxes based on availability."""
    print("=" * 60)
    print("WarmPool Status Query Example")
    print("=" * 60)

    # Create session (don't create sandbox yet)
    session = SandboxSession(
        pool_ref="csc4001-pool",
        namespace="default",
    )

    # Query pool status
    print("\n1. Checking pool status...")
    try:
        status = session.get_pool_status()
        print(f"\nPool: {session.pool_ref}")
        print(f"  Total capacity:     {status['total']}")
        print(f"  Available now:      {status['available']}")
        print(f"  Already allocated:  {status['allocated']}")
        print(f"  Ready idle:         {status['ready_idle']}")

        # Check if we can create a sandbox
        if status["available"] > 0:
            print(f"\n✓ {status['available']} sandbox(s) available for immediate use")

            # Create and use sandbox
            print("\n2. Creating sandbox...")
            session.create_sandbox()

            # Check status again after allocation
            print("\n3. Checking pool status after allocation...")
            status_after = session.get_pool_status()
            print(f"\nPool: {session.pool_ref}")
            print(f"  Total capacity:     {status_after['total']}")
            print(f"  Available now:      {status_after['available']}")
            print(f"  Already allocated:  {status_after['allocated']}")
            print(f"  Ready idle:         {status_after['ready_idle']}")

            # Execute a simple task
            print("\n4. Executing task...")
            result = session.execute(
                [{"name": "hello", "type": "Command", "command": ["echo", "Hello from sandbox!"]}]
            )
            print(f"✓ Task completed: {result['status']['state']}")

            # Cleanup
            print("\n5. Cleaning up...")
            session.delete_sandbox()
            print("✓ Sandbox deleted")

        else:
            print("\n⚠ No sandboxes available (pool is at full capacity)")
            print(f"  All {status['total']} pods are currently allocated")
            print("  Please wait for a sandbox to be released or increase pool size")

    except ValueError as e:
        print(f"\n❌ Error: {e}")
        print("\nMake sure the WarmPool exists:")
        print(f"  kubectl get warmpool {session.pool_ref} -n {session.namespace}")
    except Exception as e:
        print(f"\n❌ Unexpected error: {e}")


if __name__ == "__main__":
    main()
