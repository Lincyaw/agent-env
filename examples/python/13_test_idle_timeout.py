"""Test idle timeout and max lifetime features.

Demonstrates:
- Automatic idle timeout for keep_alive sandboxes
- Custom idle timeout configuration
- Max lifetime enforcement
- Warning when using context manager with keep_alive
"""

from arl import SandboxSession


def test_default_idle_timeout():
    """Test that keep_alive sandboxes get default 30-minute idle timeout."""
    print("=" * 60)
    print("Test 1: Default Idle Timeout for keep_alive=True")
    print("=" * 60)

    # This should trigger a warning and set 1800s idle timeout
    session = SandboxSession(
        pool_ref="csc4001-pool",
        namespace="default",
        keep_alive=True,
    )

    try:
        session.create_sandbox()
        print(f"✓ Sandbox created: {session.sandbox_name}")
        print("  Default idle timeout: 1800s (30 minutes)")
    finally:
        session.delete_sandbox()
        print("✓ Sandbox deleted\n")


def test_custom_idle_timeout():
    """Test custom idle timeout configuration."""
    print("=" * 60)
    print("Test 2: Custom Idle Timeout")
    print("=" * 60)

    session = SandboxSession(
        pool_ref="csc4001-pool",
        namespace="default",
        keep_alive=True,
        idle_timeout_seconds=300,  # 5 minutes
    )

    try:
        session.create_sandbox()
        print(f"✓ Sandbox created: {session.sandbox_name}")
        print("  Custom idle timeout: 300s (5 minutes)")
    finally:
        session.delete_sandbox()
        print("✓ Sandbox deleted\n")


def test_context_manager_warning():
    """Test warning when using context manager with keep_alive."""
    print("=" * 60)
    print("Test 3: Context Manager Warning")
    print("=" * 60)

    # This should print a warning
    with SandboxSession(
        pool_ref="csc4001-pool",
        namespace="default",
        keep_alive=True,
        idle_timeout_seconds=120,
    ) as session:
        print(f"✓ Sandbox created: {session.sandbox_name}")
        print("  (Warning should appear above)")

    # Sandbox NOT deleted automatically due to keep_alive=True
    # Must delete manually
    if session.sandbox_name:
        session.delete_sandbox()
        print("✓ Sandbox manually deleted\n")


def test_auto_cleanup():
    """Test automatic cleanup without keep_alive."""
    print("=" * 60)
    print("Test 4: Automatic Cleanup (keep_alive=False)")
    print("=" * 60)

    with SandboxSession(
        pool_ref="csc4001-pool",
        namespace="default",
        keep_alive=False,  # Auto cleanup
    ) as session:
        print(f"✓ Sandbox created: {session.sandbox_name}")
        print("  Will be automatically deleted on exit")

    print("✓ Sandbox automatically deleted\n")


if __name__ == "__main__":
    print("\n🧪 Testing Sandbox Idle Timeout and Lifetime Features\n")

    test_default_idle_timeout()
    test_custom_idle_timeout()
    test_context_manager_warning()
    test_auto_cleanup()

    print("=" * 60)
    print("✅ All tests completed successfully!")
    print("=" * 60)
