"""gRPC client for direct sidecar communication."""

import queue
import threading
from collections.abc import Iterator
from typing import Any

import grpc

from arl.pb.proto import agent_pb2, agent_pb2_grpc


class SidecarClient:
    """gRPC client for communicating directly with the sidecar container.

    This client provides low-level access to sidecar functionality including
    streaming execution and interactive shell sessions.

    Examples:
        Basic usage:

        >>> client = SidecarClient("pod-ip:9090")
        >>> response = client.update_files("/workspace", {"test.py": "print('hello')"})
        >>> print(response.success)

        Streaming execution:

        >>> for log in client.execute_stream(["python", "test.py"]):
        ...     print(log.stdout, end="")

        Interactive shell:

        >>> with client.interactive_shell() as shell:
        ...     for output in shell.run("echo hello"):
        ...         print(output.data, end="")
    """

    def __init__(self, address: str, timeout: float = 30.0) -> None:
        """Initialize the sidecar client.

        Args:
            address: Sidecar gRPC address in format "host:port" (e.g., "10.0.0.1:9090")
            timeout: Default timeout in seconds for RPC calls
        """
        self.address = address
        self.timeout = timeout
        self._channel: grpc.Channel | None = None
        self._stub: agent_pb2_grpc.AgentServiceStub | None = None

    @property
    def channel(self) -> grpc.Channel:
        """Get or create the gRPC channel (lazy initialization)."""
        if self._channel is None:
            self._channel = grpc.insecure_channel(self.address)
        return self._channel

    @property
    def stub(self) -> agent_pb2_grpc.AgentServiceStub:
        """Get the gRPC stub."""
        if self._stub is None:
            self._stub = agent_pb2_grpc.AgentServiceStub(self.channel)
        return self._stub

    def close(self) -> None:
        """Close the gRPC channel."""
        if self._channel is not None:
            self._channel.close()
            self._channel = None
            self._stub = None

    def __enter__(self) -> "SidecarClient":
        """Enter context manager."""
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_val: BaseException | None,
        exc_tb: Any,
    ) -> None:
        """Exit context manager - close channel."""
        self.close()

    def update_files(
        self,
        base_path: str,
        files: dict[str, str],
        patch: str = "",
    ) -> agent_pb2.FileResponse:
        """Update files in the sidecar workspace.

        Args:
            base_path: Base directory path for file operations
            files: Dictionary of relative path -> content
            patch: Optional unified diff patch to apply

        Returns:
            FileResponse with success status and message
        """
        request = agent_pb2.FileRequest(
            base_path=base_path,
            files=files,
            patch=patch,
        )
        return self.stub.UpdateFiles(request, timeout=self.timeout)

    def execute(
        self,
        command: list[str],
        env: dict[str, str] | None = None,
        working_dir: str = "",
        background: bool = False,
        timeout_seconds: int = 0,
    ) -> agent_pb2.ExecLog:
        """Execute a command and wait for completion.

        This is a convenience method that aggregates streaming output.
        For real-time output, use execute_stream() instead.

        Args:
            command: Command and arguments to execute
            env: Environment variables
            working_dir: Working directory for the command
            background: Run in background (service mode)
            timeout_seconds: Command timeout (0 = no timeout)

        Returns:
            ExecLog with aggregated stdout, stderr, and exit code
        """
        stdout_parts: list[str] = []
        stderr_parts: list[str] = []
        exit_code = 0

        for log in self.execute_stream(
            command, env, working_dir, background, timeout_seconds
        ):
            if log.stdout:
                stdout_parts.append(log.stdout)
            if log.stderr:
                stderr_parts.append(log.stderr)
            if log.done:
                exit_code = log.exit_code

        # Create aggregated response
        result = agent_pb2.ExecLog()
        result.stdout = "".join(stdout_parts)
        result.stderr = "".join(stderr_parts)
        result.exit_code = exit_code
        result.done = True
        return result

    def execute_stream(
        self,
        command: list[str],
        env: dict[str, str] | None = None,
        working_dir: str = "",
        background: bool = False,
        timeout_seconds: int = 0,
    ) -> Iterator[agent_pb2.ExecLog]:
        """Execute a command with streaming output.

        Args:
            command: Command and arguments to execute
            env: Environment variables
            working_dir: Working directory for the command
            background: Run in background (service mode)
            timeout_seconds: Command timeout (0 = no timeout)

        Yields:
            ExecLog messages as they are received
        """
        request = agent_pb2.ExecRequest(
            command=command,
            env=env or {},
            working_dir=working_dir,
            background=background,
            timeout_seconds=timeout_seconds,
        )
        yield from self.stub.Execute(request)

    def signal_process(self, pid: int, signal: str) -> agent_pb2.SignalResponse:
        """Send a signal to a process.

        Args:
            pid: Process ID
            signal: Signal name (e.g., "SIGTERM", "SIGKILL", "SIGINT")

        Returns:
            SignalResponse with success status and message
        """
        request = agent_pb2.SignalRequest(pid=pid, signal=signal)
        return self.stub.SignalProcess(request, timeout=self.timeout)

    def reset(self, preserve_files: bool = False) -> agent_pb2.ResetResponse:
        """Reset the workspace.

        Args:
            preserve_files: If True, keep files but kill processes

        Returns:
            ResetResponse with success status and message
        """
        request = agent_pb2.ResetRequest(preserve_files=preserve_files)
        return self.stub.Reset(request, timeout=self.timeout)

    def interactive_shell(self) -> "ShellSession":
        """Start an interactive shell session.

        Returns:
            ShellSession for bidirectional communication
        """
        return ShellSession(self.stub)


class ShellSession:
    """Interactive shell session with bidirectional streaming.

    This class manages a bidirectional gRPC stream for interactive
    shell communication.

    Examples:
        >>> with client.interactive_shell() as shell:
        ...     # Send command and read output
        ...     for output in shell.run("ls -la"):
        ...         print(output.data, end="")
        ...
        ...     # Send Ctrl+C
        ...     shell.send_signal("SIGINT")
    """

    def __init__(self, stub: agent_pb2_grpc.AgentServiceStub) -> None:
        """Initialize the shell session.

        Args:
            stub: The gRPC service stub
        """
        self._stub = stub
        self._request_iterator: _ShellInputIterator | None = None
        self._response_iterator: grpc.Future | None = None

    def __enter__(self) -> "ShellSession":
        """Enter context manager - start the shell."""
        self._request_iterator = _ShellInputIterator()
        self._response_iterator = self._stub.InteractiveShell(
            iter(self._request_iterator)
        )
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_val: BaseException | None,
        exc_tb: Any,
    ) -> None:
        """Exit context manager - close the shell."""
        self.close()

    def send_data(self, data: str) -> None:
        """Send data to the shell's stdin.

        Args:
            data: Data to send (usually includes newline for commands)
        """
        if self._request_iterator is None:
            raise RuntimeError("Shell session not started")
        input_msg = agent_pb2.ShellInput(data=data)
        self._request_iterator.send(input_msg)

    def send_signal(self, signal: str) -> None:
        """Send a signal to the shell process.

        Args:
            signal: Signal name (e.g., "SIGINT" for Ctrl+C)
        """
        if self._request_iterator is None:
            raise RuntimeError("Shell session not started")
        input_msg = agent_pb2.ShellInput(signal=signal)
        self._request_iterator.send(input_msg)

    def resize(self, rows: int, cols: int) -> None:
        """Resize the terminal.

        Args:
            rows: Number of rows
            cols: Number of columns
        """
        if self._request_iterator is None:
            raise RuntimeError("Shell session not started")
        input_msg = agent_pb2.ShellInput(resize=True, rows=rows, cols=cols)
        self._request_iterator.send(input_msg)

    def read_output(self) -> Iterator[agent_pb2.ShellOutput]:
        """Read output from the shell.

        Yields:
            ShellOutput messages as they are received
        """
        if self._response_iterator is None:
            raise RuntimeError("Shell session not started")
        yield from self._response_iterator

    def run(self, command: str) -> Iterator[agent_pb2.ShellOutput]:
        """Run a command and yield output.

        This is a convenience method that sends a command (with newline)
        and yields output until a prompt or completion.

        Args:
            command: Command to run (newline will be appended)

        Yields:
            ShellOutput messages
        """
        self.send_data(command + "\n")
        yield from self.read_output()

    def close(self) -> None:
        """Close the shell session."""
        if self._request_iterator is not None:
            self._request_iterator.close()
            self._request_iterator = None


class _ShellInputIterator:
    """Iterator for sending ShellInput messages using a thread-safe queue."""

    def __init__(self) -> None:
        """Initialize the iterator."""
        self._queue: queue.Queue[agent_pb2.ShellInput | None] = queue.Queue()
        self._closed = False

    def send(self, msg: agent_pb2.ShellInput) -> None:
        """Add a message to the queue."""
        if not self._closed:
            self._queue.put(msg)

    def close(self) -> None:
        """Mark the iterator as closed and unblock waiting consumers."""
        self._closed = True
        self._queue.put(None)  # Sentinel to unblock get()

    def __iter__(self) -> "_ShellInputIterator":
        """Return self as iterator."""
        return self

    def __next__(self) -> agent_pb2.ShellInput:
        """Get the next message (blocks until available)."""
        msg = self._queue.get(timeout=1.0)  # 1 second timeout to check closed status
        if msg is None or self._closed:
            raise StopIteration
        return msg
