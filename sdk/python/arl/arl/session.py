"""Sandbox session management for ARL via Gateway API."""

from __future__ import annotations

import base64
import json
import re
from typing import Any

from arl.gateway_client import GatewayClient
from arl.types import (
    ExecuteResponse,
    SessionInfo,
    StepResult,
    ToolResult,
    ToolsRegistry,
)

_SAFE_TOOL_NAME = re.compile(r"^[a-zA-Z0-9][a-zA-Z0-9_.-]*$")


class SandboxSession:
    """High-level sandbox session manager via the Gateway API.

    All execution goes through the Gateway HTTP API (no direct K8s API calls).
    Execute returns results synchronously - no polling needed.

    Examples:
        Using context manager (automatic cleanup):

        >>> with SandboxSession(pool_ref="python-39") as session:
        ...     result = session.execute([
        ...         {"name": "hello", "type": "command", "command": ["echo", "hello"]}
        ...     ])
        ...     print(result.results[0].output.stdout)

        Manual lifecycle management with restore:

        >>> session = SandboxSession(pool_ref="python-39", keep_alive=True)
        >>> try:
        ...     session.create_sandbox()
        ...     r1 = session.execute([...])
        ...     snap_id = r1.results[0].snapshot_id  # auto snapshot after each step
        ...     r2 = session.execute([...])
        ...     session.restore(snap_id)  # rollback to step 1 state
        ... finally:
        ...     session.delete_sandbox()

        Export trajectory for RL/SFT:

        >>> with SandboxSession(pool_ref="python-39") as session:
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
        pool_ref: str,
        namespace: str = "default",
        gateway_url: str = "http://localhost:8080",
        keep_alive: bool = False,
        timeout: float = 300.0,
        idle_timeout_seconds: int | None = None,
    ) -> None:
        self.pool_ref = pool_ref
        self.namespace = namespace
        self.keep_alive = keep_alive
        self.idle_timeout_seconds = idle_timeout_seconds

        self._client = GatewayClient(base_url=gateway_url, timeout=timeout)
        self._session_id: str | None = None
        self._session_info: SessionInfo | None = None

    @classmethod
    def attach(
        cls,
        session_id: str,
        gateway_url: str = "http://localhost:8080",
        timeout: float = 300.0,
        keep_alive: bool = True,
    ) -> SandboxSession:
        """Attach to an existing session by session ID.

        Retrieves session info from the Gateway and returns a
        SandboxSession bound to that session. No new sandbox is
        created.

        Args:
            session_id: The session ID to attach to.
            gateway_url: Gateway base URL.
            timeout: HTTP request timeout.
            keep_alive: If False, exiting a context manager will
                delete the session.  Defaults to True to avoid
                accidentally destroying a session you attached to.

        Returns:
            SandboxSession bound to the existing session.

        Raises:
            GatewayError: If the session does not exist.
        """
        client = GatewayClient(base_url=gateway_url, timeout=timeout)
        try:
            info = client.get_session(session_id)
        finally:
            client.close()

        instance = cls(
            pool_ref=info.pool_ref,
            namespace=info.namespace,
            gateway_url=gateway_url,
            keep_alive=keep_alive,
            timeout=timeout,
        )
        instance._session_id = info.id
        instance._session_info = info
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
        idle_timeout = self.idle_timeout_seconds
        if self.keep_alive and idle_timeout is None:
            idle_timeout = 1800  # 30 minutes default for keep_alive

        info = self._client.create_session(
            pool_ref=self.pool_ref,
            namespace=self.namespace,
            idle_timeout_seconds=idle_timeout,
        )
        self._session_id = info.id
        self._session_info = info
        return info

    def execute(
        self,
        steps: list[dict[str, Any]],
        trace_id: str | None = None,
    ) -> ExecuteResponse:
        """Execute steps in the sandbox. Returns synchronously.

        Args:
            steps: List of step dicts, each with 'name' and 'command'.
            trace_id: Optional trace ID for distributed tracing.

        Returns:
            ExecuteResponse with per-step results, snapshot IDs, and durations.
        """
        if self._session_id is None:
            raise RuntimeError("No session created. Call create_sandbox() first.")
        return self._client.execute(self._session_id, steps, trace_id)

    def restore(self, snapshot_id: str) -> None:
        """Restore workspace to a previous step's snapshot.

        Each step execution automatically creates a snapshot. Use the
        snapshot_id from a StepResult to restore to that step's state.

        Args:
            snapshot_id: Snapshot ID (git commit SHA) from a step result.
        """
        if self._session_id is None:
            raise RuntimeError("No session created. Call create_sandbox() first.")
        self._client.restore(self._session_id, snapshot_id)

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
        if self.keep_alive:
            import warnings

            warnings.warn(
                "Using context manager with keep_alive=True. "
                "Sandbox will NOT be automatically deleted on exit. "
                "Call delete_sandbox() manually or use keep_alive=False.",
                UserWarning,
                stacklevel=2,
            )
        self.create_sandbox()
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_val: BaseException | None,
        exc_tb: object | None,
    ) -> None:
        try:
            if not self.keep_alive:
                self.delete_sandbox()
        finally:
            self.close()
