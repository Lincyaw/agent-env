"""Interactive shell client for ARL SDK via Gateway WebSocket or iroh QUIC."""

from __future__ import annotations

import asyncio
import concurrent.futures
import json
import os
import threading
from collections.abc import Callable
from contextlib import suppress

from arl.types import ShellMessage


class InteractiveShellClient:
    """Client for interactive shell sessions via the Gateway WebSocket API,
    or via iroh QUIC when ``iroh_addr`` is provided.

    Examples:
        WebSocket (default):

        >>> client = InteractiveShellClient(gateway_url="http://localhost:8080", api_key="my-key")
        >>> client.connect("session-123")
        >>> client.send_input("ls -la\\n")
        >>> output = client.read_output()
        >>> client.close()

        iroh direct connect:

        >>> client = InteractiveShellClient(iroh_addr="<executor-iroh-addr>")
        >>> client.connect("session-123")
        >>> client.send_input("ls -la\\n")
        >>> output = client.read_output()
        >>> client.close()
    """

    def __init__(
        self,
        gateway_url: str = "http://localhost:8080",
        api_key: str | None = None,
        *,
        iroh_addr: str | None = None,
    ) -> None:
        self._gateway_url = gateway_url.rstrip("/")
        ws_url = self._gateway_url.replace("http://", "ws://").replace("https://", "wss://")
        self._ws_base_url = ws_url
        self._api_key = api_key or os.environ.get("ARL_API_KEY", "")
        self._ws: object | None = None
        self._session_id: str | None = None
        # iroh direct-connect state
        self._iroh_addr = iroh_addr
        self._iroh_loop: asyncio.AbstractEventLoop | None = None
        self._iroh_thread: threading.Thread | None = None
        self._iroh_transport: object | None = None
        self._iroh_send: object | None = None
        self._iroh_recv: object | None = None

    def connect(self, session_id: str) -> None:
        """Connect to a session's interactive shell.

        Uses iroh QUIC when ``iroh_addr`` was provided, otherwise falls
        back to Gateway WebSocket.

        Args:
            session_id: Session ID to connect to.
        """
        self._session_id = session_id
        if self._iroh_addr:
            self._connect_iroh()
            return

        try:
            import websockets.sync.client as ws_client
        except ImportError:
            raise ImportError(
                "websockets is required for interactive shell. "
                "Install it with: pip install websockets"
            ) from None

        url = f"{self._ws_base_url}/v1/sessions/{session_id}/shell"
        headers: dict[str, str] = {}
        if self._api_key:
            headers["Authorization"] = f"Bearer {self._api_key}"
        self._ws = ws_client.connect(url, additional_headers=headers)

    def _connect_iroh(self) -> None:
        """Open an iroh QUIC bidi stream for shell I/O."""
        from arl.iroh_transport import IrohTransport

        loop = asyncio.new_event_loop()
        thread = threading.Thread(
            target=self._run_iroh_loop,
            args=(loop,),
            daemon=True,
            name="iroh-shell",
        )
        thread.start()

        async def _setup() -> tuple[IrohTransport, object, object]:
            transport = IrohTransport(self._iroh_addr or "")
            await transport.connect()
            send, recv = await transport.open_shell()
            return transport, send, recv

        try:
            future = asyncio.run_coroutine_threadsafe(_setup(), loop)
            transport, send, recv = future.result()
        except Exception:
            loop.call_soon_threadsafe(loop.stop)
            thread.join(timeout=5)
            loop.close()
            raise

        self._iroh_loop = loop
        self._iroh_thread = thread
        self._iroh_transport = transport
        self._iroh_send = send
        self._iroh_recv = recv

    @staticmethod
    def _run_iroh_loop(loop: asyncio.AbstractEventLoop) -> None:
        asyncio.set_event_loop(loop)
        loop.run_forever()

    def _iroh_run(self, coro: object) -> object:
        """Schedule a coroutine on the iroh background loop and block."""
        if self._iroh_loop is None:
            raise RuntimeError("Iroh shell not connected")
        future: concurrent.futures.Future[object] = asyncio.run_coroutine_threadsafe(
            coro,  # type: ignore[arg-type]
            self._iroh_loop,
        )
        return future.result()

    def send_input(self, data: str) -> None:
        """Send input to the shell.

        Args:
            data: Input data to send.
        """
        if self._iroh_send is not None:
            from arl.iroh_transport import FRAME_EVENT, IrohTransport

            payload = json.dumps({"type": "input", "data": data}).encode()
            self._iroh_run(IrohTransport._send_frame(self._iroh_send, FRAME_EVENT, payload))
            return
        if self._ws is None:
            raise RuntimeError("Not connected. Call connect() first.")
        msg = json.dumps({"type": "input", "data": data})
        self._ws.send(msg)  # type: ignore[attr-defined]

    def send_signal(self, sig: str = "SIGINT") -> None:
        """Send an out-of-band signal to the remote shell process.

        Args:
            sig: Signal name (e.g. ``SIGINT``, ``SIGTERM``, ``SIGKILL``).
        """
        if self._iroh_send is not None:
            from arl.iroh_transport import FRAME_EVENT, IrohTransport

            payload = json.dumps({"type": "signal", "signal": sig}).encode()
            self._iroh_run(IrohTransport._send_frame(self._iroh_send, FRAME_EVENT, payload))
            return
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
        if self._iroh_send is not None:
            from arl.iroh_transport import FRAME_EVENT, IrohTransport

            payload = json.dumps({"type": "resize", "cols": cols, "rows": rows}).encode()
            self._iroh_run(IrohTransport._send_frame(self._iroh_send, FRAME_EVENT, payload))
            return
        if self._ws is None:
            raise RuntimeError("Not connected. Call connect() first.")
        msg = json.dumps({"type": "resize", "cols": cols, "rows": rows})
        self._ws.send(msg)  # type: ignore[attr-defined]

    def read_message(self, timeout: float = 1.0) -> ShellMessage | None:
        """Read the next message as a typed :class:`ShellMessage`.

        Works for both WebSocket and iroh transports.

        Args:
            timeout: Read timeout in seconds.

        Returns:
            Parsed :class:`ShellMessage`, or ``None`` on timeout / closed.
        """
        if self._iroh_recv is not None:
            return self._read_iroh_message(timeout)

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

    def _read_iroh_message(self, timeout: float) -> ShellMessage | None:
        """Read one shell message from the iroh QUIC stream."""
        from arl.iroh_transport import IrohTransport

        async def _recv() -> ShellMessage | None:
            try:
                _, data = await asyncio.wait_for(
                    IrohTransport._recv_frame(self._iroh_recv),
                    timeout=timeout,
                )
                msg: dict[str, object] = json.loads(data)
                return ShellMessage(**msg)  # type: ignore[arg-type]
            except asyncio.TimeoutError:
                return None
            except Exception:
                return None

        result = self._iroh_run(_recv())
        if isinstance(result, ShellMessage):
            return result
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
        """Close the shell connection (WebSocket or iroh)."""
        # Tear down iroh resources
        if self._iroh_send is not None:
            with suppress(Exception):
                self._iroh_run(
                    self._iroh_send.finish()  # type: ignore[attr-defined]
                )
            self._iroh_send = None
        self._iroh_recv = None
        if self._iroh_transport is not None:
            with suppress(Exception):
                self._iroh_run(
                    self._iroh_transport.close()  # type: ignore[attr-defined]
                )
            self._iroh_transport = None
        if self._iroh_loop is not None:
            self._iroh_loop.call_soon_threadsafe(self._iroh_loop.stop)
            if self._iroh_thread is not None:
                self._iroh_thread.join(timeout=5)
                self._iroh_thread = None
            self._iroh_loop.close()
            self._iroh_loop = None
        # Tear down WebSocket
        if self._ws is not None:
            with suppress(Exception):
                self._ws.close()  # type: ignore[attr-defined]
            self._ws = None

    def is_open(self) -> bool:
        """Check if the connection is open."""
        return self._ws is not None or self._iroh_send is not None

    def __enter__(self) -> InteractiveShellClient:
        return self

    def __exit__(self, *_: object) -> None:
        self.close()


def create_websocket_proxy(
    gateway_url: str,
    session_id: str,
    on_error: Callable[[str], None],
    api_key: str | None = None,
    *,
    iroh_addr: str | None = None,
) -> InteractiveShellClient:
    """Create a shell proxy for interactive sessions.

    When ``iroh_addr`` is provided, uses iroh QUIC instead of WebSocket.

    Args:
        gateway_url: Gateway base URL.
        session_id: Session ID to connect to.
        on_error: Callback for errors.
        api_key: API key for authentication.
        iroh_addr: Optional iroh endpoint address for direct QUIC transport.

    Returns:
        InteractiveShellClient instance.
    """
    shell_client = InteractiveShellClient(
        gateway_url=gateway_url, api_key=api_key, iroh_addr=iroh_addr,
    )
    try:
        shell_client.connect(session_id)
    except Exception as e:
        on_error(str(e))
        raise
    return shell_client
