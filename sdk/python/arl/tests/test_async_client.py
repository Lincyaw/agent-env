from __future__ import annotations

import json
from collections.abc import Callable

import httpx
import pytest

from arl import (
    AsyncGatewayClient,
    AsyncManagedSession,
    AsyncSandboxSession,
    GatewayError,
    GatewayOperationTimeout,
)


def _async_client_with_handler(
    handler: Callable[[httpx.Request], httpx.Response],
) -> AsyncGatewayClient:
    client = AsyncGatewayClient(base_url="http://gateway.test")
    client._client = httpx.AsyncClient(
        base_url="http://gateway.test",
        transport=httpx.MockTransport(handler),
    )
    return client


def _session_handler(request: httpx.Request) -> httpx.Response:
    if request.method == "POST" and request.url.path == "/v1/sessions":
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
    if request.method == "POST" and request.url.path == "/v1/sessions/gw-1/execute":
        body = json.loads(request.content)
        return httpx.Response(
            200,
            json={
                "sessionID": "gw-1",
                "operationID": body.get("operationID", "op-1"),
                "results": [
                    {
                        "index": 0,
                        "name": "echo",
                        "output": {"stdout": "hello\n", "stderr": "", "exit_code": 0},
                        "snapshot_id": "0",
                        "duration_ms": 1,
                        "timestamp": "2026-01-01T00:00:00Z",
                    }
                ],
                "totalDurationMs": 1,
            },
        )
    if request.method == "DELETE" and request.url.path == "/v1/sessions/gw-1":
        return httpx.Response(204)
    return httpx.Response(404, json={"error": "unexpected request"})


class TestAsyncGatewayClient:
    async def test_async_public_routes_exist(self) -> None:
        expected = [
            "create_session",
            "get_session",
            "delete_session",
            "execute",
            "get_execute_operation",
            "upload_file",
            "download_file",
            "restore",
            "replay_from",
            "get_history",
            "get_trajectory",
            "iter_session_logs",
            "list_session_logs",
            "list_sessions",
            "list_pools",
            "summary",
            "list_experiments",
            "create_pool",
            "get_pool",
            "scale_pool",
            "delete_pool",
            "destroy_pool",
            "iter_pool_logs",
            "list_pool_logs",
            "create_managed_session",
            "list_experiment_sessions",
            "delete_experiment_info",
            "delete_experiment",
            "health",
        ]
        missing = [name for name in expected if not callable(getattr(AsyncGatewayClient, name, None))]
        assert missing == []

    async def test_list_endpoints_parse_models(self) -> None:
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

        async with _async_client_with_handler(handler) as client:
            sessions = await client.list_sessions()
            assert sessions[0].id == "gw-1"
            assert sessions[0].managed is True

            experiments = await client.list_experiments()
            assert experiments[0].experiment_id == "exp-1"

    async def test_list_endpoints_pass_query_options(self) -> None:
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

        async with _async_client_with_handler(handler) as client:
            assert await client.list_sessions(
                profile="cpu",
                experiment_id="exp-1",
                status="active",
                limit=25,
                cursor="gw-1",
            ) == []
            assert await client.list_pools(include_stopped=True) == []

    async def test_summary_parses_compact_status(self) -> None:
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

        async with _async_client_with_handler(handler) as client:
            summary = await client.summary()
            assert summary.sessions == 3
            assert summary.managed_sessions == 2
            assert summary.ready_replicas == 4

    async def test_execute_uses_operation_id(self) -> None:
        def handler(request: httpx.Request) -> httpx.Response:
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

        async with _async_client_with_handler(handler) as client:
            result = await client.execute("gw-1", [{"name": "noop", "command": ["true"]}])
            assert result.operation_id

    async def test_execute_timeout_without_recover_surfaces_operation_id(self) -> None:
        def handler(request: httpx.Request) -> httpx.Response:
            raise httpx.ReadTimeout("timed out", request=request)

        async with _async_client_with_handler(handler) as client:
            with pytest.raises(GatewayOperationTimeout) as exc_info:
                await client.execute(
                    "gw-1",
                    [{"name": "sleep", "command": ["sleep", "60"]}],
                    recover=False,
                )
            assert exc_info.value.operation_id

    async def test_execute_recovers_result_after_connection_drop(self) -> None:
        def handler(request: httpx.Request) -> httpx.Response:
            if request.method == "POST" and request.url.path == "/v1/sessions/gw-1/execute":
                raise httpx.ReadError("connection reset by peer", request=request)
            if request.method == "GET" and "/operations/" in request.url.path:
                op_id = request.url.path.rsplit("/", 1)[-1]
                return httpx.Response(
                    200,
                    json={
                        "operationID": op_id,
                        "sessionID": "gw-1",
                        "status": "done",
                        "result": {
                            "sessionID": "gw-1",
                            "operationID": op_id,
                            "results": [],
                            "totalDurationMs": 3,
                        },
                    },
                )
            return httpx.Response(404, json={"error": "unexpected request"})

        async with _async_client_with_handler(handler) as client:
            result = await client.execute("gw-1", [{"name": "noop", "command": ["true"]}])
            assert result.operation_id
            assert result.total_duration_ms == 3

    async def test_execute_resubmits_when_gateway_never_saw_operation(self) -> None:
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

        async with _async_client_with_handler(handler) as client:
            result = await client.execute("gw-1", [{"name": "noop", "command": ["true"]}])
            assert result.operation_id
        assert posts == 2

    async def test_replay_response_is_typed(self) -> None:
        def handler(request: httpx.Request) -> httpx.Response:
            body = json.loads(request.content)
            assert body == {"sourceSessionID": "source", "upToStep": 3}
            return httpx.Response(200, json={"stepsReplayed": 4, "errors": 1})

        async with _async_client_with_handler(handler) as client:
            result = await client.replay_from("target", "source", up_to_step=3)
            assert result.steps_replayed == 4
            assert result.errors == 1

    async def test_delete_experiment_info_surfaces_error(self) -> None:
        def handler(request: httpx.Request) -> httpx.Response:
            return httpx.Response(200, json={"deleted": 1, "error": "partial cleanup failed"})

        async with _async_client_with_handler(handler) as client:
            with pytest.raises(GatewayError, match="partial cleanup failed"):
                await client.delete_experiment_info("exp-1")


class TestAsyncSandboxSession:
    async def test_context_manager_creates_and_deletes(self) -> None:
        session = AsyncSandboxSession(image="python:3.12", gateway_url="http://gateway.test")
        session._client = _async_client_with_handler(_session_handler)

        async with session:
            assert session.session_id == "gw-1"
            result = await session.execute([{"name": "echo", "command": ["echo", "hello"]}])
            assert result.results[0].output.stdout == "hello\n"

        assert session.session_id is None

    async def test_execute_without_create_raises(self) -> None:
        session = AsyncSandboxSession(image="python:3.12", gateway_url="http://gateway.test")
        with pytest.raises(RuntimeError, match="No session created"):
            await session.execute([{"name": "noop", "command": ["true"]}])

    async def test_session_api_parity(self) -> None:
        expected = [
            "create_sandbox",
            "execute",
            "execute_container",
            "restore",
            "replay_from",
            "upload_file",
            "download_file",
            "upload_path",
            "download_path",
            "iter_download",
            "get_history",
            "export_trajectory",
            "iter_logs",
            "get_logs",
            "delete_sandbox",
            "aclose",
        ]
        missing = [
            name for name in expected if not callable(getattr(AsyncSandboxSession, name, None))
        ]
        assert missing == []


class TestAsyncManagedSession:
    async def test_create_calls_managed_endpoint(self) -> None:
        def handler(request: httpx.Request) -> httpx.Response:
            if request.method == "POST" and request.url.path == "/v1/managed/sessions":
                body = json.loads(request.content)
                assert body["experimentId"] == "test-exp"
                assert body["image"] == "python:3.12"
                return httpx.Response(
                    201,
                    json={
                        "id": "gw-managed-1",
                        "sandboxName": "gw-managed-1",
                        "namespace": "arl",
                        "image": "python:3.12",
                        "profile": "default",
                        "podIP": "10.0.0.1",
                        "podName": "pod-1",
                        "createdAt": "2026-01-01T00:00:00Z",
                        "experimentId": "test-exp",
                    },
                )
            if request.method == "DELETE" and request.url.path == "/v1/sessions/gw-managed-1":
                return httpx.Response(204)
            return httpx.Response(404, json={"error": "unexpected request"})

        session = AsyncManagedSession(
            image="python:3.12",
            experiment_id="test-exp",
            gateway_url="http://gateway.test",
        )
        session._client = _async_client_with_handler(handler)

        async with session:
            assert session.session_id == "gw-managed-1"
            assert session.experiment_id == "test-exp"

    async def test_managed_session_api_parity(self) -> None:
        assert issubclass(AsyncManagedSession, AsyncSandboxSession)
        assert hasattr(AsyncManagedSession, "experiment_id")
