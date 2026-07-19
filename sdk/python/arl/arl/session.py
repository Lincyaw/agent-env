"""Synchronous sandbox session management for ARL via Gateway API.

Thin wrappers around the async session classes in ``async_session.py``.
Each sync method delegates to the corresponding async method via a
background event loop (see :class:`~arl.gateway_client._SyncRunner`).
"""

from __future__ import annotations

from collections.abc import Callable, Iterable, Iterator
from pathlib import Path
from typing import TYPE_CHECKING, Any, BinaryIO

from arl.async_session import AsyncDevboxSession, AsyncManagedSession, AsyncSandboxSession
from arl.configenv import ConfigEnvSpec
from arl.exceptions import SessionNotInitializedError
from arl.gateway_client import _SyncRunner
from arl.types import (
    ContainerExecuteResponse,
    DevboxConfig,
    ExecuteOperationInfo,
    ExecuteResponse,
    LogEntry,
    PrivateContainerSpec,
    ReplayResponse,
    ResourceRequirements,
    RestoreResponse,
    SessionInfo,
    StepRequest,
    StepResult,
    ToolsSpec,
    UploadFileResponse,
)

if TYPE_CHECKING:
    from arl.iroh_transport import SyncIrohBridge


class SandboxSession:
    """High-level sandbox session manager via the Gateway API.

    All execution goes through the Gateway HTTP API (no direct K8s API calls).
    Execute returns results synchronously -- no polling needed.

    Examples:
        Using context manager (automatic cleanup):

        >>> with SandboxSession(image="python:3.12", profile="code") as session:
        ...     result = session.execute([
        ...         {"name": "hello", "type": "command", "command": ["echo", "hello"]}
        ...     ])
        ...     print(result.results[0].output.stdout)

        Manual lifecycle management with restore:

        >>> session = SandboxSession(image="python:3.12", profile="code")
        >>> try:
        ...     session.create_sandbox()
        ...     r1 = session.execute([...])
        ...     snap_id = r1.results[0].snapshot_id
        ...     r2 = session.execute([...])
        ...     session.restore(snap_id)
        ... finally:
        ...     session.delete_sandbox()

        Attach to an existing persistent session:

        >>> session = SandboxSession.attach("gw-12345", gateway_url="...")
        >>> result = session.execute([{"name": "ls", "command": ["ls"]}])
        >>> session.delete_sandbox()
    """

    def __init__(
        self,
        image: str | None = None,
        *,
        mode: str | None = None,
        devbox: DevboxConfig | dict[str, Any] | None = None,
        profile: str | None = "default",
        gateway_url: str = "",
        timeout: float = 300.0,
        idle_timeout_seconds: int | None = None,
        allocation_timeout_seconds: int | None = None,
        api_key: str | None = None,
        private_containers: Iterable[PrivateContainerSpec | dict[str, Any]] | None = None,
        iroh_addr: str | None = None,
        allow_internet: bool | None = None,
    ) -> None:
        self._init_async(AsyncSandboxSession(
            image=image, mode=mode, devbox=devbox, profile=profile,
            gateway_url=gateway_url, timeout=timeout,
            idle_timeout_seconds=idle_timeout_seconds,
            allocation_timeout_seconds=allocation_timeout_seconds,
            api_key=api_key, private_containers=private_containers,
            iroh_addr=iroh_addr, allow_internet=allow_internet,
        ))

    def _init_async(self, async_session: AsyncSandboxSession) -> None:
        """Wire up the async session and runner (called by subclass constructors too)."""
        self._async = async_session
        self._runner = _SyncRunner()
        self._sync_iroh: SyncIrohBridge | None = None

    @classmethod
    def attach(
        cls,
        session_id: str,
        gateway_url: str = "",
        timeout: float = 300.0,
        api_key: str | None = None,
        iroh_addr: str | None = None,
    ) -> SandboxSession:
        """Attach to an existing session by session ID."""
        runner = _SyncRunner()
        try:
            async_session = runner.run(AsyncSandboxSession.attach(
                session_id, gateway_url=gateway_url, timeout=timeout,
                api_key=api_key, iroh_addr=iroh_addr,
            ))
        except Exception:
            runner.close()
            raise
        instance = cls.__new__(cls)
        instance._async = async_session
        instance._runner = runner
        instance._sync_iroh = None
        return instance

    # --- Properties ---

    @property
    def session_id(self) -> str | None:
        return self._async.session_id

    @property
    def session_info(self) -> SessionInfo | None:
        return self._async.session_info

    @property
    def image(self) -> str:
        return self._async.image

    @image.setter
    def image(self, value: str) -> None:
        self._async.image = value

    @property
    def profile(self) -> str:
        return self._async.profile

    @profile.setter
    def profile(self, value: str) -> None:
        self._async.profile = value

    @property
    def mode(self) -> str | None:
        return self._async.mode

    @property
    def devbox(self) -> DevboxConfig | dict[str, Any] | None:
        return self._async.devbox

    @property
    def allow_internet(self) -> bool | None:
        return self._async.allow_internet

    # --- Session lifecycle ---

    def create_sandbox(self) -> SessionInfo:
        """Create a new session (sandbox) via the Gateway."""
        return self._runner.run(self._async.create_sandbox())

    def delete_sandbox(self) -> None:
        """Delete the session and its underlying sandbox."""
        self._runner.run(self._async.delete_sandbox())

    # --- Execution ---

    def execute(
        self,
        steps: list[StepRequest | dict[str, Any]],
        trace_id: str | None = None,
        operation_id: str | None = None,
        on_output: Callable[[str, str], None] | None = None,
        recover: bool = True,
        recover_timeout: float | None = None,
    ) -> ExecuteResponse:
        """Execute steps in the sandbox. Returns synchronously."""
        return self._runner.run(self._async.execute(
            steps, trace_id, operation_id=operation_id, on_output=on_output,
            recover=recover, recover_timeout=recover_timeout,
        ))

    def get_execute_operation(self, operation_id: str) -> ExecuteOperationInfo:
        """Get the status of a pending execute operation."""
        return self._runner.run(
            self._async.get_execute_operation(operation_id)
        )

    def execute_container(
        self,
        container: str,
        steps: list[StepRequest | dict[str, Any]],
    ) -> ContainerExecuteResponse:
        """Execute steps in a configured private container."""
        return self._runner.run(
            self._async.execute_container(container, steps)
        )

    # --- Restore / replay ---

    def restore(
        self,
        snapshot_id: str,
        operation_id: str | None = None,
        recover: bool = True,
        recover_timeout: float | None = None,
    ) -> RestoreResponse:
        """Restore workspace to a previous step's snapshot."""
        return self._runner.run(self._async.restore(
            snapshot_id, operation_id=operation_id,
            recover=recover, recover_timeout=recover_timeout,
        ))

    def replay_from(
        self,
        source_session_id: str,
        up_to_step: int | None = None,
        operation_id: str | None = None,
        recover: bool = True,
        recover_timeout: float | None = None,
    ) -> ReplayResponse:
        """Replay another session's history into this session."""
        return self._runner.run(self._async.replay_from(
            source_session_id, up_to_step=up_to_step,
            operation_id=operation_id, recover=recover,
            recover_timeout=recover_timeout,
        ))

    # --- File operations ---

    def upload_file(
        self,
        path: str,
        content: str | bytes | Iterable[bytes] | BinaryIO,
        sha256: str | None = None,
    ) -> UploadFileResponse:
        """Upload one file into the session workspace."""
        return self._runner.run(
            self._async.upload_file(path, content, sha256=sha256)
        )

    def download_file(self, path: str) -> bytes:
        """Download one file from the session workspace into memory."""
        return self._runner.run(self._async.download_file(path))

    def upload_path(
        self,
        local_path: str | Path,
        remote_path: str,
        sha256: str | None = None,
    ) -> UploadFileResponse:
        """Stream a local file into the session workspace."""
        return self._runner.run(
            self._async.upload_path(local_path, remote_path, sha256=sha256)
        )

    def download_path(self, remote_path: str, local_path: str | Path) -> None:
        """Stream a session file to a local path."""
        self._runner.run(self._async.download_path(remote_path, local_path))

    def iter_download(self, path: str, chunk_size: int = 1024 * 1024) -> Iterator[bytes]:
        """Iterate over a session file as byte chunks."""
        if self._async.session_id is None:
            raise SessionNotInitializedError()
        return self._runner.iter(
            self._async._client.iter_download_file(
                self._async.session_id, path, chunk_size=chunk_size,
            )
        )

    # --- History ---

    def get_history(self) -> list[StepResult]:
        """Get complete execution history for this session."""
        return self._runner.run(self._async.get_history())

    def export_trajectory(self) -> str:
        """Export execution history as JSONL trajectory (for RL/SFT)."""
        return self._runner.run(self._async.export_trajectory())

    # --- Logs ---

    def iter_logs(
        self, *, follow: bool = False, tail: int = 100,
    ) -> Iterator[LogEntry]:
        """Iterate over session log entries."""
        if self._async.session_id is None:
            raise SessionNotInitializedError()
        return self._runner.iter(
            self._async._client.iter_session_logs(
                self._async.session_id, follow=follow, tail=tail,
            )
        )

    def get_logs(self, *, tail: int = 100) -> list[LogEntry]:
        """Return recent session log entries."""
        return self._runner.run(self._async.get_logs(tail=tail))

    # --- Suspend / resume ---

    def suspend(self) -> None:
        """Suspend the devbox session (keeps storage, terminates pod)."""
        self._runner.run(self._async.suspend())

    def resume(self) -> None:
        """Resume a suspended devbox session."""
        self._runner.run(self._async.resume())

    # --- Tunnel (iroh direct-connect only) ---

    def _get_sync_iroh(self) -> SyncIrohBridge:
        if self._async._iroh_addr is None:
            raise RuntimeError("tunnel requires iroh direct-connect (pass iroh_addr)")
        if self._sync_iroh is None:
            from arl.iroh_transport import SyncIrohBridge as _SyncIrohBridge

            self._sync_iroh = _SyncIrohBridge(self._async._iroh_addr)
        return self._sync_iroh

    def tunnel_forward(
        self,
        remote_host: str = "localhost",
        remote_port: int = 22,
        local_port: int = 0,
        local_host: str = "127.0.0.1",
    ) -> int:
        """Forward a local TCP port to a remote target inside the sandbox.

        Requires iroh direct-connect (pass ``iroh_addr`` when creating the session).
        Returns the tunnel tag that can be passed to :meth:`tunnel_stop`.
        """
        return self._get_sync_iroh().tunnel_forward(
            remote_host, remote_port, local_port, local_host,
        )

    def tunnel_stop(self, tunnel_tag: int) -> None:
        """Stop a tunnel previously started with :meth:`tunnel_forward`."""
        self._get_sync_iroh().tunnel_stop(tunnel_tag)

    def tunnel_list(self) -> list[dict[str, object]]:
        """List active tunnels on this connection."""
        return self._get_sync_iroh().tunnel_list()

    # --- Cleanup ---

    def close(self) -> None:
        """Close the underlying HTTP client, iroh transport, and event loop."""
        if self._sync_iroh is not None:
            self._sync_iroh.close()
            self._sync_iroh = None
        self._runner.run(self._async.aclose())
        self._runner.close()

    def __enter__(self) -> SandboxSession:
        if self._async.session_id is None:
            self.create_sandbox()
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_val: BaseException | None,
        exc_tb: object | None,
    ) -> None:
        try:
            if self._async._delete_on_exit:
                self.delete_sandbox()
        finally:
            self.close()


class ManagedSession(SandboxSession):
    """Ultra-simple session that handles pools automatically.

    Just specify image + experiment ID. Pool lifecycle is handled server-side.

    Examples:
        >>> with ManagedSession(image="python:3.11-slim", experiment_id="my-exp") as s:
        ...     result = s.execute([{"name": "hello", "command": ["echo", "hi"]}])
        ...     print(result.results[0].output.stdout)
    """

    def __init__(
        self,
        image: str,
        experiment_id: str,
        gateway_url: str = "",
        timeout: float = 300.0,
        resources: ResourceRequirements | None = None,
        tools: ToolsSpec | None = None,
        workspace_dir: str = "/workspace",
        idle_timeout_seconds: int | None = None,
        allocation_timeout_seconds: int | None = None,
        config_env: ConfigEnvSpec | dict[str, Any] | None = None,
        profile: str = "default",
        api_key: str | None = None,
        private_containers: Iterable[PrivateContainerSpec | dict[str, Any]] | None = None,
        mode: str | None = None,
        iroh_addr: str | None = None,
        allow_internet: bool | None = None,
    ) -> None:
        self._init_async(AsyncManagedSession(
            image=image, experiment_id=experiment_id,
            gateway_url=gateway_url, timeout=timeout,
            resources=resources, tools=tools, workspace_dir=workspace_dir,
            idle_timeout_seconds=idle_timeout_seconds,
            allocation_timeout_seconds=allocation_timeout_seconds,
            config_env=config_env, profile=profile, api_key=api_key,
            private_containers=private_containers, mode=mode,
            iroh_addr=iroh_addr, allow_internet=allow_internet,
        ))

    @property
    def experiment_id(self) -> str:
        assert isinstance(self._async, AsyncManagedSession)
        return self._async.experiment_id


class DevboxSession(SandboxSession):
    """Long-lived development sandbox session.

    Creates a devbox-mode session with extended lifecycle defaults:
    - 4-hour idle timeout (vs 10 minutes for regular sessions)

    Examples:
        >>> with DevboxSession(image="ubuntu:22.04") as devbox:
        ...     devbox.execute([{"name": "setup", "command": ["apt-get", "update"]}])
        ...     devbox.upload_file("main.py", "print('hello')")
        ...     devbox.execute([{"name": "run", "command": ["python", "main.py"]}])
    """

    def __init__(
        self,
        image: str | None = None,
        *,
        devbox: DevboxConfig | dict[str, Any] | None = None,
        profile: str | None = "default",
        gateway_url: str = "",
        timeout: float = 300.0,
        idle_timeout_seconds: int | None = None,
        allocation_timeout_seconds: int | None = None,
        api_key: str | None = None,
        private_containers: Iterable[PrivateContainerSpec | dict[str, Any]] | None = None,
        iroh_addr: str | None = None,
    ) -> None:
        self._init_async(AsyncDevboxSession(
            image=image, devbox=devbox, profile=profile,
            gateway_url=gateway_url, timeout=timeout,
            idle_timeout_seconds=idle_timeout_seconds,
            allocation_timeout_seconds=allocation_timeout_seconds,
            api_key=api_key, private_containers=private_containers,
            iroh_addr=iroh_addr,
        ))
