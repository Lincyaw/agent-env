"""Sandbox session management for ARL."""

import time
from collections.abc import Callable
from typing import TYPE_CHECKING, cast

from kubernetes import client, config

from arl.types import SandboxResource, TaskResource, TaskStep

if TYPE_CHECKING:
    from arl.interactive_shell_client import InteractiveShellClient

# Type alias for callback functions
TaskCallback = Callable[[TaskResource], None]


class SandboxSession:
    """High-level sandbox session manager with automatic lifecycle management.

    Provides context manager support for automatic resource cleanup and
    simplified task execution against sandboxes. Supports callback functions
    that can be triggered after task completion.

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

        Using callbacks:

        >>> def on_task_complete(result):
        ...     print(f"Task completed: {result['status']['state']}")
        >>>
        >>> session = SandboxSession(pool_ref="python-39", namespace="default")
        >>> session.register_callback("on_complete", on_task_complete)
        >>> with session:
        ...     result = session.execute([...])  # Callback triggered after completion
    """

    def __init__(
        self,
        pool_ref: str,
        namespace: str = "default",
        keep_alive: bool = False,
        timeout: int = 300,
    ) -> None:
        """Initialize sandbox session.

        Args:
            pool_ref: Name of the WarmPool to allocate sandbox from
            namespace: Kubernetes namespace (default: "default")
            keep_alive: If True, sandbox persists after context exit
            timeout: Maximum seconds to wait for operations (default: 300)
        """
        self.pool_ref = pool_ref
        self.namespace = namespace
        self.keep_alive = keep_alive
        self.timeout = timeout

        self.sandbox_name: str | None = None
        self._custom_api: client.CustomObjectsApi | None = None
        self._callbacks: dict[str, list[TaskCallback]] = {}

    def register_callback(self, event: str, callback: TaskCallback) -> None:
        """Register a callback function for a specific event.

        Supported events:
        - "on_task_complete": Triggered after task execution completes
        - "on_task_success": Triggered when task succeeds
        - "on_task_failure": Triggered when task fails

        Args:
            event: Event name to trigger callback on
            callback: Callback function that accepts task result dict

        Example:
            >>> def log_result(result):
            ...     print(f"Task state: {result['status']['state']}")
            >>> session.register_callback("on_task_complete", log_result)
        """
        if event not in self._callbacks:
            self._callbacks[event] = []
        self._callbacks[event].append(callback)

    def unregister_callback(self, event: str, callback: TaskCallback | None = None) -> None:
        """Unregister callback(s) for a specific event.

        Args:
            event: Event name to unregister callbacks from
            callback: Specific callback to remove. If None, removes all callbacks for event.
        """
        if event not in self._callbacks:
            return

        if callback is None:
            self._callbacks[event] = []
        else:
            self._callbacks[event] = [cb for cb in self._callbacks[event] if cb != callback]

    def _trigger_callbacks(self, event: str, result: TaskResource) -> None:
        """Trigger all callbacks registered for an event.

        Args:
            event: Event name
            result: Task result to pass to callbacks
        """
        if event in self._callbacks:
            for callback in self._callbacks[event]:
                try:
                    callback(result)
                except Exception as e:
                    # Log but don't fail on callback errors
                    print(f"Warning: Callback error for {event}: {e}")

    def execute_with_callback(
        self,
        steps: list[TaskStep],
        callback_script: str | None = None,
        trace_id: str | None = None,
    ) -> TaskResource:
        """Execute task steps and optionally run a callback script afterward.

        This is useful for SWE-bench scenarios where you want to run a test
        script after applying patches.

        Args:
            steps: List of task steps to execute
            callback_script: Optional shell script to run after task completes
            trace_id: Optional trace ID for distributed tracing

        Returns:
            Task result including callback execution results if provided

        Example:
            >>> result = session.execute_with_callback(
            ...     steps=[{"name": "apply_patch", "type": "FilePatch", ...}],
            ...     callback_script="/testbed/run_tests.sh"
            ... )
        """
        # Execute main task
        result = self.execute(steps, trace_id)

        # If callback script provided, execute it as a follow-up task
        if callback_script and result.get("status", {}).get("state") == "Succeeded":
            callback_steps: list[TaskStep] = [
                {
                    "name": "callback_script",
                    "type": "Command",
                    "command": ["/bin/bash", callback_script],
                }
            ]
            callback_result = self.execute(callback_steps, trace_id)

            # Merge callback results into main result
            result["callback_result"] = callback_result

        return result

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

    def get_pool_status(self) -> dict[str, int]:
        """Get the current status of the WarmPool.

        Returns:
            Dictionary with pool status:
            - total: Total number of pods in the pool
            - ready_idle: Number of ready idle pods available for allocation
            - allocated: Number of pods currently allocated to sandboxes
            - available: Number of sandboxes that can be created immediately

        Example:
            >>> session = SandboxSession(pool_ref="my-pool", namespace="default")
            >>> status = session.get_pool_status()
            >>> print(f"Available sandboxes: {status['available']}")
            >>> print(f"Total capacity: {status['total']}")
        """
        try:
            # Get WarmPool resource
            pool_obj: object = self.custom_api.get_namespaced_custom_object(
                group="arl.infra.io",
                version="v1alpha1",
                namespace=self.namespace,
                plural="warmpools",
                name=self.pool_ref,
            )

            if not isinstance(pool_obj, dict):
                raise RuntimeError("Invalid pool object returned")

            spec = pool_obj.get("spec", {})
            status = pool_obj.get("status", {})

            if not isinstance(spec, dict):
                spec = {}
            if not isinstance(status, dict):
                status = {}

            total = spec.get("replicas", 0)
            ready_idle = status.get("readyReplicas", 0)
            allocated = status.get("allocatedReplicas", 0)

            if not isinstance(total, int):
                total = 0
            if not isinstance(ready_idle, int):
                ready_idle = 0
            if not isinstance(allocated, int):
                allocated = 0

            return {
                "total": total,
                "ready_idle": ready_idle,
                "allocated": allocated,
                "available": ready_idle,  # Available = ready idle pods
            }

        except client.ApiException as e:
            if e.status == 404:
                raise ValueError(
                    f"WarmPool '{self.pool_ref}' not found in namespace '{self.namespace}'"
                ) from e
            raise RuntimeError(f"Failed to get pool status: {e.reason}") from e

    def create_sandbox(self) -> SandboxResource:
        """Create a new sandbox from the warm pool.

        Returns:
            Sandbox resource dictionary

        Raises:
            RuntimeError: If sandbox creation fails or times out
            ValueError: If WarmPool does not exist
        """
        # First check if the WarmPool exists
        try:
            self.custom_api.get_namespaced_custom_object(
                group="arl.infra.io",
                version="v1alpha1",
                namespace=self.namespace,
                plural="warmpools",
                name=self.pool_ref,
            )
        except client.ApiException as e:
            if e.status == 404:
                # List available pools to help user
                try:
                    pools = self.custom_api.list_namespaced_custom_object(
                        group="arl.infra.io",
                        version="v1alpha1",
                        namespace=self.namespace,
                        plural="warmpools",
                    )
                    available = [p["metadata"]["name"] for p in pools.get("items", [])]
                    pool_list = ", ".join(available) if available else "(none)"
                    raise ValueError(
                        f"WarmPool '{self.pool_ref}' not found in namespace '{self.namespace}'.\n"
                        f"Available pools: {pool_list}\n"
                        f"Create a pool first using WarmPoolManager.create_warmpool()"
                    ) from e
                except client.ApiException:
                    raise ValueError(
                        f"WarmPool '{self.pool_ref}' not found in namespace '{self.namespace}'. "
                        f"Create it first using WarmPoolManager.create_warmpool()"
                    ) from e
            raise RuntimeError(f"Failed to check WarmPool: {e}") from e

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
        try:
            sandbox = self.custom_api.create_namespaced_custom_object(
                group="arl.infra.io",
                version="v1alpha1",
                namespace=self.namespace,
                plural="sandboxes",
                body=sandbox_body,
            )
        except client.ApiException as e:
            raise RuntimeError(f"Failed to create sandbox: {e.reason}") from e

        self.sandbox_name = sandbox_name

        # Wait for sandbox to be ready
        self._wait_for_sandbox_ready()

        return cast(SandboxResource, sandbox)

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
        last_phase: str | None = None
        shown_waiting_msg = False

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

                sandbox_resource = cast(SandboxResource, sandbox_obj)
                status = sandbox_resource.get("status", {})
                phase = status.get("phase")

                # Show progress updates when phase changes
                if phase != last_phase:
                    if phase == "Allocating":
                        print(f"⏳ Allocating sandbox from pool '{self.pool_ref}'...")
                    elif phase == "Pending":
                        if not shown_waiting_msg:
                            print(f"⏳ Waiting for pod from pool '{self.pool_ref}'...")
                            print("   (This may take longer if pool has no available pods)")
                            shown_waiting_msg = True
                    last_phase = phase

                if phase == "Ready":
                    print(f"✓ Sandbox ready: {self.sandbox_name}")
                    return
                elif phase == "Failed":
                    conditions = status.get("conditions", [])
                    msg: str = (
                        conditions[0].get("message", "Unknown error")
                        if conditions
                        else "Unknown error"
                    )
                    raise RuntimeError(
                        f"Sandbox failed to start: {msg}\n"
                        f"Check: kubectl describe sandbox {self.sandbox_name} -n {self.namespace}"
                    )

            except client.ApiException as e:
                if e.status != 404:
                    raise RuntimeError(f"Failed to get sandbox status: {e.reason}") from e

            time.sleep(poll_interval)

        raise RuntimeError(
            f"Sandbox '{self.sandbox_name}' not ready after {self.timeout}s. "
            f"Current phase: {last_phase or 'unknown'}. "
            f"Check: kubectl describe sandbox {self.sandbox_name} -n {self.namespace}"
        )

    def execute(self, steps: list[TaskStep], trace_id: str | None = None) -> TaskResource:
        """Execute task steps in the sandbox.

        Triggers registered callbacks after task completion:
        - "on_task_complete": Always triggered after task finishes
        - "on_task_success": Triggered only if task succeeds
        - "on_task_failure": Triggered only if task fails

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

        task_body = {
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
            spec = task_body["spec"]
            if isinstance(spec, dict):
                spec["traceID"] = trace_id

        # Create task resource
        self.custom_api.create_namespaced_custom_object(
            group="arl.infra.io",
            version="v1alpha1",
            namespace=self.namespace,
            plural="tasks",
            body=task_body,
        )

        # Wait for task completion
        result = self._wait_for_task_completion(task_name)

        # Trigger callbacks based on task state
        self._trigger_callbacks("on_task_complete", result)

        task_state = result.get("status", {}).get("state")
        if task_state == "Succeeded":
            self._trigger_callbacks("on_task_success", result)
        elif task_state == "Failed":
            self._trigger_callbacks("on_task_failure", result)

        return result

    def _wait_for_task_completion(self, task_name: str, poll_interval: float = 0.5) -> TaskResource:
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
        last_state: str | None = None

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

                task_resource = cast(TaskResource, task_obj)
                status = task_resource.get("status", {})
                state = status.get("state")

                # Show progress updates when state changes
                if state != last_state and state:
                    if state == "Running":
                        print(f"⚙️  Task executing: {task_name}")
                    last_state = state

                if state in ("Succeeded", "Failed"):
                    if state == "Failed":
                        stderr = status.get("stderr", "")
                        if stderr:
                            print(f"❌ Task failed: {task_name}")
                            print(f"   Error: {stderr[:200]}")
                    return task_resource

            except client.ApiException as e:
                if e.status != 404:
                    raise RuntimeError(f"Failed to get task status: {e.reason}") from e

            time.sleep(poll_interval)

        raise RuntimeError(
            f"Task '{task_name}' did not complete after {self.timeout}s. "
            f"Current state: {last_state or 'unknown'}. "
            f"Check: kubectl describe task {task_name} -n {self.namespace}"
        )

    def create_interactive_shell(self, container: str = "executor") -> "InteractiveShellClient":
        """Create an interactive shell client for this sandbox.

        Args:
            container: Container name (default: "executor")

        Returns:
            InteractiveShellClient instance ready to use

        Raises:
            RuntimeError: If no sandbox is created or sandbox not ready

        Example:
            >>> session.create_sandbox()
            >>> shell = session.create_interactive_shell()
            >>> # Use shell for interactive commands
        """
        from arl.interactive_shell_client import InteractiveShellClient

        if self.sandbox_name is None:
            raise RuntimeError("No sandbox created. Call create_sandbox() first.")

        # Get pod info
        try:
            sandbox_obj = self.custom_api.get_namespaced_custom_object(
                group="arl.infra.io",
                version="v1alpha1",
                namespace=self.namespace,
                plural="sandboxes",
                name=self.sandbox_name,
            )

            if not isinstance(sandbox_obj, dict):
                raise RuntimeError("Invalid sandbox object returned")

            sandbox_resource = cast(SandboxResource, sandbox_obj)
            status = sandbox_resource.get("status", {})
            pod_name = status.get("podName")

            if not pod_name:
                raise RuntimeError(
                    f"Sandbox '{self.sandbox_name}' is not ready yet. Pod has not been allocated."
                )

            # Create and return shell client
            shell = InteractiveShellClient()
            shell.connect_sync(self.namespace, pod_name, container)
            return shell

        except client.ApiException as e:
            raise RuntimeError(f"Failed to create interactive shell: {e.reason}") from e

    def delete_sandbox(self) -> None:
        """Delete the sandbox resource.

        This method is idempotent and safe to call multiple times.
        """
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

    def __enter__(self) -> "SandboxSession":
        """Enter context manager - create sandbox."""
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
