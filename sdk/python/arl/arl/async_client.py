"""Async Gateway HTTP client for ARL SDK.

Mirrors :class:`GatewayClient` with ``httpx.AsyncClient`` so callers in an
async event loop (e.g. AgentM) can avoid ``asyncio.to_thread`` entirely.
"""

from __future__ import annotations

import json
import os
import uuid
from collections.abc import AsyncIterator, Callable, Iterable
from pathlib import Path
from typing import Any, BinaryIO, TypeVar

import httpx
from pydantic import BaseModel

from arl.auth import resolve_auth
from arl.config import resolve_from_config
from arl.configenv import ConfigEnvSpec
from arl.gateway_client import (
    GatewayError,
    GatewayOperationTimeout,
    _quote_file_path,
    _serialize_config_env,
    _serialize_private_containers,
)
from arl.types import (
    ContainerExecuteResponse,
    DeleteExperimentResponse,
    ErrorResponse,
    ExecuteOperationInfo,
    ExecuteResponse,
    ExperimentSummary,
    LogEntry,
    ManagedSessionInfo,
    PoolInfo,
    PoolLogEntry,
    PrivateContainerSpec,
    ReplayResponse,
    ResourceRequirements,
    SessionInfo,
    SessionListItem,
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
        timeout_config = httpx.Timeout(
            connect=30.0,
            read=timeout,
            write=timeout,
            pool=timeout,
        )
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

    def _handle_error(self, response: httpx.Response) -> None:
        if response.status_code >= 400:
            try:
                err = ErrorResponse.model_validate(response.json())
                raise GatewayError(response.status_code, err.error, err.detail)
            except (ValueError, KeyError):
                raise GatewayError(response.status_code, response.text) from None

    # --- Session APIs ---

    async def create_session(
        self,
        image: str | None = None,
        *,
        profile: str | None = "default",
        mode: str | None = None,
        config_env: ConfigEnvSpec | dict[str, Any] | None = None,
        idle_timeout_seconds: int | None = None,
        max_lifetime_seconds: int | None = None,
        private_containers: Iterable[PrivateContainerSpec | dict[str, Any]] | None = None,
    ) -> SessionInfo:
        if not image and not profile:
            raise ValueError("image or profile is required")
        body: dict[str, Any] = {}
        if image:
            body["image"] = image
        if profile:
            body["profile"] = profile
        if mode:
            body["mode"] = mode
        config_env_payload = _serialize_config_env(config_env)
        if config_env_payload is not None:
            body["configEnv"] = config_env_payload
        if idle_timeout_seconds is not None:
            body["idleTimeoutSeconds"] = idle_timeout_seconds
        if max_lifetime_seconds is not None:
            body["maxLifetimeSeconds"] = max_lifetime_seconds
        private_container_payload = _serialize_private_containers(private_containers)
        if private_container_payload is not None:
            body["privateContainers"] = private_container_payload
        resp = await self._client.post("/v1/sessions", json=body)
        self._handle_error(resp)
        return SessionInfo.model_validate(resp.json())

    async def get_session(self, session_id: str) -> SessionInfo:
        resp = await self._client.get(f"/v1/sessions/{session_id}")
        self._handle_error(resp)
        return SessionInfo.model_validate(resp.json())

    async def delete_session(self, session_id: str) -> None:
        resp = await self._client.delete(f"/v1/sessions/{session_id}")
        self._handle_error(resp)

    async def execute(
        self,
        session_id: str,
        steps: list[dict[str, Any]],
        trace_id: str | None = None,
        operation_id: str | None = None,
        on_output: Callable[[str, str], None] | None = None,
    ) -> ExecuteResponse:
        body: dict[str, Any] = {"steps": steps}
        if trace_id is not None:
            body["traceID"] = trace_id

        if on_output is not None:
            headers = {"Accept": "text/event-stream"}
            return await self._execute_sse(session_id, body, headers, on_output)

        op_id = operation_id or str(uuid.uuid4())
        body["operationID"] = op_id
        try:
            resp = await self._client.post(f"/v1/sessions/{session_id}/execute", json=body)
        except httpx.TimeoutException as exc:
            raise GatewayOperationTimeout(op_id, "execute operation is still pending") from exc
        self._handle_error(resp)
        result = ExecuteResponse.model_validate(resp.json())
        if not result.operation_id:
            result.operation_id = op_id
        return result

    async def get_execute_operation(
        self,
        session_id: str,
        operation_id: str,
    ) -> ExecuteOperationInfo:
        resp = await self._client.get(f"/v1/sessions/{session_id}/operations/{operation_id}")
        self._handle_error(resp)
        return ExecuteOperationInfo.model_validate(resp.json())

    async def execute_container(
        self,
        session_id: str,
        container: str,
        steps: list[dict[str, Any]],
    ) -> ContainerExecuteResponse:
        body: dict[str, Any] = {"steps": steps}
        resp = await self._client.post(
            f"/v1/sessions/{session_id}/containers/{container}/execute",
            json=body,
        )
        self._handle_error(resp)
        return ContainerExecuteResponse.model_validate(resp.json())

    async def _execute_sse(
        self,
        session_id: str,
        body: dict[str, Any],
        headers: dict[str, str],
        on_output: Callable[[str, str], None] | None,
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
                try:
                    err = ErrorResponse.model_validate(response.json())
                    raise GatewayError(response.status_code, err.error, err.detail)
                except (ValueError, KeyError):
                    raise GatewayError(response.status_code, response.text) from None

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
                        self._handle_sse_event(event_type, data_buf, results, on_output)
                    event_type = ""
                    data_buf = ""

            if event_type and data_buf:
                self._handle_sse_event(event_type, data_buf, results, on_output)

        total_ms = sum(r.duration_ms for r in results)
        return ExecuteResponse.model_validate(
            {
                "sessionID": session_id,
                "results": results,
                "totalDurationMs": total_ms,
            }
        )

    @staticmethod
    def _handle_sse_event(
        event_type: str,
        data: str,
        results: list[StepResult],
        on_output: Callable[[str, str], None] | None,
    ) -> None:
        if event_type == "output":
            if on_output is not None:
                try:
                    parsed = json.loads(data)
                    on_output(parsed.get("stdout", ""), parsed.get("stderr", ""))
                except (json.JSONDecodeError, TypeError):
                    pass
        elif event_type == "result":
            try:
                result = StepResult.model_validate(json.loads(data))
                results.append(result)
            except (json.JSONDecodeError, ValueError):
                pass

    async def upload_file(
        self,
        session_id: str,
        path: str,
        content: str | bytes | Iterable[bytes] | BinaryIO,
        sha256: str | None = None,
    ) -> UploadFileResponse:
        headers = {"Content-Type": "application/octet-stream"}
        if sha256:
            headers["X-ARL-SHA256"] = sha256
        resp = await self._client.put(
            f"/v1/sessions/{session_id}/files/{_quote_file_path(path)}",
            content=content,
            headers=headers,
        )
        self._handle_error(resp)
        return UploadFileResponse.model_validate(resp.json())

    async def download_file(
        self,
        session_id: str,
        path: str,
    ) -> bytes:
        resp = await self._client.get(f"/v1/sessions/{session_id}/files/{_quote_file_path(path)}")
        self._handle_error(resp)
        return resp.content

    async def iter_download_file(
        self,
        session_id: str,
        path: str,
        chunk_size: int = 1024 * 1024,
    ) -> AsyncIterator[bytes]:
        async with self._client.stream(
            "GET",
            f"/v1/sessions/{session_id}/files/{_quote_file_path(path)}",
        ) as resp:
            self._handle_error(resp)
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

    async def replay_from(
        self,
        session_id: str,
        source_session_id: str,
        up_to_step: int | None = None,
    ) -> ReplayResponse:
        body: dict[str, Any] = {"sourceSessionID": source_session_id}
        if up_to_step is not None:
            body["upToStep"] = up_to_step
        resp = await self._client.post(f"/v1/sessions/{session_id}/replay", json=body)
        self._handle_error(resp)
        return ReplayResponse.model_validate(resp.json())

    async def restore(self, session_id: str, snapshot_id: str) -> None:
        resp = await self._client.post(
            f"/v1/sessions/{session_id}/restore",
            json={"snapshotID": snapshot_id},
        )
        self._handle_error(resp)

    async def get_history(self, session_id: str) -> list[StepResult]:
        resp = await self._client.get(f"/v1/sessions/{session_id}/history")
        self._handle_error(resp)
        data = resp.json()
        if isinstance(data, list):
            return [StepResult.model_validate(item) for item in data]
        return []

    async def get_trajectory(self, session_id: str) -> str:
        resp = await self._client.get(f"/v1/sessions/{session_id}/trajectory")
        self._handle_error(resp)
        return resp.text

    async def list_sessions(self) -> list[SessionListItem]:
        resp = await self._client.get("/v1/sessions")
        self._handle_error(resp)
        data = resp.json()
        if isinstance(data, list):
            return [SessionListItem.model_validate(item) for item in data]
        return []

    async def iter_session_logs(
        self,
        session_id: str,
        *,
        follow: bool = False,
        tail: int = 100,
    ) -> AsyncIterator[LogEntry]:
        params = {"follow": str(follow).lower(), "tail": str(tail)}
        async with self._client.stream(
            "GET",
            f"/v1/sessions/{session_id}/logs",
            params=params,
        ) as resp:
            if resp.status_code >= 400:
                await resp.aread()
                self._handle_error(resp)
            async for line in resp.aiter_lines():
                line = line.strip()
                if not line:
                    continue
                yield LogEntry.model_validate(json.loads(line))

    async def list_session_logs(
        self,
        session_id: str,
        *,
        tail: int = 100,
    ) -> list[LogEntry]:
        return [
            entry async for entry in self.iter_session_logs(session_id, follow=False, tail=tail)
        ]

    # --- Pool APIs ---

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
        body: dict[str, Any] = {
            "name": name,
            "image": image,
            "profile": profile,
            "replicas": replicas,
            "workspaceDir": workspace_dir,
        }
        config_env_payload = _serialize_config_env(config_env)
        if config_env_payload is not None:
            body["configEnv"] = config_env_payload
        if tools is not None:
            body["tools"] = tools.model_dump(by_alias=True, exclude_none=True)
        if resources is not None:
            body["resources"] = resources.model_dump(exclude_none=True)
        if image_locality is not None:
            body["imageLocality"] = image_locality
        private_container_payload = _serialize_private_containers(private_containers)
        if private_container_payload is not None:
            body["privateContainers"] = private_container_payload
        resp = await self._client.post("/v1/pools", json=body)
        self._handle_error(resp)

    async def list_pools(self) -> list[PoolInfo]:
        resp = await self._client.get("/v1/pools")
        self._handle_error(resp)
        data = resp.json()
        if isinstance(data, list):
            return [PoolInfo.model_validate(item) for item in data]
        return []

    async def get_pool(self, name: str) -> PoolInfo:
        resp = await self._client.get(f"/v1/pools/{name}")
        self._handle_error(resp)
        return PoolInfo.model_validate(resp.json())

    async def delete_pool(self, name: str) -> None:
        resp = await self._client.delete(f"/v1/pools/{name}")
        self._handle_error(resp)

    async def destroy_pool(self, name: str) -> None:
        resp = await self._client.post(f"/v1/pools/{name}/destroy")
        self._handle_error(resp)

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
        self._handle_error(resp)
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
            "GET",
            f"/v1/pools/{name}/logs",
            params=params,
        ) as resp:
            if resp.status_code >= 400:
                await resp.aread()
                self._handle_error(resp)
            async for line in resp.aiter_lines():
                line = line.strip()
                if not line:
                    continue
                yield PoolLogEntry.model_validate(json.loads(line))

    async def list_pool_logs(
        self,
        name: str,
        *,
        tail: int = 100,
    ) -> list[PoolLogEntry]:
        return [entry async for entry in self.iter_pool_logs(name, follow=False, tail=tail)]

    # --- Managed Session APIs ---

    async def create_managed_session(
        self,
        image: str,
        experiment_id: str,
        profile: str = "default",
        mode: str | None = None,
        resources: ResourceRequirements | None = None,
        tools: ToolsSpec | None = None,
        workspace_dir: str = "/workspace",
        idle_timeout_seconds: int | None = None,
        max_lifetime_seconds: int | None = None,
        config_env: ConfigEnvSpec | dict[str, Any] | None = None,
        private_containers: Iterable[PrivateContainerSpec | dict[str, Any]] | None = None,
    ) -> ManagedSessionInfo:
        body: dict[str, Any] = {
            "image": image,
            "experimentId": experiment_id,
            "profile": profile,
            "workspaceDir": workspace_dir,
        }
        if mode:
            body["mode"] = mode
        config_env_payload = _serialize_config_env(config_env)
        if config_env_payload is not None:
            body["configEnv"] = config_env_payload
        if resources is not None:
            body["resources"] = resources.model_dump(exclude_none=True)
        if tools is not None:
            body["tools"] = tools.model_dump(by_alias=True, exclude_none=True)
        if idle_timeout_seconds is not None:
            body["idleTimeoutSeconds"] = idle_timeout_seconds
        if max_lifetime_seconds is not None:
            body["maxLifetimeSeconds"] = max_lifetime_seconds
        private_container_payload = _serialize_private_containers(private_containers)
        if private_container_payload is not None:
            body["privateContainers"] = private_container_payload
        resp = await self._client.post("/v1/managed/sessions", json=body)
        self._handle_error(resp)
        return ManagedSessionInfo.model_validate(resp.json())

    async def list_experiment_sessions(
        self,
        experiment_id: str,
    ) -> list[ManagedSessionInfo]:
        resp = await self._client.get(f"/v1/managed/experiments/{experiment_id}/sessions")
        self._handle_error(resp)
        data = resp.json()
        if isinstance(data, list):
            return [ManagedSessionInfo.model_validate(item) for item in data]
        return []

    async def list_experiments(self) -> list[ExperimentSummary]:
        resp = await self._client.get("/v1/managed/experiments")
        self._handle_error(resp)
        data = resp.json()
        if isinstance(data, list):
            return [ExperimentSummary.model_validate(item) for item in data]
        return []

    async def delete_experiment_info(
        self,
        experiment_id: str,
    ) -> DeleteExperimentResponse:
        resp = await self._client.delete(f"/v1/managed/experiments/{experiment_id}")
        self._handle_error(resp)
        result = DeleteExperimentResponse.model_validate(resp.json())
        if result.error:
            raise GatewayError(resp.status_code, result.error)
        return result

    async def delete_experiment(
        self,
        experiment_id: str,
    ) -> int:
        return (await self.delete_experiment_info(experiment_id)).deleted

    # --- Health ---

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
