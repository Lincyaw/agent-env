#!/usr/bin/env python3
"""Functional and basic performance checks for the sandbox-backed gateway."""

from __future__ import annotations

import argparse
import json
import math
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass
from typing import Any


@dataclass
class Measurement:
    create_seconds: float
    execute_seconds: float


class GatewayHTTPError(RuntimeError):
    def __init__(self, status: int, body: str) -> None:
        self.status = status
        self.body = body
        super().__init__(f"gateway returned HTTP {status}: {body}")


def call(
    base_url: str,
    method: str,
    path: str,
    body: dict[str, Any] | bytes | None = None,
    raw: bool = False,
    timeout: float = 300.0,
) -> Any:
    data = None
    headers = {}
    if isinstance(body, bytes):
        data = body
        headers["Content-Type"] = "application/octet-stream"
    elif body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(
        base_url.rstrip("/") + path,
        data=data,
        headers=headers,
        method=method,
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            payload = resp.read()
            if raw:
                return payload
            if not payload:
                return None
            return json.loads(payload.decode())
    except urllib.error.HTTPError as exc:
        payload = exc.read().decode(errors="replace")
        raise GatewayHTTPError(exc.code, payload) from exc


def create_pool(args: argparse.Namespace) -> None:
    call(
        args.gateway_url,
        "POST",
        "/v1/pools",
        {
            "name": args.pool,
            "namespace": args.namespace,
            "image": args.image,
            "profile": args.pool,
            "replicas": args.replicas,
            "workspaceDir": "/workspace",
        },
    )


def wait_pool_ready(args: argparse.Namespace) -> dict[str, Any]:
    deadline = time.time() + args.pool_ready_timeout
    query = urllib.parse.urlencode({"namespace": args.namespace})
    last: Any = None
    while time.time() < deadline:
        try:
            info = call(args.gateway_url, "GET", f"/v1/pools/{args.pool}?{query}")
        except GatewayHTTPError as exc:
            last = exc
            time.sleep(1)
            continue
        last = info
        if int(info.get("readyReplicas") or 0) >= min(args.replicas, 1):
            return info
        time.sleep(1)
    raise TimeoutError(f"pool {args.namespace}/{args.pool} not ready before timeout; last={last!r}")


def create_session(args: argparse.Namespace) -> dict[str, Any]:
    return call(
        args.gateway_url,
        "POST",
        "/v1/sessions",
        {"image": args.image, "profile": args.pool, "namespace": args.namespace},
    )


def execute(
    args: argparse.Namespace,
    session_id: str,
    command: list[str],
    name: str,
) -> dict[str, Any]:
    return call(
        args.gateway_url,
        "POST",
        f"/v1/sessions/{session_id}/execute",
        {"steps": [{"name": name, "command": command}]},
    )


def delete_session(args: argparse.Namespace, session_id: str) -> None:
    call(args.gateway_url, "DELETE", f"/v1/sessions/{session_id}")


def parse_ndjson(raw: bytes) -> list[dict[str, Any]]:
    lines = [line for line in raw.decode(errors="replace").splitlines() if line.strip()]
    return [json.loads(line) for line in lines]


def assert_stdout(resp: dict[str, Any], expected: str) -> None:
    results = resp.get("results") or []
    if not results:
        raise AssertionError(f"missing execute result: {resp}")
    output = results[0].get("output") or {}
    if output.get("exit_code") != 0:
        raise AssertionError(f"non-zero exit: {output}")
    stdout = output.get("stdout", "")
    if expected not in stdout:
        raise AssertionError(f"stdout {stdout!r} did not contain {expected!r}")


def functional_check(args: argparse.Namespace) -> None:
    session = create_session(args)
    session_id = session["id"]
    try:
        first = execute(args, session_id, ["/bin/sh", "-c", "echo ok && pwd"], "smoke")
        assert_stdout(first, "ok")
        assert_stdout(first, "/workspace")

        upload = call(
            args.gateway_url,
            "PUT",
            f"/v1/sessions/{session_id}/files/upload.txt",
            b"uploaded",
        )
        if upload.get("bytesWritten") != len(b"uploaded"):
            raise AssertionError(f"unexpected upload response: {upload}")

        downloaded = call(
            args.gateway_url,
            "GET",
            f"/v1/sessions/{session_id}/files/upload.txt",
            raw=True,
        ).decode()
        if downloaded != "uploaded":
            raise AssertionError(f"downloaded content = {downloaded!r}, want 'uploaded'")

        created = execute(
            args,
            session_id,
            ["/bin/sh", "-c", "printf one > restore.txt && cat restore.txt"],
            "create-restore-file",
        )
        assert_stdout(created, "one")
        snapshot = str(created["results"][0]["snapshot_id"])

        changed = execute(
            args,
            session_id,
            ["/bin/sh", "-c", "printf two > restore.txt && cat restore.txt"],
            "mutate-restore-file",
        )
        assert_stdout(changed, "two")

        history = call(args.gateway_url, "GET", f"/v1/sessions/{session_id}/history")
        if len(history) < 2:
            raise AssertionError(f"history too short after execute steps: {history}")

        call(
            args.gateway_url,
            "POST",
            f"/v1/sessions/{session_id}/restore",
            {"snapshotID": snapshot},
        )
        restored = execute(args, session_id, ["/bin/sh", "-c", "cat restore.txt"], "verify-restore")
        assert_stdout(restored, "one")
    finally:
        delete_session(args, session_id)


def logs_check(args: argparse.Namespace) -> dict[str, int]:
    session = create_session(args)
    session_id = session["id"]
    try:
        resp = execute(
            args,
            session_id,
            ["/bin/sh", "-c", "echo logs-ok"],
            "logs",
        )
        assert_stdout(resp, "logs-ok")

        session_logs: list[dict[str, Any]] = []
        pool_logs: list[dict[str, Any]] = []
        query = urllib.parse.urlencode({"namespace": args.namespace, "tail": 50})
        deadline = time.time() + args.logs_timeout
        while time.time() < deadline:
            session_raw = call(
                args.gateway_url,
                "GET",
                f"/v1/sessions/{session_id}/logs?tail=50",
                raw=True,
                timeout=20,
            )
            pool_raw = call(
                args.gateway_url,
                "GET",
                f"/v1/pools/{args.pool}/logs?{query}",
                raw=True,
                timeout=20,
            )
            session_logs = parse_ndjson(session_raw)
            pool_logs = parse_ndjson(pool_raw)
            if session_logs:
                break
            time.sleep(1)

        if not session_logs:
            raise AssertionError("session logs endpoint returned no log entries")
        return {"sessionLogs": len(session_logs), "poolLogs": len(pool_logs)}
    finally:
        delete_session(args, session_id)


def managed_session_check(args: argparse.Namespace) -> None:
    experiment_id = ("e2e-" + args.pool[-24:]).strip("-")
    managed = call(
        args.gateway_url,
        "POST",
        "/v1/managed/sessions",
        {
            "image": args.image,
            "profile": args.pool,
            "namespace": args.namespace,
            "experimentId": experiment_id,
            "workspaceDir": "/workspace",
        },
    )
    session_id = managed["id"]
    try:
        resp = execute(args, session_id, ["/bin/sh", "-c", "echo managed-ok"], "managed")
        assert_stdout(resp, "managed-ok")
    finally:
        call(args.gateway_url, "DELETE", f"/v1/managed/experiments/{experiment_id}")


def perf_one(args: argparse.Namespace, idx: int) -> Measurement:
    start = time.perf_counter()
    session = create_session(args)
    create_seconds = time.perf_counter() - start
    session_id = session["id"]
    try:
        exec_start = time.perf_counter()
        resp = execute(args, session_id, ["/bin/sh", "-c", f"echo perf-{idx}"], "perf")
        execute_seconds = time.perf_counter() - exec_start
        assert_stdout(resp, f"perf-{idx}")
        return Measurement(create_seconds=create_seconds, execute_seconds=execute_seconds)
    finally:
        delete_session(args, session_id)


def percentile(values: list[float], p: float) -> float:
    if not values:
        return 0.0
    ordered = sorted(values)
    index = max(0, min(len(ordered) - 1, math.ceil((p / 100.0) * len(ordered)) - 1))
    return ordered[index]


def summarize(measurements: list[Measurement]) -> dict[str, Any]:
    create = [m.create_seconds for m in measurements]
    execute_lat = [m.execute_seconds for m in measurements]
    return {
        "samples": len(measurements),
        "create_seconds": {
            "min": min(create),
            "p50": percentile(create, 50),
            "p95": percentile(create, 95),
            "max": max(create),
        },
        "execute_seconds": {
            "min": min(execute_lat),
            "p50": percentile(execute_lat, 50),
            "p95": percentile(execute_lat, 95),
            "max": max(execute_lat),
        },
    }


def perf_check(args: argparse.Namespace) -> dict[str, Any]:
    measurements: list[Measurement] = []
    with ThreadPoolExecutor(max_workers=args.concurrency) as executor:
        futures = [executor.submit(perf_one, args, i) for i in range(args.sessions)]
        for future in as_completed(futures):
            measurements.append(future.result())
    return summarize(measurements)


def metrics_check(args: argparse.Namespace) -> list[str]:
    if not args.metrics_url:
        return []

    with urllib.request.urlopen(args.metrics_url.rstrip("/") + "/metrics", timeout=20) as resp:
        metrics_text = resp.read().decode()

    required = [
        "arl_session_allocation_seconds",
        "arl_gateway_step_result_total",
        "arl_gateway_step_duration_seconds",
        "arl_gateway_sidecar_call_seconds",
    ]
    missing = [name for name in required if name not in metrics_text]
    if missing:
        raise AssertionError(f"missing Prometheus metrics: {missing}")
    if f'pool="{args.pool}"' not in metrics_text:
        raise AssertionError(f"Prometheus metrics did not include pool label {args.pool!r}")
    return required


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--gateway-url", default="http://127.0.0.1:18080")
    parser.add_argument("--metrics-url", default="")
    parser.add_argument("--namespace", default="agent-env-sandbox-port-test")
    parser.add_argument("--pool", required=True)
    parser.add_argument("--image", default="busybox:1.36.1")
    parser.add_argument("--replicas", type=int, default=1)
    parser.add_argument("--pool-ready-timeout", type=float, default=180.0)
    parser.add_argument("--logs-timeout", type=float, default=30.0)
    parser.add_argument("--sessions", type=int, default=8)
    parser.add_argument("--concurrency", type=int, default=2)
    args = parser.parse_args()

    create_pool(args)
    pool = wait_pool_ready(args)
    print(json.dumps({"event": "pool_ready", "pool": pool}, sort_keys=True))

    functional_check(args)
    print(json.dumps({"event": "functional_passed"}, sort_keys=True))

    logs = logs_check(args)
    print(json.dumps({"event": "logs_passed", "summary": logs}, sort_keys=True))

    managed_session_check(args)
    print(json.dumps({"event": "managed_session_passed"}, sort_keys=True))

    perf = perf_check(args)
    print(json.dumps({"event": "perf_summary", "summary": perf}, sort_keys=True))

    metrics = metrics_check(args)
    if metrics:
        print(json.dumps({"event": "metrics_passed", "metrics": metrics}, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(json.dumps({"event": "failed", "error": str(exc)}), file=sys.stderr)
        raise
