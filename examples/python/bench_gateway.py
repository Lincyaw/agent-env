"""Performance benchmark for ARL Gateway API.

Measures response times for:
1. Health check
2. WarmPool creation + readiness
3. Session creation (sandbox allocation from pool)
4. Single command execution
5. File patch execution
6. Multi-step execution (batch)
7. Repeated single-step execution (N iterations)
8. Restore (replay-based)
9. History / trajectory export
10. Session deletion

Usage:
    kubectl port-forward -n arl svc/arl-operator-gateway 8080:8080
    uv run python examples/python/bench_gateway.py
"""

from __future__ import annotations

import statistics
import sys
import time

from arl import GatewayClient, GatewayError, WarmPoolManager

GATEWAY_URL = "http://localhost:8080"
POOL_NAME = "bench-pool"
POOL_IMAGE = "pair-diag-cn-guangzhou.cr.volces.com/pair/ubuntu:22.04"
NAMESPACE = "arl"
POOL_REPLICAS = 5


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
    if ms < 1:
        return f"{ms * 1000:.0f}us"
    if ms < 1000:
        return f"{ms:.1f}ms"
    return f"{ms / 1000:.2f}s"


def print_stats(label: str, times_ms: list[float]) -> None:
    n = len(times_ms)
    if n == 0:
        print(f"  {label}: no data")
        return
    mn = min(times_ms)
    mx = max(times_ms)
    avg = statistics.mean(times_ms)
    med = statistics.median(times_ms)
    p95 = sorted(times_ms)[int(n * 0.95)] if n >= 5 else mx
    print(
        f"  {label:40s}  "
        f"n={n:3d}  min={fmt(mn):>8s}  avg={fmt(avg):>8s}  "
        f"med={fmt(med):>8s}  p95={fmt(p95):>8s}  max={fmt(mx):>8s}"
    )


def section(title: str) -> None:
    print(f"\n{'=' * 70}")
    print(f"  {title}")
    print(f"{'=' * 70}")


def main() -> None:
    print("ARL Gateway Performance Benchmark")
    print(f"Gateway: {GATEWAY_URL}")
    print(f"Pool: {POOL_NAME} (replicas={POOL_REPLICAS})")

    client = GatewayClient(base_url=GATEWAY_URL, timeout=300.0)
    pool_mgr = WarmPoolManager(namespace=NAMESPACE, gateway_url=GATEWAY_URL)

    # ---------------------------------------------------------------
    section("1. Health Check")
    # ---------------------------------------------------------------
    health_times: list[float] = []
    for _ in range(20):
        t = Timer()
        with t:
            client.health()
        health_times.append(t.ms)
    print_stats("GET /healthz", health_times)

    # ---------------------------------------------------------------
    section("2. WarmPool Create + Wait Ready")
    # ---------------------------------------------------------------
    # Clean up first
    try:
        pool_mgr.delete_warmpool(POOL_NAME)
        time.sleep(3)
    except GatewayError:
        pass

    t = Timer()
    with t:
        pool_mgr.create_warmpool(name=POOL_NAME, image=POOL_IMAGE, replicas=POOL_REPLICAS)
    print(f"  Pool create API call: {fmt(t.ms)}")

    t = Timer()
    with t:
        pool_mgr.wait_for_ready(POOL_NAME, timeout=300.0, poll_interval=2.0)
    print(f"  Pool ready (all {POOL_REPLICAS} replicas): {fmt(t.ms)}")

    # ---------------------------------------------------------------
    section("3. Session Creation (sandbox allocation)")
    # ---------------------------------------------------------------
    create_times: list[float] = []
    sessions: list[str] = []
    for i in range(3):
        t = Timer()
        with t:
            info = client.create_session(pool_ref=POOL_NAME, namespace=NAMESPACE)
        create_times.append(t.ms)
        sessions.append(info.id)
        print(f"  Session {i + 1}: {fmt(t.ms)}  pod={info.pod_name}")
    print_stats("POST /v1/sessions", create_times)

    # Use first session for execution benchmarks
    sid = sessions[0]

    # ---------------------------------------------------------------
    section("4. Single Command Execution")
    # ---------------------------------------------------------------
    single_cmd_times: list[float] = []
    for i in range(20):
        t = Timer()
        with t:
            resp = client.execute(
                sid,
                [
                    {"name": f"echo-{i}", "command": ["echo", "hello"]},
                ],
            )
        single_cmd_times.append(t.ms)
    print_stats("Single echo command (e2e)", single_cmd_times)
    # Also show server-side duration
    server_times = [r.duration_ms for r in resp.results]
    print(f"  Last server-side step duration: {server_times[0]}ms")

    # ---------------------------------------------------------------
    section("5. File Write via Shell")
    # ---------------------------------------------------------------
    file_times: list[float] = []
    for i in range(10):
        content = f"benchmark content {i}\n" * 100
        t = Timer()
        with t:
            client.execute(
                sid,
                [
                    {
                        "name": f"write-{i}",
                        "command": [
                            "sh",
                            "-c",
                            f"printf '%s' '{content}' > /workspace/bench_{i}.txt",
                        ],
                    },
                ],
            )
        file_times.append(t.ms)
    print_stats("Single file write (~1.5KB)", file_times)

    # Large file
    large_times: list[float] = []
    for i in range(5):
        t = Timer()
        with t:
            client.execute(
                sid,
                [
                    {
                        "name": f"large-{i}",
                        "command": [
                            "sh",
                            "-c",
                            "dd if=/dev/zero bs=1024 count=100 2>/dev/null > /workspace/large.txt",
                        ],
                    },
                ],
            )
        large_times.append(t.ms)
    print_stats("Single file write (~100KB via dd)", large_times)

    # ---------------------------------------------------------------
    section("6. Multi-step Batch Execution")
    # ---------------------------------------------------------------
    for batch_size in [1, 5, 10, 20]:
        steps = [{"name": f"step-{j}", "command": ["echo", f"step-{j}"]} for j in range(batch_size)]
        batch_times: list[float] = []
        for _ in range(5):
            t = Timer()
            with t:
                resp = client.execute(sid, steps)
            batch_times.append(t.ms)
        per_step = statistics.mean(batch_times) / batch_size
        print_stats(f"Batch of {batch_size} commands", batch_times)
        print(f"    => per-step avg: {fmt(per_step)}")

    # ---------------------------------------------------------------
    section("7. Rapid Single-step Execution (throughput)")
    # ---------------------------------------------------------------
    n_rapid = 50
    rapid_times: list[float] = []
    overall = Timer()
    with overall:
        for i in range(n_rapid):
            t = Timer()
            with t:
                client.execute(
                    sid,
                    [
                        {"name": f"r-{i}", "command": ["true"]},
                    ],
                )
            rapid_times.append(t.ms)
    print_stats(f"{n_rapid}x single 'true' command", rapid_times)
    throughput = n_rapid / (overall.ms / 1000)
    print(f"  Throughput: {throughput:.1f} steps/sec  (total: {fmt(overall.ms)})")

    # ---------------------------------------------------------------
    section("8. Restore (replay-based)")
    # ---------------------------------------------------------------
    # Use second session for restore test
    sid2 = sessions[1]

    # Create some steps to restore to
    client.execute(
        sid2,
        [
            {"name": "setup-1", "command": ["sh", "-c", "printf 'aaa\\n' > /workspace/a.txt"]},
            {"name": "setup-2", "command": ["echo", "setup done"]},
            {"name": "setup-3", "command": ["sh", "-c", "printf 'bbb\\n' > /workspace/b.txt"]},
        ],
    )

    restore_times: list[float] = []
    for target_idx in ["0", "1", "2"]:
        t = Timer()
        with t:
            client.restore(sid2, target_idx)
        restore_times.append(t.ms)
        # Refresh sid2 since restore changes the underlying pod
        info2 = client.get_session(sid2)
        print(f"  Restore to step {target_idx}: {fmt(t.ms)}  new_pod={info2.pod_name}")
    print_stats("Restore (create sandbox + replay)", restore_times)

    # ---------------------------------------------------------------
    section("9. History & Trajectory Export")
    # ---------------------------------------------------------------
    history_times: list[float] = []
    for _ in range(10):
        t = Timer()
        with t:
            client.get_history(sid)
        history_times.append(t.ms)
    print_stats("GET /history", history_times)

    traj_times: list[float] = []
    for _ in range(10):
        t = Timer()
        with t:
            client.get_trajectory(sid)
        traj_times.append(t.ms)
    print_stats("GET /trajectory", traj_times)

    history = client.get_history(sid)
    print(f"  History size: {len(history)} records")

    # ---------------------------------------------------------------
    section("10. Session Deletion")
    # ---------------------------------------------------------------
    delete_times: list[float] = []
    for s in sessions:
        t = Timer()
        with t:
            client.delete_session(s)
        delete_times.append(t.ms)
    print_stats("DELETE /v1/sessions/{id}", delete_times)

    # ---------------------------------------------------------------
    section("Cleanup")
    # ---------------------------------------------------------------
    try:
        pool_mgr.delete_warmpool(POOL_NAME)
        print(f"  Pool '{POOL_NAME}' deleted.")
    except GatewayError as e:
        print(f"  Cleanup: {e}")

    section("DONE")


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        print("\nInterrupted.")
        sys.exit(1)
