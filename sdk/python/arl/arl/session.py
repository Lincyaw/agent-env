"""Sandbox session management for ARL via Gateway API."""

from __future__ import annotations

import base64
import json
import re
from collections.abc import Callable, Iterable, Iterator
from pathlib import Path
from typing import Any, BinaryIO

from arl.configenv import ConfigEnvSpec
from arl.gateway_client import GatewayClient
from arl.types import (
    ContainerExecuteResponse,
    ExecuteResponse,
    LogEntry,
    PrivateContainerSpec,
    ReplayResponse,
    ResourceRequirements,
    SessionInfo,
    StepResult,
    ToolResult,
    ToolsRegistry,
    ToolsSpec,
    UploadFileResponse,
)

_SAFE_TOOL_NAME = re.compile(r"^[a-zA-Z0-9][a-zA-Z0-9_.-]*$")


class SandboxSession:
    """High-level sandbox session manager via the Gateway API.

    All execution goes through the Gateway HTTP API (no direct K8s API calls).
    Execute returns results synchronously - no polling needed.

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
        ...     snap_id = r1.results[0].snapshot_id  # auto snapshot after each step
        ...     r2 = session.execute([...])
        ...     session.restore(snap_id)  # rollback to step 1 state
        ... finally:
        ...     session.delete_sandbox()

        Export trajectory for RL/SFT:

        >>> with SandboxSession(image="python:3.12", profile="code") as session:
        ...     session.execute([...])
        ...     session.execute([...])
        ...     jsonl = session.export_trajectory()

        Attach to an existing persistent session:

        >>> session = SandboxSession.attach("gw-12345", gateway_url="...")
        >>> result = session.execute([{"name": "ls", "command": ["ls"]}])
        >>> session.delete_sandbox()  # explicit cleanup when done
    """

    def __init__(
        self,
        image: str | None = None,
        *,
        mode: str | None = None,
        profile: str | None = "default",
        gateway_url: str = "http://localhost:8080",
        timeout: float = 300.0,
        idle_timeout_seconds: int | None = None,
        max_lifetime_seconds: int | None = None,
        api_key: str | None = None,
        private_containers: Iterable[PrivateContainerSpec | dict[str, Any]] | None = None,
    ) -> None:
        self.image = image or ""
        self.profile = profile or ""
        self.mode = mode
        self.idle_timeout_seconds = idle_timeout_seconds
        self.max_lifetime_seconds = max_lifetime_seconds
        self.private_containers = private_containers

        self._client = GatewayClient(base_url=gateway_url, timeout=timeout, api_key=api_key)
        self._api_key = api_key
        self._session_id: str | None = None
        self._session_info: SessionInfo | None = None
        self._delete_on_exit = True

    @classmethod
    def attach(
        cls,
        session_id: str,
        gateway_url: str = "http://localhost:8080",
        timeout: float = 300.0,
        api_key: str | None = None,
    ) -> SandboxSession:
        """Attach to an existing session by session ID.

        Retrieves session info from the Gateway and returns a
        SandboxSession bound to that session. No new sandbox is
        created.

        Args:
            session_id: The session ID to attach to.
            gateway_url: Gateway base URL.
            timeout: HTTP request timeout.
            api_key: API key for authentication.

        Returns:
            SandboxSession bound to the existing session.

        Raises:
            GatewayError: If the session does not exist.
        """
        client = GatewayClient(base_url=gateway_url, timeout=timeout, api_key=api_key)
        try:
            info = client.get_session(session_id)
        finally:
            client.close()

        instance = cls(
            image=info.image or None,
            profile=info.profile or None,
            gateway_url=gateway_url,
            timeout=timeout,
            api_key=api_key,
        )
        instance._session_id = info.id
        instance._session_info = info
        instance._delete_on_exit = False
        return instance

    @property
    def session_id(self) -> str | None:
        return self._session_id

    @property
    def session_info(self) -> SessionInfo | None:
        return self._session_info

    def create_sandbox(self) -> SessionInfo:
        """Create a new session (sandbox) via the Gateway.

        Returns:
            SessionInfo with sandbox details (pod IP, pod name, etc.)
        """
        info = self._client.create_session(
            image=self.image or None,
            profile=self.profile or None,
            mode=self.mode,
            idle_timeout_seconds=self.idle_timeout_seconds,
            max_lifetime_seconds=self.max_lifetime_seconds,
            private_containers=self.private_containers,
        )
        self._session_id = info.id
        self._session_info = info
        self.image = info.image
        self.profile = info.profile
        return info

    def execute(
        self,
        steps: list[dict[str, Any]],
        trace_id: str | None = None,
        operation_id: str | None = None,
        on_output: Callable[[str, str], None] | None = None,
    ) -> ExecuteResponse:
        """Execute steps in the sandbox. Returns synchronously.

        Args:
            steps: List of step dicts, each with 'name' and 'command'.
            trace_id: Optional trace ID for distributed tracing.
            on_output: Optional callback invoked with (stdout_chunk, stderr_chunk)
                for each partial output event during streaming execution.

        Returns:
            ExecuteResponse with per-step results, snapshot IDs, and durations.
        """
        if self._session_id is None:
            raise RuntimeError("No session created. Call create_sandbox() first.")
        return self._client.execute(
            self._session_id,
            steps,
            trace_id,
            operation_id=operation_id,
            on_output=on_output,
        )

    def execute_container(
        self,
        container: str,
        steps: list[dict[str, Any]],
    ) -> ContainerExecuteResponse:
        """Execute steps in a configured private container."""
        if self._session_id is None:
            raise RuntimeError("No session created. Call create_sandbox() first.")
        return self._client.execute_container(self._session_id, container, steps)

    def restore(self, snapshot_id: str) -> None:
        """Restore workspace to a previous step's snapshot.

        Each step execution automatically creates a snapshot. Use the
        snapshot_id from a StepResult to restore to that step's state.

        Args:
            snapshot_id: Snapshot ID (step index string) from a step result.
        """
        if self._session_id is None:
            raise RuntimeError("No session created. Call create_sandbox() first.")
        self._client.restore(self._session_id, snapshot_id)

    def replay_from(
        self,
        source_session_id: str,
        up_to_step: int | None = None,
    ) -> ReplayResponse:
        """Replay another session's history into this session."""
        if self._session_id is None:
            raise RuntimeError("No session created. Call create_sandbox() first.")
        return self._client.replay_from(
            self._session_id,
            source_session_id=source_session_id,
            up_to_step=up_to_step,
        )

    def upload_file(
        self,
        path: str,
        content: str | bytes | Iterable[bytes] | BinaryIO,
        sha256: str | None = None,
    ) -> UploadFileResponse:
        """Upload one file into the session workspace.

        Args:
            path: Relative path within the workspace.
            content: Text, bytes, a binary file object, or an iterable of byte chunks.
            sha256: Optional expected SHA-256 checksum in hex.

        Returns:
            UploadFileResponse with the normalized path and byte count.
        """
        if self._session_id is None:
            raise RuntimeError("No session created. Call create_sandbox() first.")
        return self._client.upload_file(
            self._session_id,
            path=path,
            content=content,
            sha256=sha256,
        )

    def download_file(self, path: str) -> bytes:
        """Download one file from the session workspace into memory."""
        if self._session_id is None:
            raise RuntimeError("No session created. Call create_sandbox() first.")
        return self._client.download_file(self._session_id, path)

    def upload_path(
        self,
        local_path: str | Path,
        remote_path: str,
        sha256: str | None = None,
    ) -> UploadFileResponse:
        """Stream a local file into the session workspace."""
        if self._session_id is None:
            raise RuntimeError("No session created. Call create_sandbox() first.")
        return self._client.upload_path(
            self._session_id,
            local_path=local_path,
            remote_path=remote_path,
            sha256=sha256,
        )

    def download_path(self, remote_path: str, local_path: str | Path) -> None:
        """Stream a session file to a local path."""
        if self._session_id is None:
            raise RuntimeError("No session created. Call create_sandbox() first.")
        self._client.download_path(self._session_id, remote_path, local_path)

    def iter_download(self, path: str, chunk_size: int = 1024 * 1024) -> Iterator[bytes]:
        """Iterate over a session file as byte chunks."""
        if self._session_id is None:
            raise RuntimeError("No session created. Call create_sandbox() first.")
        return self._client.iter_download_file(self._session_id, path, chunk_size=chunk_size)

    def get_history(self) -> list[StepResult]:
        """Get complete execution history for this session.

        Returns:
            List of StepResult with input, output, snapshot IDs, and durations.
        """
        if self._session_id is None:
            raise RuntimeError("No session created. Call create_sandbox() first.")
        return self._client.get_history(self._session_id)

    def export_trajectory(self) -> str:
        """Export execution history as JSONL trajectory (for RL/SFT).

        Returns:
            JSONL string, one entry per step.
        """
        if self._session_id is None:
            raise RuntimeError("No session created. Call create_sandbox() first.")
        return self._client.get_trajectory(self._session_id)

    def iter_logs(
        self,
        *,
        follow: bool = False,
        tail: int = 100,
    ) -> Iterator[LogEntry]:
        """Iterate over session sidecar log entries."""
        if self._session_id is None:
            raise RuntimeError("No session created. Call create_sandbox() first.")
        return self._client.iter_session_logs(self._session_id, follow=follow, tail=tail)

    def get_logs(self, *, tail: int = 100) -> list[LogEntry]:
        """Return recent session sidecar log entries."""
        if self._session_id is None:
            raise RuntimeError("No session created. Call create_sandbox() first.")
        return self._client.list_session_logs(self._session_id, tail=tail)

    def list_tools(self) -> ToolsRegistry:
        """List all available tools in the sandbox.

        Reads /opt/arl/tools/registry.json from the executor container.

        Returns:
            ToolsRegistry with all tool manifests.

        Raises:
            RuntimeError: If no session created or registry file not found.
        """
        if self._session_id is None:
            raise RuntimeError("No session created. Call create_sandbox() first.")
        result = self._client.execute(
            self._session_id,
            [
                {"name": "_list_tools", "command": ["cat", "/opt/arl/tools/registry.json"]},
            ],
        )
        step = result.results[0]
        if step.output.exit_code != 0:
            raise RuntimeError(f"Failed to read tool registry: {step.output.stderr}")
        return ToolsRegistry.model_validate_json(step.output.stdout)

    def call_tool(
        self,
        tool_name: str,
        params: dict[str, object] | None = None,
    ) -> ToolResult:
        """Call a tool by name with JSON parameters.

        Pipes JSON params to the tool's entrypoint script via stdin.
        Uses base64 encoding to safely pass parameters without shell injection.

        Args:
            tool_name: Name of the tool (must exist in registry).
            params: Parameters dict (passed as JSON stdin to the tool).

        Returns:
            ToolResult with parsed JSON output, exit code, and stderr.

        Raises:
            ValueError: If tool_name contains unsafe characters.
            RuntimeError: If no session created.
        """
        if self._session_id is None:
            raise RuntimeError("No session created. Call create_sandbox() first.")
        if not _SAFE_TOOL_NAME.match(tool_name):
            raise ValueError(f"Invalid tool name: {tool_name!r}")

        params_json = json.dumps(params or {})
        params_b64 = base64.b64encode(params_json.encode()).decode()
        tool_dir = f"/opt/arl/tools/{tool_name}"
        # Use base64 to safely pass JSON without shell injection risk.
        # Read entrypoint from manifest via sed (busybox-compatible).
        cmd = (
            f"ENTRYPOINT=$(cat {tool_dir}/manifest.json"
            ' | sed -n \'s/.*"entrypoint":"\\([^"]*\\)".*/\\1/p\')'
            f" && printf '%s' '{params_b64}' | base64 -d | {tool_dir}/$ENTRYPOINT"
        )

        result = self._client.execute(
            self._session_id,
            [
                {"name": f"_call_{tool_name}", "command": ["sh", "-c", cmd]},
            ],
        )
        step = result.results[0]
        parsed: dict[str, object] = {}
        try:
            parsed = json.loads(step.output.stdout)
        except (json.JSONDecodeError, ValueError):
            pass
        return ToolResult(
            raw_output=step.output.stdout,
            parsed=parsed,
            exit_code=step.output.exit_code,
            stderr=step.output.stderr,
        )

    def delete_sandbox(self) -> None:
        """Delete the session and its underlying sandbox."""
        if self._session_id is None:
            return
        self._client.delete_session(self._session_id)
        self._session_id = None
        self._session_info = None

    def close(self) -> None:
        """Close the underlying HTTP client."""
        self._client.close()

    def __enter__(self) -> SandboxSession:
        if self._session_id is None:
            self.create_sandbox()
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_val: BaseException | None,
        exc_tb: object | None,
    ) -> None:
        try:
            if self._delete_on_exit:
                self.delete_sandbox()
        finally:
            self.close()


class ManagedSession(SandboxSession):
    """Ultra-simple session that handles pools automatically.

    Just specify image + experiment ID. Pool lifecycle is handled server-side.
    No need to create or manage WarmPools manually.

    Examples:
        Basic usage with context manager:

        >>> with ManagedSession(image="python:3.11-slim", experiment_id="my-exp") as s:
        ...     result = s.execute([{"name": "hello", "command": ["echo", "hi"]}])
        ...     print(result.results[0].output.stdout)

        With custom resources:

        >>> from arl import ResourceRequirements
        >>> with ManagedSession(
        ...     image="python:3.11-slim",
        ...     experiment_id="my-exp",
        ...     resources=ResourceRequirements(
        ...         requests={"cpu": "500m", "memory": "512Mi"},
        ...         limits={"cpu": "2", "memory": "2Gi"},
        ...     ),
        ... ) as s:
        ...     result = s.execute([{"name": "test", "command": ["python", "-c", "print(1)"]}])

        Batch cleanup by experiment:

        >>> from arl import GatewayClient
        >>> client = GatewayClient(base_url="http://localhost:8080")
        >>> client.delete_experiment("my-exp")
    """

    def __init__(
        self,
        image: str,
        experiment_id: str,
        gateway_url: str = "http://localhost:8080",
        timeout: float = 300.0,
        resources: ResourceRequirements | None = None,
        tools: ToolsSpec | None = None,
        workspace_dir: str = "/workspace",
        idle_timeout_seconds: int | None = None,
        max_lifetime_seconds: int | None = None,
        config_env: ConfigEnvSpec | dict[str, Any] | None = None,
        profile: str = "default",
        api_key: str | None = None,
        private_containers: Iterable[PrivateContainerSpec | dict[str, Any]] | None = None,
        mode: str | None = None,
    ) -> None:
        super().__init__(
            image=image,
            profile=profile,
            mode=mode,
            gateway_url=gateway_url,
            timeout=timeout,
            api_key=api_key,
            private_containers=private_containers,
        )
        self._image = image
        self._profile = profile
        self._mode = mode
        self._experiment_id = experiment_id
        self._resources = resources
        self._config_env = config_env
        self._tools = tools
        self._workspace_dir = workspace_dir
        self._idle_timeout_seconds = idle_timeout_seconds
        self._max_lifetime_seconds = max_lifetime_seconds
        self._private_containers = private_containers

    @property
    def experiment_id(self) -> str:
        return self._experiment_id

    def create_sandbox(self) -> SessionInfo:
        """Create a managed session via the Gateway.

        The server automatically handles pool creation and scaling.

        Returns:
            ManagedSessionInfo with session details.
        """
        info = self._client.create_managed_session(
            image=self._image,
            experiment_id=self._experiment_id,
            profile=self._profile,
            mode=self._mode,
            config_env=self._config_env,
            resources=self._resources,
            tools=self._tools,
            workspace_dir=self._workspace_dir,
            idle_timeout_seconds=self._idle_timeout_seconds,
            max_lifetime_seconds=self._max_lifetime_seconds,
            private_containers=self._private_containers,
        )
        self._session_id = info.id
        self._session_info = info
        self.image = info.image
        self.profile = info.profile
        return info


class DevboxSession(SandboxSession):
    """Long-lived development sandbox session.

    Creates a devbox-mode session with extended lifecycle defaults:
    - 4-hour idle timeout (vs 10 minutes for regular sessions)
    - No maximum lifetime (vs 1 hour for regular sessions)

    All execution, file, and shell APIs work identically to regular sessions.

    Examples:
        >>> with DevboxSession(image="ubuntu:22.04") as devbox:
        ...     devbox.execute([{"name": "setup", "command": ["apt-get", "update"]}])
        ...     devbox.upload_file("main.py", "print('hello')")
        ...     devbox.execute([{"name": "run", "command": ["python", "main.py"]}])

        Attach to an existing devbox:
        >>> devbox = DevboxSession.attach("gw-12345", gateway_url="...")
    """

    def __init__(
        self,
        image: str | None = None,
        *,
        profile: str | None = "default",
        gateway_url: str = "http://localhost:8080",
        timeout: float = 300.0,
        idle_timeout_seconds: int | None = None,
        max_lifetime_seconds: int | None = None,
        api_key: str | None = None,
        private_containers: Iterable[PrivateContainerSpec | dict[str, Any]] | None = None,
    ) -> None:
        super().__init__(
            image=image,
            mode="devbox",
            profile=profile,
            gateway_url=gateway_url,
            timeout=timeout,
            idle_timeout_seconds=idle_timeout_seconds,
            max_lifetime_seconds=max_lifetime_seconds,
            api_key=api_key,
            private_containers=private_containers,
        )
