"""iroh QUIC direct-connect transport for high-performance data-plane operations.

When the ``iroh`` package is installed (``pip install arl-env[direct]``),
:class:`IrohTransport` opens a QUIC connection to an executor agent,
bypassing the gateway HTTP path for file I/O, command execution, and
interactive shell streaming.

Wire protocol -- each QUIC bidi stream carries typed frames::

    [1 byte frame type] [4 bytes big-endian payload length] [JSON payload]

Frame types:

- ``0x01`` -- Request  (client -> executor)
- ``0x02`` -- Response (executor -> client, final)
- ``0x03`` -- Event    (executor -> client, streaming)

The ``addr_str`` parameter accepts an iroh endpoint ID as a hex string.
"""

from __future__ import annotations

import asyncio
import json
import struct
import threading
import uuid
from collections.abc import AsyncIterator, Coroutine
from typing import TypeVar

_T = TypeVar("_T")

ALPN = b"arl/executor/v2"
FRAME_REQUEST = 0x01
FRAME_RESPONSE = 0x02
FRAME_EVENT = 0x03
_MAX_FRAME_SIZE = 64 * 1024 * 1024  # 64 MiB safety cap


class IrohTransport:
    """Async QUIC connection to an executor agent via iroh.

    All public methods are coroutines.  For synchronous usage wrap
    with :class:`SyncIrohBridge`.
    """

    def __init__(self, addr_str: str) -> None:
        try:
            __import__("iroh")
        except ImportError:
            raise ImportError(
                "iroh package required for direct connect: "
                "pip install arl-env[direct]"
            ) from None
        self._addr_str = addr_str
        self._endpoint: object | None = None
        self._conn: object | None = None

    @property
    def is_connected(self) -> bool:
        """True when a QUIC connection is established."""
        return self._conn is not None

    async def connect(self) -> None:
        """Establish the QUIC connection to the remote executor."""
        import iroh
        import iroh.iroh_ffi

        loop = asyncio.get_running_loop()
        # uniffi requires a BaseEventLoop; the running loop satisfies this.
        iroh.iroh_ffi.uniffi_set_event_loop(loop)  # type: ignore[arg-type]

        opts = iroh.EndpointOptions(
            preset=iroh.preset_minimal(),
            alpns=[ALPN],
        )

        endpoint = await iroh.Endpoint.bind(opts)
        self._endpoint = endpoint
        remote_id = iroh.EndpointId.from_string(self._addr_str)
        remote_addr = iroh.EndpointAddr(
            id=remote_id, relay_url=None, addresses=[],
        )
        self._conn = await endpoint.connect(remote_addr, ALPN)

    async def close(self) -> None:
        """Tear down the connection and endpoint."""
        import iroh

        conn = self._conn
        if isinstance(conn, iroh.Connection):
            conn.close(0, b"done")
        self._conn = None
        ep = self._endpoint
        if isinstance(ep, iroh.Endpoint):
            await ep.close()
        self._endpoint = None

    # ---- framing helpers ----

    @staticmethod
    async def _send_frame(send: object, frame_type: int, data: bytes) -> None:
        """Write ``[1B type][4B BE length][payload]`` to a QUIC send stream."""
        import iroh

        header = struct.pack(">BI", frame_type, len(data))
        if isinstance(send, iroh.SendStream):
            await send.write_all(header + data)
        else:
            await send.write_all(header + data)  # type: ignore[attr-defined]

    @staticmethod
    async def _recv_frame(recv: object) -> tuple[int, bytes]:
        """Read one typed frame from a QUIC recv stream."""
        import iroh

        if isinstance(recv, iroh.RecvStream):
            header = await recv.read_exact(5)
        else:
            header = await recv.read_exact(5)  # type: ignore[attr-defined]
        frame_type = header[0]
        length = struct.unpack(">I", header[1:5])[0]
        if length > _MAX_FRAME_SIZE:
            raise ValueError(f"frame exceeds {_MAX_FRAME_SIZE} byte limit: {length}")
        if isinstance(recv, iroh.RecvStream):
            data = await recv.read_exact(length)
        else:
            data = await recv.read_exact(length)  # type: ignore[attr-defined]
        return frame_type, data

    def _require_conn(self) -> object:
        if self._conn is None:
            raise RuntimeError("Not connected. Call connect() first.")
        return self._conn

    # ---- data-plane operations ----

    async def execute(
        self,
        cmd: list[str],
        *,
        env: dict[str, str] | None = None,
        work_dir: str | None = None,
        timeout_seconds: int | None = None,
    ) -> dict[str, object]:
        """Run a command and return ``{stdout, stderr, exit_code}``."""
        import iroh

        conn = self._require_conn()
        if not isinstance(conn, iroh.Connection):
            raise RuntimeError("Invalid connection state")
        bi = await conn.open_bi()
        send_stream = bi.send()
        recv_stream = bi.recv()

        req: dict[str, object] = {
            "id": str(uuid.uuid4()),
            "type": "exec",
            "cmd": cmd,
        }
        if env:
            req["env"] = env
        if work_dir:
            req["workdir"] = work_dir
        if timeout_seconds is not None:
            req["timeout"] = timeout_seconds

        await self._send_frame(send_stream, FRAME_REQUEST, json.dumps(req).encode())
        await send_stream.finish()

        stdout_parts: list[str] = []
        stderr_parts: list[str] = []
        exit_code = 0

        while True:
            try:
                frame_type, data = await self._recv_frame(recv_stream)
            except Exception:
                break
            msg: dict[str, object] = json.loads(data)
            out = msg.get("stdout", "")
            err = msg.get("stderr", "")
            if isinstance(out, str) and out:
                stdout_parts.append(out)
            if isinstance(err, str) and err:
                stderr_parts.append(err)
            if frame_type == FRAME_RESPONSE:
                ec = msg.get("exit_code", 0)
                if isinstance(ec, int):
                    exit_code = ec
                break

        return {
            "stdout": "".join(stdout_parts),
            "stderr": "".join(stderr_parts),
            "exit_code": exit_code,
        }

    async def execute_stream(
        self,
        cmd: list[str],
        *,
        env: dict[str, str] | None = None,
        work_dir: str | None = None,
        timeout_seconds: int | None = None,
    ) -> AsyncIterator[dict[str, object]]:
        """Run a command and yield output events as they arrive."""
        import iroh

        conn = self._require_conn()
        if not isinstance(conn, iroh.Connection):
            raise RuntimeError("Invalid connection state")
        bi = await conn.open_bi()
        send_stream = bi.send()
        recv_stream = bi.recv()

        req: dict[str, object] = {
            "id": str(uuid.uuid4()),
            "type": "exec",
            "cmd": cmd,
        }
        if env:
            req["env"] = env
        if work_dir:
            req["workdir"] = work_dir
        if timeout_seconds is not None:
            req["timeout"] = timeout_seconds

        await self._send_frame(send_stream, FRAME_REQUEST, json.dumps(req).encode())
        await send_stream.finish()

        while True:
            try:
                frame_type, data = await self._recv_frame(recv_stream)
            except Exception:
                break
            msg: dict[str, object] = json.loads(data)
            yield msg
            if frame_type == FRAME_RESPONSE:
                break

    async def upload_file(self, path: str, data: bytes) -> dict[str, object]:
        """Upload a file -- raw bytes on the QUIC stream, no base64."""
        import iroh

        conn = self._require_conn()
        if not isinstance(conn, iroh.Connection):
            raise RuntimeError("Invalid connection state")
        bi = await conn.open_bi()
        send_stream = bi.send()
        recv_stream = bi.recv()

        req_payload = json.dumps(
            {"id": str(uuid.uuid4()), "type": "write", "path": path}
        ).encode()
        await self._send_frame(send_stream, FRAME_REQUEST, req_payload)
        # Raw file bytes follow the request frame -- no framing overhead.
        await send_stream.write_all(data)
        await send_stream.finish()

        _, resp_data = await self._recv_frame(recv_stream)
        resp: dict[str, object] = json.loads(resp_data)
        return resp

    async def download_file(self, path: str) -> bytes:
        """Download a file -- raw bytes from the QUIC stream."""
        import iroh

        conn = self._require_conn()
        if not isinstance(conn, iroh.Connection):
            raise RuntimeError("Invalid connection state")
        bi = await conn.open_bi()
        send_stream = bi.send()
        recv_stream = bi.recv()

        req_payload = json.dumps(
            {"id": str(uuid.uuid4()), "type": "read", "path": path}
        ).encode()
        await self._send_frame(send_stream, FRAME_REQUEST, req_payload)
        await send_stream.finish()

        chunks: list[bytes] = []
        while True:
            try:
                chunk = await recv_stream.read(65536)
                if not chunk:
                    break
                chunks.append(chunk)
            except Exception:
                break
        return b"".join(chunks)

    async def open_shell(self) -> tuple[object, object]:
        """Open a bidi stream for an interactive shell session.

        Returns ``(send_stream, recv_stream)`` for the caller to drive.
        The initial shell request frame is sent before returning.
        """
        import iroh

        conn = self._require_conn()
        if not isinstance(conn, iroh.Connection):
            raise RuntimeError("Invalid connection state")
        bi = await conn.open_bi()
        send_stream = bi.send()
        recv_stream = bi.recv()

        req_payload = json.dumps(
            {"id": str(uuid.uuid4()), "type": "shell"}
        ).encode()
        await self._send_frame(send_stream, FRAME_REQUEST, req_payload)

        return send_stream, recv_stream


class SyncIrohBridge:
    """Synchronous wrapper that runs :class:`IrohTransport` in a background thread.

    The QUIC connection is established during construction; if it fails
    the background thread is cleaned up and the exception propagates.
    """

    def __init__(self, addr_str: str) -> None:
        self._transport = IrohTransport(addr_str)
        self._loop = asyncio.new_event_loop()
        self._thread = threading.Thread(
            target=self._run_loop,
            daemon=True,
            name="iroh-bridge",
        )
        self._thread.start()
        try:
            self._run_coro(self._transport.connect())
        except Exception:
            self._stop_loop()
            raise

    def _run_loop(self) -> None:
        asyncio.set_event_loop(self._loop)
        self._loop.run_forever()

    def _run_coro(self, coro: Coroutine[object, object, _T]) -> _T:
        fut = asyncio.run_coroutine_threadsafe(coro, self._loop)
        return fut.result()

    def _stop_loop(self) -> None:
        self._loop.call_soon_threadsafe(self._loop.stop)
        self._thread.join(timeout=5)
        self._loop.close()

    @property
    def is_connected(self) -> bool:
        """True when the underlying QUIC connection is established."""
        return self._transport.is_connected

    def execute(
        self,
        cmd: list[str],
        *,
        env: dict[str, str] | None = None,
        work_dir: str | None = None,
        timeout_seconds: int | None = None,
    ) -> dict[str, object]:
        """Run a command synchronously via QUIC."""
        return self._run_coro(
            self._transport.execute(
                cmd,
                env=env,
                work_dir=work_dir,
                timeout_seconds=timeout_seconds,
            )
        )

    def upload_file(self, path: str, data: bytes) -> dict[str, object]:
        """Upload a file synchronously via QUIC."""
        return self._run_coro(self._transport.upload_file(path, data))

    def download_file(self, path: str) -> bytes:
        """Download a file synchronously via QUIC."""
        return self._run_coro(self._transport.download_file(path))

    def close(self) -> None:
        """Close the connection and stop the background thread."""
        try:
            self._run_coro(self._transport.close())
        except Exception:
            pass
        finally:
            self._stop_loop()

    def __enter__(self) -> SyncIrohBridge:
        return self

    def __exit__(self, *_: object) -> None:
        self.close()
