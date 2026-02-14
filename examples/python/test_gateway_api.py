"""Integration test for ARL Python SDK via Gateway API.

Tests all major APIs:
1. WarmPool management (create, get, delete)
2. Session lifecycle (create, execute, restore, history, trajectory, delete)
3. Command execution
4. Snapshot/restore mechanism
5. Interactive shell (WebSocket)

Prerequisites:
    - ARL operator + gateway deployed to Kubernetes
    - Gateway port-forwarded to localhost:8080
    - A WarmPool with ready pods

Usage:
    # Port-forward gateway first:
    kubectl port-forward -n arl svc/arl-operator-gateway 8080:8080

    # Run tests:
    uv run python examples/python/test_gateway_api.py
"""

from __future__ import annotations

import json
import sys
import time

from arl import (
    GatewayClient,
    GatewayError,
    InteractiveShellClient,
    PoolNotReadyError,
    SandboxSession,
    WarmPoolManager,
)

GATEWAY_URL = "http://14.103.184.145:8080"
POOL_NAME = "test-pool"
POOL_IMAGE = "pair-diag-cn-guangzhou.cr.volces.com/pair/ubuntu:22.04"
NAMESPACE = "arl"


def section(title: str) -> None:
    print(f"\n{'=' * 60}")
    print(f"  {title}")
    print(f"{'=' * 60}")


def test_health(client: GatewayClient) -> bool:
    section("1. Health Check")
    ok = client.health()
    print(f"  Gateway health: {'OK' if ok else 'FAILED'}")
    return ok


def test_pool_lifecycle(pool_mgr: WarmPoolManager) -> bool:
    section("2. WarmPool Management")

    # Create pool
    print(f"  Creating pool '{POOL_NAME}' with image '{POOL_IMAGE}'...")
    try:
        pool_mgr.create_warmpool(name=POOL_NAME, image=POOL_IMAGE, replicas=3)
        print("  Pool created.")
    except GatewayError as e:
        if "already exists" in str(e):
            print("  Pool already exists, continuing.")
        else:
            print(f"  FAILED to create pool: {e}")
            return False

    # Wait for pool to be ready (with automatic error detection)
    print("  Waiting for pool to have ready replicas...")
    try:
        info = pool_mgr.wait_for_ready(POOL_NAME, timeout=300.0, poll_interval=5.0)
        print(
            f"  Pool is ready! replicas={info.replicas} "
            f"ready={info.ready_replicas} allocated={info.allocated_replicas}"
        )
        return True
    except PoolNotReadyError as e:
        print(f"  FAILED - Pool has failing pods: {e}")
        # Show conditions for diagnosis
        for cond in e.conditions:
            print(f"    {cond.type}={cond.status}: {cond.message}")
        return False
    except TimeoutError as e:
        print(f"  TIMEOUT: {e}")
        return False


def test_basic_execution() -> bool:
    section("3. Basic Execution")

    with SandboxSession(
        pool_ref=POOL_NAME,
        namespace=NAMESPACE,
        gateway_url=GATEWAY_URL,
    ) as session:
        print(f"  Session created: {session.session_id}")
        print(f"  Pod: {session.session_info.pod_name} ({session.session_info.pod_ip})")

        # Execute command steps
        result = session.execute(
            [
                {"name": "echo", "command": ["echo", "hello world"]},
                {"name": "uname", "command": ["uname", "-a"]},
            ]
        )

        print(f"\n  Step results ({len(result.results)} steps):")
        for r in result.results:
            print(f"    [{r.index}] {r.name}: exit_code={r.output.exit_code}")
            if r.output.stdout:
                print(f"         stdout: {r.output.stdout.strip()[:80]}")
            if r.output.stderr:
                print(f"         stderr: {r.output.stderr.strip()[:80]}")
            if r.snapshot_id:
                print(f"         snapshot: {r.snapshot_id}")

        # Write file via shell command, then run it
        result2 = session.execute(
            [
                {
                    "name": "write_file",
                    "command": [
                        "sh",
                        "-c",
                        "printf '#!/bin/sh\\necho Hello from ARL!\\n' > /workspace/hello.sh",
                    ],
                },
                {
                    "name": "run_file",
                    "command": ["sh", "/workspace/hello.sh"],
                },
                {"name": "apt", "command": ["apt", "--help"]},
            ]
        )

        print("\n  Write + run results:")
        for r in result2.results:
            print(f"    [{r.index}] {r.name}: exit_code={r.output.exit_code}")
            if r.output.stdout:
                print(f"         stdout: {r.output.stdout.strip()[:80]}")
            if r.output.stderr:
                print(f"         stderr: {r.output.stderr.strip()[:80]}")

        ok = all(r.output.exit_code == 0 for r in result.results + result2.results)
        print(f"\n  Basic execution: {'PASSED' if ok else 'FAILED'}")
        return ok


def test_snapshot_restore() -> bool:
    section("4. Snapshot & Restore")

    with SandboxSession(
        pool_ref=POOL_NAME,
        namespace=NAMESPACE,
        gateway_url=GATEWAY_URL,
    ) as session:
        print(f"  Session: {session.session_id}")

        # Step 1: create a file
        r1 = session.execute(
            [
                {
                    "name": "create_v1",
                    "command": ["sh", "-c", "printf 'version=1\\n' > /workspace/data.txt"],
                },
            ]
        )
        snap1 = r1.results[0].snapshot_id
        print(f"  Step 1 (create v1): snapshot={snap1}")

        # Step 2: overwrite the file
        r2 = session.execute(
            [
                {
                    "name": "create_v2",
                    "command": ["sh", "-c", "printf 'version=2\\n' > /workspace/data.txt"],
                },
            ]
        )
        snap2 = r2.results[0].snapshot_id
        print(f"  Step 2 (create v2): snapshot={snap2}")

        # Verify current state is v2
        check = session.execute(
            [
                {"name": "check_v2", "command": ["cat", "/workspace/data.txt"]},
            ]
        )
        current = check.results[0].output.stdout.strip()
        print(f"  Current content: '{current}'")

        # Restore to step 1 (replay-based: creates new pod transparently)
        print(f"  Restoring to snapshot {snap1}...")
        session.restore(snap1)

        # Verify restored state is v1
        check2 = session.execute(
            [
                {"name": "check_v1", "command": ["cat", "/workspace/data.txt"]},
            ]
        )
        restored = check2.results[0].output.stdout.strip()
        print(f"  Restored content: '{restored}'")

        ok = current == "version=2" and restored == "version=1"
        print(f"\n  Snapshot/Restore: {'PASSED' if ok else 'FAILED'}")
        return ok


def test_history_trajectory() -> bool:
    section("5. History & Trajectory Export")

    with SandboxSession(
        pool_ref=POOL_NAME,
        namespace=NAMESPACE,
        gateway_url=GATEWAY_URL,
    ) as session:
        print(f"  Session: {session.session_id}")

        session.execute(
            [
                {"name": "step_a", "command": ["echo", "aaa"]},
                {"name": "step_b", "command": ["echo", "bbb"]},
            ]
        )

        session.execute(
            [
                {"name": "step_c", "command": ["echo", "ccc"]},
            ]
        )

        # Get history
        history = session.get_history()
        print(f"  History entries: {len(history)}")
        for h in history:
            print(
                f"    [{h.index}] {h.name}: snapshot={h.snapshot_id if h.snapshot_id else 'none'}"
            )

        # Export trajectory
        trajectory = session.export_trajectory()
        lines = [line for line in trajectory.strip().split("\n") if line]
        print(f"\n  Trajectory JSONL lines: {len(lines)}")
        for line in lines:
            entry = json.loads(line)
            print(entry)

        ok = len(history) == 3 and len(lines) == 3
        print(f"\n  History/Trajectory: {'PASSED' if ok else 'FAILED'}")
        return ok


def test_interactive_shell() -> bool:
    section("6. Interactive Shell (WebSocket)")

    with SandboxSession(
        pool_ref=POOL_NAME,
        namespace=NAMESPACE,
        gateway_url=GATEWAY_URL,
    ) as session:
        print(f"  Session: {session.session_id}")

        shell = InteractiveShellClient(gateway_url=GATEWAY_URL)
        try:
            shell.connect(session.session_id)
            print("  WebSocket connected.")

            # Send a command
            shell.send_input("echo 'shell-test-ok'\n")
            time.sleep(1)

            # Read output
            output = ""
            for _ in range(10):
                chunk = shell.read_output(timeout=0.5)
                if chunk:
                    output += chunk

            print(f"  Shell output: {output.strip()[:100]}")

            ok = "shell-test-ok" in output
            print(f"\n  Interactive Shell: {'PASSED' if ok else 'FAILED'}")
            return ok
        except ImportError:
            print("  SKIPPED (websockets not installed)")
            return True
        except Exception as e:
            print(f"  FAILED: {e}")
            return False
        finally:
            shell.close()


def test_pool_cleanup(pool_mgr: WarmPoolManager) -> None:
    section("7. Cleanup")
    try:
        pool_mgr.delete_warmpool(POOL_NAME)
        print(f"  Pool '{POOL_NAME}' deleted.")
    except GatewayError as e:
        print(f"  Pool cleanup: {e}")


def main() -> None:
    print("ARL SDK Integration Tests")
    print(f"Gateway: {GATEWAY_URL}")
    print(f"Pool: {POOL_NAME} (image: {POOL_IMAGE})")
    print(f"Namespace: {NAMESPACE}")

    client = GatewayClient(base_url=GATEWAY_URL)
    pool_mgr = WarmPoolManager(namespace=NAMESPACE, gateway_url=GATEWAY_URL)

    results: dict[str, bool] = {}

    # 1. Health
    results["health"] = test_health(client)
    if not results["health"]:
        print("\nGateway not reachable. Aborting.")
        sys.exit(1)

    # 2. Pool
    results["pool"] = test_pool_lifecycle(pool_mgr)
    if not results["pool"]:
        print("\nPool not ready. Aborting.")
        sys.exit(1)

    # 3-6. API tests
    results["execution"] = test_basic_execution()
    results["snapshot"] = test_snapshot_restore()
    results["history"] = test_history_trajectory()

    # 7. Cleanup (optional)
    # test_pool_cleanup(pool_mgr)

    # Summary
    section("Results")
    all_pass = True
    for name, passed in results.items():
        status = "PASS" if passed else "FAIL"
        print(f"  {name:20s} {status}")
        if not passed:
            all_pass = False

    print(f"\n{'ALL TESTS PASSED' if all_pass else 'SOME TESTS FAILED'}")
    sys.exit(0 if all_pass else 1)


if __name__ == "__main__":
    main()
