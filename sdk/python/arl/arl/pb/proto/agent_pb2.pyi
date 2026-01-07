from collections.abc import Iterable as _Iterable
from collections.abc import Mapping as _Mapping
from typing import ClassVar as _ClassVar

from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from google.protobuf.internal import containers as _containers

DESCRIPTOR: _descriptor.FileDescriptor

class FileRequest(_message.Message):
    __slots__ = ("base_path", "files", "patch")
    class FilesEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: str | None = ..., value: str | None = ...) -> None: ...

    BASE_PATH_FIELD_NUMBER: _ClassVar[int]
    FILES_FIELD_NUMBER: _ClassVar[int]
    PATCH_FIELD_NUMBER: _ClassVar[int]
    base_path: str
    files: _containers.ScalarMap[str, str]
    patch: str
    def __init__(
        self,
        base_path: str | None = ...,
        files: _Mapping[str, str] | None = ...,
        patch: str | None = ...,
    ) -> None: ...

class FileResponse(_message.Message):
    __slots__ = ("message", "success")
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    success: bool
    message: str
    def __init__(self, success: bool = ..., message: str | None = ...) -> None: ...

class ExecRequest(_message.Message):
    __slots__ = ("background", "command", "env", "timeout_seconds", "working_dir")
    class EnvEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: str | None = ..., value: str | None = ...) -> None: ...

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
    def __init__(
        self,
        command: _Iterable[str] | None = ...,
        env: _Mapping[str, str] | None = ...,
        working_dir: str | None = ...,
        background: bool = ...,
        timeout_seconds: int | None = ...,
    ) -> None: ...

class ExecLog(_message.Message):
    __slots__ = ("done", "exit_code", "stderr", "stdout")
    STDOUT_FIELD_NUMBER: _ClassVar[int]
    STDERR_FIELD_NUMBER: _ClassVar[int]
    EXIT_CODE_FIELD_NUMBER: _ClassVar[int]
    DONE_FIELD_NUMBER: _ClassVar[int]
    stdout: str
    stderr: str
    exit_code: int
    done: bool
    def __init__(
        self,
        stdout: str | None = ...,
        stderr: str | None = ...,
        exit_code: int | None = ...,
        done: bool = ...,
    ) -> None: ...

class SignalRequest(_message.Message):
    __slots__ = ("pid", "signal")
    PID_FIELD_NUMBER: _ClassVar[int]
    SIGNAL_FIELD_NUMBER: _ClassVar[int]
    pid: int
    signal: str
    def __init__(self, pid: int | None = ..., signal: str | None = ...) -> None: ...

class SignalResponse(_message.Message):
    __slots__ = ("message", "success")
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    success: bool
    message: str
    def __init__(self, success: bool = ..., message: str | None = ...) -> None: ...

class ResetRequest(_message.Message):
    __slots__ = ("preserve_files",)
    PRESERVE_FILES_FIELD_NUMBER: _ClassVar[int]
    preserve_files: bool
    def __init__(self, preserve_files: bool = ...) -> None: ...

class ResetResponse(_message.Message):
    __slots__ = ("message", "success")
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    success: bool
    message: str
    def __init__(self, success: bool = ..., message: str | None = ...) -> None: ...

class ShellInput(_message.Message):
    __slots__ = ("cols", "data", "resize", "rows", "signal")
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
    def __init__(
        self,
        data: str | None = ...,
        signal: str | None = ...,
        resize: bool = ...,
        rows: int | None = ...,
        cols: int | None = ...,
    ) -> None: ...

class ShellOutput(_message.Message):
    __slots__ = ("closed", "data", "exit_code")
    DATA_FIELD_NUMBER: _ClassVar[int]
    EXIT_CODE_FIELD_NUMBER: _ClassVar[int]
    CLOSED_FIELD_NUMBER: _ClassVar[int]
    data: str
    exit_code: int
    closed: bool
    def __init__(
        self, data: str | None = ..., exit_code: int | None = ..., closed: bool = ...
    ) -> None: ...
