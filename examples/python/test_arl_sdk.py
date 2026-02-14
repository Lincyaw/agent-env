"""Comprehensive integration tests for ARL Python SDK.

Tests all major features:
1. Gateway health check
2. WarmPool management (create, wait, monitor)
3. Basic command execution
4. Snapshot & restore mechanism
5. Session history & trajectory export
6. Tool provisioning and calling
7. Interactive shell (WebSocket)

Prerequisites:
    - ARL operator + gateway deployed to Kubernetes
    - Gateway accessible (port-forward or direct access)
    - Sufficient resources for test pools

Usage:
    # Basic run
    uv run python examples/python/test_arl_sdk.py

    # Verbose output with details
    uv run python examples/python/test_arl_sdk.py --verbose

    # Custom gateway URL
    uv run python examples/python/test_arl_sdk.py --gateway-url http://localhost:8080

    # Keep pool after tests (for debugging)
    uv run python examples/python/test_arl_sdk.py --skip-cleanup
"""

from __future__ import annotations

import argparse
import json
import sys
import time

from arl import (
    GatewayClient,
    GatewayError,
    InteractiveShellClient,
    PoolNotReadyError,
    ResourceRequirements,
    SandboxSession,
    WarmPoolManager,
)
from arl.types import InlineToolSpec, ToolsSpec
from rich.console import Console
from rich.panel import Panel
from rich.progress import Progress, SpinnerColumn, TextColumn
from rich.table import Table

# Configuration
DEFAULT_GATEWAY_URL = "http://localhost:8080"
DEFAULT_POOL_NAME = "test-pool"
DEFAULT_POOL_IMAGE = "pair-diag-cn-guangzhou.cr.volces.com/pair/ubuntu:22.04"
NAMESPACE = "arl"
TOOLS_POOL_NAME = "tools-demo-pool"

# Rich console for beautiful output
console = Console()


class TestResult:
    """Result of a test run."""

    def __init__(self, name: str, passed: bool, duration: float, skipped: bool = False):
        self.name = name
        self.passed = passed
        self.duration = duration
        self.skipped = skipped


def print_header(args: argparse.Namespace) -> None:
    """Print test suite header."""
    console.print()

    resource_info = ""
    if args.cpu_request or args.memory_request or args.cpu_limit or args.memory_limit:
        resource_info = "\nResources:\n"
        if args.cpu_request:
            resource_info += f"  CPU request: [yellow]{args.cpu_request}[/yellow]\n"
        if args.memory_request:
            resource_info += f"  Memory request: [yellow]{args.memory_request}[/yellow]\n"
        if args.cpu_limit:
            resource_info += f"  CPU limit: [yellow]{args.cpu_limit}[/yellow]\n"
        if args.memory_limit:
            resource_info += f"  Memory limit: [yellow]{args.memory_limit}[/yellow]"

    workspace_info = ""
    if args.workspace_dir != "/workspace":
        workspace_info = f"\nWorkspace: [yellow]{args.workspace_dir}[/yellow]"

    console.print(
        Panel.fit(
            f"[bold cyan]ARL SDK Integration Tests[/bold cyan]\n\n"
            f"Gateway: [yellow]{args.gateway_url}[/yellow]\n"
            f"Pool: [yellow]{args.pool_name}[/yellow]\n"
            f"Image: [yellow]{args.pool_image}[/yellow]\n"
            f"Namespace: [yellow]{NAMESPACE}[/yellow]"
            f"{resource_info}{workspace_info}",
            border_style="cyan",
        )
    )
    console.print()


def test_health(client: GatewayClient, verbose: bool) -> tuple[bool, float]:
    """Test 1: Gateway health check."""
    start = time.time()

    ok = client.health()
    duration = time.time() - start

    if verbose:
        status = "[green]OK[/green]" if ok else "[red]FAILED[/red]"
        console.print(f"  Gateway health: {status}")

    if not ok:
        console.print(
            "[red]Gateway not reachable. Please check:[/red]\n"
            "  1. Gateway is deployed to Kubernetes\n"
            "  2. Port-forward is running:\n"
            "     [yellow]kubectl port-forward -n arl svc/arl-operator-gateway 8080:8080[/yellow]"
        )

    return ok, duration


def test_pool_lifecycle(
    pool_mgr: WarmPoolManager,
    pool_name: str,
    pool_image: str,
    verbose: bool,
    resources: ResourceRequirements | None = None,
    workspace_dir: str = "/workspace",
) -> tuple[bool, float]:
    """Test 2: WarmPool creation and readiness."""
    start = time.time()

    # Create pool
    if verbose:
        console.print(
            f"  Creating pool '[cyan]{pool_name}[/cyan]' with image '[cyan]{pool_image}[/cyan]'..."
        )
        if resources:
            console.print(f"  Resources: {resources.model_dump(exclude_none=True)}")
        if workspace_dir != "/workspace":
            console.print(f"  Workspace: {workspace_dir}")

    try:
        pool_mgr.create_warmpool(
            name=pool_name,
            image=pool_image,
            replicas=3,
            resources=resources,
            workspace_dir=workspace_dir,
        )
        if verbose:
            console.print("  [green]✓[/green] Pool created")
    except GatewayError as e:
        if "already exists" in str(e):
            if verbose:
                console.print("  [yellow]Pool already exists, continuing[/yellow]")
        else:
            console.print(f"  [red]✗ Failed to create pool: {e}[/red]")
            return False, time.time() - start

    # Wait for pool to be ready
    if verbose:
        console.print("  Waiting for pool to have ready replicas...")

    try:
        info = pool_mgr.wait_for_ready(pool_name, timeout=300.0, poll_interval=5.0)
        if verbose:
            console.print(
                f"  [green]✓[/green] Pool ready: "
                f"replicas={info.replicas} "
                f"ready={info.ready_replicas} "
                f"allocated={info.allocated_replicas}"
            )
        duration = time.time() - start
        return True, duration
    except PoolNotReadyError as e:
        console.print(f"  [red]✗ Pool has failing pods: {e}[/red]")
        if verbose:
            for cond in e.conditions:
                console.print(f"    {cond.type}={cond.status}: {cond.message}")
        return False, time.time() - start
    except TimeoutError as e:
        console.print(f"  [red]✗ Timeout: {e}[/red]")
        console.print(
            "  Check pod status with:\n"
            f"    [yellow]kubectl get pods -n {NAMESPACE} -l warmpool={pool_name}[/yellow]"
        )
        return False, time.time() - start


def test_basic_execution(gateway_url: str, pool_name: str, verbose: bool) -> tuple[bool, float]:
    """Test 3: Basic command execution."""
    start = time.time()

    with SandboxSession(
        pool_ref=pool_name,
        namespace=NAMESPACE,
        gateway_url=gateway_url,
    ) as session:
        if verbose:
            console.print(f"  Session: [cyan]{session.session_id}[/cyan]")
            info = session.session_info
            if info:
                console.print(f"  Pod: [cyan]{info.pod_name}[/cyan] ({info.pod_ip})")

        # Execute basic commands
        result = session.execute(
            [
                {"name": "echo", "command": ["echo", "hello world"]},
                {"name": "uname", "command": ["uname", "-a"]},
            ]
        )

        if verbose:
            console.print(f"\n  Step results ({len(result.results)} steps):")
            for r in result.results:
                exit_style = "green" if r.output.exit_code == 0 else "red"
                console.print(
                    f"    [{r.index}] {r.name}: exit_code=[{exit_style}]{r.output.exit_code}[/{exit_style}]"
                )
                if r.output.stdout and verbose:
                    console.print(f"         stdout: {r.output.stdout.strip()[:80]}")
                if r.output.stderr and verbose:
                    console.print(f"         stderr: {r.output.stderr.strip()[:80]}")

        # Write and execute script
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
                {"name": "run_file", "command": ["sh", "/workspace/hello.sh"]},
                {"name": "apt", "command": ["apt", "--help"]},
            ]
        )

        if verbose:
            console.print("\n  Write + run results:")
            for r in result2.results:
                exit_style = "green" if r.output.exit_code == 0 else "red"
                console.print(
                    f"    [{r.index}] {r.name}: exit_code=[{exit_style}]{r.output.exit_code}[/{exit_style}]"
                )
                if r.output.stdout and verbose:
                    console.print(f"         stdout: {r.output.stdout.strip()[:80]}")

        ok = all(r.output.exit_code == 0 for r in result.results + result2.results)
        duration = time.time() - start
        return ok, duration


def test_snapshot_restore(gateway_url: str, pool_name: str, verbose: bool) -> tuple[bool, float]:
    """Test 4: Snapshot and restore mechanism."""
    start = time.time()

    with SandboxSession(
        pool_ref=pool_name,
        namespace=NAMESPACE,
        gateway_url=gateway_url,
    ) as session:
        if verbose:
            console.print(f"  Session: [cyan]{session.session_id}[/cyan]")

        # Create version 1
        r1 = session.execute(
            [
                {
                    "name": "create_v1",
                    "command": ["sh", "-c", "printf 'version=1\\n' > /workspace/data.txt"],
                },
            ]
        )
        snap1 = r1.results[0].snapshot_id
        if verbose:
            console.print(f"  Step 1 (create v1): snapshot=[cyan]{snap1}[/cyan]")

        # Create version 2
        r2 = session.execute(
            [
                {
                    "name": "create_v2",
                    "command": ["sh", "-c", "printf 'version=2\\n' > /workspace/data.txt"],
                },
            ]
        )
        snap2 = r2.results[0].snapshot_id
        if verbose:
            console.print(f"  Step 2 (create v2): snapshot=[cyan]{snap2}[/cyan]")

        # Verify current state is v2
        check = session.execute(
            [
                {"name": "check_v2", "command": ["cat", "/workspace/data.txt"]},
            ]
        )
        current = check.results[0].output.stdout.strip()
        if verbose:
            console.print(f"  Current content: '[yellow]{current}[/yellow]'")

        # Restore to snapshot 1
        if verbose:
            console.print(f"  Restoring to snapshot [cyan]{snap1}[/cyan]...")
        session.restore(snap1)

        # Verify restored state is v1
        check2 = session.execute(
            [
                {"name": "check_v1", "command": ["cat", "/workspace/data.txt"]},
            ]
        )
        restored = check2.results[0].output.stdout.strip()
        if verbose:
            console.print(f"  Restored content: '[yellow]{restored}[/yellow]'")

        ok = current == "version=2" and restored == "version=1"
        duration = time.time() - start
        return ok, duration


def test_history_trajectory(gateway_url: str, pool_name: str, verbose: bool) -> tuple[bool, float]:
    """Test 5: Session history and trajectory export."""
    start = time.time()

    with SandboxSession(
        pool_ref=pool_name,
        namespace=NAMESPACE,
        gateway_url=gateway_url,
    ) as session:
        if verbose:
            console.print(f"  Session: [cyan]{session.session_id}[/cyan]")

        # Execute some commands
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
        if verbose:
            console.print(f"  History entries: {len(history)}")
            for h in history:
                snap_str = f"snapshot={h.snapshot_id}" if h.snapshot_id else "no snapshot"
                console.print(f"    [{h.index}] {h.name}: {snap_str}")

        # Export trajectory
        trajectory = session.export_trajectory()
        lines = [line for line in trajectory.strip().split("\n") if line]
        if verbose:
            console.print(f"\n  Trajectory JSONL lines: {len(lines)}")
            for line in lines[:3]:  # Show first 3 lines
                entry = json.loads(line)
                step = entry.get("step", "?")
                snapshot = entry.get("snapshot_id", "none")
                action = entry.get("action", {})
                observation = entry.get("observation", {})

                # Extract command from action
                cmd = action.get("command", []) if isinstance(action, dict) else []
                cmd_str = " ".join(cmd[:3]) if cmd else "N/A"

                # Extract exit code from observation
                exit_code = (
                    observation.get("exit_code", "?") if isinstance(observation, dict) else "?"
                )

                console.print(
                    f"    step {step}: cmd=[cyan]{cmd_str}[/cyan] exit=[yellow]{exit_code}[/yellow] snapshot={snapshot}"
                )

        ok = len(history) == 3 and len(lines) == 3
        duration = time.time() - start
        return ok, duration


def test_tool_provisioning(
    pool_mgr: WarmPoolManager, gateway_url: str, verbose: bool
) -> tuple[bool, float]:
    """Test 6: Tool provisioning and calling."""
    start = time.time()

    # Define inline tool
    tools = ToolsSpec(
        inline=[
            InlineToolSpec(
                name="greet",
                description="Return a greeting message",
                parameters={"type": "object", "properties": {"name": {"type": "string"}}},
                runtime="bash",
                entrypoint="run.sh",
                timeout="10s",
                files={
                    "run.sh": (
                        "#!/bin/sh\n"
                        "read input\n"
                        '# Simple extraction without jq: get value of "name" key\n'
                        'name=$(echo "$input" | sed -n \'s/.*"name"[[:space:]]*:[[:space:]]*"\\([^"]*\\)".*/\\1/p\')\n'
                        '[ -z "$name" ] && name="world"\n'
                        'printf \'{"message": "hello %s"}\\n\' "$name"\n'
                    ),
                },
            ),
        ],
    )

    # Create temporary pool with tools
    if verbose:
        console.print(f"  Creating tools pool '[cyan]{TOOLS_POOL_NAME}[/cyan]'...")

    try:
        pool_mgr.create_warmpool(
            name=TOOLS_POOL_NAME,
            image="pair-diag-cn-guangzhou.cr.volces.com/pair/ubuntu:22.04",
            replicas=1,
            tools=tools,
        )
        if verbose:
            console.print("  Waiting for tools pool to be ready...")
        pool_mgr.wait_for_ready(TOOLS_POOL_NAME, timeout=300.0)
        if verbose:
            console.print("  [green]✓[/green] Tools pool ready")
    except Exception as e:
        console.print(f"  [red]✗ Failed to create tools pool: {e}[/red]")
        return False, time.time() - start

    # Use session to discover and call tools
    try:
        with SandboxSession(
            pool_ref=TOOLS_POOL_NAME,
            namespace=NAMESPACE,
            gateway_url=gateway_url,
        ) as session:
            # Discover tools
            registry = session.list_tools()
            if verbose:
                console.print(f"  Available tools: {[t.name for t in registry.tools]}")
                for tool in registry.tools:
                    console.print(f"    - {tool.name}: {tool.description} (runtime={tool.runtime})")

            # Call tool
            result = session.call_tool("greet", {"name": "ARL"})
            if verbose:
                console.print(f"  Tool result: {result.parsed}")
                console.print(f"  Exit code: {result.exit_code}")

            ok = result.exit_code == 0 and "hello arl" in str(result.parsed).lower()
            duration = time.time() - start
            return ok, duration
    except Exception as e:
        console.print(f"  [red]✗ Tool execution failed: {e}[/red]")
        return False, time.time() - start
    finally:
        # Cleanup tools pool
        try:
            pool_mgr.delete_warmpool(TOOLS_POOL_NAME)
            if verbose:
                console.print("  [green]✓[/green] Tools pool cleaned up")
        except Exception:
            pass  # Ignore cleanup errors


def test_interactive_shell(gateway_url: str, pool_name: str, verbose: bool) -> tuple[bool, float]:
    """Test 7: Interactive shell via WebSocket."""
    start = time.time()

    try:
        # Check if websockets is available
        import websockets  # noqa: F401
    except ImportError:
        if verbose:
            console.print("  [yellow]SKIPPED[/yellow] - websockets not installed")
            console.print("  Install with: [cyan]uv add websockets[/cyan]")
        return True, 0.0  # Not a failure, just skipped

    try:
        with SandboxSession(
            pool_ref=pool_name,
            namespace=NAMESPACE,
            gateway_url=gateway_url,
        ) as session:
            if verbose:
                console.print(f"  Session: [cyan]{session.session_id}[/cyan]")
            assert session.session_id is not None
            shell = InteractiveShellClient(gateway_url=gateway_url)
            try:
                shell.connect(session.session_id)
                if verbose:
                    console.print("  [green]✓[/green] WebSocket connected")

                # Send command
                shell.send_input("echo 'shell-test-ok'\n")
                time.sleep(1)

                # Read output
                output = ""
                for _ in range(10):
                    chunk = shell.read_output(timeout=0.5)
                    if chunk:
                        output += chunk

                if verbose:
                    console.print(f"  Shell output: {output.strip()[:100]}")

                ok = "shell-test-ok" in output
                duration = time.time() - start
                return ok, duration
            finally:
                shell.close()
    except Exception as e:
        console.print(f"  [red]✗ Interactive shell failed: {e}[/red]")
        return False, time.time() - start


def test_keep_alive_reattach(gateway_url: str, pool_name: str, verbose: bool) -> tuple[bool, float]:
    """Test 8: Keep-alive session and reattach by session ID."""
    start = time.time()

    # Phase 1: create a keep_alive session, write data, exit without cleanup
    session1 = SandboxSession(
        pool_ref=pool_name,
        namespace=NAMESPACE,
        gateway_url=gateway_url,
        keep_alive=True,
    )
    session1.create_sandbox()
    sid = session1.session_id
    assert sid is not None
    if verbose:
        console.print(f"  Phase 1 - created session: [cyan]{sid}[/cyan]")

    r1 = session1.execute(
        [{"name": "write", "command": ["sh", "-c", "echo persist-ok > /workspace/flag.txt"]}]
    )
    if verbose:
        console.print(f"  Phase 1 - wrote flag: exit_code={r1.results[0].output.exit_code}")
    # Intentionally do NOT call delete_sandbox — session stays alive

    # Phase 2: reattach using session ID
    try:
        session2 = SandboxSession.attach(sid, gateway_url=gateway_url)
    except Exception as e:
        console.print(f"  [red]\u2717 Failed to attach: {e}[/red]")
        # Cleanup
        session1.delete_sandbox()
        return False, time.time() - start

    if verbose:
        info = session2.session_info
        pod = info.pod_name if info else "unknown"
        console.print(f"  Phase 2 - attached to session: [cyan]{sid}[/cyan] (pod={pod})")

    r2 = session2.execute([{"name": "read", "command": ["cat", "/workspace/flag.txt"]}])
    content = r2.results[0].output.stdout.strip()
    if verbose:
        console.print(f"  Phase 2 - read flag: '[yellow]{content}[/yellow]'")

    # Phase 3: verify history spans both phases
    history = session2.get_history()
    if verbose:
        console.print(f"  Phase 2 - history entries: {len(history)}")

    # Cleanup
    session2.delete_sandbox()
    if verbose:
        console.print("  [green]\u2713[/green] Session cleaned up")

    ok = content == "persist-ok" and len(history) >= 2
    return ok, time.time() - start


def print_summary(results: list[TestResult]) -> None:
    """Print test results summary table."""
    table = Table(title="\n[bold]Test Results[/bold]", show_header=True, header_style="bold cyan")
    table.add_column("Test", style="white", width=25)
    table.add_column("Status", justify="center", width=10)
    table.add_column("Duration", justify="right", width=12)

    passed_count = 0
    total_duration = 0.0

    for result in results:
        if result.skipped:
            status = "[yellow]SKIP[/yellow]"
        elif result.passed:
            status = "[green]✓[/green]"
            passed_count += 1
        else:
            status = "[red]✗[/red]"

        duration_str = f"{result.duration:.2f}s"
        total_duration += result.duration

        table.add_row(result.name, status, duration_str)

    # Add summary row
    table.add_section()
    non_skipped = [r for r in results if not r.skipped]
    summary = f"{passed_count}/{len(non_skipped)} PASSED • Total: {total_duration:.2f}s"
    table.add_row("[bold]Summary[/bold]", "", f"[bold]{summary}[/bold]")

    console.print(table)


def main() -> None:
    """Run all tests."""
    parser = argparse.ArgumentParser(
        description="Comprehensive ARL SDK integration tests",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument(
        "--verbose", "-v", action="store_true", help="Show detailed output for each test step"
    )
    parser.add_argument(
        "--gateway-url",
        default=DEFAULT_GATEWAY_URL,
        help=f"Gateway URL (default: {DEFAULT_GATEWAY_URL})",
    )
    parser.add_argument(
        "--pool-name",
        default=DEFAULT_POOL_NAME,
        help=f"WarmPool name (default: {DEFAULT_POOL_NAME})",
    )
    parser.add_argument(
        "--pool-image",
        default=DEFAULT_POOL_IMAGE,
        help=f"Pool image (default: {DEFAULT_POOL_IMAGE})",
    )
    parser.add_argument(
        "--skip-cleanup", action="store_true", help="Skip pool cleanup after tests (for debugging)"
    )
    parser.add_argument(
        "--cpu-request",
        help="CPU request (e.g., '100m', '0.5', '1')",
    )
    parser.add_argument(
        "--memory-request",
        help="Memory request (e.g., '128Mi', '512Mi', '1Gi')",
    )
    parser.add_argument(
        "--cpu-limit",
        help="CPU limit (e.g., '1', '2', '4')",
    )
    parser.add_argument(
        "--memory-limit",
        help="Memory limit (e.g., '1Gi', '2Gi', '4Gi')",
    )
    parser.add_argument(
        "--workspace-dir",
        default="/workspace",
        help="Workspace directory mount path (default: /workspace)",
    )

    args = parser.parse_args()

    # Build resource requirements if any resource args provided
    resources = None
    if args.cpu_request or args.memory_request or args.cpu_limit or args.memory_limit:
        requests = {}
        limits = {}
        if args.cpu_request:
            requests["cpu"] = args.cpu_request
        if args.memory_request:
            requests["memory"] = args.memory_request
        if args.cpu_limit:
            limits["cpu"] = args.cpu_limit
        if args.memory_limit:
            limits["memory"] = args.memory_limit

        try:
            resources = ResourceRequirements(requests=requests, limits=limits)
            if args.verbose:
                console.print(
                    f"\n[dim]Resource requirements: {resources.model_dump(exclude_none=True)}[/dim]"
                )
        except ValueError as e:
            console.print(f"\n[red]Invalid resource specification: {e}[/red]")
            sys.exit(1)

    # Print header
    print_header(args)

    # Initialize clients
    client = GatewayClient(base_url=args.gateway_url)
    pool_mgr = WarmPoolManager(namespace=NAMESPACE, gateway_url=args.gateway_url)

    results: list[TestResult] = []
    test_count = 8

    # Test 1: Health check
    with Progress(
        SpinnerColumn(),
        TextColumn("[progress.description]{task.description}"),
        console=console,
    ) as progress:
        task = progress.add_task(f"[1/{test_count}] Health Check", total=None)
        passed, duration = test_health(client, args.verbose)
        progress.update(task, completed=True)

    status = "[green]✓[/green]" if passed else "[red]✗[/red]"
    console.print(f"[1/{test_count}] {status} Health Check ([cyan]{duration:.2f}s[/cyan])")
    results.append(TestResult("Health Check", passed, duration))

    if not passed:
        console.print("\n[red]Gateway not reachable. Aborting.[/red]")
        sys.exit(1)

    # Test 2: Pool lifecycle
    with Progress(
        SpinnerColumn(),
        TextColumn("[progress.description]{task.description}"),
        console=console,
    ) as progress:
        task = progress.add_task(f"[2/{test_count}] WarmPool Management", total=None)
        passed, duration = test_pool_lifecycle(
            pool_mgr,
            args.pool_name,
            args.pool_image,
            args.verbose,
            resources=resources,
            workspace_dir=args.workspace_dir,
        )
        progress.update(task, completed=True)

    status = "[green]✓[/green]" if passed else "[red]✗[/red]"
    console.print(f"[2/{test_count}] {status} WarmPool Management ([cyan]{duration:.2f}s[/cyan])")
    results.append(TestResult("WarmPool Management", passed, duration))

    if not passed:
        console.print("\n[red]Pool not ready. Aborting.[/red]")
        sys.exit(1)

    # Test 3: Basic execution
    with Progress(
        SpinnerColumn(),
        TextColumn("[progress.description]{task.description}"),
        console=console,
    ) as progress:
        task = progress.add_task(f"[3/{test_count}] Basic Execution", total=None)
        passed, duration = test_basic_execution(args.gateway_url, args.pool_name, args.verbose)
        progress.update(task, completed=True)

    status = "[green]✓[/green]" if passed else "[red]✗[/red]"
    console.print(f"[3/{test_count}] {status} Basic Execution ([cyan]{duration:.2f}s[/cyan])")
    results.append(TestResult("Basic Execution", passed, duration))

    # Test 4: Snapshot & restore
    with Progress(
        SpinnerColumn(),
        TextColumn("[progress.description]{task.description}"),
        console=console,
    ) as progress:
        task = progress.add_task(f"[4/{test_count}] Snapshot & Restore", total=None)
        passed, duration = test_snapshot_restore(args.gateway_url, args.pool_name, args.verbose)
        progress.update(task, completed=True)

    status = "[green]✓[/green]" if passed else "[red]✗[/red]"
    console.print(f"[4/{test_count}] {status} Snapshot & Restore ([cyan]{duration:.2f}s[/cyan])")
    results.append(TestResult("Snapshot & Restore", passed, duration))

    # Test 5: History & trajectory
    with Progress(
        SpinnerColumn(),
        TextColumn("[progress.description]{task.description}"),
        console=console,
    ) as progress:
        task = progress.add_task(f"[5/{test_count}] History & Trajectory", total=None)
        passed, duration = test_history_trajectory(args.gateway_url, args.pool_name, args.verbose)
        progress.update(task, completed=True)

    status = "[green]✓[/green]" if passed else "[red]✗[/red]"
    console.print(f"[5/{test_count}] {status} History & Trajectory ([cyan]{duration:.2f}s[/cyan])")
    results.append(TestResult("History & Trajectory", passed, duration))

    # Test 6: Tool provisioning
    with Progress(
        SpinnerColumn(),
        TextColumn("[progress.description]{task.description}"),
        console=console,
    ) as progress:
        task = progress.add_task(f"[6/{test_count}] Tool Provisioning", total=None)
        passed, duration = test_tool_provisioning(pool_mgr, args.gateway_url, args.verbose)
        progress.update(task, completed=True)

    status = "[green]✓[/green]" if passed else "[red]✗[/red]"
    console.print(f"[6/{test_count}] {status} Tool Provisioning ([cyan]{duration:.2f}s[/cyan])")
    results.append(TestResult("Tool Provisioning", passed, duration))

    # Test 7: Interactive shell
    with Progress(
        SpinnerColumn(),
        TextColumn("[progress.description]{task.description}"),
        console=console,
    ) as progress:
        task = progress.add_task(f"[7/{test_count}] Interactive Shell", total=None)
        passed, duration = test_interactive_shell(args.gateway_url, args.pool_name, args.verbose)
        progress.update(task, completed=True)

    if duration == 0.0:
        # Skipped
        console.print(
            f"[7/{test_count}] [yellow]⊘[/yellow] Interactive Shell ([yellow]SKIPPED[/yellow])"
        )
        results.append(TestResult("Interactive Shell", True, duration, skipped=True))
    else:
        status = "[green]✓[/green]" if passed else "[red]✗[/red]"
        console.print(f"[7/{test_count}] {status} Interactive Shell ([cyan]{duration:.2f}s[/cyan])")
        results.append(TestResult("Interactive Shell", passed, duration))

    # Test 8: Keep-alive & reattach
    with Progress(
        SpinnerColumn(),
        TextColumn("[progress.description]{task.description}"),
        console=console,
    ) as progress:
        task = progress.add_task(f"[8/{test_count}] Keep-Alive & Reattach", total=None)
        passed, duration = test_keep_alive_reattach(args.gateway_url, args.pool_name, args.verbose)
        progress.update(task, completed=True)

    status = "[green]\u2713[/green]" if passed else "[red]\u2717[/red]"
    console.print(f"[8/{test_count}] {status} Keep-Alive & Reattach ([cyan]{duration:.2f}s[/cyan])")
    results.append(TestResult("Keep-Alive & Reattach", passed, duration))

    # Cleanup
    if not args.skip_cleanup:
        console.print(f"\n[dim]Cleaning up pool '{args.pool_name}'...[/dim]")
        try:
            pool_mgr.delete_warmpool(args.pool_name)
            console.print(f"[dim]Pool '{args.pool_name}' deleted.[/dim]")
        except Exception as e:
            console.print(f"[yellow]Pool cleanup failed: {e}[/yellow]")
    else:
        console.print("\n[yellow]Skipping cleanup (--skip-cleanup)[/yellow]")

    # Print summary
    print_summary(results)

    # Exit with appropriate code
    all_passed = all(r.passed for r in results if not r.skipped)
    if all_passed:
        console.print("\n[bold green]ALL TESTS PASSED[/bold green]")
        sys.exit(0)
    else:
        console.print("\n[bold red]SOME TESTS FAILED[/bold red]")
        sys.exit(1)


if __name__ == "__main__":
    main()
