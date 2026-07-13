from __future__ import annotations

import json
from collections.abc import Callable

import httpx

from arl import (
    GatewayClient,
    GatewayError,
    GatewayOperationTimeout,
    InteractiveShellClient,
    SandboxSession,
    WarmPoolManager,
)


def _client_with_handler(handler: Callable[[httpx.Request], httpx.Response]) -> GatewayClient:
    client = GatewayClient(base_url="http://gateway.test")
    client.close()
    client._client = httpx.Client(
        base_url="http://gateway.test",
        transport=httpx.MockTransport(handler),
    )
    return client


def test_gateway_public_routes_have_python_sdk_entrypoints() -> None:
    expected = [
        (GatewayClient, "create_session"),
        (GatewayClient, "get_session"),
        (GatewayClient, "delete_session"),
        (GatewayClient, "execute"),
        (GatewayClient, "get_execute_operation"),
        (GatewayClient, "upload_file"),
        (GatewayClient, "download_file"),
        (GatewayClient, "restore"),
        (GatewayClient, "replay_from"),
        (GatewayClient, "get_history"),
        (GatewayClient, "get_trajectory"),
        (GatewayClient, "iter_session_logs"),
        (GatewayClient, "list_session_logs"),
        (GatewayClient, "list_sessions"),
        (GatewayClient, "list_pools"),
        (GatewayClient, "summary"),
        (GatewayClient, "list_experiments"),
        (GatewayClient, "create_pool"),
        (GatewayClient, "get_pool"),
        (GatewayClient, "scale_pool"),
        (GatewayClient, "delete_pool"),
        (GatewayClient, "destroy_pool"),
        (GatewayClient, "iter_pool_logs"),
        (GatewayClient, "list_pool_logs"),
        (GatewayClient, "create_managed_session"),
        (GatewayClient, "list_experiment_sessions"),
        (GatewayClient, "delete_experiment_info"),
        (GatewayClient, "delete_experiment"),
        (GatewayClient, "health"),
        (InteractiveShellClient, "connect"),
        (SandboxSession, "replay_from"),
        (SandboxSession, "iter_logs"),
        (SandboxSession, "get_logs"),
        (WarmPoolManager, "list_warmpools"),
        (WarmPoolManager, "drain_warmpool"),
        (WarmPoolManager, "destroy_warmpool"),
        (WarmPoolManager, "iter_logs"),
        (WarmPoolManager, "get_logs"),
    ]

    missing = [
        f"{cls.__name__}.{name}" for cls, name in expected if not callable(getattr(cls, name, None))
    ]

    assert missing == []


def test_list_endpoints_parse_models() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        if request.method == "GET" and request.url.path == "/v1/sessions":
            return httpx.Response(
                200,
                json=[
                    {
                        "id": "gw-1",
                        "sandboxName": "gw-1",
                        "namespace": "default",
                        "image": "python:3.12",
                        "profile": "default",
                        "podIP": "10.0.0.1",
                        "podName": "pod-1",
                        "createdAt": "2026-01-01T00:00:00Z",
                        "managed": True,
                        "experimentId": "exp-1",
                    }
                ],
            )
        if request.method == "GET" and request.url.path == "/v1/pools":
            return httpx.Response(
                200,
                json=[
                    {
                        "name": "pool-1",
                        "namespace": "default",
                        "image": "python:3.12",
                        "profile": "default",
                        "replicas": 2,
                        "readyReplicas": 1,
                        "allocatedReplicas": 1,
                        "state": "running",
                    }
                ],
            )
        if request.method == "GET" and request.url.path == "/v1/managed/experiments":
            return httpx.Response(
                200,
                json=[
                    {
                        "experimentId": "exp-1",
                        "sessionCount": 2,
                        "image": "python:3.12",
                        "profile": "default",
                        "namespace": "default",
                    }
                ],
            )
        return httpx.Response(404, json={"error": "unexpected request"})

    with _client_with_handler(handler) as client:
        sessions = client.list_sessions()
        assert sessions[0].id == "gw-1"
        assert sessions[0].managed is True
        assert sessions[0].experiment_id == "exp-1"

        pools = client.list_pools()
        assert pools[0].name == "pool-1"
        assert pools[0].ready_replicas == 1
        assert pools[0].state == "running"

        experiments = client.list_experiments()
        assert experiments[0].experiment_id == "exp-1"
        assert experiments[0].session_count == 2


def test_list_endpoints_pass_query_options() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        if request.method == "GET" and request.url.path == "/v1/sessions":
            assert request.url.params["profile"] == "cpu"
            assert request.url.params["experiment"] == "exp-1"
            assert request.url.params["status"] == "active"
            assert request.url.params["limit"] == "25"
            assert request.url.params["cursor"] == "gw-1"
            return httpx.Response(200, json=[])
        if request.method == "GET" and request.url.path == "/v1/pools":
            assert request.url.params["includeStopped"] == "true"
            return httpx.Response(200, json=[])
        return httpx.Response(404, json={"error": "unexpected request"})

    with _client_with_handler(handler) as client:
        assert client.list_sessions(
            profile="cpu",
            experiment_id="exp-1",
            status="active",
            limit=25,
            cursor="gw-1",
        ) == []
        assert client.list_pools(include_stopped=True) == []


def test_summary_parses_compact_status() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "GET"
        assert request.url.path == "/v1/summary"
        return httpx.Response(
            200,
            json={
                "sessions": 3,
                "managedSessions": 2,
                "pools": 1,
                "readyReplicas": 4,
                "allocatedReplicas": 2,
                "experiments": 1,
            },
        )

    with _client_with_handler(handler) as client:
        summary = client.summary()
        assert summary.sessions == 3
        assert summary.managed_sessions == 2
        assert summary.ready_replicas == 4


def test_log_stream_endpoints_parse_ndjson() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        if request.method == "GET" and request.url.path == "/v1/sessions/gw-1/logs":
            assert request.url.params["follow"] == "false"
            assert request.url.params["tail"] == "10"
            return httpx.Response(
                200,
                content=b'{"timestamp":"t1","level":"info","message":"ready","source":"sidecar"}\n',
            )
        if request.method == "GET" and request.url.path == "/v1/pools/pool-1/logs":
            assert request.url.params["follow"] == "true"
            assert request.url.params["tail"] == "5"
            return httpx.Response(
                200,
                content=(
                    b'{"podName":"pod-1","timestamp":"t1","level":"info",'
                    b'"message":"ready","source":"sidecar"}\n'
                ),
            )
        return httpx.Response(404, json={"error": "unexpected request"})

    with _client_with_handler(handler) as client:
        session_logs = client.list_session_logs("gw-1", tail=10)
        assert session_logs[0].message == "ready"

        pool_logs = list(
            client.iter_pool_logs(
                "pool-1",
                follow=True,
                tail=5,
            )
        )
        assert pool_logs[0].pod_name == "pod-1"
        assert pool_logs[0].source == "sidecar"


def test_create_pool_exposes_image_locality_payload() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "POST"
        assert request.url.path == "/v1/pools"
        body = json.loads(request.content)
        assert body["imageLocality"] is True
        return httpx.Response(201, json={"name": "pool-1", "status": "created"})

    with _client_with_handler(handler) as client:
        client.create_pool(
            name="pool-1",
            image="python:3.12",
            image_locality=True,
        )


def test_create_session_omits_namespace_by_default() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "POST"
        assert request.url.path == "/v1/sessions"
        body = json.loads(request.content)
        assert "namespace" not in body
        assert "allowColdStart" not in body
        return httpx.Response(
            201,
            json={
                "id": "gw-1",
                "sandboxName": "gw-1",
                "namespace": "arl",
                "image": "python:3.12",
                "profile": "default",
                "podIP": "10.0.0.1",
                "podName": "pod-1",
                "createdAt": "2026-01-01T00:00:00Z",
            },
        )

    with _client_with_handler(handler) as client:
        session = client.create_session(image="python:3.12")
        assert session.namespace == "arl"


def test_pool_delete_drains_and_destroy_uses_explicit_endpoint() -> None:
    seen: list[tuple[str, str]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        seen.append((request.method, request.url.path))
        return httpx.Response(204)

    with _client_with_handler(handler) as client:
        client.delete_pool("pool-1")
        client.destroy_pool("pool-1")

    assert seen == [
        ("DELETE", "/v1/pools/pool-1"),
        ("POST", "/v1/pools/pool-1/destroy"),
    ]


def test_execute_uses_operation_id_without_sse_by_default() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "POST"
        assert request.url.path == "/v1/sessions/gw-1/execute"
        assert request.headers.get("accept") != "text/event-stream"
        body = json.loads(request.content)
        assert body["operationID"]
        return httpx.Response(
            200,
            json={
                "sessionID": "gw-1",
                "operationID": body["operationID"],
                "results": [],
                "totalDurationMs": 0,
            },
        )

    with _client_with_handler(handler) as client:
        result = client.execute("gw-1", [{"name": "noop", "command": ["true"]}])
        assert result.operation_id


def test_execute_parses_step_input() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "POST"
        assert request.url.path == "/v1/sessions/gw-1/execute"
        body = json.loads(request.content)
        return httpx.Response(
            200,
            json={
                "sessionID": "gw-1",
                "operationID": body["operationID"],
                "results": [
                    {
                        "index": 0,
                        "name": "echo",
                        "input": {"name": "echo", "command": ["echo", "ok"]},
                        "output": {"stdout": "ok\n", "stderr": "", "exit_code": 0},
                        "snapshot_id": "0",
                        "duration_ms": 1,
                        "timestamp": "2026-01-01T00:00:00Z",
                    }
                ],
                "totalDurationMs": 1,
            },
        )

    with _client_with_handler(handler) as client:
        result = client.execute("gw-1", [{"name": "echo", "command": ["echo", "ok"]}])
        assert result.results[0].input == {"name": "echo", "command": ["echo", "ok"]}


def test_replay_response_is_typed() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "POST"
        assert request.url.path == "/v1/sessions/target/replay"
        body = json.loads(request.content)
        assert body == {"sourceSessionID": "source", "upToStep": 3}
        return httpx.Response(200, json={"stepsReplayed": 4, "errors": 1})

    with _client_with_handler(handler) as client:
        result = client.replay_from("target", "source", up_to_step=3)
        assert result.steps_replayed == 4
        assert result.errors == 1


def test_delete_experiment_info_surfaces_backend_error_field() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "DELETE"
        assert request.url.path == "/v1/managed/experiments/exp-1"
        return httpx.Response(200, json={"deleted": 1, "error": "partial cleanup failed"})

    with _client_with_handler(handler) as client:
        try:
            client.delete_experiment_info("exp-1")
        except GatewayError as exc:
            assert "partial cleanup failed" in str(exc)
        else:
            raise AssertionError("expected delete_experiment_info to surface error")


def test_execute_timeout_without_recover_surfaces_operation_id() -> None:
    calls = 0

    def handler(request: httpx.Request) -> httpx.Response:
        nonlocal calls
        calls += 1
        raise httpx.ReadTimeout("timed out", request=request)

    with _client_with_handler(handler) as client:
        try:
            client.execute(
                "gw-1",
                [{"name": "sleep", "command": ["sleep", "60"]}],
                recover=False,
            )
        except GatewayOperationTimeout as exc:
            assert exc.operation_id
        else:
            raise AssertionError("expected GatewayOperationTimeout")
    assert calls == 1


def _operation_payload(
    op_id: str,
    status: str,
    result: dict[str, object] | None = None,
    error: str = "",
) -> dict[str, object]:
    payload: dict[str, object] = {
        "operationID": op_id,
        "sessionID": "gw-1",
        "status": status,
        "error": error,
    }
    if result is not None:
        payload["result"] = result
    return payload


def test_execute_recovers_result_after_connection_drop() -> None:
    posted_op_ids: list[str] = []

    def handler(request: httpx.Request) -> httpx.Response:
        if request.method == "POST" and request.url.path == "/v1/sessions/gw-1/execute":
            posted_op_ids.append(json.loads(request.content)["operationID"])
            raise httpx.ReadError("connection reset by peer", request=request)
        if request.method == "GET" and "/operations/" in request.url.path:
            op_id = request.url.path.rsplit("/", 1)[-1]
            return httpx.Response(
                200,
                json=_operation_payload(
                    op_id,
                    "done",
                    result={
                        "sessionID": "gw-1",
                        "operationID": op_id,
                        "results": [],
                        "totalDurationMs": 3,
                    },
                ),
            )
        return httpx.Response(404, json={"error": "unexpected request"})

    with _client_with_handler(handler) as client:
        result = client.execute("gw-1", [{"name": "noop", "command": ["true"]}])
        assert posted_op_ids and result.operation_id == posted_op_ids[0]
        assert result.total_duration_ms == 3


def test_execute_resubmits_when_gateway_never_saw_operation() -> None:
    posts = 0

    def handler(request: httpx.Request) -> httpx.Response:
        nonlocal posts
        if request.method == "POST" and request.url.path == "/v1/sessions/gw-1/execute":
            posts += 1
            if posts == 1:
                raise httpx.ConnectError("connection refused", request=request)
            body = json.loads(request.content)
            return httpx.Response(
                200,
                json={
                    "sessionID": "gw-1",
                    "operationID": body["operationID"],
                    "results": [],
                    "totalDurationMs": 0,
                },
            )
        if request.method == "GET" and "/operations/" in request.url.path:
            op_id = request.url.path.rsplit("/", 1)[-1]
            return httpx.Response(404, json={"error": f"operation {op_id} not found"})
        return httpx.Response(404, json={"error": "unexpected request"})

    with _client_with_handler(handler) as client:
        result = client.execute("gw-1", [{"name": "noop", "command": ["true"]}])
        assert result.operation_id
    assert posts == 2


def test_execute_recovery_surfaces_operation_error() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        if request.method == "POST" and request.url.path == "/v1/sessions/gw-1/execute":
            raise httpx.ReadError("connection reset by peer", request=request)
        if request.method == "GET" and "/operations/" in request.url.path:
            op_id = request.url.path.rsplit("/", 1)[-1]
            return httpx.Response(200, json=_operation_payload(op_id, "error", error="boom"))
        return httpx.Response(404, json={"error": "unexpected request"})

    with _client_with_handler(handler) as client:
        try:
            client.execute("gw-1", [{"name": "noop", "command": ["true"]}])
        except GatewayError as exc:
            assert "boom" in str(exc)
        else:
            raise AssertionError("expected GatewayError")


def test_execute_recovery_respects_recover_timeout() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        if request.method == "POST" and request.url.path == "/v1/sessions/gw-1/execute":
            raise httpx.ReadTimeout("timed out", request=request)
        if request.method == "GET" and "/operations/" in request.url.path:
            op_id = request.url.path.rsplit("/", 1)[-1]
            return httpx.Response(200, json=_operation_payload(op_id, "running"))
        return httpx.Response(404, json={"error": "unexpected request"})

    with _client_with_handler(handler) as client:
        try:
            client.execute(
                "gw-1",
                [{"name": "sleep", "command": ["sleep", "60"]}],
                recover_timeout=0.05,
            )
        except GatewayOperationTimeout as exc:
            assert exc.operation_id
        else:
            raise AssertionError("expected GatewayOperationTimeout")


def test_execute_recovery_stops_when_session_is_gone() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        if request.method == "POST" and request.url.path == "/v1/sessions/gw-1/execute":
            raise httpx.ReadError("connection reset by peer", request=request)
        if request.method == "GET" and "/operations/" in request.url.path:
            return httpx.Response(404, json={"error": "session gw-1 not found"})
        return httpx.Response(404, json={"error": "unexpected request"})

    with _client_with_handler(handler) as client:
        try:
            client.execute("gw-1", [{"name": "noop", "command": ["true"]}])
        except GatewayError as exc:
            assert exc.status_code == 404
            assert "session" in exc.error
        else:
            raise AssertionError("expected GatewayError")
