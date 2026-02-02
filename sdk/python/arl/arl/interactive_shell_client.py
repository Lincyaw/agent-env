"""Interactive shell client for Python SDK.

This module provides a Python client for interactive shell sessions
that can be integrated with frontends via WebSocket.
"""

import asyncio
from collections.abc import Callable

from kubernetes import client, config
from kubernetes.stream import stream


class InteractiveShellClient:
    """Client for interactive shell sessions with pods.

    This provides a Python interface to execute interactive shells
    in pods, suitable for WebSocket integration.

    Examples:
        >>> client = InteractiveShellClient()
        >>> await client.connect("default", "my-pod", "executor")
        >>> await client.send_input("ls -la\\n")
        >>> output = await client.read_output()
        >>> await client.close()
    """

    def __init__(self):
        """Initialize the interactive shell client."""
        config.load_kube_config()
        self.v1 = client.CoreV1Api()
        self.ws_client = None
        self.namespace = None
        self.pod_name = None
        self.container = None

    def connect_sync(self, namespace: str, pod_name: str, container: str = "executor", command: list[str] | None = None) -> None:
        """Connect to a pod's interactive shell (synchronous).

        Args:
            namespace: Kubernetes namespace
            pod_name: Pod name
            container: Container name (default: executor)
            command: Shell command to run (default: ["/bin/sh"])
        """
        self.namespace = namespace
        self.pod_name = pod_name
        self.container = container

        if command is None:
            command = ["/bin/sh"]

        # Create WebSocket connection to pod
        self.ws_client = stream(
            self.v1.connect_get_namespaced_pod_exec,
            pod_name,
            namespace,
            container=container,
            command=command,
            stderr=True,
            stdin=True,
            stdout=True,
            tty=False,
            _preload_content=False,
        )

    async def connect(self, namespace: str, pod_name: str, container: str = "executor", command: list[str] | None = None) -> None:
        """Connect to a pod's interactive shell (async wrapper).

        Args:
            namespace: Kubernetes namespace
            pod_name: Pod name
            container: Container name (default: executor)
            command: Shell command to run (default: ["/bin/sh"])
        """
        self.connect_sync(namespace, pod_name, container, command)

    async def send_input(self, data: str) -> None:
        """Send input to the shell (async).

        Args:
            data: Input data to send
        """
        self.send_input_sync(data)

    def send_input_sync(self, data: str) -> None:
        """Send input to the shell (synchronous).

        Args:
            data: Input data to send
        """
        if self.ws_client:
            self.ws_client.write_stdin(data)

    async def read_output(self, timeout: float = 0.1) -> str:
        """Read output from the shell (async).

        Args:
            timeout: Read timeout in seconds

        Returns:
            Output data
        """
        return self.read_output_sync(timeout)

    def read_output_sync(self, timeout: float = 0.1) -> str:
        """Read output from the shell (synchronous).

        Args:
            timeout: Read timeout in seconds

        Returns:
            Output data
        """
        if not self.ws_client:
            return ""

        output = ""

        try:
            # IMPORTANT: Must call update() to process WebSocket data
            self.ws_client.update(timeout=timeout)

            # Read stdout
            while self.ws_client.peek_stdout():
                data = self.ws_client.read_stdout()
                if data:
                    output += data
                else:
                    break

            # Read stderr
            while self.ws_client.peek_stderr():
                data = self.ws_client.read_stderr()
                if data:
                    output += data
                else:
                    break
        except Exception:
            # Ignore read errors
            pass

        return output

    async def close(self) -> None:
        """Close the shell connection (async)."""
        self.close_sync()

    def close_sync(self) -> None:
        """Close the shell connection (synchronous)."""
        if self.ws_client:
            self.ws_client.close()
            self.ws_client = None

    def is_open(self) -> bool:
        """Check if the connection is open.

        Returns:
            True if connection is open
        """
        return self.ws_client is not None and self.ws_client.is_open()


async def create_websocket_proxy(
    namespace: str,
    pod_name: str,
    container: str,
    on_output: Callable[[str], None],
    on_error: Callable[[str], None],
    on_close: Callable[[], None],
) -> InteractiveShellClient:
    """Create a WebSocket proxy for interactive shell.

    This is a helper function for integrating with WebSocket servers.

    Args:
        namespace: Kubernetes namespace
        pod_name: Pod name
        container: Container name
        on_output: Callback for output data
        on_error: Callback for errors
        on_close: Callback when connection closes

    Returns:
        InteractiveShellClient instance

    Example:
        >>> async def handle_output(data):
        ...     print(f"Output: {data}")
        >>>
        >>> client = await create_websocket_proxy(
        ...     "default", "my-pod", "executor",
        ...     on_output=handle_output,
        ...     on_error=lambda e: print(f"Error: {e}"),
        ...     on_close=lambda: print("Closed")
        ... )
    """
    client = InteractiveShellClient()

    try:
        await client.connect(namespace, pod_name, container)

        # Start output reading loop
        async def read_loop():
            try:
                while client.is_open():
                    output = await client.read_output()
                    if output:
                        on_output(output)
                    await asyncio.sleep(0.01)
            except Exception as e:
                on_error(str(e))
            finally:
                on_close()

        # Start the read loop in background
        asyncio.create_task(read_loop())

        return client

    except Exception as e:
        on_error(str(e))
        raise
