"""Gateway HTTP client for ARL SDK."""

from __future__ import annotations

import json
import os
from collections.abc import Callable
from typing import Any

import httpx

from arl.auth import resolve_auth
from arl.configenv import ConfigEnvSpec
from arl.types import (
    ErrorResponse,
    ExecuteResponse,
    ManagedSessionInfo,
    PoolCondition,
    PoolInfo,
    ResourceRequirements,
    SessionInfo,
    StepResult,
    ToolsSpec,
    UploadFileResponse,
)


def _serialize_config_env(
    config_env: ConfigEnvSpec | dict[str, Any] | None,
) -> dict[str, Any] | None:
    if config_env is None:
        return None
    if isinstance(config_env, ConfigEnvSpec):
        return config_env.to_request_payload()
    return config_env


class GatewayError(Exception):
    """Error from gateway API."""

    def __init__(self, status_code: int, error: str, detail: str = "") -> None:
        self.status_code = status_code
        self.error = error
        self.detail = detail
        super().__init__(
            f"Gateway error ({status_code}): {error}" + (f" - {detail}" if detail else "")
        )


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
        base_url: str = "http://localhost:8080",
        timeout: float = 300.0,
        api_key: str | None = None,
        auth: httpx.Auth | None = None,
    ) -> None:
        self._base_url = base_url.rstrip("/")
        # Resolve the credential: an explicit `auth` flow (e.g. ApiKeyAuth or a
        # future refreshing SsoTokenAuth) takes precedence; otherwise fall back
        # to `api_key` / ARL_API_KEY for backward compatibility.
        resolved_auth = resolve_auth(auth, api_key or os.environ.get("ARL_API_KEY", ""))
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
        pool_ref: str,
        namespace: str = "default",
        idle_timeout_seconds: int | None = None,
    ) -> SessionInfo:
        body: dict[str, Any] = {"poolRef": pool_ref, "namespace": namespace}
        if idle_timeout_seconds is not None:
            body["idleTimeoutSeconds"] = idle_timeout_seconds
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

    def execute(
        self,
        session_id: str,
        steps: list[dict[str, Any]],
        trace_id: str | None = None,
        on_output: Callable[[str, str], None] | None = None,
    ) -> ExecuteResponse:
        body: dict[str, Any] = {"steps": steps}
        if trace_id is not None:
            body["traceID"] = trace_id

        headers = {"Accept": "text/event-stream"}
        try:
            return self._execute_sse(session_id, body, headers, on_output)
        except (httpx.HTTPStatusError, GatewayError):
            raise
        except Exception:
            # Server does not support SSE (old version) — fall back to non-streaming
            resp = self._client.post(f"/v1/sessions/{session_id}/execute", json=body)
            self._handle_error(resp)
            return ExecuteResponse.model_validate(resp.json())

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
                        self._handle_sse_event(
                            event_type, data_buf, results, on_output
                        )
                    event_type = ""
                    data_buf = ""

            # Handle any trailing event without final blank line
            if event_type and data_buf:
                self._handle_sse_event(event_type, data_buf, results, on_output)

        total_ms = sum(r.duration_ms for r in results)
        return ExecuteResponse(
            sessionID=session_id,
            results=results,
            totalDurationMs=total_ms,
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

    def upload_file(
        self,
        session_id: str,
        path: str,
        content: str,
        encoding: str = "text",
    ) -> UploadFileResponse:
        body = {"path": path, "content": content, "encoding": encoding}
        resp = self._client.post(f"/v1/sessions/{session_id}/files", json=body)
        self._handle_error(resp)
        return UploadFileResponse.model_validate(resp.json())

    def download_file(
        self,
        session_id: str,
        path: str,
    ) -> bytes:
        resp = self._client.get(f"/v1/sessions/{session_id}/files/{path}")
        self._handle_error(resp)
        return resp.content

    def replay_from(
        self,
        session_id: str,
        source_session_id: str,
        up_to_step: int | None = None,
    ) -> dict:
        body: dict = {"sourceSessionID": source_session_id}
        if up_to_step is not None:
            body["upToStep"] = up_to_step
        resp = self._client.post(f"/v1/sessions/{session_id}/replay", json=body)
        self._handle_error(resp)
        return resp.json()

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

    # --- Pool APIs ---

    def create_pool(
        self,
        name: str,
        namespace: str,
        image: str,
        replicas: int = 2,
        tools: ToolsSpec | None = None,
        resources: ResourceRequirements | None = None,
        workspace_dir: str = "/workspace",
        config_env: ConfigEnvSpec | dict[str, Any] | None = None,
    ) -> None:
        body: dict[str, Any] = {
            "name": name,
            "namespace": namespace,
            "image": image,
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
        resp = self._client.post("/v1/pools", json=body)
        self._handle_error(resp)

    def get_pool(self, name: str, namespace: str = "") -> PoolInfo:
        params = {}
        if namespace:
            params["namespace"] = namespace
        resp = self._client.get(f"/v1/pools/{name}", params=params)
        self._handle_error(resp)
        return PoolInfo.model_validate(resp.json())

    def delete_pool(self, name: str, namespace: str = "") -> None:
        params = {}
        if namespace:
            params["namespace"] = namespace
        resp = self._client.delete(f"/v1/pools/{name}", params=params)
        self._handle_error(resp)

    def scale_pool(
        self,
        name: str,
        replicas: int,
        namespace: str = "",
        resources: ResourceRequirements | None = None,
    ) -> PoolInfo:
        """Scale a WarmPool and optionally update resource requirements.

        Args:
            name: Name of the WarmPool.
            replicas: Desired number of replicas (non-negative).
            namespace: Kubernetes namespace (default: "").
            resources: Optional resource requirements (CPU/memory requests and limits).

        Returns:
            Updated PoolInfo.
        """
        body: dict[str, Any] = {"replicas": replicas}
        if namespace:
            body["namespace"] = namespace
        if resources is not None:
            body["resources"] = resources.model_dump(exclude_none=True)
        resp = self._client.patch(f"/v1/pools/{name}", json=body)
        self._handle_error(resp)
        return PoolInfo.model_validate(resp.json())

    # --- Managed Session APIs ---

    def create_managed_session(
        self,
        image: str,
        experiment_id: str,
        namespace: str = "default",
        resources: ResourceRequirements | None = None,
        tools: ToolsSpec | None = None,
        workspace_dir: str = "/workspace",
        max_replicas: int | None = None,
        min_replicas: int | None = None,
        scale_up_step: int | None = None,
        config_env: ConfigEnvSpec | dict[str, Any] | None = None,
    ) -> ManagedSessionInfo:
        """Create a managed session with automatic pool management.

        The server automatically creates and scales WarmPools. Just specify
        the image and experiment ID.

        Args:
            image: Container image for the executor.
            experiment_id: Experiment identifier for grouping and management.
            namespace: Kubernetes namespace.
            resources: Optional CPU/memory requirements (used on first pool creation).
            tools: Optional tools specification (used on first pool creation).
            workspace_dir: Workspace mount path.
            max_replicas: Per-pool scale ceiling hint. The server scales eagerly
                up to this value instead of scaling incrementally.
            min_replicas: Per-pool scale floor hint. The server will not scale
                below this value during scale-down (0 = use server default).
            scale_up_step: Max replicas to add per scale-up event
                (0 = use server default).

        Returns:
            ManagedSessionInfo with session details and experiment metadata.
        """
        body: dict[str, Any] = {
            "image": image,
            "experimentId": experiment_id,
            "namespace": namespace,
            "workspaceDir": workspace_dir,
        }
        config_env_payload = _serialize_config_env(config_env)
        if config_env_payload is not None:
            body["configEnv"] = config_env_payload
        if resources is not None:
            body["resources"] = resources.model_dump(exclude_none=True)
        if tools is not None:
            body["tools"] = tools.model_dump(by_alias=True, exclude_none=True)
        if max_replicas is not None:
            body["maxReplicas"] = max_replicas
        if min_replicas is not None:
            body["minReplicas"] = min_replicas
        if scale_up_step is not None:
            body["scaleUpStep"] = scale_up_step
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

    def delete_experiment(
        self,
        experiment_id: str,
    ) -> int:
        """Delete all sessions for an experiment.

        Args:
            experiment_id: Experiment identifier.

        Returns:
            Number of sessions deleted.
        """
        resp = self._client.delete(f"/v1/managed/experiments/{experiment_id}")
        self._handle_error(resp)
        data = resp.json()
        return int(data.get("deleted", 0))

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
