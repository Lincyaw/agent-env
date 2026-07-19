"""Synchronous Gateway HTTP client for ARL SDK.

Thin wrapper around :class:`~arl.async_client.AsyncGatewayClient`; every
public method blocks until the underlying async call completes.  A dedicated
background event loop thread ensures correct ``httpx.AsyncClient`` connection
pooling and safe use from Jupyter / existing event loops.
"""

from __future__ import annotations

from collections.abc import Callable, Iterable, Iterator
from pathlib import Path
from typing import Any, BinaryIO, TypeVar

import httpx

from arl._base import (
    OPERATION_POLL_INTERVAL_SECONDS as _OPERATION_POLL_INTERVAL_SECONDS,  # noqa: F401
)
from arl._base import (
    OPERATION_STATUS_DONE as _OPERATION_STATUS_DONE,  # noqa: F401
)
from arl._base import (
    OPERATION_STATUS_ERROR as _OPERATION_STATUS_ERROR,  # noqa: F401
)
from arl._base import (
    GatewayError,
    GatewayOperationTimeout,
    LoopThread,
    PoolNotReadyError,
)
from arl._base import (
    serialize_config_env as _serialize_config_env,  # noqa: F401
)
from arl._base import (
    serialize_private_containers as _serialize_private_containers,  # noqa: F401
)
from arl.async_client import AsyncGatewayClient
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

_T = TypeVar("_T")

# Re-export everything that used to live here so downstream ``from arl.gateway_client import ...``
# keeps working.
__all__ = [
    "GatewayClient",
    "GatewayError",
    "GatewayOperationTimeout",
    "PoolNotReadyError",
]


# ---------------------------------------------------------------------------
# Sync runner -- persistent background event loop
# ---------------------------------------------------------------------------


_SyncRunner = LoopThread  # alias kept for internal readability


# ---------------------------------------------------------------------------
# GatewayClient (sync facade)
# ---------------------------------------------------------------------------


class GatewayClient:
    """Synchronous HTTP client for the ARL Gateway API.

    Wraps :class:`AsyncGatewayClient`; every method blocks until the
    underlying async call completes.
    """

    def __init__(
        self,
        base_url: str = "",
        timeout: float = 300.0,
        api_key: str | None = None,
        auth: httpx.Auth | None = None,
    ) -> None:
        self._async = AsyncGatewayClient(
            base_url=base_url, timeout=timeout, api_key=api_key, auth=auth,
        )
        self._runner = _SyncRunner()
        self._base_url = self._async._base_url

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
        allow_internet: bool | None = None,
    ) -> SessionInfo:
        return self._runner.run(self._async.create_session(
            image, profile=profile, mode=mode, devbox=devbox,
            config_env=config_env, idle_timeout_seconds=idle_timeout_seconds,
            allocation_timeout_seconds=allocation_timeout_seconds,
            private_containers=private_containers, allow_internet=allow_internet,
        ))

    def get_session(self, session_id: str) -> SessionInfo:
        return self._runner.run(self._async.get_session(session_id))

    def delete_session(self, session_id: str) -> None:
        self._runner.run(self._async.delete_session(session_id))

    def fork_session(self, session_id: str, step: int) -> ForkSessionResponse:
        """Fork a session from a historical checkpoint step."""
        return self._runner.run(self._async.fork_session(session_id, step))

    def suspend_session(self, session_id: str) -> None:
        self._runner.run(self._async.suspend_session(session_id))

    def resume_session(self, session_id: str) -> None:
        self._runner.run(self._async.resume_session(session_id))

    def update_network_policy(
        self,
        session_id: str,
        *,
        allow_internet: bool,
        egress_cidrs: list[str] | None = None,
    ) -> None:
        self._runner.run(self._async.update_network_policy(
            session_id, allow_internet=allow_internet, egress_cidrs=egress_cidrs,
        ))

    def execute(
        self,
        session_id: str,
        steps: list[StepRequest | dict[str, Any]],
        trace_id: str | None = None,
        operation_id: str | None = None,
        on_output: Callable[[str, str], None] | None = None,
        recover: bool = True,
        recover_timeout: float | None = None,
    ) -> ExecuteResponse:
        return self._runner.run(self._async.execute(
            session_id, steps, trace_id, operation_id=operation_id,
            on_output=on_output, recover=recover, recover_timeout=recover_timeout,
        ))

    def get_execute_operation(
        self, session_id: str, operation_id: str,
    ) -> ExecuteOperationInfo:
        return self._runner.run(
            self._async.get_execute_operation(session_id, operation_id)
        )

    def execute_container(
        self,
        session_id: str,
        container: str,
        steps: list[StepRequest | dict[str, Any]],
    ) -> ContainerExecuteResponse:
        return self._runner.run(
            self._async.execute_container(session_id, container, steps)
        )

    # --- File APIs ---

    def upload_file(
        self,
        session_id: str,
        path: str,
        content: str | bytes | Iterable[bytes] | BinaryIO,
        sha256: str | None = None,
    ) -> UploadFileResponse:
        return self._runner.run(
            self._async.upload_file(session_id, path, content, sha256=sha256)
        )

    def download_file(self, session_id: str, path: str) -> bytes:
        return self._runner.run(self._async.download_file(session_id, path))

    def iter_download_file(
        self,
        session_id: str,
        path: str,
        chunk_size: int = 1024 * 1024,
    ) -> Iterator[bytes]:
        return self._runner.iter(
            self._async.iter_download_file(session_id, path, chunk_size)
        )

    def upload_path(
        self,
        session_id: str,
        local_path: str | Path,
        remote_path: str,
        sha256: str | None = None,
    ) -> UploadFileResponse:
        return self._runner.run(self._async.upload_path(
            session_id, local_path, remote_path, sha256=sha256,
        ))

    def download_path(
        self,
        session_id: str,
        remote_path: str,
        local_path: str | Path,
    ) -> None:
        self._runner.run(self._async.download_path(session_id, remote_path, local_path))

    def send_stdin(self, session_id: str, handle: str, data: str) -> None:
        self._runner.run(self._async.send_stdin(session_id, handle, data))

    # --- Long-running operations ---

    def replay_from(
        self,
        session_id: str,
        source_session_id: str,
        up_to_step: int | None = None,
        operation_id: str | None = None,
        recover: bool = True,
        recover_timeout: float | None = None,
    ) -> ReplayResponse:
        return self._runner.run(self._async.replay_from(
            session_id, source_session_id, up_to_step=up_to_step,
            operation_id=operation_id, recover=recover, recover_timeout=recover_timeout,
        ))

    def restore(
        self,
        session_id: str,
        snapshot_id: str,
        operation_id: str | None = None,
        recover: bool = True,
        recover_timeout: float | None = None,
    ) -> RestoreResponse:
        return self._runner.run(self._async.restore(
            session_id, snapshot_id, operation_id=operation_id,
            recover=recover, recover_timeout=recover_timeout,
        ))

    # --- History / trajectory ---

    def get_history(self, session_id: str) -> list[StepResult]:
        return self._runner.run(self._async.get_history(session_id))

    def get_trajectory(self, session_id: str) -> str:
        return self._runner.run(self._async.get_trajectory(session_id))

    def list_sessions(
        self,
        *,
        profile: str | None = None,
        experiment_id: str | None = None,
        status: str | None = None,
        limit: int | None = None,
        cursor: str | None = None,
    ) -> list[SessionListItem]:
        return self._runner.run(self._async.list_sessions(
            profile=profile, experiment_id=experiment_id,
            status=status, limit=limit, cursor=cursor,
        ))

    def iter_session_logs(
        self,
        session_id: str,
        *,
        follow: bool = False,
        tail: int = 100,
    ) -> Iterator[LogEntry]:
        return self._runner.iter(
            self._async.iter_session_logs(session_id, follow=follow, tail=tail)
        )

    def list_session_logs(
        self, session_id: str, *, tail: int = 100,
    ) -> list[LogEntry]:
        return self._runner.run(
            self._async.list_session_logs(session_id, tail=tail)
        )

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
        self._runner.run(self._async.create_pool(
            name, image, replicas, profile, tools=tools, resources=resources,
            workspace_dir=workspace_dir, config_env=config_env,
            image_locality=image_locality, private_containers=private_containers,
        ))

    def list_pools(self, *, include_stopped: bool = False) -> list[PoolInfo]:
        return self._runner.run(
            self._async.list_pools(include_stopped=include_stopped)
        )

    def summary(self) -> GatewaySummary:
        return self._runner.run(self._async.summary())

    def get_pool(self, name: str) -> PoolInfo:
        return self._runner.run(self._async.get_pool(name))

    def delete_pool(self, name: str) -> None:
        self._runner.run(self._async.delete_pool(name))

    def destroy_pool(self, name: str) -> None:
        self._runner.run(self._async.destroy_pool(name))

    def scale_pool(
        self,
        name: str,
        replicas: int,
        resources: ResourceRequirements | None = None,
    ) -> PoolInfo:
        return self._runner.run(
            self._async.scale_pool(name, replicas, resources=resources)
        )

    def iter_pool_logs(
        self,
        name: str,
        *,
        follow: bool = False,
        tail: int = 100,
    ) -> Iterator[PoolLogEntry]:
        return self._runner.iter(
            self._async.iter_pool_logs(name, follow=follow, tail=tail)
        )

    def list_pool_logs(self, name: str, *, tail: int = 100) -> list[PoolLogEntry]:
        return self._runner.run(self._async.list_pool_logs(name, tail=tail))

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
        allow_internet: bool | None = None,
    ) -> ManagedSessionInfo:
        return self._runner.run(self._async.create_managed_session(
            image, experiment_id, profile, mode=mode, devbox=devbox,
            resources=resources, tools=tools, workspace_dir=workspace_dir,
            idle_timeout_seconds=idle_timeout_seconds,
            allocation_timeout_seconds=allocation_timeout_seconds,
            config_env=config_env, private_containers=private_containers,
            allow_internet=allow_internet,
        ))

    def list_experiment_sessions(
        self, experiment_id: str,
    ) -> list[ManagedSessionInfo]:
        return self._runner.run(
            self._async.list_experiment_sessions(experiment_id)
        )

    def list_experiments(self) -> list[ExperimentSummary]:
        return self._runner.run(self._async.list_experiments())

    def delete_experiment_info(
        self, experiment_id: str,
    ) -> DeleteExperimentResponse:
        return self._runner.run(
            self._async.delete_experiment_info(experiment_id)
        )

    def delete_experiment(self, experiment_id: str) -> int:
        return self._runner.run(self._async.delete_experiment(experiment_id))

    # --- Build API ---

    def build_image(
        self,
        image: str,
        context: BinaryIO | bytes,
        *,
        build_args: dict[str, str] | None = None,
        timeout: int | None = None,
        cache: bool = True,
    ) -> BuildResponse:
        return self._runner.run(self._async.build_image(
            image, context, build_args=build_args, timeout=timeout, cache=cache,
        ))

    # --- Iroh ---

    def get_iroh_addr(self, session_id: str) -> str:
        return self._runner.run(self._async.get_iroh_addr(session_id))

    # --- Health ---

    def health(self) -> bool:
        return self._runner.run(self._async.health())

    def close(self) -> None:
        self._runner.run(self._async.aclose())
        self._runner.close()

    def __enter__(self) -> GatewayClient:
        return self

    def __exit__(self, *_: object) -> None:
        self.close()
