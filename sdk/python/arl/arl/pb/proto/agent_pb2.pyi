from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class FileRequest(_message.Message):
    __slots__ = ("base_path", "files", "patch")
    class FilesEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    BASE_PATH_FIELD_NUMBER: _ClassVar[int]
    FILES_FIELD_NUMBER: _ClassVar[int]
    PATCH_FIELD_NUMBER: _ClassVar[int]
    base_path: str
    files: _containers.ScalarMap[str, str]
    patch: str
    def __init__(self, base_path: _Optional[str] = ..., files: _Optional[_Mapping[str, str]] = ..., patch: _Optional[str] = ...) -> None: ...

class FileResponse(_message.Message):
    __slots__ = ("success", "message")
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    success: bool
    message: str
    def __init__(self, success: bool = ..., message: _Optional[str] = ...) -> None: ...

class ExecRequest(_message.Message):
    __slots__ = ("command", "env", "working_dir", "background", "timeout_seconds")
    class EnvEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    COMMAND_FIELD_NUMBER: _ClassVar[int]
    ENV_FIELD_NUMBER: _ClassVar[int]
    WORKING_DIR_FIELD_NUMBER: _ClassVar[int]
    BACKGROUND_FIELD_NUMBER: _ClassVar[int]
    TIMEOUT_SECONDS_FIELD_NUMBER: _ClassVar[int]
    command: _containers.RepeatedScalarFieldContainer[str]
    env: _containers.ScalarMap[str, str]
    working_dir: str
    background: bool
    timeout_seconds: int
    def __init__(self, command: _Optional[_Iterable[str]] = ..., env: _Optional[_Mapping[str, str]] = ..., working_dir: _Optional[str] = ..., background: bool = ..., timeout_seconds: _Optional[int] = ...) -> None: ...

class ExecLog(_message.Message):
    __slots__ = ("stdout", "stderr", "exit_code", "done")
    STDOUT_FIELD_NUMBER: _ClassVar[int]
    STDERR_FIELD_NUMBER: _ClassVar[int]
    EXIT_CODE_FIELD_NUMBER: _ClassVar[int]
    DONE_FIELD_NUMBER: _ClassVar[int]
    stdout: str
    stderr: str
    exit_code: int
    done: bool
    def __init__(self, stdout: _Optional[str] = ..., stderr: _Optional[str] = ..., exit_code: _Optional[int] = ..., done: bool = ...) -> None: ...

class SignalRequest(_message.Message):
    __slots__ = ("pid", "signal")
    PID_FIELD_NUMBER: _ClassVar[int]
    SIGNAL_FIELD_NUMBER: _ClassVar[int]
    pid: int
    signal: str
    def __init__(self, pid: _Optional[int] = ..., signal: _Optional[str] = ...) -> None: ...

class SignalResponse(_message.Message):
    __slots__ = ("success", "message")
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    success: bool
    message: str
    def __init__(self, success: bool = ..., message: _Optional[str] = ...) -> None: ...

class ResetRequest(_message.Message):
    __slots__ = ("preserve_files",)
    PRESERVE_FILES_FIELD_NUMBER: _ClassVar[int]
    preserve_files: bool
    def __init__(self, preserve_files: bool = ...) -> None: ...

class ResetResponse(_message.Message):
    __slots__ = ("success", "message")
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    success: bool
    message: str
    def __init__(self, success: bool = ..., message: _Optional[str] = ...) -> None: ...

class ShellInput(_message.Message):
    __slots__ = ("data", "signal", "resize", "rows", "cols")
    DATA_FIELD_NUMBER: _ClassVar[int]
    SIGNAL_FIELD_NUMBER: _ClassVar[int]
    RESIZE_FIELD_NUMBER: _ClassVar[int]
    ROWS_FIELD_NUMBER: _ClassVar[int]
    COLS_FIELD_NUMBER: _ClassVar[int]
    data: str
    signal: str
    resize: bool
    rows: int
    cols: int
    def __init__(self, data: _Optional[str] = ..., signal: _Optional[str] = ..., resize: bool = ..., rows: _Optional[int] = ..., cols: _Optional[int] = ...) -> None: ...

class ShellOutput(_message.Message):
    __slots__ = ("data", "exit_code", "closed")
    DATA_FIELD_NUMBER: _ClassVar[int]
    EXIT_CODE_FIELD_NUMBER: _ClassVar[int]
    CLOSED_FIELD_NUMBER: _ClassVar[int]
    data: str
    exit_code: int
    closed: bool
    def __init__(self, data: _Optional[str] = ..., exit_code: _Optional[int] = ..., closed: bool = ...) -> None: ...
