"""Gateway HTTP client for ARL SDK."""

from __future__ import annotations

import json
import os
import uuid
from collections.abc import Callable, Iterable, Iterator
from pathlib import Path
from typing import Any, BinaryIO, TypeVar
from urllib.parse import quote

import httpx
from pydantic import BaseModel

from arl.auth import resolve_auth
from arl.config import resolve_from_config
from arl.configenv import ConfigEnvSpec
from arl.types import (
    ContainerExecuteResponse,
    DeleteExperimentResponse,
    DevboxConfig,
    ErrorResponse,
    ExecuteOperationInfo,
    ExecuteResponse,
    ExperimentSummary,
    GatewaySummary,
    ListDirResult,
    LogEntry,
    ManagedSessionInfo,
    PoolCondition,
    PoolInfo,
    PoolLogEntry,
    PrivateContainerSpec,
    ReplayResponse,
    ResourceRequirements,
    SessionInfo,
    SessionListItem,
    StatResult,
    StepResult,
    ToolsSpec,
    UploadFileResponse,
)

_ModelT = TypeVar("_ModelT", bound=BaseModel)


def _serialize_config_env(
    config_env: ConfigEnvSpec | dict[str, Any] | None,
) -> dict[str, Any] | None:
    if config_env is None:
        return None
    if isinstance(config_env, ConfigEnvSpec):
        return config_env.to_request_payload()
    return config_env


def _quote_file_path(path: str) -> str:
    normalized = path.strip().replace("\\", "/").lstrip("/")
    if not normalized:
        raise ValueError("path is required")
    return quote(normalized, safe="/")


def _serialize_private_containers(
    private_containers: Iterable[PrivateContainerSpec | dict[str, Any]] | None,
) -> list[dict[str, Any]] | None:
    if private_containers is None:
        return None
    payload: list[dict[str, Any]] = []
    for container in private_containers:
        if isinstance(container, PrivateContainerSpec):
            payload.append(container.model_dump(by_alias=True, exclude_none=True))
        else:
            payload.append(container)
    return payload


class GatewayError(Exception):
    """Error from gateway API."""

    def __init__(self, status_code: int, error: str, detail: str = "") -> None:
        self.status_code = status_code
        self.error = error
        self.detail = detail
        super().__init__(
            f"Gateway error ({status_code}): {error}" + (f" - {detail}" if detail else "")
        )


class GatewayOperationTimeout(TimeoutError):
    """Raised when an idempotent execute operation outlives the HTTP request."""

    def __init__(self, operation_id: str, message: str) -> None:
        self.operation_id = operation_id
        super().__init__(f"{message}; operation_id={operation_id}")


class PoolNotReadyError(Exception):
    """Raised when a WarmPool has failing pods or cannot become ready.

    Attributes:
        pool_name: Name of the pool.
        conditions: List of PoolCondition objects from the pool status.
        message: Human-readable description of the failure.
    """

    def __init__(
        self, pool_name: str, message: str, conditions: list[PoolCondition] | None = None
    ) -> None:
        self.pool_name = pool_name
        self.conditions = conditions or []
        super().__init__(f"Pool '{pool_name}' not ready: {message}")


class GatewayClient:
    """HTTP client for the ARL Gateway API."""

    def __init__(
        self,
        base_url: str = "",
        timeout: float = 300.0,
        api_key: str | None = None,
        auth: httpx.Auth | None = None,
    ) -> None:
        # Resolve gateway_url and api_key from the config-file fallback chain
        # (arg > env var > ~/.config/arl/config.yaml active context > default).
        cfg_url, cfg_key = resolve_from_config(
            gateway_url=base_url,
            api_key=api_key or "",
        )
        self._base_url = cfg_url.rstrip("/")
        resolved_auth = resolve_auth(auth, cfg_key)
        # Use explicit timeout configuration with longer connect timeout
        timeout_config = httpx.Timeout(
            connect=30.0,  # 30s for TCP connection (fail fast, rely on retries)
            read=timeout,  # Use provided timeout for read operations
            write=timeout,  # Use provided timeout for write operations
            pool=timeout,  # Use provided timeout for pool operations
        )
        # Respect standard HTTP proxy environment variables.
        # httpx does not auto-detect proxies when a custom transport is provided,
        # so we read them explicitly and forward to HTTPTransport.
        # `or None` so an empty-string proxy var (a real environment quirk)
        # falls through instead of crashing httpx with "Unknown scheme for ''".
        proxy_url = os.environ.get("http_proxy") or os.environ.get("HTTP_PROXY") or None
        # Configure transport with retries and keepalive management to avoid
        # stale connections causing ConnectTimeout during long polling loops.
        transport = httpx.HTTPTransport(
            retries=3,  # TCP-level retries on connection failure
            proxy=proxy_url,
            limits=httpx.Limits(
                max_connections=20,
                max_keepalive_connections=5,
                keepalive_expiry=30.0,  # Close idle connections before LB/NAT timeout
            ),
        )
        self._client = httpx.Client(
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

    def create_session(
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
        if devbox is not None:
            if isinstance(devbox, DevboxConfig):
                body["devbox"] = devbox.model_dump(by_alias=True, exclude_none=True)
            else:
                body["devbox"] = devbox
        config_env_payload = _serialize_config_env(config_env)
        if config_env_payload is not None:
            body["configEnv"] = config_env_payload
        if idle_timeout_seconds is not None:
            body["idleTimeoutSeconds"] = idle_timeout_seconds
        if allocation_timeout_seconds is not None:
            body["allocationTimeoutSeconds"] = allocation_timeout_seconds
        private_container_payload = _serialize_private_containers(private_containers)
        if private_container_payload is not None:
            body["privateContainers"] = private_container_payload
        resp = self._client.post("/v1/sessions", json=body)
        self._handle_error(resp)
        return SessionInfo.model_validate(resp.json())

    def get_session(self, session_id: str) -> SessionInfo:
        resp = self._client.get(f"/v1/sessions/{session_id}")
        self._handle_error(resp)
        return SessionInfo.model_validate(resp.json())

    def delete_session(self, session_id: str) -> None:
        resp = self._client.delete(f"/v1/sessions/{session_id}")
        self._handle_error(resp)

    def suspend_session(self, session_id: str) -> None:
        resp = self._client.post(f"/v1/sessions/{session_id}/suspend")
        self._handle_error(resp)

    def resume_session(self, session_id: str) -> None:
        resp = self._client.post(f"/v1/sessions/{session_id}/resume")
        self._handle_error(resp)

    def execute(
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
            return self._execute_sse(session_id, body, headers, on_output)

        op_id = operation_id or str(uuid.uuid4())
        body["operationID"] = op_id
        try:
            resp = self._client.post(f"/v1/sessions/{session_id}/execute", json=body)
        except httpx.TimeoutException as exc:
            raise GatewayOperationTimeout(op_id, "execute operation is still pending") from exc
        self._handle_error(resp)
        result = ExecuteResponse.model_validate(resp.json())
        if not result.operation_id:
            result.operation_id = op_id
        return result

    def get_execute_operation(
        self,
        session_id: str,
        operation_id: str,
    ) -> ExecuteOperationInfo:
        resp = self._client.get(f"/v1/sessions/{session_id}/operations/{operation_id}")
        self._handle_error(resp)
        return ExecuteOperationInfo.model_validate(resp.json())

    def execute_container(
        self,
        session_id: str,
        container: str,
        steps: list[dict[str, Any]],
    ) -> ContainerExecuteResponse:
        body: dict[str, Any] = {"steps": steps}
        resp = self._client.post(
            f"/v1/sessions/{session_id}/containers/{container}/execute",
            json=body,
        )
        self._handle_error(resp)
        return ContainerExecuteResponse.model_validate(resp.json())

    def _execute_sse(
        self,
        session_id: str,
        body: dict[str, Any],
        headers: dict[str, str],
        on_output: Callable[[str, str], None] | None,
    ) -> ExecuteResponse:
        results: list[StepResult] = []
        with self._client.stream(
            "POST",
            f"/v1/sessions/{session_id}/execute",
            json=body,
            headers=headers,
        ) as response:
            if response.status_code >= 400:
                # Read the full body for error handling
                response.read()
                try:
                    err = ErrorResponse.model_validate(response.json())
                    raise GatewayError(response.status_code, err.error, err.detail)
                except (ValueError, KeyError):
                    raise GatewayError(response.status_code, response.text) from None

            content_type = response.headers.get("content-type", "")
            if "text/event-stream" not in content_type:
                # Server responded with JSON (old version), parse directly
                response.read()
                return ExecuteResponse.model_validate(response.json())

            # Parse SSE stream
            event_type = ""
            data_buf = ""
            for line in response.iter_lines():
                if line.startswith("event: "):
                    event_type = line[7:]
                    data_buf = ""
                elif line.startswith("data: "):
                    data_buf = line[6:]
                elif line == "":
                    # Empty line = end of event
                    if event_type and data_buf:
                        self._handle_sse_event(event_type, data_buf, results, on_output)
                    event_type = ""
                    data_buf = ""

            # Handle any trailing event without final blank line
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

    @staticmethod
    def _iter_ndjson_models(
        response: httpx.Response,
        model: type[_ModelT],
    ) -> Iterator[_ModelT]:
        for line in response.iter_lines():
            line = line.strip()
            if not line:
                continue
            yield model.model_validate(json.loads(line))

    def upload_file(
        self,
        session_id: str,
        path: str,
        content: str | bytes | Iterable[bytes] | BinaryIO,
        sha256: str | None = None,
    ) -> UploadFileResponse:
        headers = {"Content-Type": "application/octet-stream"}
        if sha256:
            headers["X-ARL-SHA256"] = sha256
        resp = self._client.put(
            f"/v1/sessions/{session_id}/files/{_quote_file_path(path)}",
            content=content,
            headers=headers,
        )
        self._handle_error(resp)
        return UploadFileResponse.model_validate(resp.json())

    def download_file(
        self,
        session_id: str,
        path: str,
    ) -> bytes:
        resp = self._client.get(f"/v1/sessions/{session_id}/files/{_quote_file_path(path)}")
        self._handle_error(resp)
        return resp.content

    def iter_download_file(
        self,
        session_id: str,
        path: str,
        chunk_size: int = 1024 * 1024,
    ) -> Iterator[bytes]:
        with self._client.stream(
            "GET",
            f"/v1/sessions/{session_id}/files/{_quote_file_path(path)}",
        ) as resp:
            self._handle_error(resp)
            for chunk in resp.iter_bytes(chunk_size=chunk_size):
                if chunk:
                    yield chunk

    def upload_path(
        self,
        session_id: str,
        local_path: str | Path,
        remote_path: str,
        sha256: str | None = None,
    ) -> UploadFileResponse:
        with Path(local_path).open("rb") as file:
            return self.upload_file(session_id, remote_path, file, sha256=sha256)

    def download_path(
        self,
        session_id: str,
        remote_path: str,
        local_path: str | Path,
    ) -> None:
        target = Path(local_path)
        target.parent.mkdir(parents=True, exist_ok=True)
        with target.open("wb") as file:
            for chunk in self.iter_download_file(session_id, remote_path):
                file.write(chunk)

    def stat_file(self, session_id: str, path: str) -> StatResult:
        """Get file metadata without downloading."""
        encoded = quote(path.lstrip("/"), safe="")
        resp = self._client.get(f"/v1/sessions/{session_id}/stat/{encoded}")
        self._handle_error(resp)
        return StatResult.model_validate(resp.json())

    def list_dir(
        self,
        session_id: str,
        path: str,
        recursive: bool = False,
    ) -> ListDirResult:
        """List directory contents."""
        encoded = quote(path.lstrip("/"), safe="")
        params = {"recursive": "true"} if recursive else {}
        resp = self._client.get(
            f"/v1/sessions/{session_id}/ls/{encoded}",
            params=params,
        )
        self._handle_error(resp)
        return ListDirResult.model_validate(resp.json())

    def send_stdin(self, session_id: str, handle: str, data: str) -> None:
        """Send stdin data to a running process."""
        resp = self._client.post(
            f"/v1/sessions/{session_id}/stdin",
            json={"handle": handle, "data": data},
        )
        self._handle_error(resp)

    def replay_from(
        self,
        session_id: str,
        source_session_id: str,
        up_to_step: int | None = None,
    ) -> ReplayResponse:
        body: dict[str, Any] = {"sourceSessionID": source_session_id}
        if up_to_step is not None:
            body["upToStep"] = up_to_step
        resp = self._client.post(f"/v1/sessions/{session_id}/replay", json=body)
        self._handle_error(resp)
        return ReplayResponse.model_validate(resp.json())

    def restore(self, session_id: str, snapshot_id: str) -> None:
        resp = self._client.post(
            f"/v1/sessions/{session_id}/restore",
            json={"snapshotID": snapshot_id},
        )
        self._handle_error(resp)

    def get_history(self, session_id: str) -> list[StepResult]:
        resp = self._client.get(f"/v1/sessions/{session_id}/history")
        self._handle_error(resp)
        data = resp.json()
        if isinstance(data, list):
            return [StepResult.model_validate(item) for item in data]
        return []

    def get_trajectory(self, session_id: str) -> str:
        resp = self._client.get(f"/v1/sessions/{session_id}/trajectory")
        self._handle_error(resp)
        return resp.text

    def list_sessions(
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
        resp = self._client.get("/v1/sessions", params=params)
        self._handle_error(resp)
        data = resp.json()
        if isinstance(data, list):
            return [SessionListItem.model_validate(item) for item in data]
        return []

    def iter_session_logs(
        self,
        session_id: str,
        *,
        follow: bool = False,
        tail: int = 100,
    ) -> Iterator[LogEntry]:
        params = {"follow": str(follow).lower(), "tail": str(tail)}
        with self._client.stream(
            "GET",
            f"/v1/sessions/{session_id}/logs",
            params=params,
        ) as resp:
            if resp.status_code >= 400:
                resp.read()
                self._handle_error(resp)
            yield from self._iter_ndjson_models(resp, LogEntry)

    def list_session_logs(
        self,
        session_id: str,
        *,
        tail: int = 100,
    ) -> list[LogEntry]:
        return list(self.iter_session_logs(session_id, follow=False, tail=tail))

    # --- Pool APIs ---

    def create_pool(
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
        resp = self._client.post("/v1/pools", json=body)
        self._handle_error(resp)

    def list_pools(self, *, include_stopped: bool = False) -> list[PoolInfo]:
        params = {"includeStopped": "true"} if include_stopped else None
        resp = self._client.get("/v1/pools", params=params)
        self._handle_error(resp)
        data = resp.json()
        if isinstance(data, list):
            return [PoolInfo.model_validate(item) for item in data]
        return []

    def summary(self) -> GatewaySummary:
        resp = self._client.get("/v1/summary")
        self._handle_error(resp)
        return GatewaySummary.model_validate(resp.json())

    def get_pool(self, name: str) -> PoolInfo:
        resp = self._client.get(f"/v1/pools/{name}")
        self._handle_error(resp)
        return PoolInfo.model_validate(resp.json())

    def delete_pool(self, name: str) -> None:
        resp = self._client.delete(f"/v1/pools/{name}")
        self._handle_error(resp)

    def destroy_pool(self, name: str) -> None:
        resp = self._client.post(f"/v1/pools/{name}/destroy")
        self._handle_error(resp)

    def scale_pool(
        self,
        name: str,
        replicas: int,
        resources: ResourceRequirements | None = None,
    ) -> PoolInfo:
        """Scale a WarmPool and optionally update resource requirements.

        Args:
            name: Name of the WarmPool.
            replicas: Desired number of replicas (non-negative).
            resources: Optional resource requirements (CPU/memory requests and limits).

        Returns:
            Updated PoolInfo.
        """
        body: dict[str, Any] = {"replicas": replicas}
        if resources is not None:
            body["resources"] = resources.model_dump(exclude_none=True)
        resp = self._client.patch(f"/v1/pools/{name}", json=body)
        self._handle_error(resp)
        return PoolInfo.model_validate(resp.json())

    def iter_pool_logs(
        self,
        name: str,
        *,
        follow: bool = False,
        tail: int = 100,
    ) -> Iterator[PoolLogEntry]:
        params = {"follow": str(follow).lower(), "tail": str(tail)}
        with self._client.stream(
            "GET",
            f"/v1/pools/{name}/logs",
            params=params,
        ) as resp:
            if resp.status_code >= 400:
                resp.read()
                self._handle_error(resp)
            yield from self._iter_ndjson_models(resp, PoolLogEntry)

    def list_pool_logs(
        self,
        name: str,
        *,
        tail: int = 100,
    ) -> list[PoolLogEntry]:
        return list(self.iter_pool_logs(name, follow=False, tail=tail))

    # --- Managed Session APIs ---

    def create_managed_session(
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
    ) -> ManagedSessionInfo:
        """Create a managed session with automatic pool management.

        The server automatically creates a sandbox-backed pool when needed.

        Args:
            image: Container image for the executor.
            experiment_id: Experiment identifier for grouping and management.
            profile: Resource profile for pool selection.
            resources: Optional CPU/memory requirements (used on first pool creation).
            tools: Optional tools specification (used on first pool creation).
            workspace_dir: Workspace mount path.
            idle_timeout_seconds: Per-session idle TTL. The gateway deletes the
                session after this many seconds without execute/file activity.

        Returns:
            ManagedSessionInfo with session details and experiment metadata.
        """
        body: dict[str, Any] = {
            "image": image,
            "experimentId": experiment_id,
            "profile": profile,
            "workspaceDir": workspace_dir,
        }
        if mode:
            body["mode"] = mode
        if devbox is not None:
            if isinstance(devbox, DevboxConfig):
                body["devbox"] = devbox.model_dump(by_alias=True, exclude_none=True)
            else:
                body["devbox"] = devbox
        config_env_payload = _serialize_config_env(config_env)
        if config_env_payload is not None:
            body["configEnv"] = config_env_payload
        if resources is not None:
            body["resources"] = resources.model_dump(exclude_none=True)
        if tools is not None:
            body["tools"] = tools.model_dump(by_alias=True, exclude_none=True)
        if idle_timeout_seconds is not None:
            body["idleTimeoutSeconds"] = idle_timeout_seconds
        if allocation_timeout_seconds is not None:
            body["allocationTimeoutSeconds"] = allocation_timeout_seconds
        private_container_payload = _serialize_private_containers(private_containers)
        if private_container_payload is not None:
            body["privateContainers"] = private_container_payload
        resp = self._client.post("/v1/managed/sessions", json=body)
        self._handle_error(resp)
        return ManagedSessionInfo.model_validate(resp.json())

    def list_experiment_sessions(
        self,
        experiment_id: str,
    ) -> list[ManagedSessionInfo]:
        """List all active sessions for an experiment.

        Args:
            experiment_id: Experiment identifier.

        Returns:
            List of ManagedSessionInfo for the experiment.
        """
        resp = self._client.get(f"/v1/managed/experiments/{experiment_id}/sessions")
        self._handle_error(resp)
        data = resp.json()
        if isinstance(data, list):
            return [ManagedSessionInfo.model_validate(item) for item in data]
        return []

    def list_experiments(self) -> list[ExperimentSummary]:
        """List managed-session experiment summaries."""
        resp = self._client.get("/v1/managed/experiments")
        self._handle_error(resp)
        data = resp.json()
        if isinstance(data, list):
            return [ExperimentSummary.model_validate(item) for item in data]
        return []

    def delete_experiment_info(
        self,
        experiment_id: str,
    ) -> DeleteExperimentResponse:
        """Delete all sessions for an experiment and return the full response.

        Args:
            experiment_id: Experiment identifier.

        Returns:
            DeleteExperimentResponse with deletion count and optional error.
        """
        resp = self._client.delete(f"/v1/managed/experiments/{experiment_id}")
        self._handle_error(resp)
        result = DeleteExperimentResponse.model_validate(resp.json())
        if result.error:
            raise GatewayError(resp.status_code, result.error)
        return result

    def delete_experiment(
        self,
        experiment_id: str,
    ) -> int:
        """Delete all sessions for an experiment.

        Returns:
            Number of sessions deleted.
        """
        return self.delete_experiment_info(experiment_id).deleted

    # --- Iroh direct-connect ---

    def get_iroh_addr(self, session_id: str) -> str:
        """Fetch the iroh direct-connect endpoint address for a session.

        Returns an empty string if the executor is not running with v2
        protocol or the address is not yet available.
        """
        resp = self._client.get(f"/v1/sessions/{session_id}/iroh-addr")
        self._handle_error(resp)
        data = resp.json()
        addr: str = data.get("addr", "")
        return addr

    # --- Health ---

    def health(self) -> bool:
        try:
            resp = self._client.get("/healthz")
            return resp.status_code == 200
        except httpx.HTTPError:
            return False

    def close(self) -> None:
        self._client.close()

    def __enter__(self) -> GatewayClient:
        return self

    def __exit__(self, *_: object) -> None:
        self.close()
