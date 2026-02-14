"""Interactive shell client for ARL SDK via Gateway WebSocket."""

from __future__ import annotations

import json
from collections.abc import Callable

from arl.types import ShellMessage


class InteractiveShellClient:
    """Client for interactive shell sessions via the Gateway WebSocket API.

    Examples:
        >>> client = InteractiveShellClient(gateway_url="http://localhost:8080")
        >>> client.connect("session-123")
        >>> client.send_input("ls -la\\n")
        >>> output = client.read_output()
        >>> client.close()
    """

    def __init__(self, gateway_url: str = "http://localhost:8080") -> None:
        self._gateway_url = gateway_url.rstrip("/")
        ws_url = self._gateway_url.replace("http://", "ws://").replace("https://", "wss://")
        self._ws_base_url = ws_url
        self._ws: object | None = None
        self._session_id: str | None = None

    def connect(self, session_id: str) -> None:
        """Connect to a session's interactive shell via WebSocket.

        Args:
            session_id: Session ID to connect to.
        """
        try:
            import websockets.sync.client as ws_client
        except ImportError:
            raise ImportError(
                "websockets is required for interactive shell. "
                "Install it with: pip install websockets"
            ) from None

        self._session_id = session_id
        url = f"{self._ws_base_url}/v1/sessions/{session_id}/shell"
        self._ws = ws_client.connect(url)

    def send_input(self, data: str) -> None:
        """Send input to the shell.

        Args:
            data: Input data to send.
        """
        if self._ws is None:
            raise RuntimeError("Not connected. Call connect() first.")
        msg = json.dumps({"type": "input", "data": data})
        self._ws.send(msg)  # type: ignore[attr-defined]

    def send_signal(self, sig: str = "SIGINT") -> None:
        """Send an out-of-band signal to the remote shell process.

        Args:
            sig: Signal name (e.g. ``SIGINT``, ``SIGTERM``, ``SIGKILL``).
        """
        if self._ws is None:
            raise RuntimeError("Not connected. Call connect() first.")
        msg = json.dumps({"type": "signal", "signal": sig})
        self._ws.send(msg)  # type: ignore[attr-defined]

    def send_resize(self, cols: int, rows: int) -> None:
        """Notify the remote PTY of a terminal size change.

        Args:
            cols: Number of columns.
            rows: Number of rows.
        """
        if self._ws is None:
            raise RuntimeError("Not connected. Call connect() first.")
        msg = json.dumps({"type": "resize", "cols": cols, "rows": rows})
        self._ws.send(msg)  # type: ignore[attr-defined]

    def read_message(self, timeout: float = 1.0) -> ShellMessage | None:
        """Read the next WebSocket message as a typed :class:`ShellMessage`.

        Unlike :meth:`read_output` this returns the full message including
        ``exit`` and ``error`` types, giving callers the ability to react to
        shell termination and errors.

        Args:
            timeout: Read timeout in seconds.

        Returns:
            Parsed :class:`ShellMessage`, or ``None`` on timeout / closed.
        """
        if self._ws is None:
            return None

        try:
            raw = self._ws.recv(timeout=timeout)  # type: ignore[attr-defined]
            if isinstance(raw, bytes):
                raw = raw.decode()
            data = json.loads(raw)
            return ShellMessage(**data)
        except TimeoutError:
            return None
        except Exception:
            return None

    def read_output(self, timeout: float = 1.0) -> str:
        """Read output from the shell.

        Args:
            timeout: Read timeout in seconds.

        Returns:
            Output data.
        """
        msg = self.read_message(timeout)
        if msg is not None and msg.type == "output":
            return msg.data
        return ""

    def close(self) -> None:
        """Close the shell connection."""
        if self._ws is not None:
            try:
                self._ws.close()  # type: ignore[attr-defined]
            except Exception:
                pass
            self._ws = None

    def is_open(self) -> bool:
        """Check if the connection is open."""
        return self._ws is not None

    def __enter__(self) -> InteractiveShellClient:
        return self

    def __exit__(self, *_: object) -> None:
        self.close()


def create_websocket_proxy(
    gateway_url: str,
    session_id: str,
    on_error: Callable[[str], None],
) -> InteractiveShellClient:
    """Create a WebSocket proxy for interactive shell.

    Args:
        gateway_url: Gateway base URL.
        session_id: Session ID to connect to.
        on_output: Callback for output data.
        on_error: Callback for errors.
        on_close: Callback when connection closes.

    Returns:
        InteractiveShellClient instance.
    """
    shell_client = InteractiveShellClient(gateway_url=gateway_url)
    try:
        shell_client.connect(session_id)
    except Exception as e:
        on_error(str(e))
        raise
    return shell_client
