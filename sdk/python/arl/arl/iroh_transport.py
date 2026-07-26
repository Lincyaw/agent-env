"""iroh QUIC direct-connect transport for high-performance data-plane operations.

When ``iroh`` and ``protobuf`` are installed (``pip install arl-env[direct]``),
:class:`IrohTransport` opens a QUIC connection to an executor agent via iroh,
bypassing the gateway HTTP path for command execution, file I/O, and
interactive shell streaming.

Wire protocol -- typed framing on each QUIC bidi stream::

    [1 byte msg type] [4 bytes big-endian length] [protobuf payload]

Message types:

- ``0x01`` -- Request  (client -> executor, protobuf ``Request``)
- ``0x02`` -- Response (executor -> client, protobuf ``Response``)
- ``0x03`` -- Event    (executor -> client, protobuf ``Event``)

The ``addr_str`` accepts an iroh EndpointId hex string. With ``preset_n0``,
iroh resolves the full address via DNS/PKARR automatically.
"""

from __future__ import annotations

import asyncio
import struct
from contextlib import suppress
from typing import TypeVar

from arl._base import LoopThread

_T = TypeVar("_T")

ALPN = b"arl/executor/v2"
MSG_REQUEST = 0x01
MSG_RESPONSE = 0x02
MSG_EVENT = 0x03
_MAX_MSG_SIZE = 64 * 1024 * 1024


class IrohTransport:
    """Async QUIC connection to an executor agent via iroh."""

    def __init__(self, addr_str: str, token: str | None = None) -> None:
        try:
            __import__("iroh")
        except ImportError:
            raise ImportError(
                "iroh package required for direct connect: pip install arl-env[direct]"
            ) from None
        # Parse addr: either the JSON the executor publishes
        # ({"id", "relay_url", "direct_addresses"}) or a plain hex EndpointId.
        raw = addr_str.strip()
        self._relay_url: str | None = None
        self._direct_addresses: list[str] = []
        try:
            import json as _json

            parsed = _json.loads(raw)
            if isinstance(parsed, dict):
                self._addr_str = parsed.get("id", raw)
                self._relay_url = parsed.get("relay_url")
                direct = parsed.get("direct_addresses") or []
                if isinstance(direct, list):
                    self._direct_addresses = [str(a) for a in direct if a]
            else:
                self._addr_str = raw
        except (ValueError, TypeError):
            self._addr_str = raw
        self._token = token
        self._endpoint: object | None = None
        self._conn: object | None = None
        self._tag_counter = 0
        self._tunnel_servers: dict[int, asyncio.AbstractServer] = {}
        self._tunnel_tasks: dict[int, asyncio.Task[None]] = {}

    def _next_tag(self) -> int:
        self._tag_counter += 1
        return self._tag_counter

    @property
    def is_connected(self) -> bool:
        return self._conn is not None

    async def connect(self) -> None:
        import iroh
        import iroh.iroh_ffi

        loop = asyncio.get_running_loop()
        iroh.iroh_ffi.uniffi_set_event_loop(loop)  # type: ignore[arg-type]

        opts = iroh.EndpointOptions(
            preset=iroh.preset_n0(),
            alpns=[ALPN],
        )
        endpoint = await iroh.Endpoint.bind(opts)
        self._endpoint = endpoint

        remote_id = iroh.EndpointId.from_string(self._addr_str)
        # Pass the executor's direct addresses so a client on the same network
        # dials the sandbox straight away. Without them iroh can only reach the
        # peer through a relay, which in-cluster is both slower and blocked by
        # the sandbox egress policy. EndpointAddr takes the relay URL as a
        # plain string.
        remote_addr = iroh.EndpointAddr(
            id=remote_id,
            relay_url=self._relay_url or None,
            addresses=list(self._direct_addresses),
        )
        self._conn = await endpoint.connect(remote_addr, ALPN)

    async def close(self) -> None:
        import iroh

        conn = self._conn
        if isinstance(conn, iroh.Connection):
            conn.close(0, b"done")
        self._conn = None
        ep = self._endpoint
        if isinstance(ep, iroh.Endpoint):
            await ep.close()
        self._endpoint = None

    # ---- wire helpers ----

    @staticmethod
    async def _send_typed(send: object, msg_type: int, data: bytes) -> None:
        header = struct.pack(">BI", msg_type, len(data))
        await send.write_all(header + data)  # type: ignore[attr-defined]

    @staticmethod
    async def _recv_typed(recv: object) -> tuple[int, bytes]:
        header = await recv.read_exact(5)  # type: ignore[attr-defined]
        msg_type = header[0]
        length = struct.unpack(">I", header[1:5])[0]
        if length > _MAX_MSG_SIZE:
            raise ValueError(f"message too large: {length}")
        data = await recv.read_exact(length)  # type: ignore[attr-defined]
        return msg_type, data

    def _require_conn(self) -> object:
        if self._conn is None:
            raise RuntimeError("Not connected")
        return self._conn

    async def _open_stream(self) -> tuple[object, object]:
        """Open a bidi stream and authenticate it.

        The executor treats every QUIC stream as its own session and refuses
        anything but Auth until the sandbox's token is presented.
        """
        import iroh

        from .pb import executor_v2_pb2 as pb

        conn = self._require_conn()
        if not isinstance(conn, iroh.Connection):
            raise RuntimeError("Invalid connection")

        bi = await conn.open_bi()
        send, recv = bi.send(), bi.recv()

        if self._token:
            req = pb.Request(tag=self._next_tag())
            req.auth.CopyFrom(pb.AuthRequest(token=self._token))
            await self._send_typed(send, MSG_REQUEST, req.SerializeToString())
            _, data = await self._recv_typed(recv)
            resp = pb.Response()
            resp.ParseFromString(data)
            if resp.HasField("error"):
                raise RuntimeError(f"executor rejected authentication: {resp.error.message}")
        return send, recv

    # ---- data-plane operations ----

    async def execute(
        self,
        cmd: list[str],
        *,
        env: dict[str, str] | None = None,
        work_dir: str | None = None,
        timeout_seconds: int | None = None,
    ) -> dict[str, object]:
        """Run a command, collect stdout/stderr/exit_code."""

        from .pb import executor_v2_pb2 as pb

        send, recv = await self._open_stream()

        tag = self._next_tag()
        req = pb.Request(tag=tag)
        spawn = pb.SpawnRequest(command=cmd, stdin=False)
        if env:
            for k, v in env.items():
                spawn.env[k] = v
        if work_dir:
            spawn.working_dir = work_dir
        if timeout_seconds:
            spawn.timeout_seconds = timeout_seconds
        req.spawn.CopyFrom(spawn)

        await self._send_typed(send, MSG_REQUEST, req.SerializeToString())

        stdout_parts: list[bytes] = []
        stderr_parts: list[bytes] = []
        exit_code: int | None = None

        try:
            while True:
                try:
                    msg_type, data = await self._recv_typed(recv)
                except Exception as exc:
                    if exit_code is None:
                        raise RuntimeError("QUIC stream dropped before exit event") from exc
                    break

                if msg_type == MSG_RESPONSE:
                    resp = pb.Response()
                    resp.ParseFromString(data)
                    if resp.HasField("error"):
                        raise RuntimeError(f"executor error: {resp.error.message}")
                    if resp.HasField("spawn"):
                        continue
                    break
                elif msg_type == MSG_EVENT:
                    evt = pb.Event()
                    evt.ParseFromString(data)
                    if evt.HasField("stdout"):
                        stdout_parts.append(evt.stdout.data)
                    elif evt.HasField("stderr"):
                        stderr_parts.append(evt.stderr.data)
                    elif evt.HasField("exit"):
                        exit_code = evt.exit.exit_code
                        break
        finally:
            await send.finish()  # type: ignore[attr-defined]

        return {
            "stdout": b"".join(stdout_parts).decode("utf-8", errors="replace"),
            "stderr": b"".join(stderr_parts).decode("utf-8", errors="replace"),
            "exit_code": exit_code if exit_code is not None else -1,
        }

    async def upload_file(self, path: str, data: bytes) -> dict[str, object]:
        """Upload file via protobuf write request + raw data frames."""

        from .pb import executor_v2_pb2 as pb

        send, recv = await self._open_stream()

        tag = self._next_tag()
        req = pb.Request(tag=tag)
        req.write.CopyFrom(pb.WriteRequest(path=path))
        await self._send_typed(send, MSG_REQUEST, req.SerializeToString())

        # Send raw data as length-prefixed frames: [4B len][bytes]...[4B zero]
        chunk_size = 1024 * 1024
        offset = 0
        while offset < len(data):
            chunk = data[offset : offset + chunk_size]
            await send.write_all(struct.pack(">I", len(chunk)) + chunk)  # type: ignore[attr-defined]
            offset += len(chunk)
        await send.write_all(struct.pack(">I", 0))  # type: ignore[attr-defined]
        await send.finish()  # type: ignore[attr-defined]

        _, resp_data = await self._recv_typed(recv)
        resp = pb.Response()
        resp.ParseFromString(resp_data)
        if resp.HasField("error"):
            raise RuntimeError(f"write error: {resp.error.message}")
        if resp.HasField("write"):
            return {
                "bytes_written": resp.write.bytes_written,
                "sha256": resp.write.sha256,
            }
        return {}

    async def download_file(self, path: str) -> bytes:
        """Download file via protobuf read request + raw data frames."""

        from .pb import executor_v2_pb2 as pb

        send, recv = await self._open_stream()

        tag = self._next_tag()
        req = pb.Request(tag=tag)
        req.read.CopyFrom(pb.ReadRequest(path=path))
        await self._send_typed(send, MSG_REQUEST, req.SerializeToString())
        await send.finish()  # type: ignore[attr-defined]

        # The executor answers with a typed ReadResponse carrying only the
        # size and digest, then streams the bytes as raw length-prefixed
        # frames terminated by a zero-length frame.
        _, data = await self._recv_typed(recv)
        resp = pb.Response()
        resp.ParseFromString(data)
        if resp.HasField("error"):
            raise RuntimeError(f"read error: {resp.error.message}")
        if not resp.HasField("read"):
            raise RuntimeError("unexpected response to read request")

        expected = resp.read.size_bytes
        chunks = await self._recv_data_frames(recv)
        body = b"".join(chunks)
        if len(body) != expected:
            raise RuntimeError(
                f"file download incomplete: got {len(body)} bytes, expected {expected}"
            )
        return body

    @staticmethod
    async def _recv_data_frames(recv: object) -> list[bytes]:
        """Read ``[4B length][payload]`` frames until the zero-length terminator."""
        chunks: list[bytes] = []
        while True:
            header = await recv.read_exact(4)  # type: ignore[attr-defined]
            (length,) = struct.unpack(">I", header)
            if length == 0:
                return chunks
            if length > _MAX_MSG_SIZE:
                raise ValueError(f"data frame too large: {length}")
            chunks.append(bytes(await recv.read_exact(length)))  # type: ignore[attr-defined]

    async def ping(self) -> bool:
        """Send a ping, return True if pong received."""

        from .pb import executor_v2_pb2 as pb

        send, recv = await self._open_stream()

        tag = self._next_tag()
        req = pb.Request(tag=tag)
        req.ping.CopyFrom(pb.PingRequest())
        await self._send_typed(send, MSG_REQUEST, req.SerializeToString())
        await send.finish()  # type: ignore[attr-defined]

        _, data = await self._recv_typed(recv)
        resp = pb.Response()
        resp.ParseFromString(data)
        return bool(resp.HasField("ping"))

    # ---- interactive shell ----

    async def open_shell(
        self,
        command: list[str] | None = None,
        *,
        rows: int = 24,
        cols: int = 80,
        env: dict[str, str] | None = None,
        work_dir: str | None = None,
    ) -> IrohShellSession:
        """Spawn a PTY-backed process and return a live shell session.

        The session owns one QUIC bidi stream: requests (input, signal,
        resize) go out on the send half, stdout/stderr/exit events arrive
        on the recv half.
        """

        from .pb import executor_v2_pb2 as pb

        send, recv = await self._open_stream()

        tag = self._next_tag()
        req = pb.Request(tag=tag)
        spawn = pb.SpawnRequest(
            command=command or ["/bin/bash", "-i"],
            pty=True,
            stdin=True,
            rows=rows,
            cols=cols,
        )
        if env:
            for k, v in env.items():
                spawn.env[k] = v
        if work_dir:
            spawn.working_dir = work_dir
        req.spawn.CopyFrom(spawn)
        await self._send_typed(send, MSG_REQUEST, req.SerializeToString())

        # The spawn ack carries the process_tag every later request needs.
        msg_type, data = await self._recv_typed(recv)
        if msg_type != MSG_RESPONSE:
            raise RuntimeError("expected a spawn response before shell output")
        resp = pb.Response()
        resp.ParseFromString(data)
        if resp.HasField("error"):
            raise RuntimeError(f"shell spawn error: {resp.error.message}")
        if not resp.HasField("spawn"):
            raise RuntimeError("unexpected response to shell spawn request")

        return IrohShellSession(self, send, recv, resp.spawn.process_tag)

    # ---- tunnel operations ----

    async def tunnel_open(self, remote_host: str, remote_port: int) -> int:
        """Register a tunnel target, return the tunnel tag for data streams."""

        from .pb import executor_v2_pb2 as pb

        send, recv = await self._open_stream()

        tag = self._next_tag()
        req = pb.Request(tag=tag)
        req.tunnel.CopyFrom(pb.TunnelRequest(host=remote_host, port=remote_port))
        await self._send_typed(send, MSG_REQUEST, req.SerializeToString())
        await send.finish()  # type: ignore[attr-defined]

        _, data = await self._recv_typed(recv)
        resp = pb.Response()
        resp.ParseFromString(data)
        if resp.HasField("error"):
            raise RuntimeError(f"tunnel error: {resp.error.message}")
        return tag

    async def tunnel_close(self, tunnel_tag: int) -> None:
        """Close a registered tunnel."""

        from .pb import executor_v2_pb2 as pb

        send, recv = await self._open_stream()

        tag = self._next_tag()
        req = pb.Request(tag=tag)
        req.close_tunnel.CopyFrom(pb.CloseTunnelRequest(tunnel_tag=tunnel_tag))
        await self._send_typed(send, MSG_REQUEST, req.SerializeToString())
        await send.finish()  # type: ignore[attr-defined]

        _, data = await self._recv_typed(recv)
        resp = pb.Response()
        resp.ParseFromString(data)
        if resp.HasField("error"):
            raise RuntimeError(f"close_tunnel error: {resp.error.message}")

    async def tunnel_list(self) -> list[dict[str, object]]:
        """List active tunnels."""

        from .pb import executor_v2_pb2 as pb

        send, recv = await self._open_stream()

        tag = self._next_tag()
        req = pb.Request(tag=tag)
        req.list_tunnels.CopyFrom(pb.ListTunnelsRequest())
        await self._send_typed(send, MSG_REQUEST, req.SerializeToString())
        await send.finish()  # type: ignore[attr-defined]

        _, data = await self._recv_typed(recv)
        resp = pb.Response()
        resp.ParseFromString(data)
        if resp.HasField("error"):
            raise RuntimeError(f"list_tunnels error: {resp.error.message}")
        return [{"tag": t.tag, "host": t.host, "port": t.port} for t in resp.list_tunnels.tunnels]

    async def tunnel_forward(
        self,
        remote_host: str,
        remote_port: int,
        local_port: int,
        local_host: str = "127.0.0.1",
    ) -> int:
        """Forward local_port -> remote_host:remote_port through QUIC tunnel.

        Returns the tunnel tag. The local TCP listener runs until the tunnel
        is closed or the connection drops.
        """
        tunnel_tag = await self.tunnel_open(remote_host, remote_port)

        server = await asyncio.start_server(
            lambda r, w: self._handle_tunnel_conn(tunnel_tag, r, w),
            local_host,
            local_port,
        )
        self._tunnel_servers[tunnel_tag] = server

        task = asyncio.get_running_loop().create_task(self._serve_tunnel(tunnel_tag, server))
        self._tunnel_tasks[tunnel_tag] = task
        return tunnel_tag

    async def _serve_tunnel(self, tag: int, server: asyncio.AbstractServer) -> None:
        try:
            async with server:
                await server.serve_forever()
        except asyncio.CancelledError:
            pass

    async def _handle_tunnel_conn(
        self,
        tunnel_tag: int,
        reader: asyncio.StreamReader,
        writer: asyncio.StreamWriter,
    ) -> None:
        import iroh

        conn = self._conn
        if not isinstance(conn, iroh.Connection):
            writer.close()
            return

        try:
            bi = await conn.open_bi()
            send = bi.send()
            recv = bi.recv()

            hdr = struct.pack(">BI", STREAM_TYPE_TUNNEL, tunnel_tag)
            await send.write_all(hdr)  # type: ignore[arg-type]

            await asyncio.gather(
                self._fwd_local_to_quic(reader, send),
                self._fwd_quic_to_local(recv, writer),
                return_exceptions=True,
            )
        except Exception:
            pass
        finally:
            writer.close()
            with suppress(Exception):
                await writer.wait_closed()

    async def _fwd_local_to_quic(self, reader: asyncio.StreamReader, send: object) -> None:
        while True:
            data = await reader.read(8192)
            if not data:
                break
            await send.write_all(data)  # type: ignore[attr-defined]
        await send.finish()  # type: ignore[attr-defined]

    async def _fwd_quic_to_local(self, recv: object, writer: asyncio.StreamWriter) -> None:
        buf = bytearray(8192)
        while True:
            try:
                n = await recv.read(buf)  # type: ignore[attr-defined]
                if n is None or n == 0:
                    break
                writer.write(bytes(buf[:n]))
                await writer.drain()
            except Exception:
                break

    async def tunnel_stop(self, tunnel_tag: int) -> None:
        """Stop forwarding and close the tunnel."""
        task = self._tunnel_tasks.pop(tunnel_tag, None)
        if task is not None:
            task.cancel()
        server = self._tunnel_servers.pop(tunnel_tag, None)
        if server is not None:
            server.close()
        try:
            await self.tunnel_close(tunnel_tag)
        except Exception:
            pass


STREAM_TYPE_TUNNEL = 0x05


class IrohShellSession:
    """One PTY-backed process on a dedicated QUIC bidi stream."""

    def __init__(
        self,
        transport: IrohTransport,
        send: object,
        recv: object,
        process_tag: int,
    ) -> None:
        self._transport = transport
        self._send = send
        self._recv = recv
        self._process_tag = process_tag
        self._closed = False

    @property
    def process_tag(self) -> int:
        return self._process_tag

    @property
    def is_open(self) -> bool:
        return not self._closed

    async def _request(self, req: object) -> None:
        if self._closed:
            raise RuntimeError("shell session is closed")
        payload = req.SerializeToString()  # type: ignore[attr-defined]
        await IrohTransport._send_typed(self._send, MSG_REQUEST, payload)

    async def send_input(self, data: bytes) -> None:
        """Write bytes to the process stdin."""
        from .pb import executor_v2_pb2 as pb

        req = pb.Request(tag=self._transport._next_tag())
        req.write_in.CopyFrom(pb.WriteInRequest(process_tag=self._process_tag, data=data))
        await self._request(req)

    async def send_signal(self, signal: str) -> None:
        """Deliver a signal (``SIGINT``, ``SIGTERM``, ...) to the process."""
        from .pb import executor_v2_pb2 as pb

        req = pb.Request(tag=self._transport._next_tag())
        req.signal.CopyFrom(pb.SignalRequest(process_tag=self._process_tag, signal=signal))
        await self._request(req)

    async def resize(self, rows: int, cols: int) -> None:
        """Tell the remote PTY its window changed size."""
        from .pb import executor_v2_pb2 as pb

        req = pb.Request(tag=self._transport._next_tag())
        req.resize.CopyFrom(pb.ResizeRequest(process_tag=self._process_tag, rows=rows, cols=cols))
        await self._request(req)

    async def read_event(self) -> tuple[str, bytes, int]:
        """Read the next shell event.

        Returns a ``(kind, data, exit_code)`` triple where kind is one of
        ``output``, ``exit``, ``error``, or ``closed``. Acknowledgements for
        input/signal/resize requests share this stream and are skipped.
        """
        from .pb import executor_v2_pb2 as pb

        while True:
            if self._closed:
                return ("closed", b"", 0)
            try:
                msg_type, data = await IrohTransport._recv_typed(self._recv)
            except Exception:
                self._closed = True
                return ("closed", b"", 0)

            if msg_type == MSG_EVENT:
                evt = pb.Event()
                evt.ParseFromString(data)
                if evt.HasField("stdout"):
                    return ("output", evt.stdout.data, 0)
                if evt.HasField("stderr"):
                    return ("output", evt.stderr.data, 0)
                if evt.HasField("exit"):
                    self._closed = True
                    return ("exit", b"", evt.exit.exit_code)
                continue

            if msg_type == MSG_RESPONSE:
                resp = pb.Response()
                resp.ParseFromString(data)
                if resp.HasField("error"):
                    return ("error", resp.error.message.encode(), 0)
                # write_in / signal / resize acks carry no shell output.
                continue

    async def close(self) -> None:
        """Finish the send half; the executor reaps the process."""
        if self._closed:
            return
        self._closed = True
        with suppress(Exception):
            await self._send.finish()  # type: ignore[attr-defined]


class SyncIrohBridge:
    """Synchronous wrapper running :class:`IrohTransport` in a background thread."""

    def __init__(
        self,
        addr_str: str,
        loop_thread: LoopThread | None = None,
        token: str | None = None,
    ) -> None:
        self._transport = IrohTransport(addr_str, token=token)
        self._owns_loop = loop_thread is None
        self._lt = loop_thread or LoopThread(name="iroh-bridge")
        try:
            self._lt.run(self._transport.connect())
        except Exception:
            if self._owns_loop:
                self._lt.close()
            raise

    @property
    def is_connected(self) -> bool:
        return self._transport.is_connected

    def execute(
        self,
        cmd: list[str],
        *,
        env: dict[str, str] | None = None,
        work_dir: str | None = None,
        timeout_seconds: int | None = None,
    ) -> dict[str, object]:
        return self._lt.run(
            self._transport.execute(
                cmd, env=env, work_dir=work_dir, timeout_seconds=timeout_seconds
            )
        )

    def upload_file(self, path: str, data: bytes) -> dict[str, object]:
        return self._lt.run(self._transport.upload_file(path, data))

    def download_file(self, path: str) -> bytes:
        return self._lt.run(self._transport.download_file(path))

    def ping(self) -> bool:
        return self._lt.run(self._transport.ping())

    def open_shell(
        self,
        command: list[str] | None = None,
        *,
        rows: int = 24,
        cols: int = 80,
        env: dict[str, str] | None = None,
        work_dir: str | None = None,
    ) -> IrohShellSession:
        return self._lt.run(
            self._transport.open_shell(command, rows=rows, cols=cols, env=env, work_dir=work_dir)
        )

    def tunnel_open(self, remote_host: str, remote_port: int) -> int:
        return self._lt.run(self._transport.tunnel_open(remote_host, remote_port))

    def tunnel_close(self, tunnel_tag: int) -> None:
        self._lt.run(self._transport.tunnel_close(tunnel_tag))

    def tunnel_list(self) -> list[dict[str, object]]:
        return self._lt.run(self._transport.tunnel_list())

    def tunnel_forward(
        self,
        remote_host: str,
        remote_port: int,
        local_port: int,
        local_host: str = "127.0.0.1",
    ) -> int:
        return self._lt.run(
            self._transport.tunnel_forward(remote_host, remote_port, local_port, local_host)
        )

    def tunnel_stop(self, tunnel_tag: int) -> None:
        self._lt.run(self._transport.tunnel_stop(tunnel_tag))

    def close(self) -> None:
        try:
            self._lt.run(self._transport.close())
        except Exception:
            pass
        finally:
            if self._owns_loop:
                self._lt.close()

    def __enter__(self) -> SyncIrohBridge:
        return self

    def __exit__(self, *_: object) -> None:
        self.close()
