"""ARL - High-level API for Agent Runtime Layer."""

from arl.gateway_client import GatewayClient, GatewayError, PoolNotReadyError
from arl.interactive_shell_client import InteractiveShellClient, create_websocket_proxy
from arl.session import SandboxSession
from arl.types import (
    ErrorResponse,
    ExecuteResponse,
    InlineToolSpec,
    PoolCondition,
    PoolInfo,
    SessionInfo,
    ShellMessage,
    StepOutput,
    StepRequest,
    StepResult,
    ToolManifest,
    ToolResult,
    ToolsConfigMapSource,
    ToolsImageSource,
    ToolsRegistry,
    ToolsSpec,
    TrajectoryEntry,
)
from arl.warmpool import WarmPoolManager

__version__ = "0.2.0"
__all__ = [
    "ErrorResponse",
    "ExecuteResponse",
    "GatewayClient",
    "GatewayError",
    "InlineToolSpec",
    "InteractiveShellClient",
    "PoolCondition",
    "PoolInfo",
    "PoolNotReadyError",
    "SandboxSession",
    "SessionInfo",
    "ShellMessage",
    "StepOutput",
    "StepRequest",
    "StepResult",
    "ToolManifest",
    "ToolResult",
    "ToolsConfigMapSource",
    "ToolsImageSource",
    "ToolsRegistry",
    "ToolsSpec",
    "TrajectoryEntry",
    "WarmPoolManager",
    "create_websocket_proxy",
]
