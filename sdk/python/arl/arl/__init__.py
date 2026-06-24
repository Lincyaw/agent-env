"""ARL - High-level API for Agent Runtime Layer."""

from arl.auth import ApiKeyAuth, SsoTokenAuth
from arl.configenv import (
    ConfigEnvSpec,
    ConfigMapTemplate,
    SecretEnvVarRef,
    SecretInjection,
    SecretTemplate,
    VolumeInjection,
)
from arl.gateway_client import GatewayClient, GatewayError, PoolNotReadyError
from arl.interactive_shell_client import InteractiveShellClient, create_websocket_proxy
from arl.session import ManagedSession, SandboxSession
from arl.types import (
    ErrorResponse,
    ExecuteResponse,
    InlineToolSpec,
    ManagedSessionInfo,
    PoolCondition,
    PoolInfo,
    ResourceRequirements,
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
    UploadFileResponse,
)
from arl.warmpool import WarmPoolManager

__version__ = "0.8.0"
__all__ = [
    "ApiKeyAuth",
    "ConfigEnvSpec",
    "ConfigMapTemplate",
    "ErrorResponse",
    "ExecuteResponse",
    "GatewayClient",
    "GatewayError",
    "InlineToolSpec",
    "InteractiveShellClient",
    "ManagedSession",
    "ManagedSessionInfo",
    "PoolCondition",
    "PoolInfo",
    "PoolNotReadyError",
    "ResourceRequirements",
    "SandboxSession",
    "SecretEnvVarRef",
    "SecretInjection",
    "SecretTemplate",
    "SessionInfo",
    "ShellMessage",
    "SsoTokenAuth",
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
    "UploadFileResponse",
    "VolumeInjection",
    "WarmPoolManager",
    "create_websocket_proxy",
]
