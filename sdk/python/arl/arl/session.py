"""Sandbox session management for ARL."""

import time
from collections.abc import Iterator
from typing import Any, cast

from kubernetes import client, config

from arl.sidecar_client import SidecarClient
from arl.types import TaskStep


class SandboxSession:
    """High-level sandbox session manager with automatic lifecycle management.

    Provides context manager support for automatic resource cleanup and
    simplified task execution against sandboxes.

    Examples:
        Using context manager (automatic cleanup):

        >>> with SandboxSession(pool_ref="python-39", namespace="default") as session:
        ...     result = session.execute([{"name": "test", "type": "Command",
        ...                                "command": ["echo", "hello"]}])

        Manual lifecycle management (for sandbox reuse):

        >>> session = SandboxSession(pool_ref="python-39", namespace="default",
        ...                          keep_alive=True)
        >>> try:
        ...     session.create_sandbox()
        ...     result1 = session.execute([...])
        ...     result2 = session.execute([...])  # Reuses same sandbox
        ... finally:
        ...     session.delete_sandbox()

        Streaming execution (direct gRPC):

        >>> with SandboxSession(pool_ref="python-39") as session:
        ...     for log in session.execute_stream(["python", "-c", "print('hello')"]):
        ...         print(log.stdout, end="")
    """

    DEFAULT_GRPC_PORT = 9090

    def __init__(
        self,
        pool_ref: str,
        namespace: str = "default",
        keep_alive: bool = False,
        timeout: int = 300,
        grpc_port: int = DEFAULT_GRPC_PORT,
    ) -> None:
        """Initialize sandbox session.

        Args:
            pool_ref: Name of the WarmPool to allocate sandbox from
            namespace: Kubernetes namespace (default: "default")
            keep_alive: If True, sandbox persists after context exit
            timeout: Maximum seconds to wait for operations (default: 300)
            grpc_port: gRPC port for sidecar communication (default: 9090)
        """
        self.pool_ref = pool_ref
        self.namespace = namespace
        self.keep_alive = keep_alive
        self.timeout = timeout
        self.grpc_port = grpc_port

        self.sandbox_name: str | None = None
        self._pod_ip: str | None = None
        self._custom_api: client.CustomObjectsApi | None = None
        self._sidecar_client: SidecarClient | None = None
        self._entered = False

    @property
    def custom_api(self) -> client.CustomObjectsApi:
        """Get Kubernetes custom objects API client (lazy initialization)."""
        if self._custom_api is None:
            try:
                config.load_incluster_config()
            except config.ConfigException:
                config.load_kube_config()
            self._custom_api = client.CustomObjectsApi()
        return self._custom_api

    @property
    def pod_ip(self) -> str | None:
        """Get the pod IP of the sandbox (available after sandbox is ready)."""
        return self._pod_ip

    @property
    def sidecar_client(self) -> SidecarClient:
        """Get gRPC client for direct sidecar communication.

        Raises:
            RuntimeError: If sandbox is not ready or pod IP is not available
        """
        if self._pod_ip is None:
            raise RuntimeError("Sandbox not ready - pod IP not available")
        if self._sidecar_client is None:
            self._sidecar_client = SidecarClient(
                f"{self._pod_ip}:{self.grpc_port}",
                timeout=float(self.timeout),
            )
        return self._sidecar_client

    def create_sandbox(self) -> dict[str, Any]:
        """Create a new sandbox from the warm pool.

        Returns:
            Sandbox resource dictionary

        Raises:
            RuntimeError: If sandbox creation fails or times out
        """
        sandbox_name = f"session-{int(time.time())}"

        sandbox_body = {
            "apiVersion": "arl.infra.io/v1alpha1",
            "kind": "Sandbox",
            "metadata": {
                "name": sandbox_name,
                "namespace": self.namespace,
            },
            "spec": {
                "poolRef": self.pool_ref,
                "keepAlive": self.keep_alive,
            },
        }

        # Create sandbox resource
        sandbox = self.custom_api.create_namespaced_custom_object(
            group="arl.infra.io",
            version="v1alpha1",
            namespace=self.namespace,
            plural="sandboxes",
            body=sandbox_body,
        )

        self.sandbox_name = sandbox_name

        # Wait for sandbox to be ready
        self._wait_for_sandbox_ready()

        return cast(dict[str, Any], sandbox)

    def _wait_for_sandbox_ready(self, poll_interval: float = 1.0) -> None:
        """Wait for sandbox to reach Ready state.

        Args:
            poll_interval: Seconds between status checks

        Raises:
            RuntimeError: If sandbox doesn't become ready within timeout
        """
        if self.sandbox_name is None:
            raise RuntimeError("No sandbox created")

        start_time: float = time.time()

        while time.time() - start_time < self.timeout:
            try:
                sandbox_obj: object = self.custom_api.get_namespaced_custom_object(
                    group="arl.infra.io",
                    version="v1alpha1",
                    namespace=self.namespace,
                    plural="sandboxes",
                    name=self.sandbox_name,
                )

                if not isinstance(sandbox_obj, dict):
                    continue

                status: dict[str, Any] = cast(dict[str, Any], sandbox_obj.get("status", {}))
                phase: str | None = status.get("phase")

                if phase == "Ready":
                    # Capture pod IP for direct gRPC communication
                    self._pod_ip = status.get("podIP")
                    return
                elif phase == "Failed":
                    conditions: list[dict[str, Any]] = status.get("conditions", [])
                    msg: str = (
                        conditions[0].get("message", "Unknown error")
                        if conditions
                        else "Unknown error"
                    )
                    raise RuntimeError(f"Sandbox failed to start: {msg}")

            except client.ApiException as e:
                if e.status != 404:
                    raise

            time.sleep(poll_interval)

        raise RuntimeError(f"Sandbox '{self.sandbox_name}' not ready after {self.timeout}s")

    def execute(self, steps: list[TaskStep], trace_id: str | None = None) -> dict[str, Any]:
        """Execute task steps in the sandbox.

        Args:
            steps: List of task steps to execute
            trace_id: Optional trace ID for distributed tracing (e.g., uuid.uuid4())

        Returns:
            Task resource dictionary with status

        Raises:
            RuntimeError: If no sandbox exists or task execution fails
        """
        if self.sandbox_name is None:
            raise RuntimeError("No sandbox created. Call create_sandbox() first.")

        task_name = f"{self.sandbox_name}-task-{int(time.time() * 1000)}"

        task_body: dict[str, Any] = {
            "apiVersion": "arl.infra.io/v1alpha1",
            "kind": "Task",
            "metadata": {
                "name": task_name,
                "namespace": self.namespace,
            },
            "spec": {
                "sandboxRef": self.sandbox_name,
                "steps": steps,
            },
        }

        # Add trace ID if provided
        if trace_id is not None:
            task_body["spec"]["traceID"] = trace_id

        # Create task resource
        self.custom_api.create_namespaced_custom_object(
            group="arl.infra.io",
            version="v1alpha1",
            namespace=self.namespace,
            plural="tasks",
            body=task_body,
        )

        # Wait for task completion
        return self._wait_for_task_completion(task_name)

    def _wait_for_task_completion(
        self, task_name: str, poll_interval: float = 0.5
    ) -> dict[str, Any]:
        """Wait for task to complete.

        Args:
            task_name: Name of the task resource
            poll_interval: Seconds between status checks

        Returns:
            Completed task resource dictionary

        Raises:
            RuntimeError: If task doesn't complete within timeout
        """
        start_time: float = time.time()

        while time.time() - start_time < self.timeout:
            try:
                task_obj: object = self.custom_api.get_namespaced_custom_object(
                    group="arl.infra.io",
                    version="v1alpha1",
                    namespace=self.namespace,
                    plural="tasks",
                    name=task_name,
                )

                if not isinstance(task_obj, dict):
                    continue

                status: dict[str, Any] = cast(dict[str, Any], task_obj.get("status", {}))
                state: str | None = status.get("state")

                if state in ("Succeeded", "Failed"):
                    return task_obj

            except client.ApiException as e:
                if e.status != 404:
                    raise

            time.sleep(poll_interval)

        raise RuntimeError(f"Task '{task_name}' did not complete after {self.timeout}s")

    def delete_sandbox(self) -> None:
        """Delete the sandbox resource.

        This method is idempotent and safe to call multiple times.
        """
        # Close sidecar client if open
        if self._sidecar_client is not None:
            self._sidecar_client.close()
            self._sidecar_client = None

        if self.sandbox_name is None:
            return

        try:
            self.custom_api.delete_namespaced_custom_object(
                group="arl.infra.io",
                version="v1alpha1",
                namespace=self.namespace,
                plural="sandboxes",
                name=self.sandbox_name,
            )
        except client.ApiException as e:
            if e.status != 404:
                raise

        self.sandbox_name = None
        self._pod_ip = None

    def execute_stream(
        self,
        command: list[str],
        env: dict[str, str] | None = None,
        working_dir: str = "",
        timeout_seconds: int = 0,
    ) -> Iterator[Any]:
        """Execute a command with streaming output via gRPC.

        This method bypasses the Task CRD and communicates directly with
        the sidecar for real-time output streaming.

        Args:
            command: Command and arguments to execute
            env: Environment variables
            working_dir: Working directory for the command
            timeout_seconds: Command timeout (0 = no timeout)

        Yields:
            ExecLog messages with stdout/stderr as they are received

        Raises:
            RuntimeError: If sandbox is not ready
        """
        yield from self.sidecar_client.execute_stream(
            command=command,
            env=env,
            working_dir=working_dir,
            timeout_seconds=timeout_seconds,
        )

    def execute_direct(
        self,
        command: list[str],
        env: dict[str, str] | None = None,
        working_dir: str = "",
        timeout_seconds: int = 0,
    ) -> Any:
        """Execute a command directly via gRPC (non-streaming).

        This method bypasses the Task CRD and communicates directly with
        the sidecar. Use execute_stream() for real-time output.

        Args:
            command: Command and arguments to execute
            env: Environment variables
            working_dir: Working directory for the command
            timeout_seconds: Command timeout (0 = no timeout)

        Returns:
            ExecLog with aggregated stdout, stderr, and exit code

        Raises:
            RuntimeError: If sandbox is not ready
        """
        return self.sidecar_client.execute(
            command=command,
            env=env,
            working_dir=working_dir,
            timeout_seconds=timeout_seconds,
        )

    def interactive_shell(self) -> Any:
        """Start an interactive shell session via gRPC.

        Returns:
            ShellSession for bidirectional communication

        Raises:
            RuntimeError: If sandbox is not ready

        Examples:
            >>> with session.interactive_shell() as shell:
            ...     for output in shell.run("ls -la"):
            ...         print(output.data, end="")
        """
        return self.sidecar_client.interactive_shell()

    def __enter__(self) -> "SandboxSession":
        """Enter context manager - create sandbox."""
        self._entered = True
        self.create_sandbox()
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_val: BaseException | None,
        exc_tb: object | None,
    ) -> None:
        """Exit context manager - cleanup sandbox unless keep_alive=True."""
        if not self.keep_alive:
            self.delete_sandbox()
