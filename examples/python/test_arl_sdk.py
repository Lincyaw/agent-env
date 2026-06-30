"""Integration smoke tests for the sandbox-backed ARL Python SDK.

The suite validates the currently supported gateway surface:

1. Gateway health
2. SandboxWarmPool lifecycle and scale
3. SDK execution through SSE
4. SDK file upload and download
5. Snapshot restore
6. Cross-session replay
7. History and trajectory export
8. Session and pool logs
9. Interactive shell, when the optional websockets package is installed
10. Detach and reattach
11. Managed sessions
12. Optional observability endpoints

Usage:
    uv run python examples/python/test_arl_sdk.py \
      --gateway-url http://127.0.0.1:18080 \
      --pool-image busybox:latest

    uv run python examples/python/test_arl_sdk.py \
      --gateway-url http://127.0.0.1:18080 \
      --metrics-url http://127.0.0.1:19091 \
      --prometheus-url http://127.0.0.1:19090 \
      --grafana-url http://127.0.0.1:13000 \
      --clickhouse-url http://127.0.0.1:18123 \
      --clickhouse-user default \
      --clickhouse-password clickhouse123
"""

from __future__ import annotations

import argparse
import contextlib
import hashlib
import json
import os
import sys
import time
from collections.abc import Callable
from dataclasses import dataclass
from typing import Any

import httpx
from arl import (
    GatewayClient,
    InteractiveShellClient,
    ManagedSession,
    ResourceRequirements,
    SandboxSession,
    WarmPoolManager,
)
from rich.console import Console
from rich.panel import Panel
from rich.progress import Progress, SpinnerColumn, TextColumn
from rich.table import Table

DEFAULT_GATEWAY_URL = "http://localhost:8080"
DEFAULT_POOL_IMAGE = "busybox:latest"

console = Console()


class SkipTestError(Exception):
    """Raised by a test when an optional dependency or endpoint is absent."""


@dataclass
class TestResult:
    name: str
    passed: bool
    duration: float
    skipped: bool = False
    detail: str = ""


def print_header(args: argparse.Namespace) -> None:
    resource_lines: list[str] = []
    if args.cpu_request:
        resource_lines.append(f"  CPU request: [yellow]{args.cpu_request}[/yellow]")
    if args.memory_request:
        resource_lines.append(f"  Memory request: [yellow]{args.memory_request}[/yellow]")
    if args.cpu_limit:
        resource_lines.append(f"  CPU limit: [yellow]{args.cpu_limit}[/yellow]")
    if args.memory_limit:
        resource_lines.append(f"  Memory limit: [yellow]{args.memory_limit}[/yellow]")

    observability = [
        value
        for value in (
            args.metrics_url,
            args.prometheus_url,
            args.grafana_url,
            args.clickhouse_url,
        )
        if value
    ]

    console.print()
    console.print(
        Panel.fit(
            "[bold cyan]ARL SDK Integration Tests[/bold cyan]\n\n"
            f"Gateway: [yellow]{args.gateway_url}[/yellow]\n"
            f"Pool: [yellow]{args.pool_name}[/yellow]\n"
            f"Image: [yellow]{args.pool_image}[/yellow]\n"
            f"Replicas: [yellow]{args.pool_replicas}[/yellow]\n"
            f"Workspace: [yellow]{args.workspace_dir}[/yellow]\n"
            f"Observability endpoints: [yellow]{len(observability)}[/yellow]"
            + (("\nResources:\n" + "\n".join(resource_lines)) if resource_lines else ""),
            border_style="cyan",
        )
    )
    console.print()


def build_resources(args: argparse.Namespace) -> ResourceRequirements | None:
    if not (args.cpu_request or args.memory_request or args.cpu_limit or args.memory_limit):
        return None

    requests: dict[str, str] = {}
    limits: dict[str, str] = {}
    if args.cpu_request:
        requests["cpu"] = args.cpu_request
    if args.memory_request:
        requests["memory"] = args.memory_request
    if args.cpu_limit:
        limits["cpu"] = args.cpu_limit
    if args.memory_limit:
        limits["memory"] = args.memory_limit
    return ResourceRequirements(requests=requests, limits=limits)


def normalize_image(image: str) -> str:
    normalized = image.removeprefix("docker.io/library/").removeprefix("docker.io/")
    if ":" not in normalized and "@" not in normalized:
        normalized += ":latest"
    return normalized


def managed_pool_name(image: str, gateway_namespace: str, profile: str) -> str:
    identity = f"{gateway_namespace}/{profile or 'default'}/{normalize_image(image)}"
    digest = hashlib.sha256(identity.encode()).hexdigest()[:12]
    return f"managed-{digest}"


def assert_step_success(result: Any, expected_stdout: str | None = None) -> None:
    if not result.results:
        raise AssertionError("execute returned no step results")
    step = result.results[0]
    if step.output.exit_code != 0:
        raise AssertionError(f"step exited {step.output.exit_code}: {step.output.stderr}")
    if expected_stdout is not None and expected_stdout not in step.output.stdout:
        raise AssertionError(f"stdout {step.output.stdout!r} did not contain {expected_stdout!r}")


def http_get_text(url: str, timeout: float = 20.0, **kwargs: Any) -> str:
    with httpx.Client(timeout=timeout) as client:
        resp = client.get(url, **kwargs)
        resp.raise_for_status()
        return resp.text


def parse_ndjson(raw: str) -> list[dict[str, Any]]:
    return [json.loads(line) for line in raw.splitlines() if line.strip()]


def run_test(
    index: int,
    total: int,
    name: str,
    fn: Callable[[], None],
) -> TestResult:
    start = time.time()
    with Progress(
        SpinnerColumn(),
        TextColumn("[progress.description]{task.description}"),
        console=console,
        transient=True,
    ) as progress:
        task = progress.add_task(f"[{index}/{total}] {name}", total=None)
        try:
            fn()
            progress.update(task, completed=True)
        except SkipTestError as exc:
            progress.update(task, completed=True)
            duration = time.time() - start
            console.print(f"[{index}/{total}] SKIP {name} ({duration:.2f}s) {exc}")
            return TestResult(
                name=name,
                passed=True,
                duration=duration,
                skipped=True,
                detail=str(exc),
            )
        except Exception as exc:
            progress.update(task, completed=True)
            duration = time.time() - start
            console.print(f"[{index}/{total}] FAIL {name} ({duration:.2f}s): {exc}")
            return TestResult(name=name, passed=False, duration=duration, detail=str(exc))

    duration = time.time() - start
    console.print(f"[{index}/{total}] PASS {name} ({duration:.2f}s)")
    return TestResult(name=name, passed=True, duration=duration)


def test_health(client: GatewayClient, args: argparse.Namespace) -> None:
    if not client.health():
        raise AssertionError(
            "gateway health check failed; run kubectl port-forward "
            "-n <gateway-namespace> svc/agent-env-gateway 8080:8080"
        )


def test_pool_lifecycle(
    pool_mgr: WarmPoolManager,
    args: argparse.Namespace,
    resources: ResourceRequirements | None,
) -> None:
    with contextlib.suppress(Exception):
        pool_mgr.delete_warmpool(args.pool_name)
        time.sleep(1)

    pool_mgr.create_warmpool(
        name=args.pool_name,
        image=args.pool_image,
        replicas=args.pool_replicas,
        profile=args.pool_name,
        resources=resources,
        workspace_dir=args.workspace_dir,
    )
    info = pool_mgr.wait_for_ready(
        args.pool_name,
        timeout=args.pool_ready_timeout,
        poll_interval=2.0,
        min_ready=max(1, args.pool_replicas),
    )
    if info.ready_replicas < max(1, args.pool_replicas):
        raise AssertionError(f"pool ready replicas too low: {info.ready_replicas}")

    if args.skip_scale:
        return

    scaled_replicas = max(1, args.pool_replicas) + 1
    scaled = pool_mgr.scale_warmpool(args.pool_name, scaled_replicas)
    if scaled.replicas != scaled_replicas:
        raise AssertionError(f"scale response replicas={scaled.replicas}, want {scaled_replicas}")
    pool_mgr.wait_for_ready(
        args.pool_name,
        timeout=args.pool_ready_timeout,
        poll_interval=2.0,
        min_ready=scaled_replicas,
    )
    restored = pool_mgr.scale_warmpool(args.pool_name, args.pool_replicas)
    if restored.replicas != args.pool_replicas:
        raise AssertionError(f"scale-back replicas={restored.replicas}, want {args.pool_replicas}")


def test_basic_execution_sse(args: argparse.Namespace) -> None:
    chunks: list[str] = []

    def on_output(stdout: str, stderr: str) -> None:
        chunks.append(stdout + stderr)

    with SandboxSession(
        image=args.pool_image,
        profile=args.pool_name,
        gateway_url=args.gateway_url,
    ) as session:
        result = session.execute(
            [
                {"name": "sse", "command": ["sh", "-c", "echo sse-ok && pwd"]},
            ],
            on_output=on_output,
        )
        assert_step_success(result, "sse-ok")
        assert_step_success(result, args.workspace_dir)

    if "sse-ok" not in "".join(chunks):
        raise AssertionError("SSE output callback did not receive command output")


def test_file_upload_download(client: GatewayClient, args: argparse.Namespace) -> None:
    with SandboxSession(
        image=args.pool_image,
        profile=args.pool_name,
        gateway_url=args.gateway_url,
    ) as session:
        assert session.session_id is not None
        text_resp = session.upload_file("nested/text.txt", "text-ok")
        if text_resp.bytes_written != len("text-ok"):
            raise AssertionError(f"unexpected text upload response: {text_resp}")

        raw_bytes = b"\x00arl-bytes\xff"
        client.upload_file(session.session_id, "nested/blob.bin", raw_bytes)

        text = client.download_file(session.session_id, "nested/text.txt").decode()
        blob = client.download_file(session.session_id, "nested/blob.bin")
        if text != "text-ok":
            raise AssertionError(f"downloaded text={text!r}")
        if blob != raw_bytes:
            raise AssertionError(f"downloaded bytes={blob!r}")


def test_snapshot_restore(args: argparse.Namespace) -> None:
    with SandboxSession(
        image=args.pool_image,
        profile=args.pool_name,
        gateway_url=args.gateway_url,
    ) as session:
        first = session.execute(
            [
                {
                    "name": "create_v1",
                    "command": ["sh", "-c", "printf 'version=1\\n' > /workspace/data.txt"],
                },
            ]
        )
        snapshot_id = first.results[0].snapshot_id
        if not snapshot_id:
            raise AssertionError("first step did not return a snapshot id")

        session.execute(
            [
                {
                    "name": "create_v2",
                    "command": ["sh", "-c", "printf 'version=2\\n' > /workspace/data.txt"],
                },
            ]
        )
        current = session.execute([{"name": "check_v2", "command": ["cat", "/workspace/data.txt"]}])
        assert_step_success(current, "version=2")

        session.restore(snapshot_id)
        restored = session.execute(
            [{"name": "check_v1", "command": ["cat", "/workspace/data.txt"]}]
        )
        assert_step_success(restored, "version=1")


def test_replay(client: GatewayClient, args: argparse.Namespace) -> None:
    with (
        SandboxSession(
            image=args.pool_image,
            profile=args.pool_name,
            gateway_url=args.gateway_url,
        ) as source,
        SandboxSession(
            image=args.pool_image,
            profile=args.pool_name,
            gateway_url=args.gateway_url,
        ) as target,
    ):
        assert source.session_id is not None
        assert target.session_id is not None

        source.execute(
            [
                {
                    "name": "create_replay",
                    "command": ["sh", "-c", "printf first > /workspace/replay.txt"],
                },
                {
                    "name": "mutate_replay",
                    "command": ["sh", "-c", "printf second > /workspace/replay.txt"],
                },
            ]
        )

        partial = client.replay_from(target.session_id, source.session_id, up_to_step=0)
        if partial.steps_replayed < 1:
            raise AssertionError(f"partial replay response={partial}")
        partial_check = target.execute(
            [{"name": "check_partial_replay", "command": ["cat", "/workspace/replay.txt"]}]
        )
        assert_step_success(partial_check, "first")

        full = client.replay_from(target.session_id, source.session_id)
        if full.steps_replayed < 2:
            raise AssertionError(f"full replay response={full}")
        full_check = target.execute(
            [{"name": "check_full_replay", "command": ["cat", "/workspace/replay.txt"]}]
        )
        assert_step_success(full_check, "second")


def test_history_trajectory(args: argparse.Namespace) -> None:
    with SandboxSession(
        image=args.pool_image,
        profile=args.pool_name,
        gateway_url=args.gateway_url,
    ) as session:
        session.execute(
            [
                {"name": "step_a", "command": ["echo", "aaa"]},
                {"name": "step_b", "command": ["echo", "bbb"]},
            ]
        )
        session.execute([{"name": "step_c", "command": ["echo", "ccc"]}])

        history = session.get_history()
        trajectory = session.export_trajectory()
        lines = [line for line in trajectory.splitlines() if line.strip()]

        if len(history) < 3:
            raise AssertionError(f"history too short: {len(history)}")
        if len(lines) < 3:
            raise AssertionError(f"trajectory too short: {len(lines)}")
        for line in lines:
            json.loads(line)


def test_logs(args: argparse.Namespace) -> None:
    with SandboxSession(
        image=args.pool_image,
        profile=args.pool_name,
        gateway_url=args.gateway_url,
    ) as session:
        assert session.session_id is not None
        result = session.execute([{"name": "logs", "command": ["sh", "-c", "echo logs-ok"]}])
        assert_step_success(result, "logs-ok")

        session_logs: list[dict[str, Any]] = []
        pool_logs: list[dict[str, Any]] = []
        deadline = time.time() + args.logs_timeout
        while time.time() < deadline:
            with httpx.Client(base_url=args.gateway_url, timeout=20.0) as http:
                session_resp = http.get(
                    f"/v1/sessions/{session.session_id}/logs",
                    params={"tail": 50},
                )
                session_resp.raise_for_status()
                pool_resp = http.get(
                    f"/v1/pools/{args.pool_name}/logs",
                    params={"tail": 50},
                )
                pool_resp.raise_for_status()

            session_logs = parse_ndjson(session_resp.text)
            pool_logs = parse_ndjson(pool_resp.text)
            if session_logs:
                break
            time.sleep(1)

        if not session_logs:
            raise AssertionError("session logs endpoint returned no entries")
        if args.verbose:
            console.print(f"  Session logs: {len(session_logs)}, pool logs: {len(pool_logs)}")


def test_interactive_shell(args: argparse.Namespace) -> None:
    try:
        import websockets  # noqa: F401
    except ImportError as exc:
        raise SkipTestError("optional dependency 'websockets' is not installed") from exc

    with SandboxSession(
        image=args.pool_image,
        profile=args.pool_name,
        gateway_url=args.gateway_url,
    ) as session:
        assert session.session_id is not None
        shell = InteractiveShellClient(gateway_url=args.gateway_url)
        try:
            shell.connect(session.session_id)
            shell.send_input("echo shell-test-ok\n")
            output = ""
            deadline = time.time() + 10.0
            while time.time() < deadline and "shell-test-ok" not in output:
                output += shell.read_output(timeout=0.5)
            if "shell-test-ok" not in output:
                raise AssertionError(f"shell output did not contain marker: {output!r}")
        finally:
            shell.close()


def test_detach_reattach(args: argparse.Namespace) -> None:
    first = SandboxSession(
        image=args.pool_image,
        profile=args.pool_name,
        gateway_url=args.gateway_url,
    )
    first.create_sandbox()
    session_id = first.session_id
    assert session_id is not None

    try:
        result = first.execute(
            [{"name": "write", "command": ["sh", "-c", "echo persist-ok > /workspace/flag.txt"]}]
        )
        assert_step_success(result)
        first.close()

        attached = SandboxSession.attach(
            session_id,
            gateway_url=args.gateway_url,
        )
        try:
            read = attached.execute([{"name": "read", "command": ["cat", "/workspace/flag.txt"]}])
            assert_step_success(read, "persist-ok")
            if len(attached.get_history()) < 2:
                raise AssertionError("reattached session history did not include both steps")
        finally:
            attached.delete_sandbox()
            attached.close()
    except Exception:
        with contextlib.suppress(Exception):
            first.delete_sandbox()
        raise


def test_managed_session(client: GatewayClient, args: argparse.Namespace) -> None:
    experiment_id = f"sdk-{int(time.time())}"
    profile = "default"
    auto_pool = ""

    try:
        with ManagedSession(
            image=args.pool_image,
            experiment_id=experiment_id,
            gateway_url=args.gateway_url,
            profile=profile,
            workspace_dir=args.workspace_dir,
        ) as session:
            if session.session_info is not None:
                auto_pool = managed_pool_name(
                    args.pool_image,
                    session.session_info.namespace,
                    profile,
                )
            result = session.execute([{"name": "managed", "command": ["echo", "managed-ok"]}])
            assert_step_success(result, "managed-ok")

            first = session.execute(
                [{"name": "managed_v1", "command": ["sh", "-c", "echo v1 > /workspace/m.txt"]}]
            )
            snapshot_id = first.results[0].snapshot_id
            session.execute(
                [{"name": "managed_v2", "command": ["sh", "-c", "echo v2 > /workspace/m.txt"]}]
            )
            session.restore(snapshot_id)
            restored = session.execute(
                [{"name": "managed_check", "command": ["cat", "/workspace/m.txt"]}]
            )
            assert_step_success(restored, "v1")

        remaining = client.list_experiment_sessions(experiment_id)
        if remaining:
            raise AssertionError(f"managed session cleanup left {len(remaining)} sessions")
    finally:
        with contextlib.suppress(Exception):
            client.delete_experiment(experiment_id)
        with contextlib.suppress(Exception):
            if auto_pool:
                client.delete_pool(auto_pool)


def test_observability(args: argparse.Namespace) -> None:
    if not any((args.metrics_url, args.prometheus_url, args.grafana_url, args.clickhouse_url)):
        raise SkipTestError("no observability endpoint URLs were provided")

    if args.metrics_url:
        text = http_get_text(args.metrics_url.rstrip("/") + "/metrics")
        required = [
            "arl_session_allocation_seconds",
            "arl_gateway_step_result_total",
            "arl_gateway_step_duration_seconds",
            "arl_gateway_sidecar_call_seconds",
        ]
        missing = [metric for metric in required if metric not in text]
        if missing:
            raise AssertionError(f"gateway metrics missing: {missing}")

    if args.prometheus_url:
        with httpx.Client(base_url=args.prometheus_url.rstrip("/"), timeout=20.0) as http:
            ready = http.get("/-/ready")
            ready.raise_for_status()
            query = http.get(
                "/api/v1/query",
                params={"query": "arl_gateway_step_result_total"},
            )
            query.raise_for_status()
            payload = query.json()
            if payload.get("status") != "success":
                raise AssertionError(f"Prometheus query failed: {payload}")

    if args.grafana_url:
        with httpx.Client(base_url=args.grafana_url.rstrip("/"), timeout=20.0) as http:
            health = http.get("/api/health")
            health.raise_for_status()
            payload = health.json()
            if payload.get("database") != "ok":
                raise AssertionError(f"Grafana health failed: {payload}")

    if args.clickhouse_url:
        auth = None
        user = args.clickhouse_user or os.environ.get("CLICKHOUSE_USER")
        password = args.clickhouse_password or os.environ.get("CLICKHOUSE_PASSWORD")
        if user or password:
            auth = (user or "default", password or "")
        with httpx.Client(
            base_url=args.clickhouse_url.rstrip("/"),
            timeout=20.0,
            auth=auth,
        ) as http:
            resp = http.get("/", params={"query": "SELECT count() FROM arl.trajectory"})
            resp.raise_for_status()
            count = int(resp.text.strip())
            if count < 1:
                raise AssertionError("ClickHouse arl.trajectory has no rows")


def print_summary(results: list[TestResult]) -> None:
    table = Table(title="\nTest Results", show_header=True, header_style="bold cyan")
    table.add_column("Test", style="white")
    table.add_column("Status", justify="center")
    table.add_column("Duration", justify="right")
    table.add_column("Detail", overflow="fold")

    passed = 0
    skipped = 0
    total_duration = 0.0
    for result in results:
        total_duration += result.duration
        if result.skipped:
            status = "[yellow]SKIP[/yellow]"
            skipped += 1
        elif result.passed:
            status = "[green]PASS[/green]"
            passed += 1
        else:
            status = "[red]FAIL[/red]"
        table.add_row(result.name, status, f"{result.duration:.2f}s", result.detail)

    non_skipped = len([result for result in results if not result.skipped])
    table.add_section()
    table.add_row(
        "[bold]Summary[/bold]",
        "",
        f"[bold]{total_duration:.2f}s[/bold]",
        f"[bold]{passed}/{non_skipped} passed, {skipped} skipped[/bold]",
    )
    console.print(table)


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Integration smoke tests for the sandbox-backed ARL SDK",
        formatter_class=argparse.ArgumentDefaultsHelpFormatter,
    )
    parser.add_argument("--verbose", "-v", action="store_true")
    parser.add_argument("--gateway-url", default=DEFAULT_GATEWAY_URL)
    parser.add_argument("--pool-name", default="")
    parser.add_argument("--pool-image", default=DEFAULT_POOL_IMAGE)
    parser.add_argument("--pool-replicas", type=int, default=1)
    parser.add_argument("--pool-ready-timeout", type=float, default=180.0)
    parser.add_argument("--logs-timeout", type=float, default=30.0)
    parser.add_argument("--workspace-dir", default="/workspace")
    parser.add_argument("--skip-scale", action="store_true")
    parser.add_argument("--skip-cleanup", action="store_true")
    parser.add_argument("--cpu-request")
    parser.add_argument("--memory-request")
    parser.add_argument("--cpu-limit")
    parser.add_argument("--memory-limit")
    parser.add_argument("--metrics-url", default="")
    parser.add_argument("--prometheus-url", default="")
    parser.add_argument("--grafana-url", default="")
    parser.add_argument("--clickhouse-url", default="")
    parser.add_argument("--clickhouse-user", default="")
    parser.add_argument("--clickhouse-password", default="")
    args = parser.parse_args()

    if args.pool_replicas < 1:
        console.print("[red]--pool-replicas must be >= 1[/red]")
        return 2
    if not args.pool_name:
        args.pool_name = f"sdk-smoke-{int(time.time())}"

    try:
        resources = build_resources(args)
    except ValueError as exc:
        console.print(f"[red]Invalid resource specification: {exc}[/red]")
        return 2

    print_header(args)

    client = GatewayClient(base_url=args.gateway_url)
    pool_mgr = WarmPoolManager(
        gateway_url=args.gateway_url,
        timeout=args.pool_ready_timeout,
    )

    tests: list[tuple[str, Callable[[], None]]] = [
        ("Gateway Health", lambda: test_health(client, args)),
        ("Pool Lifecycle", lambda: test_pool_lifecycle(pool_mgr, args, resources)),
        ("Execution + SSE", lambda: test_basic_execution_sse(args)),
        ("File Upload/Download", lambda: test_file_upload_download(client, args)),
        ("Snapshot Restore", lambda: test_snapshot_restore(args)),
        ("Replay", lambda: test_replay(client, args)),
        ("History + Trajectory", lambda: test_history_trajectory(args)),
        ("Logs", lambda: test_logs(args)),
        ("Interactive Shell", lambda: test_interactive_shell(args)),
        ("Detach/Reattach", lambda: test_detach_reattach(args)),
        ("Managed Sessions", lambda: test_managed_session(client, args)),
        ("Observability", lambda: test_observability(args)),
    ]

    results: list[TestResult] = []
    try:
        total = len(tests)
        for index, (name, fn) in enumerate(tests, start=1):
            result = run_test(index, total, name, fn)
            results.append(result)
            if name in {"Gateway Health", "Pool Lifecycle"} and not result.passed:
                break
    finally:
        if not args.skip_cleanup:
            with contextlib.suppress(Exception):
                pool_mgr.delete_warmpool(args.pool_name)
        pool_mgr.close()
        client.close()

    print_summary(results)
    all_required_passed = all(result.passed for result in results if not result.skipped)
    return 0 if all_required_passed else 1


if __name__ == "__main__":
    sys.exit(main())
