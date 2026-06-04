"""Performance benchmark for ARL Gateway API.

Usage:
    kubectl port-forward -n arl svc/arl-operator-gateway 8080:8080

    # Run all benchmarks
    uv run python examples/python/bench_gateway.py full

    # Run warmpool scale benchmark (8 pools × 8 replicas)
    uv run python examples/python/bench_gateway.py warmpool-scale

    # Run session creation benchmark
    uv run python examples/python/bench_gateway.py session-bench

    # Run execution benchmark
    uv run python examples/python/bench_gateway.py exec-bench
"""

from __future__ import annotations

import atexit
import contextlib
import json
import math
import re
import shutil
import signal
import statistics
import subprocess
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass
from datetime import datetime
from typing import cast
from urllib.parse import urlparse

import typer
from arl import GatewayClient, GatewayError, WarmPoolManager
from rich.console import Console
from rich.table import Table

app = typer.Typer(help="ARL Gateway performance benchmarks.")
console = Console()

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------
DEFAULT_GATEWAY = "http://localhost:8080"
DEFAULT_IMAGE = "pair-diag-cn-guangzhou.cr.volces.com/code/pillow_final:ffcc0670381f91d6c70d74a059d8d2e296aac678"
DEFAULT_NAMESPACE = "arl"
DEFAULT_SVC = "svc/arl-operator-gateway"
DEFAULT_SVC_PORT = 8080

# Global handle so atexit can clean it up
_port_forward_proc: subprocess.Popen[bytes] | None = None


# ---------------------------------------------------------------------------
# Port-forward helper
# ---------------------------------------------------------------------------


def _cleanup_port_forward() -> None:
    global _port_forward_proc  # noqa: PLW0603
    if _port_forward_proc is not None and _port_forward_proc.poll() is None:
        _port_forward_proc.send_signal(signal.SIGTERM)
        try:
            _port_forward_proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            _port_forward_proc.kill()
        _port_forward_proc = None


def ensure_port_forward(
    gateway_url: str,
    namespace: str,
    svc: str = DEFAULT_SVC,
    svc_port: int = DEFAULT_SVC_PORT,
) -> None:
    """Start ``kubectl port-forward`` in the background if needed.

    Uses the *local* port parsed from ``gateway_url`` and forwards to
    ``svc_port`` on the service.  Waits up to 10 s for the port to become
    reachable before returning.
    """
    global _port_forward_proc  # noqa: PLW0603

    if not shutil.which("kubectl"):
        console.print("[yellow]kubectl not found, skipping port-forward.[/yellow]")
        return

    parsed = urlparse(gateway_url)
    local_port = parsed.port or 8080

    # If something is already listening, do nothing.
    import socket

    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.settimeout(1)
        if s.connect_ex(("127.0.0.1", local_port)) == 0:
            console.print(f"[dim]Port {local_port} already open, skipping port-forward.[/dim]")
            return

    cmd = [
        "kubectl",
        "port-forward",
        "-n",
        namespace,
        svc,
        f"{local_port}:{svc_port}",
    ]
    console.print(f"[cyan]Starting:[/cyan] {' '.join(cmd)}")
    _port_forward_proc = subprocess.Popen(
        cmd,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.PIPE,
    )
    atexit.register(_cleanup_port_forward)

    # Wait for the port to become reachable
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
            s.settimeout(1)
            if s.connect_ex(("127.0.0.1", local_port)) == 0:
                console.print("[green]Port-forward ready.[/green]")
                return
        # Check process hasn't died
        if _port_forward_proc.poll() is not None:
            stderr = (_port_forward_proc.stderr or b"").read().decode(errors="replace")  # type: ignore[union-attr]
            console.print(f"[red]Port-forward failed: {stderr}[/red]")
            raise typer.Exit(code=1)
        time.sleep(0.3)

    raise typer.BadParameter(f"Port-forward did not become ready on port {local_port} within 10s")


# ---------------------------------------------------------------------------
# Common helpers
# ---------------------------------------------------------------------------


class Timer:
    """Context manager that records elapsed time in milliseconds."""

    def __init__(self) -> None:
        self.ms: float = 0.0

    def __enter__(self) -> Timer:
        self._start = time.perf_counter()
        return self

    def __exit__(self, *_: object) -> None:
        self.ms = (time.perf_counter() - self._start) * 1000


def fmt(ms: float) -> str:
    """Format milliseconds to human-readable string."""
    if ms < 1:
        return f"{ms * 1000:.0f}us"
    if ms < 1000:
        return f"{ms:.1f}ms"
    return f"{ms / 1000:.2f}s"


def compute_stats(times_ms: list[float]) -> dict[str, float]:
    """Compute min/avg/med/p95/max from a list of durations in ms."""
    n = len(times_ms)
    if n == 0:
        return {}
    sorted_t = sorted(times_ms)
    return {
        "n": n,
        "min": min(times_ms),
        "avg": statistics.mean(times_ms),
        "med": statistics.median(times_ms),
        "p95": sorted_t[int(n * 0.95)] if n >= 5 else max(times_ms),
        "max": max(times_ms),
        "first": times_ms[0],
    }


def stats_table(title: str, rows: list[tuple[str, list[float]]]) -> Table:
    """Build a Rich table with timing statistics."""
    table = Table(title=title, show_lines=True)
    table.add_column("Label", style="cyan", min_width=30)
    table.add_column("N", justify="right")
    table.add_column("First", justify="right", style="yellow")
    table.add_column("Min", justify="right")
    table.add_column("Avg", justify="right", style="green")
    table.add_column("Med", justify="right")
    table.add_column("P95", justify="right", style="magenta")
    table.add_column("Max", justify="right", style="red")
    for label, times in rows:
        s = compute_stats(times)
        if not s:
            table.add_row(label, "0", "-", "-", "-", "-", "-", "-")
            continue
        table.add_row(
            label,
            str(int(s["n"])),
            fmt(s["first"]),
            fmt(s["min"]),
            fmt(s["avg"]),
            fmt(s["med"]),
            fmt(s["p95"]),
            fmt(s["max"]),
        )
    return table


def safe_cleanup_pool(pool_mgr: WarmPoolManager, name: str) -> None:
    """Delete a pool, ignoring errors if it doesn't exist."""
    try:
        pool_mgr.delete_warmpool(name)
        time.sleep(2)
    except GatewayError:
        pass


def _ensure_pool(
    pool_mgr: WarmPoolManager,
    name: str,
    image: str,
    replicas: int,
    timeout: float,
) -> None:
    """Reuse an existing pool if it has enough ready replicas, otherwise create one.

    If previous sessions left pods in allocated state, they are cleaned up
    first so the pool can return to a fully-ready state.
    """
    try:
        info = pool_mgr.get_warmpool(name)
        info.ready_replicas + info.allocated_replicas

        # Clean up stale sessions that hold pods in allocated state
        if info.allocated_replicas > 0:
            console.print(
                f"[yellow]Pool [cyan]{name}[/cyan] has {info.allocated_replicas} "
                f"allocated pods from stale sessions, cleaning up...[/yellow]"
            )
            _cleanup_stale_sessions(pool_mgr, name)
            # Re-check after cleanup
            info = pool_mgr.get_warmpool(name)
            info.ready_replicas + info.allocated_replicas

        if info.ready_replicas >= replicas:
            console.print(
                f"Reusing existing pool [cyan]{name}[/cyan] "
                f"({info.ready_replicas}/{info.replicas} ready)"
            )
            return

        if info.replicas < replicas:
            console.print(
                f"Pool [cyan]{name}[/cyan] exists ({info.ready_replicas}/{info.replicas} ready), "
                f"scaling to {replicas}..."
            )
            pool_mgr.scale_warmpool(name, replicas=replicas)

        pool_mgr.wait_for_ready(name, timeout=timeout, poll_interval=2.0, min_ready=replicas)
        return
    except GatewayError:
        pass  # pool doesn't exist, create it

    console.print(f"Creating pool [cyan]{name}[/cyan] with {replicas} replicas...")
    pool_mgr.create_warmpool(name=name, image=image, replicas=replicas)
    pool_mgr.wait_for_ready(name, timeout=timeout, poll_interval=2.0, min_ready=replicas)


def _cleanup_stale_sessions(pool_mgr: WarmPoolManager, pool_name: str) -> None:
    """Delete any sessions still holding pods from a pool."""
    client = GatewayClient(base_url=pool_mgr._client._base_url, timeout=30)
    try:
        # Use pool label (set by gateway on sandbox creation)
        result = subprocess.run(
            [
                "kubectl",
                "get",
                "sandbox",
                "-n",
                pool_mgr.namespace,
                "-l",
                f"arl.infra.io/pool={pool_name}",
                "-o",
                "jsonpath={.items[*].metadata.name}",
            ],
            capture_output=True,
            text=True,
            timeout=15,
        )
        names = [n for n in result.stdout.strip().split() if n]

        if not names:
            # Fallback for legacy sandboxes without pool label
            result = subprocess.run(
                [
                    "kubectl",
                    "get",
                    "sandbox",
                    "-n",
                    pool_mgr.namespace,
                    "-o",
                    'jsonpath={range .items[?(@.spec.poolRef=="'
                    + pool_name
                    + '")]}{.metadata.name}{"\\n"}{end}',
                ],
                capture_output=True,
                text=True,
                timeout=15,
            )
            names = [n for n in result.stdout.strip().splitlines() if n]

        if not names:
            console.print("[dim]No stale sandboxes found.[/dim]")
            return
        console.print(f"[yellow]Deleting {len(names)} stale sessions...[/yellow]")
        for name in names:
            try:
                client.delete_session(name)
            except Exception:
                subprocess.run(
                    ["kubectl", "delete", "sandbox", name, "-n", pool_mgr.namespace],
                    capture_output=True,
                    timeout=10,
                )
        # Give controller time to release pods back to idle
        time.sleep(5)
    except Exception as exc:
        console.print(f"[yellow]Could not clean stale sessions: {exc}[/yellow]")


# ---------------------------------------------------------------------------
# Prometheus metrics scraper
# ---------------------------------------------------------------------------


def scrape_metrics(gateway_url: str) -> dict[str, list[tuple[dict[str, str], float]]]:
    """Scrape /metrics and parse Prometheus exposition format.

    Returns a dict mapping metric name to list of (labels_dict, value).
    """
    import httpx

    resp = httpx.get(f"{gateway_url.rstrip('/')}/metrics", timeout=10.0)
    resp.raise_for_status()

    result: dict[str, list[tuple[dict[str, str], float]]] = {}
    for line in resp.text.splitlines():
        if line.startswith("#") or not line.strip():
            continue
        # Parse: metric_name{label="val",...} value
        # or:    metric_name value
        if "{" in line:
            name_part, rest = line.split("{", 1)
            labels_part, value_part = rest.rsplit("}", 1)
            labels: dict[str, str] = {}
            for pair in _split_labels(labels_part):
                if "=" in pair:
                    k, v = pair.split("=", 1)
                    labels[k] = v.strip('"')
            val = float(value_part.strip())
        else:
            parts = line.split()
            if len(parts) < 2:
                continue
            name_part = parts[0]
            labels = {}
            val = float(parts[1])

        result.setdefault(name_part.strip(), []).append((labels, val))
    return result


def _split_labels(s: str) -> list[str]:
    """Split label pairs handling quoted values that may contain commas."""
    pairs: list[str] = []
    current: list[str] = []
    in_quotes = False
    for ch in s:
        if ch == '"':
            in_quotes = not in_quotes
            current.append(ch)
        elif ch == "," and not in_quotes:
            pairs.append("".join(current).strip())
            current = []
        else:
            current.append(ch)
    if current:
        pairs.append("".join(current).strip())
    return pairs


def get_pool_metrics(gateway_url: str, pool_name: str) -> dict[str, float]:
    """Extract key WarmPool metrics for a specific pool.

    Returns dict with keys like 'first_pod_ready_s', 'all_pods_ready_s', etc.
    """
    metrics = scrape_metrics(gateway_url)
    result: dict[str, float] = {}

    # Helper: find histogram _sum for a pool
    def hist_sum(metric_base: str) -> float | None:
        entries = metrics.get(f"{metric_base}_sum", [])
        for labels, val in entries:
            if labels.get("pool") == pool_name:
                return val
        return None

    def hist_count(metric_base: str) -> float | None:
        entries = metrics.get(f"{metric_base}_count", [])
        for labels, val in entries:
            if labels.get("pool") == pool_name:
                return val
        return None

    # First pod ready time
    s = hist_sum("arl_warmpool_first_pod_ready_seconds")
    c = hist_count("arl_warmpool_first_pod_ready_seconds")
    if s is not None and c is not None and c > 0:
        result["first_pod_ready_avg_s"] = s / c
        result["first_pod_ready_count"] = c

    # All pods ready time
    s = hist_sum("arl_warmpool_all_pods_ready_seconds")
    c = hist_count("arl_warmpool_all_pods_ready_seconds")
    if s is not None and c is not None and c > 0:
        result["all_pods_ready_avg_s"] = s / c
        result["all_pods_ready_count"] = c

    # Pod schedule duration
    s = hist_sum("arl_warmpool_pod_schedule_seconds")
    c = hist_count("arl_warmpool_pod_schedule_seconds")
    if s is not None and c is not None and c > 0:
        result["pod_schedule_avg_s"] = s / c

    # Container start duration (aggregate across containers)
    total_s, total_c = 0.0, 0.0
    for labels, val in metrics.get("arl_warmpool_container_start_seconds_sum", []):
        if labels.get("pool") == pool_name:
            total_s += val
    for labels, val in metrics.get("arl_warmpool_container_start_seconds_count", []):
        if labels.get("pool") == pool_name:
            total_c += val
    if total_c > 0:
        result["container_start_avg_s"] = total_s / total_c

    # Pod ready duration (aggregate across nodes)
    total_s, total_c = 0.0, 0.0
    for labels, val in metrics.get("arl_warmpool_pod_ready_seconds_sum", []):
        if labels.get("pool") == pool_name:
            total_s += val
    for labels, val in metrics.get("arl_warmpool_pod_ready_seconds_count", []):
        if labels.get("pool") == pool_name:
            total_c += val
    if total_c > 0:
        result["pod_ready_avg_s"] = total_s / total_c

    # Image pull errors (only include when > 0)
    err_total = 0.0
    for labels, val in metrics.get("arl_warmpool_image_pull_errors_total", []):
        if labels.get("pool") == pool_name:
            err_total += val
    if err_total > 0:
        result["image_pull_errors"] = err_total

    return result


def print_operator_metrics_table(
    gateway_url: str,
    pool_names: list[str],
) -> None:
    """Print a consolidated operator performance metrics table for all pools."""
    all_metrics: dict[str, dict[str, float]] = {}
    for name in pool_names:
        try:
            pm = get_pool_metrics(gateway_url, name)
            if pm:
                all_metrics[name] = pm
        except Exception:
            pass

    if not all_metrics:
        console.print("  [dim]No operator metrics available (metrics endpoint unreachable?)[/dim]")
        return

    # Determine which metric columns have data across any pool
    metric_columns: list[tuple[str, str]] = [
        ("first_pod_ready_avg_s", "1st Pod Ready"),
        ("all_pods_ready_avg_s", "All Pods Ready"),
        ("pod_schedule_avg_s", "Pod Schedule"),
        ("container_start_avg_s", "Container Start"),
        ("pod_ready_avg_s", "Pod Ready"),
        ("image_pull_errors", "Pull Errors"),
    ]
    active_cols = [
        (key, label)
        for key, label in metric_columns
        if any(key in pm for pm in all_metrics.values())
    ]
    if not active_cols:
        console.print("  [dim]No performance metrics recorded by operator.[/dim]")
        return

    table = Table(title="Operator Performance Metrics", show_lines=True)
    table.add_column("Pool", style="cyan")
    for _, label in active_cols:
        table.add_column(label, justify="right", style="green")

    for name in pool_names:
        pm = all_metrics.get(name, {})
        cells: list[str] = []
        for key, _ in active_cols:
            val = pm.get(key)
            if val is None:
                cells.append("-")
            elif key == "image_pull_errors":
                cells.append(f"{val:.0f}")
            else:
                cells.append(f"{val:.2f}s")
        table.add_row(name, *cells)

    # Summary row: averages across pools
    summary_cells: list[str] = []
    for key, _ in active_cols:
        values = [pm[key] for pm in all_metrics.values() if key in pm]
        if not values:
            summary_cells.append("-")
        elif key == "image_pull_errors":
            summary_cells.append(f"{sum(values):.0f}")
        else:
            summary_cells.append(f"{sum(values) / len(values):.2f}s")
    table.add_row("[bold]avg/total[/bold]", *summary_cells)

    console.print(table)


# =========================================================================
# Command: warmpool-scale
# =========================================================================


@app.command()
def warmpool_scale(
    num_pools: int = typer.Option(8, "--pools", "-p", help="Number of WarmPools to create."),
    replicas: int = typer.Option(8, "--replicas", "-r", help="Replicas per pool."),
    image: str = typer.Option(DEFAULT_IMAGE, "--image", "-i", help="Container image."),
    namespace: str = typer.Option(DEFAULT_NAMESPACE, "--namespace", "-n", help="K8s namespace."),
    gateway_url: str = typer.Option(DEFAULT_GATEWAY, "--gateway", "-g", help="Gateway URL."),
    timeout: float = typer.Option(600.0, "--timeout", help="Max wait seconds per pool."),
    cleanup: bool = typer.Option(True, "--cleanup/--no-cleanup", help="Delete pools after test."),
    port_forward: bool = typer.Option(
        True, "--port-forward/--no-port-forward", help="Auto kubectl port-forward."
    ),
) -> None:
    """Benchmark WarmPool scale: create N pools × M replicas and measure readiness."""
    if port_forward:
        ensure_port_forward(gateway_url, namespace)
    console.rule(f"[bold]WarmPool Scale Benchmark: {num_pools} pools × {replicas} replicas")

    pool_mgr = WarmPoolManager(namespace=namespace, gateway_url=gateway_url, timeout=timeout)

    pool_names = [f"bench-scale-{i}" for i in range(num_pools)]

    # Clean up any leftovers
    console.print("[dim]Cleaning up old pools...[/dim]")
    for name in pool_names:
        safe_cleanup_pool(pool_mgr, name)

    # --- Phase 1: Create pools one-by-one, record API call time and creation timestamp ---
    create_api_times: list[float] = []
    created_at: dict[str, float] = {}  # pool name -> perf_counter at creation
    console.print(f"\n[bold cyan]Phase 1:[/bold cyan] Creating {num_pools} pools...")
    for idx, name in enumerate(pool_names):
        t = Timer()
        with t:
            pool_mgr.create_warmpool(name=name, image=image, replicas=replicas)
        created_at[name] = time.perf_counter()
        create_api_times.append(t.ms)
        console.print(f"  [{idx + 1}/{num_pools}] {name} create API: {fmt(t.ms)}")

    # --- Phase 2: Wait for ALL pools concurrently, measure e2e from creation ---
    msg = f"Waiting for all pools to reach {replicas} ready replicas (concurrent)..."
    console.print(f"\n[bold cyan]Phase 2:[/bold cyan] {msg}")
    ready_e2e: dict[str, float] = {}  # pool name -> ms from creation to ready

    def _wait_one(name: str) -> tuple[str, float]:
        pool_mgr.wait_for_ready(name, timeout=timeout, poll_interval=2.0, min_ready=replicas)
        elapsed_ms = (time.perf_counter() - created_at[name]) * 1000
        return name, elapsed_ms

    overall = Timer()
    with overall, ThreadPoolExecutor(max_workers=num_pools) as executor:
        futures = {executor.submit(_wait_one, n): n for n in pool_names}
        for future in as_completed(futures):
            name, elapsed_ms = future.result()
            ready_e2e[name] = elapsed_ms
            idx = pool_names.index(name)
            console.print(f"  [{idx + 1}/{num_pools}] {name} ready: {fmt(elapsed_ms)}")

    # Preserve original order
    ready_times = [ready_e2e[n] for n in pool_names]

    console.print(f"\n  Total wall-clock for all pools ready: {fmt(overall.ms)}")

    # --- Results table ---
    console.print()
    results_table = stats_table(
        "WarmPool Scale Results",
        [
            ("Pool create API call", create_api_times),
            ("Pool ready (e2e wait)", ready_times),
        ],
    )
    console.print(results_table)

    # --- Per-pool detail table ---
    detail = Table(title="Per-Pool Breakdown", show_lines=True)
    detail.add_column("Pool", style="cyan")
    detail.add_column("Create API", justify="right")
    detail.add_column("Ready E2E", justify="right", style="green")
    for i, name in enumerate(pool_names):
        detail.add_row(name, fmt(create_api_times[i]), fmt(ready_times[i]))
    console.print(detail)

    # --- Node locality check ---
    console.print("\n[bold cyan]Node Locality Check:[/bold cyan]")
    try:
        locality_table = Table(title="Pod → Node Distribution", show_lines=True)
        locality_table.add_column("Pool", style="cyan")
        locality_table.add_column("Nodes", justify="right")
        locality_table.add_column("Distribution", style="dim")
        for name in pool_names:
            result = subprocess.run(
                [
                    "kubectl",
                    "get",
                    "pods",
                    "-n",
                    namespace,
                    "-l",
                    f"arl.infra.io/pool={name}",
                    "-o",
                    "jsonpath={.items[*].spec.nodeName}",
                ],
                capture_output=True,
                text=True,
                timeout=10,
            )
            nodes = result.stdout.strip().split()
            node_counts: dict[str, int] = {}
            for node in nodes:
                if node:
                    node_counts[node] = node_counts.get(node, 0) + 1
            unique = len(node_counts)
            dist = ", ".join(f"{n}×{c}" for n, c in sorted(node_counts.items()))
            color = "green" if unique <= 2 else ("yellow" if unique <= 4 else "red")
            locality_table.add_row(name, f"[{color}]{unique}[/{color}]", dist)
        console.print(locality_table)
    except Exception as exc:
        console.print(f"  [yellow]Could not check node locality: {exc}[/yellow]")

    # --- Operator metrics ---
    console.print("\n[bold cyan]Operator Performance Metrics:[/bold cyan]")
    try:
        print_operator_metrics_table(gateway_url, pool_names)
    except Exception as exc:
        console.print(f"  [yellow]Could not fetch operator metrics: {exc}[/yellow]")

    # --- Cleanup ---
    if cleanup:
        console.print("\n[dim]Cleaning up pools...[/dim]")
        for name in pool_names:
            safe_cleanup_pool(pool_mgr, name)
        console.print("[green]Done.[/green]")


# =========================================================================
# Command: session-bench
# =========================================================================


@app.command()
def session_bench(
    pool_name: str = typer.Option("bench-session-pool", "--pool", help="Pool name to use."),
    replicas: int = typer.Option(10, "--replicas", "-r", help="Pool replicas."),
    num_sessions: int = typer.Option(10, "--sessions", "-s", help="Number of sessions to create."),
    image: str = typer.Option(DEFAULT_IMAGE, "--image", "-i", help="Container image."),
    namespace: str = typer.Option(DEFAULT_NAMESPACE, "--namespace", "-n", help="K8s namespace."),
    gateway_url: str = typer.Option(DEFAULT_GATEWAY, "--gateway", "-g", help="Gateway URL."),
    timeout: float = typer.Option(300.0, "--timeout", help="Max wait seconds."),
    cleanup: bool = typer.Option(
        True, "--cleanup/--no-cleanup", help="Delete resources after test."
    ),
    port_forward: bool = typer.Option(
        True, "--port-forward/--no-port-forward", help="Auto kubectl port-forward."
    ),
) -> None:
    """Benchmark session creation: first response time, average, percentiles."""
    if port_forward:
        ensure_port_forward(gateway_url, namespace)
    console.rule(f"[bold]Session Creation Benchmark: {num_sessions} sessions from {pool_name}")

    client = GatewayClient(base_url=gateway_url, timeout=timeout)
    pool_mgr = WarmPoolManager(namespace=namespace, gateway_url=gateway_url, timeout=timeout)

    # Reuse pool if it already exists and has enough replicas; otherwise create
    _ensure_pool(pool_mgr, pool_name, image, replicas, timeout)
    console.print("[green]Pool ready.[/green]\n")

    # --- Create sessions ---
    console.print(f"[bold cyan]Creating {num_sessions} sessions...[/bold cyan]")
    create_times: list[float] = []
    sessions: list[str] = []
    for i in range(num_sessions):
        t = Timer()
        with t:
            info = client.create_session(pool_ref=pool_name, namespace=namespace)
        create_times.append(t.ms)
        sessions.append(info.id)
        console.print(f"  [{i + 1}/{num_sessions}] {fmt(t.ms)}  pod={info.pod_name}")

    # --- Results ---
    console.print()
    console.print(
        stats_table(
            "Session Creation Latency",
            [("POST /v1/sessions", create_times)],
        )
    )

    s = compute_stats(create_times)
    if s:
        console.print(f"\n  [yellow]First response:[/yellow] {fmt(s['first'])}")
        console.print(f"  [green]Average:[/green]        {fmt(s['avg'])}")
        console.print(f"  [magenta]P95:[/magenta]            {fmt(s['p95'])}")

    # --- Delete sessions ---
    console.print(f"\n[bold cyan]Deleting {len(sessions)} sessions...[/bold cyan]")
    delete_times: list[float] = []
    for sid in sessions:
        t = Timer()
        with t:
            client.delete_session(sid)
        delete_times.append(t.ms)

    console.print(
        stats_table(
            "Session Deletion Latency",
            [("DELETE /v1/sessions/{id}", delete_times)],
        )
    )

    # --- Cleanup ---
    if cleanup:
        safe_cleanup_pool(pool_mgr, pool_name)
        console.print("[green]Pool cleaned up.[/green]")


# =========================================================================
# Command: exec-bench
# =========================================================================


@app.command()
def exec_bench(
    pool_name: str = typer.Option("bench-exec-pool", "--pool", help="Pool name."),
    replicas: int = typer.Option(2, "--replicas", "-r", help="Pool replicas."),
    image: str = typer.Option(DEFAULT_IMAGE, "--image", "-i", help="Container image."),
    namespace: str = typer.Option(DEFAULT_NAMESPACE, "--namespace", "-n", help="K8s namespace."),
    gateway_url: str = typer.Option(DEFAULT_GATEWAY, "--gateway", "-g", help="Gateway URL."),
    timeout: float = typer.Option(300.0, "--timeout", help="Max wait seconds."),
    cleanup: bool = typer.Option(
        True, "--cleanup/--no-cleanup", help="Delete resources after test."
    ),
    port_forward: bool = typer.Option(
        True, "--port-forward/--no-port-forward", help="Auto kubectl port-forward."
    ),
) -> None:
    """Benchmark execution performance: single commands, batches, throughput."""
    if port_forward:
        ensure_port_forward(gateway_url, namespace)
    console.rule("[bold]Execution Benchmark")

    client = GatewayClient(base_url=gateway_url, timeout=timeout)
    pool_mgr = WarmPoolManager(namespace=namespace, gateway_url=gateway_url, timeout=timeout)

    # Setup pool + session
    _ensure_pool(pool_mgr, pool_name, image, replicas, timeout)

    info = client.create_session(pool_ref=pool_name, namespace=namespace)
    sid = info.id
    console.print(f"Session: {sid}  pod={info.pod_name}\n")

    rows: list[tuple[str, list[float]]] = []

    # 1. Single echo command
    console.print("[bold cyan]1. Single echo command (20 iterations)[/bold cyan]")
    single_times: list[float] = []
    for i in range(20):
        t = Timer()
        with t:
            client.execute(sid, [{"name": f"echo-{i}", "command": ["echo", "hello"]}])
        single_times.append(t.ms)
    rows.append(("Single echo", single_times))

    # 2. File write
    console.print("[bold cyan]2. File write ~1.5KB (10 iterations)[/bold cyan]")
    file_times: list[float] = []
    for i in range(10):
        content = f"benchmark content {i}\n" * 100
        t = Timer()
        with t:
            cmd = f"printf '%s' '{content}' > /workspace/bench_{i}.txt"
            client.execute(
                sid,
                [{"name": f"write-{i}", "command": ["sh", "-c", cmd]}],
            )
        file_times.append(t.ms)
    rows.append(("File write (~1.5KB)", file_times))

    # 3. Batch execution
    for batch_size in [5, 10, 20]:
        console.print(f"[bold cyan]3. Batch of {batch_size} commands (5 iterations)[/bold cyan]")
        steps = [{"name": f"step-{j}", "command": ["echo", f"step-{j}"]} for j in range(batch_size)]
        batch_times: list[float] = []
        for _ in range(5):
            t = Timer()
            with t:
                client.execute(sid, steps)
            batch_times.append(t.ms)
        per_step = statistics.mean(batch_times) / batch_size
        rows.append((f"Batch x{batch_size}", batch_times))
        console.print(f"  per-step avg: {fmt(per_step)}")

    # 4. Throughput test
    n_rapid = 50
    console.print(f"[bold cyan]4. Throughput: {n_rapid}x 'true' command[/bold cyan]")
    rapid_times: list[float] = []
    overall = Timer()
    with overall:
        for i in range(n_rapid):
            t = Timer()
            with t:
                client.execute(sid, [{"name": f"r-{i}", "command": ["true"]}])
            rapid_times.append(t.ms)
    throughput = n_rapid / (overall.ms / 1000)
    rows.append((f"{n_rapid}x 'true'", rapid_times))
    console.print(f"  Throughput: {throughput:.1f} steps/sec  (total: {fmt(overall.ms)})")

    # Print results
    console.print()
    console.print(stats_table("Execution Benchmark Results", rows))

    # Cleanup
    client.delete_session(sid)
    if cleanup:
        safe_cleanup_pool(pool_mgr, pool_name)
        console.print("[green]Cleaned up.[/green]")


# =========================================================================
# Command: managed-bench
# =========================================================================


@app.command()
def managed_bench(
    concurrency: int = typer.Option(
        32, "--concurrency", "-c", help="Concurrent managed sessions."
    ),
    image: str = typer.Option(DEFAULT_IMAGE, "--image", "-i"),
    namespace: str = typer.Option(DEFAULT_NAMESPACE, "--namespace", "-n"),
    gateway_url: str = typer.Option(DEFAULT_GATEWAY, "--gateway", "-g"),
    timeout: float = typer.Option(300.0, "--timeout"),
    max_replicas: int = typer.Option(
        0, "--max-replicas", "-m",
        help="maxReplicas hint (0 = none, server scales incrementally).",
    ),
    execute: bool = typer.Option(
        True, "--execute/--no-execute",
        help="Run a command in each session after creation.",
    ),
    cleanup: bool = typer.Option(
        True, "--cleanup/--no-cleanup",
    ),
    port_forward: bool = typer.Option(
        True, "--port-forward/--no-port-forward",
    ),
) -> None:
    """Stress-test managed sessions: concurrent creation, scaling, execution.

    Launches N concurrent ManagedSession.create_sandbox() calls to the same
    image, measuring pool auto-scaling, session creation latency, and
    optionally per-session execution latency.

    Examples:
        # 32 concurrent sessions (default)
        uv run python examples/python/bench_gateway.py managed-bench

        # 128 concurrent with eager scaling hint
        uv run python examples/python/bench_gateway.py managed-bench -c 128 -m 128

        # 512 concurrent, no execute, no cleanup
        uv run python examples/python/bench_gateway.py managed-bench \
            -c 512 --no-execute --no-cleanup
    """
    if port_forward:
        ensure_port_forward(gateway_url, namespace)

    exp_id = f"bench-managed-{int(time.time())}"
    mr_label = f", maxReplicas={max_replicas}" if max_replicas > 0 else ""
    console.rule(
        f"[bold]Managed Session Bench: "
        f"{concurrency} concurrent{mr_label}"
    )
    console.print(f"  experiment: [cyan]{exp_id}[/cyan]")
    console.print(f"  image: [dim]{image}[/dim]")

    client = GatewayClient(base_url=gateway_url, timeout=timeout)

    # ------------------------------------------------------------------
    # Phase 1: Concurrent session creation
    # ------------------------------------------------------------------
    console.print(
        f"\n[bold cyan]Phase 1:[/bold cyan] "
        f"Creating {concurrency} managed sessions concurrently..."
    )

    create_times: list[float] = []
    create_errors: list[str] = []
    session_ids: list[str] = []
    lock = __import__("threading").Lock()

    mr = max_replicas if max_replicas > 0 else None

    def _create_one(idx: int) -> tuple[int, float, str | None, str | None]:
        t = Timer()
        try:
            with t:
                info = client.create_managed_session(
                    image=image,
                    experiment_id=exp_id,
                    namespace=namespace,
                    max_replicas=mr,
                )
            return idx, t.ms, info.id, None
        except Exception as exc:
            return idx, t.ms, None, str(exc)

    wall = Timer()
    with wall, ThreadPoolExecutor(max_workers=concurrency) as pool:
        futures = [pool.submit(_create_one, i) for i in range(concurrency)]
        done_count = 0
        for future in as_completed(futures):
            idx, elapsed, sid, err = future.result()
            done_count += 1
            with lock:
                create_times.append(elapsed)
                if sid:
                    session_ids.append(sid)
                if err:
                    create_errors.append(err)
            # Progress every 10% or on errors
            if done_count % max(1, concurrency // 10) == 0 or err:
                status = f"[green]{fmt(elapsed)}[/green]"
                if err:
                    short = err[:80]
                    status = f"[red]ERR: {short}[/red]"
                console.print(
                    f"  [{done_count}/{concurrency}] {status}"
                )

    console.print(
        f"\n  Wall-clock: [bold]{fmt(wall.ms)}[/bold]  "
        f"OK={len(session_ids)}  "
        f"Errors={len(create_errors)}"
    )

    if create_errors:
        # Show unique error messages
        unique_errs: dict[str, int] = {}
        for e in create_errors:
            # Truncate for grouping
            key = e[:120]
            unique_errs[key] = unique_errs.get(key, 0) + 1
        console.print("\n  [red]Error summary:[/red]")
        for msg, count in sorted(
            unique_errs.items(), key=lambda x: -x[1]
        ):
            console.print(f"    {count}x  {msg}")

    # ------------------------------------------------------------------
    # Phase 2: Execute a command in each session
    # ------------------------------------------------------------------
    exec_times: list[float] = []
    exec_errors: list[str] = []

    if execute and session_ids:
        console.print(
            f"\n[bold cyan]Phase 2:[/bold cyan] "
            f"Executing 'echo ok' in {len(session_ids)} sessions..."
        )

        def _exec_one(
            sid: str,
        ) -> tuple[float, str | None]:
            t = Timer()
            try:
                with t:
                    resp = client.execute(
                        sid,
                        [{"name": "bench", "command": ["echo", "ok"]}],
                    )
                ec = resp.results[0].output.exit_code if resp.results else -1
                if ec != 0:
                    return t.ms, f"exit_code={ec}"
                return t.ms, None
            except Exception as exc:
                return t.ms, str(exc)

        exec_wall = Timer()
        with exec_wall, ThreadPoolExecutor(
            max_workers=min(concurrency, 64)
        ) as pool:
            futures = [
                pool.submit(_exec_one, sid) for sid in session_ids
            ]
            done_count = 0
            for future in as_completed(futures):
                elapsed, err = future.result()
                done_count += 1
                with lock:
                    exec_times.append(elapsed)
                    if err:
                        exec_errors.append(err)
                if done_count % max(1, len(session_ids) // 10) == 0:
                    console.print(
                        f"  [{done_count}/{len(session_ids)}]"
                    )

        console.print(
            f"\n  Exec wall-clock: [bold]{fmt(exec_wall.ms)}[/bold]  "
            f"OK={len(exec_times) - len(exec_errors)}  "
            f"Errors={len(exec_errors)}"
        )

    # ------------------------------------------------------------------
    # Results
    # ------------------------------------------------------------------
    console.print()
    rows: list[tuple[str, list[float]]] = [
        (f"Session create ({concurrency} concurrent)", create_times),
    ]
    if exec_times:
        rows.append(
            (f"Execute ({len(session_ids)} sessions)", exec_times)
        )
    console.print(stats_table("Managed Session Benchmark", rows))

    # Throughput summary
    if create_times:
        ok_count = len(session_ids)
        tput = ok_count / (wall.ms / 1000) if wall.ms > 0 else 0
        console.print(
            f"\n  Throughput: [bold green]{tput:.1f}[/bold green] "
            f"sessions/sec  "
            f"({ok_count} sessions in {fmt(wall.ms)})"
        )

    # ------------------------------------------------------------------
    # Cleanup
    # ------------------------------------------------------------------
    if cleanup and session_ids:
        console.print(
            f"\n[dim]Cleaning up experiment {exp_id} "
            f"({len(session_ids)} sessions)...[/dim]"
        )
        try:
            deleted = client.delete_experiment(exp_id)
            console.print(f"[green]Deleted {deleted} sessions.[/green]")
        except Exception as exc:
            console.print(f"[yellow]Cleanup error: {exc}[/yellow]")
    elif not cleanup:
        console.print(
            f"\n[yellow]Skipped cleanup. "
            f"To clean up later:[/yellow]\n"
            f"  curl -X DELETE {gateway_url}/v1/managed/"
            f"experiments/{exp_id}"
        )


# =========================================================================
# Command: full
# =========================================================================


@app.command()
def full(
    image: str = typer.Option(DEFAULT_IMAGE, "--image", "-i", help="Container image."),
    namespace: str = typer.Option(DEFAULT_NAMESPACE, "--namespace", "-n", help="K8s namespace."),
    gateway_url: str = typer.Option(DEFAULT_GATEWAY, "--gateway", "-g", help="Gateway URL."),
    timeout: float = typer.Option(600.0, "--timeout", help="Max wait seconds."),
    port_forward: bool = typer.Option(
        True, "--port-forward/--no-port-forward", help="Auto kubectl port-forward."
    ),
) -> None:
    """Run all benchmarks sequentially."""
    if port_forward:
        ensure_port_forward(gateway_url, namespace)
    console.rule("[bold green]Full Benchmark Suite")

    # 1. Health check
    console.rule("[bold]Health Check")
    client = GatewayClient(base_url=gateway_url, timeout=timeout)
    health_times: list[float] = []
    for _ in range(20):
        t = Timer()
        with t:
            client.health()
        health_times.append(t.ms)
    console.print(stats_table("Health Check", [("GET /healthz", health_times)]))

    # 2. WarmPool scale
    console.print()
    warmpool_scale(
        num_pools=8,
        replicas=8,
        image=image,
        namespace=namespace,
        gateway_url=gateway_url,
        timeout=timeout,
        cleanup=True,
        port_forward=False,
    )

    # 3. Session bench
    console.print()
    session_bench(
        pool_name="bench-full-session",
        replicas=10,
        num_sessions=10,
        image=image,
        namespace=namespace,
        gateway_url=gateway_url,
        timeout=timeout,
        cleanup=True,
        port_forward=False,
    )

    # 4. Exec bench
    console.print()
    exec_bench(
        pool_name="bench-full-exec",
        replicas=2,
        image=image,
        namespace=namespace,
        gateway_url=gateway_url,
        timeout=timeout,
        cleanup=True,
        port_forward=False,
    )

    console.rule("[bold green]All Benchmarks Complete")


# =========================================================================
# image-bench: image pull speed / startup speed / cache-hit rate
# =========================================================================
#
# Goal: large images, want fast startup, but cannot pre-warm too many.
# This command measures the trade-off triangle directly:
#   pre-warm count (cost) <-> cold-start tail latency <-> cache-hit rate.
#
# Cache-hit is computed two complementary ways:
#   1. Authoritative: snapshot node.status.images BEFORE creating the pool,
#      then per pod check whether its node already had the image (robust,
#      does not depend on event retention).
#   2. Pull time: parse kubelet "Pulled" events for the real pull seconds on
#      a miss ("Successfully pulled image X in <dur>") vs "already present".

# Default SWE-bench public image (large, ~multi-GB). Override with --image.
DEFAULT_SWEBENCH_IMAGE = "swebench/swesmith.x86_64.emotion_1776_js-emotion.b882bcba"

POOL_LABEL = "arl.infra.io/pool"

_PULL_DUR_RE = re.compile(r"pulled image .* in ([0-9hmsµun.]+)")
_PULL_SIZE_RE = re.compile(r"[Ii]mage size:\s*(\d+)\s*bytes")
_GO_DUR_RE = re.compile(r"([0-9.]+)(ms|µs|us|ns|h|m|s)")


@dataclass
class PodObs:
    """Per-pod observation for the image benchmark."""

    name: str
    node: str
    ready_s: float | None
    cache_hit: bool | None  # from node pre-snapshot (authoritative)
    pull_result: str | None  # "hit" / "miss" from kubelet events
    pull_s: float | None


# ---------------------------------------------------------------------------
# kubectl + parsing helpers
# ---------------------------------------------------------------------------


def _kubectl_json(args: list[str], timeout: float = 30.0) -> dict[str, object]:
    proc = subprocess.run(
        ["kubectl", *args, "-o", "json"],
        capture_output=True,
        text=True,
        timeout=timeout,
    )
    if proc.returncode != 0:
        raise RuntimeError(f"kubectl {' '.join(args)} failed: {proc.stderr.strip()}")
    parsed: dict[str, object] = json.loads(proc.stdout)
    return parsed


def _kubectl_apply(obj: dict[str, object], timeout: float = 30.0) -> None:
    proc = subprocess.run(
        ["kubectl", "apply", "-f", "-"],
        input=json.dumps(obj),
        capture_output=True,
        text=True,
        timeout=timeout,
    )
    if proc.returncode != 0:
        raise RuntimeError(f"kubectl apply failed: {proc.stderr.strip()}")


def _norm_image(img: str) -> str:
    """Add :latest when no tag/digest is present (mirrors operator behaviour)."""
    if "@" in img or ":" in img.rsplit("/", 1)[-1]:
        return img
    return img + ":latest"


def _go_duration_seconds(s: str) -> float:
    total = 0.0
    unit = {"h": 3600.0, "m": 60.0, "s": 1.0, "ms": 1e-3, "us": 1e-6, "µs": 1e-6, "ns": 1e-9}
    for num, u in _GO_DUR_RE.findall(s):
        total += float(num) * unit[u]
    return total


def parse_pulled(msg: str) -> tuple[str, float, int]:
    """Classify a kubelet 'Pulled' event message -> (result, seconds, size_bytes)."""
    if "already present on machine" in msg:
        return "hit", 0.0, 0
    secs = 0.0
    m = _PULL_DUR_RE.search(msg)
    if m:
        secs = _go_duration_seconds(m.group(1))
    size = 0
    ms = _PULL_SIZE_RE.search(msg)
    if ms:
        size = int(ms.group(1))
    return "miss", secs, size


def _parse_ts(ts: str) -> datetime:
    return datetime.fromisoformat(ts.replace("Z", "+00:00"))


def snapshot_node_images(image: str) -> tuple[set[str], int]:
    """Return (node names that already cache `image`, max known image size bytes)."""
    data = _kubectl_json(["get", "nodes"])
    items = cast(list[dict[str, object]], data.get("items", []))
    want = _norm_image(image)
    nodes: set[str] = set()
    size = 0
    for node in items:
        meta = cast(dict[str, object], node.get("metadata", {}))
        name = cast(str, meta.get("name", "") or "")
        status = cast(dict[str, object], node.get("status", {}))
        images = cast(list[dict[str, object]], status.get("images", []) or [])
        for img in images:
            names = cast(list[str], img.get("names", []) or [])
            if any(_norm_image(n) == want or n == image for n in names):
                nodes.add(name)
                sb = img.get("sizeBytes")
                if isinstance(sb, int):
                    size = max(size, sb)
                break
    return nodes, size


def apply_warmpool(
    name: str,
    image: str,
    replicas: int,
    namespace: str,
    spread_factor: float | None = None,
    node_hostname: str | None = None,
) -> None:
    """Create/update a WarmPool CRD directly (full control over imageLocality)."""
    container: dict[str, object] = {
        "name": "executor",
        "image": image,
        # IfNotPresent so a cached image is honoured — required for clean hit/miss.
        "imagePullPolicy": "IfNotPresent",
        "command": ["/bin/sh", "-c", "sleep infinity"],
        "volumeMounts": [{"name": "workspace", "mountPath": "/workspace"}],
    }
    pod_spec: dict[str, object] = {
        "containers": [container],
        "volumes": [{"name": "workspace", "emptyDir": {}}],
    }
    if node_hostname:
        pod_spec["nodeSelector"] = {"kubernetes.io/hostname": node_hostname}
    spec: dict[str, object] = {
        "replicas": replicas,
        "template": {"spec": pod_spec},
    }
    if spread_factor is not None:
        spec["imageLocality"] = {"enabled": True, "spreadFactor": spread_factor}
    obj: dict[str, object] = {
        "apiVersion": "arl.infra.io/v1alpha1",
        "kind": "WarmPool",
        "metadata": {"name": name, "namespace": namespace},
        "spec": spec,
    }
    _kubectl_apply(obj)


def delete_warmpool_k(name: str, namespace: str) -> None:
    subprocess.run(
        ["kubectl", "delete", "warmpool", name, "-n", namespace, "--ignore-not-found"],
        capture_output=True,
        text=True,
        timeout=60,
    )


def list_pool_pods(namespace: str, pool: str) -> list[dict[str, object]]:
    data = _kubectl_json(["get", "pods", "-n", namespace, "-l", f"{POOL_LABEL}={pool}"])
    return cast(list[dict[str, object]], data.get("items", []))


def _pod_name(obj: dict[str, object]) -> str:
    meta = cast(dict[str, object], obj.get("metadata", {}))
    return cast(str, meta.get("name", "") or "")


def _pod_node(pod: dict[str, object]) -> str:
    spec = cast(dict[str, object], pod.get("spec", {}))
    return cast(str, spec.get("nodeName", "") or "")


def _pod_ready(pod: dict[str, object]) -> bool:
    status = cast(dict[str, object], pod.get("status", {}))
    if status.get("phase") != "Running":
        return False
    css = cast(list[dict[str, object]], status.get("containerStatuses", []) or [])
    return bool(css) and all(bool(cs.get("ready")) for cs in css)


def _pod_failed(pod: dict[str, object]) -> str | None:
    """Return a failure reason (ImagePullBackOff/ErrImagePull/...) or None."""
    status = cast(dict[str, object], pod.get("status", {}))
    groups = ["initContainerStatuses", "containerStatuses"]
    for g in groups:
        for cs in cast(list[dict[str, object]], status.get(g, []) or []):
            state = cast(dict[str, object], cs.get("state", {}))
            waiting = cast(dict[str, object], state.get("waiting", {}) or {})
            reason = cast(str, waiting.get("reason", "") or "")
            if reason in ("ImagePullBackOff", "ErrImagePull", "CrashLoopBackOff"):
                return reason
    return None


def pod_ready_seconds(pod: dict[str, object]) -> float | None:
    meta = cast(dict[str, object], pod.get("metadata", {}))
    created = cast(str, meta.get("creationTimestamp", "") or "")
    status = cast(dict[str, object], pod.get("status", {}))
    conds = cast(list[dict[str, object]], status.get("conditions", []) or [])
    if not created:
        return None
    for c in conds:
        if c.get("type") == "Ready" and c.get("status") == "True":
            ltt = cast(str, c.get("lastTransitionTime", "") or "")
            if ltt:
                return (_parse_ts(ltt) - _parse_ts(created)).total_seconds()
    return None


def wait_pool_pods_ready(
    namespace: str,
    pool: str,
    want: int,
    timeout: float,
    poll: float = 3.0,
) -> tuple[list[dict[str, object]], dict[str, str]]:
    """Poll until `want` pods are ready or timeout. Returns (pods, failures)."""
    deadline = time.monotonic() + timeout
    pods: list[dict[str, object]] = []
    failures: dict[str, str] = {}
    while time.monotonic() < deadline:
        pods = list_pool_pods(namespace, pool)
        ready = sum(1 for p in pods if _pod_ready(p))
        for p in pods:
            reason = _pod_failed(p)
            if reason:
                failures[_pod_name(p)] = reason
        if ready >= want:
            return pods, failures
        time.sleep(poll)
    return pods, failures


def pull_events(
    namespace: str,
    image: str,
    pod_names: list[str],
) -> dict[str, tuple[str, float, int]]:
    """Map pod -> (result, seconds, size_bytes) for the user image's Pulled event."""
    try:
        data = _kubectl_json(
            ["get", "events", "-n", namespace, "--field-selector", "reason=Pulled"]
        )
    except RuntimeError:
        return {}
    items = cast(list[dict[str, object]], data.get("items", []))
    names = set(pod_names)
    repo = _norm_image(image).split("@")[0].split(":")[0]
    out: dict[str, tuple[str, float, int]] = {}
    for ev in items:
        involved = cast(dict[str, object], ev.get("involvedObject", {}))
        if involved.get("kind") != "Pod":
            continue
        pod = cast(str, involved.get("name", "") or "")
        if pod not in names:
            continue
        msg = cast(str, ev.get("message", "") or "")
        if repo not in msg:
            continue  # ignore sidecar / executor-agent pulls
        result, secs, size = parse_pulled(msg)
        prev = out.get(pod)
        if prev is None or secs > prev[1]:
            out[pod] = (result, secs, size)
    return out


def collect_pod_obs(
    namespace: str,
    pool: str,
    image: str,
    nodes_with_image: set[str],
    pods: list[dict[str, object]],
) -> list[PodObs]:
    names = [_pod_name(p) for p in pods]
    events = pull_events(namespace, image, names)
    obs: list[PodObs] = []
    for p in pods:
        name = _pod_name(p)
        node = _pod_node(p)
        ev = events.get(name)
        obs.append(
            PodObs(
                name=name,
                node=node,
                ready_s=pod_ready_seconds(p),
                cache_hit=(node in nodes_with_image) if node else None,
                pull_result=ev[0] if ev else None,
                pull_s=ev[1] if ev else None,
            )
        )
    return obs


def _fmt_bytes(n: int) -> str:
    f = float(n)
    for unit in ["B", "KiB", "MiB", "GiB", "TiB"]:
        if f < 1024 or unit == "TiB":
            return f"{f:.1f}{unit}"
        f /= 1024
    return f"{f:.1f}TiB"


# ---------------------------------------------------------------------------
# Scenarios
# ---------------------------------------------------------------------------


def _scenario_cold(image: str, replicas: int, namespace: str, timeout: float) -> None:
    console.rule("[bold]Scenario 1: large-image cold pull")
    pool = "imgbench-cold"
    delete_warmpool_k(pool, namespace)
    time.sleep(3)

    nodes_with, presize = snapshot_node_images(image)
    console.print(
        f"  image already cached on [cyan]{len(nodes_with)}[/cyan] node(s) before start"
        + (f"  (size {_fmt_bytes(presize)})" if presize else "")
    )
    console.print(f"  creating pool [cyan]{pool}[/cyan] replicas={replicas} (spreadFactor=1.0)...")
    # spreadFactor=1.0 => spread widely so we land on cold nodes and observe real pulls.
    apply_warmpool(pool, image, replicas, namespace, spread_factor=1.0)

    pods, failures = wait_pool_pods_ready(namespace, pool, replicas, timeout)
    obs = collect_pod_obs(namespace, pool, image, nodes_with, pods)

    names = [o.name for o in obs]
    ev_map = pull_events(namespace, image, names)
    miss = [o for o in obs if o.pull_result == "miss" and o.pull_s and o.pull_s > 0]
    hit = [o for o in obs if o.pull_result == "hit"]
    pull_times = [o.pull_s for o in miss if o.pull_s is not None]
    ready_times = [o.ready_s * 1000 for o in obs if o.ready_s is not None]
    sizes = [ev_map[o.name][2] for o in miss if o.name in ev_map]
    size = max([presize, *sizes]) if (sizes or presize) else 0

    console.print()
    rows: list[tuple[str, list[float]]] = []
    if pull_times:
        rows.append(("Image pull (miss)", [t * 1000 for t in pull_times]))
    if ready_times:
        rows.append(("Pod creation→ready", ready_times))
    if rows:
        console.print(stats_table("Cold Pull Results", rows))
    console.print(
        f"\n  pods={len(obs)}  fresh-pull(miss)={len(miss)}  cache-hit={len(hit)}  "
        f"failed={len(failures)}"
    )
    if size and pull_times:
        med_pull = statistics.median(pull_times)
        if med_pull > 0:
            bw = size / med_pull
            console.print(
                f"  image size ~{_fmt_bytes(size)}  median pull {fmt(med_pull * 1000)}  "
                f"effective ~{_fmt_bytes(int(bw))}/s"
            )
    if failures:
        uniq: dict[str, int] = {}
        for r in failures.values():
            uniq[r] = uniq.get(r, 0) + 1
        console.print(f"  [red]pull failures:[/red] {uniq}")
    delete_warmpool_k(pool, namespace)


def _scenario_spread(
    image: str,
    replicas: int,
    namespace: str,
    timeout: float,
    factors: list[float],
) -> None:
    console.rule("[bold]Scenario 2: cache-hit rate vs spreadFactor")
    console.print(
        "  [dim]note: the image stays cached across runs, so absolute hit-rate climbs over\n"
        "  the sweep; the locality signal is unique-nodes and all-ready time.[/dim]\n"
    )
    table = Table(title=f"spreadFactor sweep (replicas={replicas})", show_lines=True)
    table.add_column("spreadFactor", style="cyan")
    table.add_column("k (pref nodes)", justify="right")
    table.add_column("unique nodes", justify="right")
    table.add_column("cache-hit", justify="right", style="green")
    table.add_column("ready p50", justify="right")
    table.add_column("ready p95", justify="right", style="magenta")

    for sf in factors:
        pool = f"imgbench-sf-{str(sf).replace('.', '-')}"
        delete_warmpool_k(pool, namespace)
        time.sleep(2)
        nodes_with, _ = snapshot_node_images(image)
        apply_warmpool(pool, image, replicas, namespace, spread_factor=sf)
        pods, _ = wait_pool_pods_ready(namespace, pool, replicas, timeout)
        obs = collect_pod_obs(namespace, pool, image, nodes_with, pods)
        used_nodes = {o.node for o in obs if o.node}
        hits = [o for o in obs if o.cache_hit]
        hit_rate = (len(hits) / len(obs) * 100) if obs else 0.0
        rt = sorted(o.ready_s for o in obs if o.ready_s is not None)
        p50 = rt[len(rt) // 2] if rt else 0.0
        p95 = rt[min(len(rt) - 1, int(len(rt) * 0.95))] if rt else 0.0
        k = max(1, math.ceil(replicas * sf))
        table.add_row(
            str(sf),
            str(k),
            str(len(used_nodes)),
            f"{hit_rate:.0f}%",
            fmt(p50 * 1000),
            fmt(p95 * 1000),
        )
        console.print(f"  [dim]sf={sf}: {len(used_nodes)} nodes, hit {hit_rate:.0f}%[/dim]")
        delete_warmpool_k(pool, namespace)
    console.print()
    console.print(table)


def _scenario_prewarm(
    image: str,
    namespace: str,
    gateway_url: str,
    timeout: float,
    levels: list[int],
) -> None:
    console.rule("[bold]Scenario 3: pre-warm buffer vs cold-start tail")
    console.print(
        "  [dim]managed sessions auto-create/scale a pool; concurrency beyond the warm\n"
        "  buffer pays the cold image pull as acquisition latency.[/dim]\n"
    )
    client = GatewayClient(base_url=gateway_url, timeout=timeout)
    exp_base = f"imgbench-prewarm-{int(time.time())}"
    rows: list[tuple[str, list[float]]] = []
    for lvl in levels:
        exp = f"{exp_base}-{lvl}"
        console.print(f"  level=[cyan]{lvl}[/cyan] concurrent sessions...")
        acq: list[float] = []
        errs = 0

        def _one(_i: int, exp_id: str = exp, n: int = lvl) -> tuple[float, bool]:
            t = Timer()
            try:
                with t:
                    client.create_managed_session(
                        image=image,
                        experiment_id=exp_id,
                        namespace=namespace,
                        max_replicas=n,
                    )
                return t.ms, True
            except Exception:
                return t.ms, False

        with ThreadPoolExecutor(max_workers=lvl) as pool:
            for ms, ok in pool.map(_one, range(lvl)):
                acq.append(ms)
                if not ok:
                    errs += 1
        rows.append((f"acquire @ {lvl} concurrent (err={errs})", acq))
        with contextlib.suppress(Exception):
            client.delete_experiment(exp)
        time.sleep(3)
    console.print()
    console.print(stats_table("Managed Session Acquisition Latency", rows))


def _scenario_hitmiss(image: str, namespace: str, timeout: float) -> None:
    console.rule("[bold]Scenario 4: cache-hit vs miss (pod ready time)")
    nodes_with, _ = snapshot_node_images(image)
    all_nodes_data = _kubectl_json(["get", "nodes"])
    all_nodes = [
        _pod_name(n)
        for n in cast(list[dict[str, object]], all_nodes_data.get("items", []))
    ]
    cold_nodes = [n for n in all_nodes if n and n not in nodes_with]
    warm_node = next(iter(nodes_with), None)
    cold_node = cold_nodes[0] if cold_nodes else None
    if not warm_node or not cold_node:
        console.print(
            "  [yellow]need at least one warm and one cold node; "
            f"warm={len(nodes_with)} cold={len(cold_nodes)}. Run scenario 1 first.[/yellow]"
        )
        return

    results: list[tuple[str, str, float | None, str | None]] = []
    for label, node in [("cache-hit", warm_node), ("cache-miss(cold)", cold_node)]:
        pool = f"imgbench-{label.split('(')[0]}"
        delete_warmpool_k(pool, namespace)
        time.sleep(2)
        snap, _ = snapshot_node_images(image)
        apply_warmpool(pool, image, 1, namespace, node_hostname=node)
        pods, _ = wait_pool_pods_ready(namespace, pool, 1, timeout)
        obs = collect_pod_obs(namespace, pool, image, snap, pods)
        o = obs[0] if obs else None
        results.append(
            (label, node, o.ready_s if o else None, o.pull_result if o else None)
        )
        delete_warmpool_k(pool, namespace)

    table = Table(title="Cache-hit vs miss (single pod, pinned node)", show_lines=True)
    table.add_column("Case", style="cyan")
    table.add_column("Node")
    table.add_column("Pod ready", justify="right", style="green")
    table.add_column("Pull result", justify="right")
    for label, node, rs, pr in results:
        table.add_row(label, node, fmt(rs * 1000) if rs is not None else "-", pr or "-")
    console.print(table)


@app.command()
def image_bench(
    scenario: str = typer.Option(
        "all", "--scenario", "-s", help="all | cold | spread | prewarm | hitmiss"
    ),
    image: str = typer.Option(DEFAULT_SWEBENCH_IMAGE, "--image", "-i", help="Large image to test."),
    replicas: int = typer.Option(6, "--replicas", "-r", help="Replicas for cold/spread."),
    namespace: str = typer.Option(DEFAULT_NAMESPACE, "--namespace", "-n", help="K8s namespace."),
    gateway_url: str = typer.Option(DEFAULT_GATEWAY, "--gateway", "-g", help="Gateway URL."),
    timeout: float = typer.Option(900.0, "--timeout", help="Max wait seconds per pool."),
    port_forward: bool = typer.Option(
        True, "--port-forward/--no-port-forward", help="Auto kubectl port-forward (prewarm only)."
    ),
) -> None:
    """Benchmark image pull speed, startup speed, and cache-hit rate for large images.

    Examples:
        uv run python examples/python/bench_gateway.py image-bench -s cold
        uv run python examples/python/bench_gateway.py image-bench -s spread -r 8
        uv run python examples/python/bench_gateway.py image-bench   # runs all four
    """
    if not shutil.which("kubectl"):
        raise typer.BadParameter("kubectl is required for image-bench")

    console.rule(f"[bold green]Image Benchmark  (image={image})")
    console.print(f"  scenario=[cyan]{scenario}[/cyan]  namespace={namespace}\n")

    run_all = scenario == "all"
    if run_all or scenario == "cold":
        _scenario_cold(image, replicas, namespace, timeout)
    if run_all or scenario == "spread":
        _scenario_spread(image, replicas, namespace, timeout, [0.125, 0.25, 0.5, 1.0])
    if run_all or scenario == "prewarm":
        if port_forward:
            ensure_port_forward(gateway_url, namespace)
        _scenario_prewarm(image, namespace, gateway_url, timeout, [2, 5, 12])
    if run_all or scenario == "hitmiss":
        _scenario_hitmiss(image, namespace, timeout)

    console.rule("[bold green]Image Benchmark complete")


# =========================================================================
# Entry point
# =========================================================================

if __name__ == "__main__":
    app()
