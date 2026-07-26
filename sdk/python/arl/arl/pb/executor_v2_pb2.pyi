from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Iterable as _Iterable, Mapping as _Mapping, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class AuthRequest(_message.Message):
    __slots__ = ["token"]
    TOKEN_FIELD_NUMBER: _ClassVar[int]
    token: str
    def __init__(self, token: _Optional[str] = ...) -> None: ...

class AuthResponse(_message.Message):
    __slots__ = ["claimed"]
    CLAIMED_FIELD_NUMBER: _ClassVar[int]
    claimed: bool
    def __init__(self, claimed: bool = ...) -> None: ...

class CheckpointDownloadRequest(_message.Message):
    __slots__ = ["single_step", "through"]
    SINGLE_STEP_FIELD_NUMBER: _ClassVar[int]
    THROUGH_FIELD_NUMBER: _ClassVar[int]
    single_step: bool
    through: int
    def __init__(self, through: _Optional[int] = ..., single_step: bool = ...) -> None: ...

class CheckpointDownloadResponse(_message.Message):
    __slots__ = ["size_bytes"]
    SIZE_BYTES_FIELD_NUMBER: _ClassVar[int]
    size_bytes: int
    def __init__(self, size_bytes: _Optional[int] = ...) -> None: ...

class CheckpointListRequest(_message.Message):
    __slots__ = []
    def __init__(self) -> None: ...

class CheckpointListResponse(_message.Message):
    __slots__ = ["steps"]
    STEPS_FIELD_NUMBER: _ClassVar[int]
    steps: _containers.RepeatedScalarFieldContainer[int]
    def __init__(self, steps: _Optional[_Iterable[int]] = ...) -> None: ...

class CloseTunnelRequest(_message.Message):
    __slots__ = ["tunnel_tag"]
    TUNNEL_TAG_FIELD_NUMBER: _ClassVar[int]
    tunnel_tag: int
    def __init__(self, tunnel_tag: _Optional[int] = ...) -> None: ...

class CloseTunnelResponse(_message.Message):
    __slots__ = []
    def __init__(self) -> None: ...

class ErrorResponse(_message.Message):
    __slots__ = ["code", "message"]
    CODE_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    code: int
    message: str
    def __init__(self, code: _Optional[int] = ..., message: _Optional[str] = ...) -> None: ...

class Event(_message.Message):
    __slots__ = ["exit", "fs_change", "stderr", "stdout", "tag"]
    EXIT_FIELD_NUMBER: _ClassVar[int]
    FS_CHANGE_FIELD_NUMBER: _ClassVar[int]
    STDERR_FIELD_NUMBER: _ClassVar[int]
    STDOUT_FIELD_NUMBER: _ClassVar[int]
    TAG_FIELD_NUMBER: _ClassVar[int]
    exit: ExitEvent
    fs_change: FsChangeEvent
    stderr: StderrEvent
    stdout: StdoutEvent
    tag: int
    def __init__(self, tag: _Optional[int] = ..., stdout: _Optional[_Union[StdoutEvent, _Mapping]] = ..., stderr: _Optional[_Union[StderrEvent, _Mapping]] = ..., exit: _Optional[_Union[ExitEvent, _Mapping]] = ..., fs_change: _Optional[_Union[FsChangeEvent, _Mapping]] = ...) -> None: ...

class ExitEvent(_message.Message):
    __slots__ = ["exit_code", "process_tag"]
    EXIT_CODE_FIELD_NUMBER: _ClassVar[int]
    PROCESS_TAG_FIELD_NUMBER: _ClassVar[int]
    exit_code: int
    process_tag: int
    def __init__(self, process_tag: _Optional[int] = ..., exit_code: _Optional[int] = ...) -> None: ...

class FsChangeEvent(_message.Message):
    __slots__ = ["event_type", "path", "watch_id"]
    EVENT_TYPE_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    WATCH_ID_FIELD_NUMBER: _ClassVar[int]
    event_type: str
    path: str
    watch_id: int
    def __init__(self, watch_id: _Optional[int] = ..., path: _Optional[str] = ..., event_type: _Optional[str] = ...) -> None: ...

class ListTunnelsRequest(_message.Message):
    __slots__ = []
    def __init__(self) -> None: ...

class ListTunnelsResponse(_message.Message):
    __slots__ = ["tunnels"]
    TUNNELS_FIELD_NUMBER: _ClassVar[int]
    tunnels: _containers.RepeatedCompositeFieldContainer[TunnelInfo]
    def __init__(self, tunnels: _Optional[_Iterable[_Union[TunnelInfo, _Mapping]]] = ...) -> None: ...

class PingRequest(_message.Message):
    __slots__ = []
    def __init__(self) -> None: ...

class PingResponse(_message.Message):
    __slots__ = ["iroh_addr"]
    IROH_ADDR_FIELD_NUMBER: _ClassVar[int]
    iroh_addr: str
    def __init__(self, iroh_addr: _Optional[str] = ...) -> None: ...

class ReadRequest(_message.Message):
    __slots__ = ["path"]
    PATH_FIELD_NUMBER: _ClassVar[int]
    path: str
    def __init__(self, path: _Optional[str] = ...) -> None: ...

class ReadResponse(_message.Message):
    __slots__ = ["sha256", "size_bytes"]
    SHA256_FIELD_NUMBER: _ClassVar[int]
    SIZE_BYTES_FIELD_NUMBER: _ClassVar[int]
    sha256: str
    size_bytes: int
    def __init__(self, size_bytes: _Optional[int] = ..., sha256: _Optional[str] = ...) -> None: ...

class Request(_message.Message):
    __slots__ = ["auth", "checkpoint_download", "checkpoint_list", "close_tunnel", "list_tunnels", "ping", "read", "resize", "signal", "spawn", "tag", "tunnel", "unwatch", "watch", "write", "write_in"]
    AUTH_FIELD_NUMBER: _ClassVar[int]
    CHECKPOINT_DOWNLOAD_FIELD_NUMBER: _ClassVar[int]
    CHECKPOINT_LIST_FIELD_NUMBER: _ClassVar[int]
    CLOSE_TUNNEL_FIELD_NUMBER: _ClassVar[int]
    LIST_TUNNELS_FIELD_NUMBER: _ClassVar[int]
    PING_FIELD_NUMBER: _ClassVar[int]
    READ_FIELD_NUMBER: _ClassVar[int]
    RESIZE_FIELD_NUMBER: _ClassVar[int]
    SIGNAL_FIELD_NUMBER: _ClassVar[int]
    SPAWN_FIELD_NUMBER: _ClassVar[int]
    TAG_FIELD_NUMBER: _ClassVar[int]
    TUNNEL_FIELD_NUMBER: _ClassVar[int]
    UNWATCH_FIELD_NUMBER: _ClassVar[int]
    WATCH_FIELD_NUMBER: _ClassVar[int]
    WRITE_FIELD_NUMBER: _ClassVar[int]
    WRITE_IN_FIELD_NUMBER: _ClassVar[int]
    auth: AuthRequest
    checkpoint_download: CheckpointDownloadRequest
    checkpoint_list: CheckpointListRequest
    close_tunnel: CloseTunnelRequest
    list_tunnels: ListTunnelsRequest
    ping: PingRequest
    read: ReadRequest
    resize: ResizeRequest
    signal: SignalRequest
    spawn: SpawnRequest
    tag: int
    tunnel: TunnelRequest
    unwatch: UnwatchRequest
    watch: WatchRequest
    write: WriteRequest
    write_in: WriteInRequest
    def __init__(self, tag: _Optional[int] = ..., ping: _Optional[_Union[PingRequest, _Mapping]] = ..., spawn: _Optional[_Union[SpawnRequest, _Mapping]] = ..., write_in: _Optional[_Union[WriteInRequest, _Mapping]] = ..., signal: _Optional[_Union[SignalRequest, _Mapping]] = ..., resize: _Optional[_Union[ResizeRequest, _Mapping]] = ..., read: _Optional[_Union[ReadRequest, _Mapping]] = ..., write: _Optional[_Union[WriteRequest, _Mapping]] = ..., tunnel: _Optional[_Union[TunnelRequest, _Mapping]] = ..., watch: _Optional[_Union[WatchRequest, _Mapping]] = ..., unwatch: _Optional[_Union[UnwatchRequest, _Mapping]] = ..., close_tunnel: _Optional[_Union[CloseTunnelRequest, _Mapping]] = ..., list_tunnels: _Optional[_Union[ListTunnelsRequest, _Mapping]] = ..., checkpoint_download: _Optional[_Union[CheckpointDownloadRequest, _Mapping]] = ..., checkpoint_list: _Optional[_Union[CheckpointListRequest, _Mapping]] = ..., auth: _Optional[_Union[AuthRequest, _Mapping]] = ...) -> None: ...

class ResizeRequest(_message.Message):
    __slots__ = ["cols", "process_tag", "rows"]
    COLS_FIELD_NUMBER: _ClassVar[int]
    PROCESS_TAG_FIELD_NUMBER: _ClassVar[int]
    ROWS_FIELD_NUMBER: _ClassVar[int]
    cols: int
    process_tag: int
    rows: int
    def __init__(self, process_tag: _Optional[int] = ..., rows: _Optional[int] = ..., cols: _Optional[int] = ...) -> None: ...

class ResizeResponse(_message.Message):
    __slots__ = []
    def __init__(self) -> None: ...

class Response(_message.Message):
    __slots__ = ["auth", "checkpoint_download", "checkpoint_list", "close_tunnel", "error", "list_tunnels", "ping", "read", "resize", "signal", "spawn", "tag", "tunnel", "unwatch", "watch", "write", "write_in"]
    AUTH_FIELD_NUMBER: _ClassVar[int]
    CHECKPOINT_DOWNLOAD_FIELD_NUMBER: _ClassVar[int]
    CHECKPOINT_LIST_FIELD_NUMBER: _ClassVar[int]
    CLOSE_TUNNEL_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    LIST_TUNNELS_FIELD_NUMBER: _ClassVar[int]
    PING_FIELD_NUMBER: _ClassVar[int]
    READ_FIELD_NUMBER: _ClassVar[int]
    RESIZE_FIELD_NUMBER: _ClassVar[int]
    SIGNAL_FIELD_NUMBER: _ClassVar[int]
    SPAWN_FIELD_NUMBER: _ClassVar[int]
    TAG_FIELD_NUMBER: _ClassVar[int]
    TUNNEL_FIELD_NUMBER: _ClassVar[int]
    UNWATCH_FIELD_NUMBER: _ClassVar[int]
    WATCH_FIELD_NUMBER: _ClassVar[int]
    WRITE_FIELD_NUMBER: _ClassVar[int]
    WRITE_IN_FIELD_NUMBER: _ClassVar[int]
    auth: AuthResponse
    checkpoint_download: CheckpointDownloadResponse
    checkpoint_list: CheckpointListResponse
    close_tunnel: CloseTunnelResponse
    error: ErrorResponse
    list_tunnels: ListTunnelsResponse
    ping: PingResponse
    read: ReadResponse
    resize: ResizeResponse
    signal: SignalResponse
    spawn: SpawnResponse
    tag: int
    tunnel: TunnelResponse
    unwatch: UnwatchResponse
    watch: WatchResponse
    write: WriteResponse
    write_in: WriteInResponse
    def __init__(self, tag: _Optional[int] = ..., ping: _Optional[_Union[PingResponse, _Mapping]] = ..., spawn: _Optional[_Union[SpawnResponse, _Mapping]] = ..., write_in: _Optional[_Union[WriteInResponse, _Mapping]] = ..., signal: _Optional[_Union[SignalResponse, _Mapping]] = ..., resize: _Optional[_Union[ResizeResponse, _Mapping]] = ..., read: _Optional[_Union[ReadResponse, _Mapping]] = ..., write: _Optional[_Union[WriteResponse, _Mapping]] = ..., tunnel: _Optional[_Union[TunnelResponse, _Mapping]] = ..., watch: _Optional[_Union[WatchResponse, _Mapping]] = ..., unwatch: _Optional[_Union[UnwatchResponse, _Mapping]] = ..., error: _Optional[_Union[ErrorResponse, _Mapping]] = ..., close_tunnel: _Optional[_Union[CloseTunnelResponse, _Mapping]] = ..., list_tunnels: _Optional[_Union[ListTunnelsResponse, _Mapping]] = ..., checkpoint_download: _Optional[_Union[CheckpointDownloadResponse, _Mapping]] = ..., checkpoint_list: _Optional[_Union[CheckpointListResponse, _Mapping]] = ..., auth: _Optional[_Union[AuthResponse, _Mapping]] = ...) -> None: ...

class SignalRequest(_message.Message):
    __slots__ = ["process_tag", "signal"]
    PROCESS_TAG_FIELD_NUMBER: _ClassVar[int]
    SIGNAL_FIELD_NUMBER: _ClassVar[int]
    process_tag: int
    signal: str
    def __init__(self, process_tag: _Optional[int] = ..., signal: _Optional[str] = ...) -> None: ...

class SignalResponse(_message.Message):
    __slots__ = []
    def __init__(self) -> None: ...

class SpawnRequest(_message.Message):
    __slots__ = ["cols", "command", "env", "pty", "rows", "stdin", "timeout_seconds", "working_dir"]
    class EnvEntry(_message.Message):
        __slots__ = ["key", "value"]
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    COLS_FIELD_NUMBER: _ClassVar[int]
    COMMAND_FIELD_NUMBER: _ClassVar[int]
    ENV_FIELD_NUMBER: _ClassVar[int]
    PTY_FIELD_NUMBER: _ClassVar[int]
    ROWS_FIELD_NUMBER: _ClassVar[int]
    STDIN_FIELD_NUMBER: _ClassVar[int]
    TIMEOUT_SECONDS_FIELD_NUMBER: _ClassVar[int]
    WORKING_DIR_FIELD_NUMBER: _ClassVar[int]
    cols: int
    command: _containers.RepeatedScalarFieldContainer[str]
    env: _containers.ScalarMap[str, str]
    pty: bool
    rows: int
    stdin: bool
    timeout_seconds: int
    working_dir: str
    def __init__(self, command: _Optional[_Iterable[str]] = ..., env: _Optional[_Mapping[str, str]] = ..., working_dir: _Optional[str] = ..., timeout_seconds: _Optional[int] = ..., pty: bool = ..., stdin: bool = ..., rows: _Optional[int] = ..., cols: _Optional[int] = ...) -> None: ...

class SpawnResponse(_message.Message):
    __slots__ = ["pid", "process_tag"]
    PID_FIELD_NUMBER: _ClassVar[int]
    PROCESS_TAG_FIELD_NUMBER: _ClassVar[int]
    pid: int
    process_tag: int
    def __init__(self, process_tag: _Optional[int] = ..., pid: _Optional[int] = ...) -> None: ...

class StderrEvent(_message.Message):
    __slots__ = ["data", "process_tag"]
    DATA_FIELD_NUMBER: _ClassVar[int]
    PROCESS_TAG_FIELD_NUMBER: _ClassVar[int]
    data: bytes
    process_tag: int
    def __init__(self, process_tag: _Optional[int] = ..., data: _Optional[bytes] = ...) -> None: ...

class StdoutEvent(_message.Message):
    __slots__ = ["data", "process_tag"]
    DATA_FIELD_NUMBER: _ClassVar[int]
    PROCESS_TAG_FIELD_NUMBER: _ClassVar[int]
    data: bytes
    process_tag: int
    def __init__(self, process_tag: _Optional[int] = ..., data: _Optional[bytes] = ...) -> None: ...

class TunnelInfo(_message.Message):
    __slots__ = ["host", "port", "tag"]
    HOST_FIELD_NUMBER: _ClassVar[int]
    PORT_FIELD_NUMBER: _ClassVar[int]
    TAG_FIELD_NUMBER: _ClassVar[int]
    host: str
    port: int
    tag: int
    def __init__(self, tag: _Optional[int] = ..., host: _Optional[str] = ..., port: _Optional[int] = ...) -> None: ...

class TunnelRequest(_message.Message):
    __slots__ = ["host", "port"]
    HOST_FIELD_NUMBER: _ClassVar[int]
    PORT_FIELD_NUMBER: _ClassVar[int]
    host: str
    port: int
    def __init__(self, host: _Optional[str] = ..., port: _Optional[int] = ...) -> None: ...

class TunnelResponse(_message.Message):
    __slots__ = []
    def __init__(self) -> None: ...

class UnwatchRequest(_message.Message):
    __slots__ = ["watch_id"]
    WATCH_ID_FIELD_NUMBER: _ClassVar[int]
    watch_id: int
    def __init__(self, watch_id: _Optional[int] = ...) -> None: ...

class UnwatchResponse(_message.Message):
    __slots__ = []
    def __init__(self) -> None: ...

class WatchRequest(_message.Message):
    __slots__ = ["event_types", "path", "recursive"]
    EVENT_TYPES_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    RECURSIVE_FIELD_NUMBER: _ClassVar[int]
    event_types: _containers.RepeatedScalarFieldContainer[str]
    path: str
    recursive: bool
    def __init__(self, path: _Optional[str] = ..., recursive: bool = ..., event_types: _Optional[_Iterable[str]] = ...) -> None: ...

class WatchResponse(_message.Message):
    __slots__ = ["watch_id"]
    WATCH_ID_FIELD_NUMBER: _ClassVar[int]
    watch_id: int
    def __init__(self, watch_id: _Optional[int] = ...) -> None: ...

class WriteInRequest(_message.Message):
    __slots__ = ["data", "process_tag"]
    DATA_FIELD_NUMBER: _ClassVar[int]
    PROCESS_TAG_FIELD_NUMBER: _ClassVar[int]
    data: bytes
    process_tag: int
    def __init__(self, process_tag: _Optional[int] = ..., data: _Optional[bytes] = ...) -> None: ...

class WriteInResponse(_message.Message):
    __slots__ = []
    def __init__(self) -> None: ...

class WriteRequest(_message.Message):
    __slots__ = ["expected_sha256", "path", "size_hint"]
    EXPECTED_SHA256_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    SIZE_HINT_FIELD_NUMBER: _ClassVar[int]
    expected_sha256: str
    path: str
    size_hint: int
    def __init__(self, path: _Optional[str] = ..., expected_sha256: _Optional[str] = ..., size_hint: _Optional[int] = ...) -> None: ...

class WriteResponse(_message.Message):
    __slots__ = ["bytes_written", "sha256"]
    BYTES_WRITTEN_FIELD_NUMBER: _ClassVar[int]
    SHA256_FIELD_NUMBER: _ClassVar[int]
    bytes_written: int
    sha256: str
    def __init__(self, bytes_written: _Optional[int] = ..., sha256: _Optional[str] = ...) -> None: ...
