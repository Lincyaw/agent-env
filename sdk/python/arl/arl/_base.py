"""Shared non-I/O logic for ARL gateway clients and sessions.

Contains constants, exceptions, serialization helpers, payload builders,
and response parsers used by both async and sync client implementations.
"""

from __future__ import annotations

import asyncio
import threading
from collections.abc import AsyncIterator, Iterable, Iterator
from typing import Any, BinaryIO, Coroutine, TypeVar

import httpx
from pydantic import BaseModel

from arl.configenv import ConfigEnvSpec
from arl.exceptions import ArlError
from arl.types import (
    DevboxConfig,
    ErrorResponse,
    PrivateContainerSpec,
    ResourceRequirements,
    StepOutput,
    StepRequest,
    StepResult,
    ToolsSpec,
)

_ModelT = TypeVar("_ModelT", bound=BaseModel)
_T = TypeVar("_T")

# ---------------------------------------------------------------------------
# Sync-over-async bridge
# ---------------------------------------------------------------------------


class LoopThread:
    """Persistent background event-loop thread for running async code synchronously.

    Shared by :class:`~arl.gateway_client._SyncRunner` and
    :class:`~arl.iroh_transport.SyncIrohBridge` so each sync session uses a
    single loop thread instead of two.
    """

    def __init__(self, name: str = "arl-sync") -> None:
        self._loop = asyncio.new_event_loop()
        self._thread = threading.Thread(
            target=self._loop.run_forever, daemon=True, name=name,
        )
        self._thread.start()

    @property
    def loop(self) -> asyncio.AbstractEventLoop:
        return self._loop

    def run(self, coro: Coroutine[Any, Any, _T], timeout: float | None = None) -> _T:
        """Submit a coroutine and block until it completes."""
        return asyncio.run_coroutine_threadsafe(coro, self._loop).result(timeout=timeout)

    def iter(self, ait: AsyncIterator[_T]) -> Iterator[_T]:
        """Bridge an async iterator to a synchronous one."""
        anext_fn = ait.__anext__
        while True:
            try:
                yield asyncio.run_coroutine_threadsafe(anext_fn(), self._loop).result()
            except StopAsyncIteration:
                break

    def close(self) -> None:
        self._loop.call_soon_threadsafe(self._loop.stop)
        self._thread.join(timeout=5)
        self._loop.close()


# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

OPERATION_POLL_INTERVAL_SECONDS = 2.0
OPERATION_STATUS_DONE = "done"
OPERATION_STATUS_ERROR = "error"

# ---------------------------------------------------------------------------
# Exceptions
# ---------------------------------------------------------------------------


class GatewayError(ArlError):
    """Error from gateway API."""

    def __init__(self, status_code: int, error: str, detail: str = "") -> None:
        self.status_code = status_code
        self.error = error
        self.detail = detail
        super().__init__(
            f"Gateway error ({status_code}): {error}" + (f" - {detail}" if detail else "")
        )


class GatewayOperationTimeout(ArlError, TimeoutError):
    """Raised when an idempotent execute operation outlives the HTTP request."""

    def __init__(self, operation_id: str, message: str) -> None:
        self.operation_id = operation_id
        super().__init__(f"{message}; operation_id={operation_id}")


class PoolNotReadyError(ArlError):
    """Raised when a WarmPool has failing pods or cannot become ready.

    Attributes:
        pool_name: Name of the pool.
        conditions: List of PoolCondition objects from the pool status.
        message: Human-readable description of the failure.
    """

    def __init__(
        self, pool_name: str, message: str, conditions: list[object] | None = None
    ) -> None:
        self.pool_name = pool_name
        self.conditions = conditions or []
        super().__init__(f"Pool '{pool_name}' not ready: {message}")


# ---------------------------------------------------------------------------
# Response / error helpers
# ---------------------------------------------------------------------------


def handle_error(response: httpx.Response) -> None:
    """Raise :class:`GatewayError` when *response* indicates a failure."""
    if response.status_code >= 400:
        try:
            err = ErrorResponse.model_validate(response.json())
            raise GatewayError(response.status_code, err.error, err.detail)
        except (ValueError, KeyError):
            raise GatewayError(response.status_code, response.text) from None


def validate_list(data: object, model: type[_ModelT]) -> list[_ModelT]:
    """Parse a JSON array into a list of pydantic models (empty on non-list)."""
    if isinstance(data, list):
        return [model.model_validate(item) for item in data]
    return []


# ---------------------------------------------------------------------------
# Serialization helpers
# ---------------------------------------------------------------------------


def serialize_config_env(
    config_env: ConfigEnvSpec | dict[str, Any] | None,
) -> dict[str, Any] | None:
    if config_env is None:
        return None
    if isinstance(config_env, ConfigEnvSpec):
        return config_env.to_request_payload()
    return config_env


def serialize_private_containers(
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


def serialize_steps(steps: list[StepRequest | dict[str, Any]]) -> list[dict[str, Any]]:
    return [s.model_dump(by_alias=True) if isinstance(s, StepRequest) else s for s in steps]


# ---------------------------------------------------------------------------
# Payload builders
# ---------------------------------------------------------------------------


def _serialize_devbox(
    devbox: DevboxConfig | dict[str, object] | None,
) -> dict[str, object] | None:
    if devbox is None:
        return None
    if isinstance(devbox, DevboxConfig):
        return devbox.model_dump(by_alias=True, exclude_none=True)
    return devbox


def build_create_session_body(
    image: str | None,
    profile: str | None,
    mode: str | None,
    devbox: DevboxConfig | dict[str, object] | None,
    config_env: ConfigEnvSpec | dict[str, Any] | None,
    idle_timeout_seconds: int | None,
    allocation_timeout_seconds: int | None,
    private_containers: Iterable[PrivateContainerSpec | dict[str, Any]] | None,
    allow_internet: bool | None,
) -> dict[str, Any]:
    body: dict[str, Any] = {}
    if image:
        body["image"] = image
    if profile:
        body["profile"] = profile
    if mode:
        body["mode"] = mode
    dv = _serialize_devbox(devbox)
    if dv is not None:
        body["devbox"] = dv
    ce = serialize_config_env(config_env)
    if ce is not None:
        body["configEnv"] = ce
    if idle_timeout_seconds is not None:
        body["idleTimeoutSeconds"] = idle_timeout_seconds
    if allocation_timeout_seconds is not None:
        body["allocationTimeoutSeconds"] = allocation_timeout_seconds
    pc = serialize_private_containers(private_containers)
    if pc is not None:
        body["privateContainers"] = pc
    if allow_internet is not None:
        body["allowInternet"] = allow_internet
    return body


def build_create_pool_body(
    name: str,
    image: str,
    replicas: int,
    profile: str,
    tools: ToolsSpec | None,
    resources: ResourceRequirements | None,
    workspace_dir: str,
    config_env: ConfigEnvSpec | dict[str, Any] | None,
    image_locality: dict[str, Any] | bool | None,
    private_containers: Iterable[PrivateContainerSpec | dict[str, Any]] | None,
) -> dict[str, Any]:
    body: dict[str, Any] = {
        "name": name,
        "image": image,
        "profile": profile,
        "replicas": replicas,
        "workspaceDir": workspace_dir,
    }
    ce = serialize_config_env(config_env)
    if ce is not None:
        body["configEnv"] = ce
    if tools is not None:
        body["tools"] = tools.model_dump(by_alias=True, exclude_none=True)
    if resources is not None:
        body["resources"] = resources.model_dump(exclude_none=True)
    if image_locality is not None:
        body["imageLocality"] = image_locality
    pc = serialize_private_containers(private_containers)
    if pc is not None:
        body["privateContainers"] = pc
    return body


def build_create_managed_session_body(
    image: str,
    experiment_id: str,
    profile: str,
    mode: str | None,
    devbox: DevboxConfig | dict[str, object] | None,
    config_env: ConfigEnvSpec | dict[str, Any] | None,
    resources: ResourceRequirements | None,
    tools: ToolsSpec | None,
    workspace_dir: str,
    idle_timeout_seconds: int | None,
    allocation_timeout_seconds: int | None,
    private_containers: Iterable[PrivateContainerSpec | dict[str, Any]] | None,
    allow_internet: bool | None,
) -> dict[str, Any]:
    body: dict[str, Any] = {
        "image": image,
        "experimentId": experiment_id,
        "profile": profile,
        "workspaceDir": workspace_dir,
    }
    if mode:
        body["mode"] = mode
    dv = _serialize_devbox(devbox)
    if dv is not None:
        body["devbox"] = dv
    ce = serialize_config_env(config_env)
    if ce is not None:
        body["configEnv"] = ce
    if resources is not None:
        body["resources"] = resources.model_dump(exclude_none=True)
    if tools is not None:
        body["tools"] = tools.model_dump(by_alias=True, exclude_none=True)
    if idle_timeout_seconds is not None:
        body["idleTimeoutSeconds"] = idle_timeout_seconds
    if allocation_timeout_seconds is not None:
        body["allocationTimeoutSeconds"] = allocation_timeout_seconds
    pc = serialize_private_containers(private_containers)
    if pc is not None:
        body["privateContainers"] = pc
    if allow_internet is not None:
        body["allowInternet"] = allow_internet
    return body


# ---------------------------------------------------------------------------
# Content helper (used by session classes)
# ---------------------------------------------------------------------------


def read_content(content: str | bytes | Iterable[bytes] | BinaryIO) -> bytes:
    """Materialize mixed content types into a single ``bytes`` object."""
    if isinstance(content, str):
        return content.encode()
    if isinstance(content, bytes):
        return content
    read_fn = getattr(content, "read", None)
    if callable(read_fn):
        result: bytes = read_fn()
        return result
    return b"".join(content)


# ---------------------------------------------------------------------------
# Iroh step helpers (shared by sync/async sessions)
# ---------------------------------------------------------------------------


def prepare_iroh_step(
    step: dict[str, Any],
) -> tuple[list[str], dict[str, str] | None, str | None, int | None]:
    """Extract iroh execution params from a step dict."""
    cmd: list[str] = step.get("command", [])
    env_raw = step.get("env")
    env: dict[str, str] | None = (
        {str(k): str(v) for k, v in env_raw.items()} if isinstance(env_raw, dict) else None
    )
    work_dir_raw = step.get("workDir") or step.get("work_dir")
    work_dir = str(work_dir_raw) if work_dir_raw else None
    timeout_raw = (
        step.get("timeoutSeconds") or step.get("timeout_seconds") or step.get("timeout")
    )
    timeout_s = int(str(timeout_raw)) if timeout_raw is not None else None
    return cmd, env, work_dir, timeout_s


def parse_iroh_step_result(i: int, step: dict[str, Any], raw: dict[str, Any]) -> StepResult:
    """Build a StepResult from an iroh execution response."""
    exit_code_val = raw.get("exit_code", 0)
    output = StepOutput(
        stdout=str(raw.get("stdout", "")),
        stderr=str(raw.get("stderr", "")),
        exit_code=int(str(exit_code_val)),
    )
    return StepResult(index=i, name=step.get("name", ""), output=output)


def build_iroh_execute_response(
    session_id: str, results: list[StepResult]
) -> dict[str, Any]:
    """Build the dict for ExecuteResponse.model_validate after iroh exec."""
    return {
        "sessionID": session_id,
        "results": [r.model_dump() for r in results],
        "totalDurationMs": 0,
    }
