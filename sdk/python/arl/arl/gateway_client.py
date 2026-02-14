"""Gateway HTTP client for ARL SDK."""

from __future__ import annotations

from typing import Any

import httpx

from arl.types import (
    ErrorResponse,
    ExecuteResponse,
    PoolCondition,
    PoolInfo,
    ResourceRequirements,
    SessionInfo,
    StepResult,
    ToolsSpec,
)


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

    def __init__(self, base_url: str = "http://localhost:8080", timeout: float = 300.0) -> None:
        self._base_url = base_url.rstrip("/")
        self._client = httpx.Client(base_url=self._base_url, timeout=timeout)

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
    ) -> ExecuteResponse:
        body: dict[str, Any] = {"steps": steps}
        if trace_id is not None:
            body["traceID"] = trace_id
        resp = self._client.post(f"/v1/sessions/{session_id}/execute", json=body)
        self._handle_error(resp)
        return ExecuteResponse.model_validate(resp.json())

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
    ) -> None:
        body: dict[str, Any] = {
            "name": name,
            "namespace": namespace,
            "image": image,
            "replicas": replicas,
            "workspaceDir": workspace_dir,
        }
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
