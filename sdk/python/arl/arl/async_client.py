"""Async Gateway HTTP client for ARL SDK (primary implementation).

All gateway logic lives here.  The synchronous :class:`GatewayClient` in
``gateway_client.py`` is a thin wrapper that delegates every call to this
class via a background event loop.
"""

from __future__ import annotations

import asyncio
import inspect
import json
import os
import time
import uuid
from collections.abc import AsyncIterator, Awaitable, Callable, Iterable
from pathlib import Path
from typing import Any, BinaryIO, TypeVar

import httpx
from pydantic import BaseModel

from arl._base import (
    OPERATION_POLL_INTERVAL_SECONDS,
    OPERATION_STATUS_DONE,
    OPERATION_STATUS_ERROR,
    GatewayError,
    GatewayOperationTimeout,
    build_create_managed_session_body,
    build_create_pool_body,
    build_create_session_body,
    handle_error,
    serialize_steps,
    validate_list,
)
from arl.auth import resolve_auth
from arl.config import resolve_from_config
from arl.configenv import ConfigEnvSpec
from arl.types import (
    BuildResponse,
    ContainerExecuteResponse,
    DeleteExperimentResponse,
    DevboxConfig,
    ExecuteOperationInfo,
    ExecuteResponse,
    ExperimentSummary,
    ForkSessionResponse,
    GatewaySummary,
    LogEntry,
    ManagedSessionInfo,
    PoolInfo,
    PoolLogEntry,
    PrivateContainerSpec,
    ReplayResponse,
    ResourceRequirements,
    RestoreResponse,
    SessionInfo,
    SessionListItem,
    StepRequest,
    StepResult,
    ToolsSpec,
    UploadFileResponse,
)

_ModelT = TypeVar("_ModelT", bound=BaseModel)


class AsyncGatewayClient:
    """Async HTTP client for the ARL Gateway API."""

    def __init__(
        self,
        base_url: str = "",
        timeout: float = 300.0,
        api_key: str | None = None,
        auth: httpx.Auth | None = None,
    ) -> None:
        cfg_url, cfg_key = resolve_from_config(
            gateway_url=base_url,
            api_key=api_key or "",
        )
        self._base_url = cfg_url.rstrip("/")
        resolved_auth = resolve_auth(auth, cfg_key)
        timeout_config = httpx.Timeout(connect=30.0, read=timeout, write=timeout, pool=timeout)
        proxy_url = os.environ.get("http_proxy") or os.environ.get("HTTP_PROXY") or None
        transport = httpx.AsyncHTTPTransport(
            retries=3,
            proxy=proxy_url,
            limits=httpx.Limits(
                max_connections=20,
                max_keepalive_connections=5,
                keepalive_expiry=30.0,
            ),
        )
        self._client = httpx.AsyncClient(
            base_url=self._base_url,
            timeout=timeout_config,
            transport=transport,
            auth=resolved_auth,
        )

    # ------------------------------------------------------------------
    # Session APIs
    # ------------------------------------------------------------------

    async def create_session(
        self,
        image: str | None = None,
        *,
        profile: str | None = "default",
        mode: str | None = None,
        devbox: DevboxConfig | dict[str, object] | None = None,
        config_env: ConfigEnvSpec | dict[str, Any] | None = None,
        idle_timeout_seconds: int | None = None,
        allocation_timeout_seconds: int | None = None,
        private_containers: Iterable[PrivateContainerSpec | dict[str, Any]] | None = None,
        allow_internet: bool | None = None,
    ) -> SessionInfo:
        if not image and not profile:
            raise ValueError("image or profile is required")
        body = build_create_session_body(
            image, profile, mode, devbox, config_env,
            idle_timeout_seconds, allocation_timeout_seconds,
            private_containers, allow_internet,
        )
        resp = await self._client.post("/v1/sessions", json=body)
        handle_error(resp)
        return SessionInfo.model_validate(resp.json())

    async def get_session(self, session_id: str) -> SessionInfo:
        resp = await self._client.get(f"/v1/sessions/{session_id}")
        handle_error(resp)
        return SessionInfo.model_validate(resp.json())

    async def delete_session(self, session_id: str) -> None:
        resp = await self._client.delete(f"/v1/sessions/{session_id}")
        handle_error(resp)

    async def fork_session(self, session_id: str, step: int) -> ForkSessionResponse:
        """Fork a session from a historical checkpoint step.

        Creates a new session with the filesystem state of the source
        session at the given step.  Requires checkpoint to be enabled on the
        gateway (SANDBOX_CHECKPOINT_ENABLED=true).
        """
        resp = await self._client.post(
            f"/v1/sessions/{session_id}/fork", json={"step": step}
        )
        handle_error(resp)
        return ForkSessionResponse.model_validate(resp.json())

    async def suspend_session(self, session_id: str) -> None:
        resp = await self._client.post(f"/v1/sessions/{session_id}/suspend")
        handle_error(resp)

    async def resume_session(self, session_id: str) -> None:
        resp = await self._client.post(f"/v1/sessions/{session_id}/resume")
        handle_error(resp)

    async def update_network_policy(
        self,
        session_id: str,
        *,
        allow_internet: bool,
        egress_cidrs: list[str] | None = None,
    ) -> None:
        """Toggle a running session's egress network policy."""
        body: dict[str, Any] = {"allowInternet": allow_internet}
        if egress_cidrs:
            body["egressCIDRs"] = egress_cidrs
        resp = await self._client.patch(
            f"/v1/sessions/{session_id}/network-policy", json=body
        )
        handle_error(resp)

    async def execute(
        self,
        session_id: str,
        steps: list[StepRequest | dict[str, Any]],
        trace_id: str | None = None,
        operation_id: str | None = None,
        on_output: Callable[[str, str], None | Awaitable[None]] | None = None,
        recover: bool = True,
        recover_timeout: float | None = None,
    ) -> ExecuteResponse:
        """Execute steps in a session.

        Non-streaming calls are idempotent via ``operationID``.  With
        ``recover=True`` (default) the client polls the operation after a
        transport failure and returns the result as if the connection had
        never broken.

        ``on_output`` may be a regular or an ``async`` callable.
        """
        body: dict[str, Any] = {"steps": serialize_steps(steps)}
        if trace_id is not None:
            body["traceID"] = trace_id

        if on_output is not None:
            return await self._execute_sse(
                session_id, body, {"Accept": "text/event-stream"}, on_output,
            )

        op_id = operation_id or str(uuid.uuid4())
        body["operationID"] = op_id
        deadline = time.monotonic() + recover_timeout if recover_timeout is not None else None
        try:
            resp = await self._client.post(
                f"/v1/sessions/{session_id}/execute", json=body
            )
        except httpx.TransportError as exc:
            if recover:
                return await self._poll_execute_operation(
                    session_id, op_id, deadline=deadline, resubmit_body=body,
                )
            if isinstance(exc, httpx.TimeoutException):
                raise GatewayOperationTimeout(
                    op_id, "execute operation is still pending"
                ) from exc
            raise
        if resp.status_code == 202:
            return await self._poll_execute_operation(
                session_id, op_id, deadline=deadline,
            )
        handle_error(resp)
        result = ExecuteResponse.model_validate(resp.json())
        if not result.operation_id:
            result.operation_id = op_id
        return result

    async def get_execute_operation(
        self, session_id: str, operation_id: str,
    ) -> ExecuteOperationInfo:
        resp = await self._client.get(
            f"/v1/sessions/{session_id}/operations/{operation_id}"
        )
        handle_error(resp)
        return ExecuteOperationInfo.model_validate(resp.json())

    async def execute_container(
        self,
        session_id: str,
        container: str,
        steps: list[StepRequest | dict[str, Any]],
    ) -> ContainerExecuteResponse:
        body: dict[str, Any] = {"steps": serialize_steps(steps)}
        resp = await self._client.post(
            f"/v1/sessions/{session_id}/containers/{container}/execute",
            json=body,
        )
        handle_error(resp)
        return ContainerExecuteResponse.model_validate(resp.json())

    # ------------------------------------------------------------------
    # File APIs
    # ------------------------------------------------------------------

    async def upload_file(
        self,
        session_id: str,
        path: str,
        content: str | bytes | Iterable[bytes] | BinaryIO,
        sha256: str | None = None,
    ) -> UploadFileResponse:
        headers: dict[str, str] = {
            "Content-Type": "application/octet-stream",
            "X-ARL-Path": path,
        }
        if sha256:
            headers["X-ARL-SHA256"] = sha256
        resp = await self._client.post(
            f"/v1/sessions/{session_id}/upload-file",
            content=content,
            headers=headers,
        )
        handle_error(resp)
        return UploadFileResponse.model_validate(resp.json())

    async def download_file(self, session_id: str, path: str) -> bytes:
        resp = await self._client.post(
            f"/v1/sessions/{session_id}/download-file", json={"path": path},
        )
        handle_error(resp)
        return resp.content

    async def iter_download_file(
        self,
        session_id: str,
        path: str,
        chunk_size: int = 1024 * 1024,
    ) -> AsyncIterator[bytes]:
        async with self._client.stream(
            "POST",
            f"/v1/sessions/{session_id}/download-file",
            json={"path": path},
        ) as resp:
            handle_error(resp)
            async for chunk in resp.aiter_bytes(chunk_size=chunk_size):
                if chunk:
                    yield chunk

    async def upload_path(
        self,
        session_id: str,
        local_path: str | Path,
        remote_path: str,
        sha256: str | None = None,
    ) -> UploadFileResponse:
        with Path(local_path).open("rb") as file:
            return await self.upload_file(session_id, remote_path, file, sha256=sha256)

    async def download_path(
        self,
        session_id: str,
        remote_path: str,
        local_path: str | Path,
    ) -> None:
        target = Path(local_path)
        target.parent.mkdir(parents=True, exist_ok=True)
        with target.open("wb") as file:
            async for chunk in self.iter_download_file(session_id, remote_path):
                file.write(chunk)

    async def send_stdin(self, session_id: str, handle: str, data: str) -> None:
        resp = await self._client.post(
            f"/v1/sessions/{session_id}/stdin",
            json={"handle": handle, "data": data},
        )
        handle_error(resp)

    # ------------------------------------------------------------------
    # Long-running operation helpers
    # ------------------------------------------------------------------

    async def _submit_operation(
        self,
        session_id: str,
        url: str,
        body: dict[str, Any],
        operation_id: str,
        response_model: type[_ModelT],
        recover: bool = True,
        recover_timeout: float | None = None,
    ) -> _ModelT:
        deadline = time.monotonic() + recover_timeout if recover_timeout is not None else None
        try:
            resp = await self._client.post(url, json=body)
        except httpx.TransportError as exc:
            if recover:
                raw = await self._poll_operation(
                    session_id, operation_id,
                    deadline=deadline, resubmit_url=url, resubmit_body=body,
                )
                return response_model.model_validate(raw)
            if isinstance(exc, httpx.TimeoutException):
                raise GatewayOperationTimeout(
                    operation_id, "operation is still pending"
                ) from exc
            raise
        if resp.status_code == 202:
            raw = await self._poll_operation(
                session_id, operation_id, deadline=deadline,
            )
            return response_model.model_validate(raw)
        handle_error(resp)
        return response_model.model_validate(resp.json())

    async def replay_from(
        self,
        session_id: str,
        source_session_id: str,
        up_to_step: int | None = None,
        operation_id: str | None = None,
        recover: bool = True,
        recover_timeout: float | None = None,
    ) -> ReplayResponse:
        body: dict[str, Any] = {"sourceSessionID": source_session_id}
        if up_to_step is not None:
            body["upToStep"] = up_to_step
        op_id = operation_id or str(uuid.uuid4())
        body["operationID"] = op_id
        return await self._submit_operation(
            session_id, f"/v1/sessions/{session_id}/replay",
            body, op_id, ReplayResponse, recover, recover_timeout,
        )

    async def restore(
        self,
        session_id: str,
        snapshot_id: str,
        operation_id: str | None = None,
        recover: bool = True,
        recover_timeout: float | None = None,
    ) -> RestoreResponse:
        body: dict[str, Any] = {"snapshotID": snapshot_id}
        op_id = operation_id or str(uuid.uuid4())
        body["operationID"] = op_id
        return await self._submit_operation(
            session_id, f"/v1/sessions/{session_id}/restore",
            body, op_id, RestoreResponse, recover, recover_timeout,
        )

    # ------------------------------------------------------------------
    # History / trajectory
    # ------------------------------------------------------------------

    async def get_history(self, session_id: str) -> list[StepResult]:
        resp = await self._client.get(f"/v1/sessions/{session_id}/history")
        handle_error(resp)
        return validate_list(resp.json(), StepResult)

    async def get_trajectory(self, session_id: str) -> str:
        resp = await self._client.get(f"/v1/sessions/{session_id}/trajectory")
        handle_error(resp)
        return resp.text

    async def list_sessions(
        self,
        *,
        profile: str | None = None,
        experiment_id: str | None = None,
        status: str | None = None,
        limit: int | None = None,
        cursor: str | None = None,
    ) -> list[SessionListItem]:
        params: dict[str, str | int] = {}
        if profile:
            params["profile"] = profile
        if experiment_id:
            params["experiment"] = experiment_id
        if status:
            params["status"] = status
        if limit is not None:
            params["limit"] = limit
        if cursor:
            params["cursor"] = cursor
        resp = await self._client.get("/v1/sessions", params=params)
        handle_error(resp)
        return validate_list(resp.json(), SessionListItem)

    async def iter_session_logs(
        self,
        session_id: str,
        *,
        follow: bool = False,
        tail: int = 100,
    ) -> AsyncIterator[LogEntry]:
        params = {"follow": str(follow).lower(), "tail": str(tail)}
        async with self._client.stream(
            "GET", f"/v1/sessions/{session_id}/logs", params=params,
        ) as resp:
            if resp.status_code >= 400:
                await resp.aread()
                handle_error(resp)
            async for line in resp.aiter_lines():
                line = line.strip()
                if not line:
                    continue
                yield LogEntry.model_validate(json.loads(line))

    async def list_session_logs(
        self, session_id: str, *, tail: int = 100,
    ) -> list[LogEntry]:
        return [
            entry
            async for entry in self.iter_session_logs(
                session_id, follow=False, tail=tail,
            )
        ]

    # ------------------------------------------------------------------
    # Pool APIs
    # ------------------------------------------------------------------

    async def create_pool(
        self,
        name: str,
        image: str,
        replicas: int = 2,
        profile: str = "default",
        tools: ToolsSpec | None = None,
        resources: ResourceRequirements | None = None,
        workspace_dir: str = "/workspace",
        config_env: ConfigEnvSpec | dict[str, Any] | None = None,
        image_locality: dict[str, Any] | bool | None = None,
        private_containers: Iterable[PrivateContainerSpec | dict[str, Any]] | None = None,
    ) -> None:
        body = build_create_pool_body(
            name, image, replicas, profile, tools, resources,
            workspace_dir, config_env, image_locality, private_containers,
        )
        resp = await self._client.post("/v1/pools", json=body)
        handle_error(resp)

    async def list_pools(self, *, include_stopped: bool = False) -> list[PoolInfo]:
        params = {"includeStopped": "true"} if include_stopped else None
        resp = await self._client.get("/v1/pools", params=params)
        handle_error(resp)
        return validate_list(resp.json(), PoolInfo)

    async def summary(self) -> GatewaySummary:
        resp = await self._client.get("/v1/summary")
        handle_error(resp)
        return GatewaySummary.model_validate(resp.json())

    async def get_pool(self, name: str) -> PoolInfo:
        resp = await self._client.get(f"/v1/pools/{name}")
        handle_error(resp)
        return PoolInfo.model_validate(resp.json())

    async def delete_pool(self, name: str) -> None:
        resp = await self._client.delete(f"/v1/pools/{name}")
        handle_error(resp)

    async def destroy_pool(self, name: str) -> None:
        resp = await self._client.post(f"/v1/pools/{name}/destroy")
        handle_error(resp)

    async def scale_pool(
        self,
        name: str,
        replicas: int,
        resources: ResourceRequirements | None = None,
    ) -> PoolInfo:
        body: dict[str, Any] = {"replicas": replicas}
        if resources is not None:
            body["resources"] = resources.model_dump(exclude_none=True)
        resp = await self._client.patch(f"/v1/pools/{name}", json=body)
        handle_error(resp)
        return PoolInfo.model_validate(resp.json())

    async def iter_pool_logs(
        self,
        name: str,
        *,
        follow: bool = False,
        tail: int = 100,
    ) -> AsyncIterator[PoolLogEntry]:
        params = {"follow": str(follow).lower(), "tail": str(tail)}
        async with self._client.stream(
            "GET", f"/v1/pools/{name}/logs", params=params,
        ) as resp:
            if resp.status_code >= 400:
                await resp.aread()
                handle_error(resp)
            async for line in resp.aiter_lines():
                line = line.strip()
                if not line:
                    continue
                yield PoolLogEntry.model_validate(json.loads(line))

    async def list_pool_logs(self, name: str, *, tail: int = 100) -> list[PoolLogEntry]:
        return [
            entry async for entry in self.iter_pool_logs(name, follow=False, tail=tail)
        ]

    # ------------------------------------------------------------------
    # Managed Session APIs
    # ------------------------------------------------------------------

    async def create_managed_session(
        self,
        image: str,
        experiment_id: str,
        profile: str = "default",
        mode: str | None = None,
        devbox: DevboxConfig | dict[str, object] | None = None,
        resources: ResourceRequirements | None = None,
        tools: ToolsSpec | None = None,
        workspace_dir: str = "/workspace",
        idle_timeout_seconds: int | None = None,
        allocation_timeout_seconds: int | None = None,
        config_env: ConfigEnvSpec | dict[str, Any] | None = None,
        private_containers: Iterable[PrivateContainerSpec | dict[str, Any]] | None = None,
        allow_internet: bool | None = None,
    ) -> ManagedSessionInfo:
        body = build_create_managed_session_body(
            image, experiment_id, profile, mode, devbox, config_env,
            resources, tools, workspace_dir, idle_timeout_seconds,
            allocation_timeout_seconds, private_containers, allow_internet,
        )
        resp = await self._client.post("/v1/managed/sessions", json=body)
        handle_error(resp)
        return ManagedSessionInfo.model_validate(resp.json())

    async def list_experiment_sessions(
        self, experiment_id: str,
    ) -> list[ManagedSessionInfo]:
        resp = await self._client.get(
            f"/v1/managed/experiments/{experiment_id}/sessions"
        )
        handle_error(resp)
        return validate_list(resp.json(), ManagedSessionInfo)

    async def list_experiments(self) -> list[ExperimentSummary]:
        resp = await self._client.get("/v1/managed/experiments")
        handle_error(resp)
        return validate_list(resp.json(), ExperimentSummary)

    async def delete_experiment_info(
        self, experiment_id: str,
    ) -> DeleteExperimentResponse:
        resp = await self._client.delete(
            f"/v1/managed/experiments/{experiment_id}"
        )
        handle_error(resp)
        result = DeleteExperimentResponse.model_validate(resp.json())
        if result.error:
            raise GatewayError(resp.status_code, result.error)
        return result

    async def delete_experiment(self, experiment_id: str) -> int:
        return (await self.delete_experiment_info(experiment_id)).deleted

    # ------------------------------------------------------------------
    # Build API
    # ------------------------------------------------------------------

    async def build_image(
        self,
        image: str,
        context: BinaryIO | bytes,
        *,
        build_args: dict[str, str] | None = None,
        timeout: int | None = None,
        cache: bool = True,
    ) -> BuildResponse:
        """Build a container image via the gateway's Kaniko build API."""
        data: dict[str, str] = {"image": image, "cache": str(cache).lower()}
        if build_args is not None:
            data["build_args"] = json.dumps(build_args)
        if timeout is not None:
            data["timeout"] = str(timeout)
        files = {"context": ("context.tar.gz", context, "application/gzip")}
        build_timeout = httpx.Timeout(
            connect=30.0,
            read=float(timeout or 1800),
            write=300.0,
            pool=float(timeout or 1800),
        )
        resp = await self._client.post(
            "/v1/build", data=data, files=files, timeout=build_timeout,
        )
        handle_error(resp)
        result = BuildResponse.model_validate(resp.json())
        if result.status == "failed":
            raise GatewayError(422, f"image build failed: {result.log}")
        return result

    # ------------------------------------------------------------------
    # Iroh direct-connect
    # ------------------------------------------------------------------

    async def get_iroh_addr(self, session_id: str) -> str:
        resp = await self._client.get(f"/v1/sessions/{session_id}/iroh-addr")
        handle_error(resp)
        data = resp.json()
        addr: str = data.get("addr", "")
        return addr

    # ------------------------------------------------------------------
    # Health
    # ------------------------------------------------------------------

    async def health(self) -> bool:
        try:
            resp = await self._client.get("/healthz")
            return resp.status_code == 200
        except httpx.HTTPError:
            return False

    async def aclose(self) -> None:
        await self._client.aclose()

    async def __aenter__(self) -> AsyncGatewayClient:
        return self

    async def __aexit__(self, *_: object) -> None:
        await self.aclose()

    # ------------------------------------------------------------------
    # Internal: operation polling
    # ------------------------------------------------------------------

    async def _poll_operation(
        self,
        session_id: str,
        operation_id: str,
        *,
        deadline: float | None,
        resubmit_url: str | None = None,
        resubmit_body: dict[str, Any] | None = None,
    ) -> dict[str, object]:
        url = resubmit_url or f"/v1/sessions/{session_id}/execute"
        while True:
            op: ExecuteOperationInfo | None = None
            try:
                op = await self.get_execute_operation(session_id, operation_id)
            except httpx.TransportError:
                pass
            except GatewayError as exc:
                if exc.status_code != 404 or "operation" not in exc.error:
                    raise
                if resubmit_body is not None:
                    try:
                        resp = await self._client.post(url, json=resubmit_body)
                    except httpx.TransportError:
                        pass
                    else:
                        handle_error(resp)
                        result: dict[str, object] = resp.json()
                        return result
                else:
                    raise
            if op is not None:
                status = op.status.lower()
                if status == OPERATION_STATUS_ERROR:
                    raise GatewayError(
                        500, op.error or f"operation {operation_id} failed"
                    )
                if op.result is not None:
                    return op.result
                if status == OPERATION_STATUS_DONE:
                    raise GatewayError(
                        500, f"operation {operation_id} finished without result"
                    )
            sleep_for = OPERATION_POLL_INTERVAL_SECONDS
            if deadline is not None:
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise GatewayOperationTimeout(
                        operation_id, "operation is still pending"
                    )
                sleep_for = min(sleep_for, remaining)
            await asyncio.sleep(sleep_for)

    async def _poll_execute_operation(
        self,
        session_id: str,
        operation_id: str,
        *,
        deadline: float | None,
        resubmit_body: dict[str, Any] | None = None,
    ) -> ExecuteResponse:
        raw = await self._poll_operation(
            session_id,
            operation_id,
            deadline=deadline,
            resubmit_url=f"/v1/sessions/{session_id}/execute",
            resubmit_body=resubmit_body,
        )
        result = ExecuteResponse.model_validate(raw)
        if not result.operation_id:
            result.operation_id = operation_id
        return result

    # ------------------------------------------------------------------
    # Internal: SSE streaming
    # ------------------------------------------------------------------

    async def _execute_sse(
        self,
        session_id: str,
        body: dict[str, Any],
        headers: dict[str, str],
        on_output: Callable[[str, str], None | Awaitable[None]] | None,
    ) -> ExecuteResponse:
        results: list[StepResult] = []
        async with self._client.stream(
            "POST",
            f"/v1/sessions/{session_id}/execute",
            json=body,
            headers=headers,
        ) as response:
            if response.status_code >= 400:
                await response.aread()
                handle_error(response)

            content_type = response.headers.get("content-type", "")
            if "text/event-stream" not in content_type:
                await response.aread()
                return ExecuteResponse.model_validate(response.json())

            event_type = ""
            data_buf = ""
            async for line in response.aiter_lines():
                if line.startswith("event: "):
                    event_type = line[7:]
                    data_buf = ""
                elif line.startswith("data: "):
                    data_buf = line[6:]
                elif line == "":
                    if event_type and data_buf:
                        await self._dispatch_sse(
                            event_type, data_buf, results, on_output,
                        )
                    event_type = ""
                    data_buf = ""

            if event_type and data_buf:
                await self._dispatch_sse(
                    event_type, data_buf, results, on_output,
                )

        total_ms = sum(r.duration_ms for r in results)
        return ExecuteResponse.model_validate(
            {"sessionID": session_id, "results": results, "totalDurationMs": total_ms}
        )

    @staticmethod
    async def _dispatch_sse(
        event_type: str,
        data: str,
        results: list[StepResult],
        on_output: Callable[[str, str], None | Awaitable[None]] | None,
    ) -> None:
        if event_type == "output":
            if on_output is not None:
                try:
                    parsed = json.loads(data)
                    rv = on_output(parsed.get("stdout", ""), parsed.get("stderr", ""))
                    if inspect.isawaitable(rv):
                        await rv
                except (json.JSONDecodeError, TypeError):
                    pass
        elif event_type == "result":
            try:
                results.append(StepResult.model_validate(json.loads(data)))
            except (json.JSONDecodeError, ValueError):
                pass
