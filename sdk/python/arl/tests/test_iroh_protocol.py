"""Protocol-level tests for the iroh QUIC transport.

These drive :class:`IrohShellSession` and the file-transfer framing against
in-memory stream halves, so the wire format is verified without a live
executor. The frame layouts mirror rust/executor-agent/src/executor/agent.rs.
"""

from __future__ import annotations

import asyncio
import json
import struct

import pytest
from arl.iroh_transport import (
    MSG_EVENT,
    MSG_REQUEST,
    MSG_RESPONSE,
    IrohShellSession,
    IrohTransport,
)
from arl.pb import executor_v2_pb2 as pb


class FakeSend:
    """Collects everything the client writes to the send half."""

    def __init__(self) -> None:
        self.buf = bytearray()
        self.finished = False

    async def write_all(self, data: bytes) -> None:
        self.buf.extend(data)

    async def finish(self) -> None:
        self.finished = True

    def frames(self) -> list[tuple[int, bytes]]:
        """Decode the buffer as a sequence of [type][len][payload] frames."""
        out: list[tuple[int, bytes]] = []
        i = 0
        while i < len(self.buf):
            msg_type = self.buf[i]
            (length,) = struct.unpack(">I", self.buf[i + 1 : i + 5])
            out.append((msg_type, bytes(self.buf[i + 5 : i + 5 + length])))
            i += 5 + length
        return out

    def requests(self) -> list[pb.Request]:
        reqs = []
        for msg_type, payload in self.frames():
            assert msg_type == MSG_REQUEST
            req = pb.Request()
            req.ParseFromString(payload)
            reqs.append(req)
        return reqs


class FakeRecv:
    """Replays a scripted byte stream to the client's recv half."""

    def __init__(self, data: bytes = b"") -> None:
        self.buf = bytearray(data)
        self.pos = 0

    def feed(self, data: bytes) -> None:
        self.buf.extend(data)

    async def read_exact(self, n: int) -> bytes:
        if self.pos + n > len(self.buf):
            raise ConnectionError("stream closed")
        chunk = bytes(self.buf[self.pos : self.pos + n])
        self.pos += n
        return chunk


def typed(msg_type: int, message: object) -> bytes:
    payload = message.SerializeToString()  # type: ignore[attr-defined]
    return struct.pack(">BI", msg_type, len(payload)) + payload


def event_stdout(process_tag: int, data: bytes) -> bytes:
    evt = pb.Event(tag=0)
    evt.stdout.CopyFrom(pb.StdoutEvent(process_tag=process_tag, data=data))
    return typed(MSG_EVENT, evt)


def event_stderr(process_tag: int, data: bytes) -> bytes:
    evt = pb.Event(tag=0)
    evt.stderr.CopyFrom(pb.StderrEvent(process_tag=process_tag, data=data))
    return typed(MSG_EVENT, evt)


def event_exit(process_tag: int, code: int) -> bytes:
    evt = pb.Event(tag=0)
    evt.exit.CopyFrom(pb.ExitEvent(process_tag=process_tag, exit_code=code))
    return typed(MSG_EVENT, evt)


def response_write_in_ack() -> bytes:
    resp = pb.Response(tag=0)
    resp.write_in.CopyFrom(pb.WriteInResponse())
    return typed(MSG_RESPONSE, resp)


def make_session(recv_data: bytes = b"", process_tag: int = 7) -> tuple[
    IrohShellSession, FakeSend, FakeRecv
]:
    transport = IrohTransport.__new__(IrohTransport)  # bypass the iroh import check
    transport._tag_counter = 0  # type: ignore[attr-defined]
    send, recv = FakeSend(), FakeRecv(recv_data)
    return IrohShellSession(transport, send, recv, process_tag), send, recv


def test_send_input_emits_write_in_with_process_tag() -> None:
    session, send, _ = make_session()
    asyncio.run(session.send_input(b"ls -la\n"))

    (req,) = send.requests()
    assert req.HasField("write_in")
    assert req.write_in.process_tag == 7
    assert req.write_in.data == b"ls -la\n"


def test_send_signal_and_resize_target_the_process() -> None:
    session, send, _ = make_session()
    asyncio.run(session.send_signal("SIGINT"))
    asyncio.run(session.resize(40, 120))

    sig, resize = send.requests()
    assert sig.signal.process_tag == 7
    assert sig.signal.signal == "SIGINT"
    assert resize.resize.process_tag == 7
    assert resize.resize.rows == 40
    assert resize.resize.cols == 120


def test_each_request_gets_a_fresh_tag() -> None:
    session, send, _ = make_session()
    asyncio.run(session.send_input(b"a"))
    asyncio.run(session.send_input(b"b"))
    asyncio.run(session.send_signal("SIGTERM"))

    tags = [r.tag for r in send.requests()]
    assert len(set(tags)) == 3, f"tags must be unique, got {tags}"


def test_read_event_maps_stdout_and_stderr_to_output() -> None:
    session, _, _ = make_session(event_stdout(7, b"hello ") + event_stderr(7, b"world"))

    assert asyncio.run(session.read_event()) == ("output", b"hello ", 0)
    assert asyncio.run(session.read_event()) == ("output", b"world", 0)


def test_read_event_skips_request_acknowledgements() -> None:
    # write_in acks share the stream with output; they must not surface.
    stream = response_write_in_ack() + response_write_in_ack() + event_stdout(7, b"out")
    session, _, _ = make_session(stream)

    assert asyncio.run(session.read_event()) == ("output", b"out", 0)


def test_exit_event_closes_the_session() -> None:
    session, _, _ = make_session(event_exit(7, 42))

    assert asyncio.run(session.read_event()) == ("exit", b"", 42)
    assert not session.is_open
    # Further reads report closure instead of hanging on a dead stream.
    assert asyncio.run(session.read_event()) == ("closed", b"", 0)


def test_error_response_surfaces_the_message() -> None:
    resp = pb.Response(tag=0)
    resp.error.CopyFrom(pb.ErrorResponse(code=8, message="no such file"))
    session, _, _ = make_session(typed(MSG_RESPONSE, resp))

    kind, data, _ = asyncio.run(session.read_event())
    assert kind == "error"
    assert data == b"no such file"


def test_dropped_stream_reports_closed_not_an_exception() -> None:
    session, _, _ = make_session(b"")

    assert asyncio.run(session.read_event()) == ("closed", b"", 0)
    assert not session.is_open


def test_close_finishes_the_send_half_once() -> None:
    session, send, _ = make_session()
    asyncio.run(session.close())
    assert send.finished
    assert not session.is_open

    asyncio.run(session.close())  # idempotent

    with pytest.raises(RuntimeError, match="closed"):
        asyncio.run(session.send_input(b"x"))


def test_recv_data_frames_reads_until_zero_terminator() -> None:
    payload = b"".join(
        [
            struct.pack(">I", 5),
            b"hello",
            struct.pack(">I", 6),
            b" world",
            struct.pack(">I", 0),
        ]
    )
    recv = FakeRecv(payload)

    chunks = asyncio.run(IrohTransport._recv_data_frames(recv))
    assert b"".join(chunks) == b"hello world"


def test_recv_data_frames_rejects_oversized_frame() -> None:
    recv = FakeRecv(struct.pack(">I", 1 << 30))

    with pytest.raises(ValueError, match="too large"):
        asyncio.run(IrohTransport._recv_data_frames(recv))


def test_recv_typed_rejects_oversized_message() -> None:
    recv = FakeRecv(struct.pack(">BI", MSG_RESPONSE, 1 << 30))

    with pytest.raises(ValueError, match="too large"):
        asyncio.run(IrohTransport._recv_typed(recv))


class ScriptedTransport:
    """Stands in for IrohTransport so the shell client can be driven offline."""

    def __init__(self, script: bytes) -> None:
        self.script = script
        self.send = FakeSend()
        self.closed = False
        self.opened_with: dict[str, object] = {}

    async def connect(self) -> None:
        return None

    async def open_shell(
        self,
        command: list[str] | None = None,
        *,
        rows: int = 24,
        cols: int = 80,
        env: dict[str, str] | None = None,
        work_dir: str | None = None,
    ) -> IrohShellSession:
        self.opened_with = {"command": command, "rows": rows, "cols": cols}
        transport = IrohTransport.__new__(IrohTransport)
        transport._tag_counter = 0  # type: ignore[attr-defined]
        return IrohShellSession(transport, self.send, FakeRecv(self.script), 3)

    async def close(self) -> None:
        self.closed = True


def make_shell_client(monkeypatch: pytest.MonkeyPatch, script: bytes):
    from arl import interactive_shell_client as mod

    transport = ScriptedTransport(script)
    monkeypatch.setattr(mod, "IrohTransport", lambda _addr: transport)
    client = mod.InteractiveShellClient(iroh_addr="deadbeef", rows=30, cols=100)
    client.connect("sess-1")
    return client, transport


def test_shell_client_spawns_with_requested_window(monkeypatch: pytest.MonkeyPatch) -> None:
    client, transport = make_shell_client(monkeypatch, b"")
    try:
        assert transport.opened_with["rows"] == 30
        assert transport.opened_with["cols"] == 100
        assert client.is_open()
    finally:
        client.close()


def test_shell_client_reads_output_and_exit(monkeypatch: pytest.MonkeyPatch) -> None:
    script = event_stdout(3, b"total 0\n") + event_exit(3, 0)
    client, _ = make_shell_client(monkeypatch, script)
    try:
        assert client.read_output(timeout=2.0) == "total 0\n"
        msg = client.read_message(timeout=2.0)
        assert msg is not None
        assert msg.type == "exit"
        assert msg.exit_code == 0
    finally:
        client.close()


def test_shell_client_input_signal_resize_reach_the_wire(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    client, transport = make_shell_client(monkeypatch, b"")
    try:
        client.send_input("echo hi\n")
        client.send_signal("SIGINT")
        client.send_resize(cols=90, rows=45)
    finally:
        client.close()

    write_in, signal, resize = transport.send.requests()
    assert write_in.write_in.data == b"echo hi\n"
    assert signal.signal.signal == "SIGINT"
    assert resize.resize.rows == 45
    assert resize.resize.cols == 90


def test_shell_client_decodes_utf8_safely(monkeypatch: pytest.MonkeyPatch) -> None:
    # A multi-byte char split across events must not raise.
    client, _ = make_shell_client(monkeypatch, event_stdout(3, b"\xe4\xbd\xa0\xe5"))
    try:
        out = client.read_output(timeout=2.0)
        assert out.startswith("你")
    finally:
        client.close()


def test_shell_client_close_tears_down_transport(monkeypatch: pytest.MonkeyPatch) -> None:
    client, transport = make_shell_client(monkeypatch, b"")
    client.close()

    assert transport.closed
    assert transport.send.finished
    assert not client.is_open()


def test_addr_json_exposes_direct_addresses(monkeypatch: pytest.MonkeyPatch) -> None:
    import arl.iroh_transport as mod

    # __init__ probes for the iroh package; the parsing below is independent.
    monkeypatch.setattr(mod, "__import__", __import__, raising=False)
    t = mod.IrohTransport(
        json.dumps(
            {
                "id": "abc123",
                "relay_url": "http://relay.test:3340",
                "direct_addresses": ["10.0.0.1:9091", "10.0.0.2:9091"],
            }
        )
    )
    assert t._addr_str == "abc123"
    assert t._relay_url == "http://relay.test:3340"
    assert t._direct_addresses == ["10.0.0.1:9091", "10.0.0.2:9091"]


def test_addr_json_without_relay_still_yields_direct(monkeypatch: pytest.MonkeyPatch) -> None:
    import arl.iroh_transport as mod

    # This is what the executor publishes when the relay never connects.
    t = mod.IrohTransport(
        json.dumps({"id": "abc123", "relay_url": None, "direct_addresses": ["10.0.0.1:9091"]})
    )
    assert t._relay_url is None
    assert t._direct_addresses == ["10.0.0.1:9091"]


def test_plain_hex_addr_still_supported(monkeypatch: pytest.MonkeyPatch) -> None:
    import arl.iroh_transport as mod

    t = mod.IrohTransport("deadbeef")
    assert t._addr_str == "deadbeef"
    assert t._relay_url is None
    assert t._direct_addresses == []


def test_malformed_direct_addresses_are_ignored(monkeypatch: pytest.MonkeyPatch) -> None:
    import arl.iroh_transport as mod

    t = mod.IrohTransport(json.dumps({"id": "abc", "direct_addresses": "not-a-list"}))
    assert t._direct_addresses == []
