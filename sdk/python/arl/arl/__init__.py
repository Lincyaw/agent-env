"""ARL - High-level API for Agent Runtime Layer."""

from arl._base import GatewayError, GatewayOperationTimeout, PoolNotReadyError
from arl.async_client import AsyncGatewayClient
from arl.async_session import AsyncDevboxSession, AsyncManagedSession, AsyncSandboxSession
from arl.auth import ApiKeyAuth, SsoTokenAuth
from arl.config import ArlConfig, ContextConfig, load_config, resolve_from_config
from arl.configenv import (
    ConfigEnvSpec,
    ConfigMapTemplate,
    SecretEnvVarRef,
    SecretInjection,
    SecretTemplate,
    VolumeInjection,
)
from arl.exceptions import ArlError, SessionNotInitializedError
from arl.gateway_client import GatewayClient
from arl.interactive_shell_client import InteractiveShellClient, create_websocket_proxy
from arl.iroh_transport import IrohTransport, SyncIrohBridge
from arl.session import DevboxSession, ManagedSession, SandboxSession
from arl.types import (
    BuildResponse,
    ConnectionInfo,
    ContainerExecuteResponse,
    DeleteExperimentResponse,
    DevboxConfig,
    DevboxPort,
    ErrorResponse,
    ExecuteOperationInfo,
    ExecuteResponse,
    ExperimentSummary,
    ForkSessionResponse,
    GatewaySummary,
    GitConfig,
    InlineToolSpec,
    LogEntry,
    ManagedSessionInfo,
    PoolCondition,
    PoolInfo,
    PoolLogEntry,
    PortInfo,
    PrivateContainerSpec,
    ReplayResponse,
    ResourceRequirements,
    RestoreResponse,
    SessionInfo,
    SessionListItem,
    ShellMessage,
    SSHInfo,
    StepOutput,
    StepRequest,
    StepResult,
    ToolsConfigMapSource,
    ToolsImageSource,
    ToolsSpec,
    UploadFileResponse,
)
from arl.warmpool import WarmPoolManager

try:
    from importlib.metadata import version as _meta_version

    __version__ = _meta_version("arl-env")
except Exception:
    __version__ = "0.0.0"
__all__ = [
    "ApiKeyAuth",
    "ArlConfig",
    "ArlError",
    "AsyncDevboxSession",
    "AsyncGatewayClient",
    "AsyncManagedSession",
    "AsyncSandboxSession",
    "BuildResponse",
    "ConfigEnvSpec",
    "ConfigMapTemplate",
    "ConnectionInfo",
    "ContainerExecuteResponse",
    "ContextConfig",
    "DeleteExperimentResponse",
    "DevboxConfig",
    "DevboxPort",
    "DevboxSession",
    "ErrorResponse",
    "ExecuteOperationInfo",
    "ExecuteResponse",
    "ExperimentSummary",
    "ForkSessionResponse",
    "GatewayClient",
    "GatewayError",
    "GatewayOperationTimeout",
    "GatewaySummary",
    "GitConfig",
    "InlineToolSpec",
    "InteractiveShellClient",
    "IrohTransport",
    "LogEntry",
    "ManagedSession",
    "ManagedSessionInfo",
    "PoolCondition",
    "PoolInfo",
    "PoolLogEntry",
    "PoolNotReadyError",
    "PortInfo",
    "PrivateContainerSpec",
    "ReplayResponse",
    "ResourceRequirements",
    "RestoreResponse",
    "SSHInfo",
    "SandboxSession",
    "SecretEnvVarRef",
    "SecretInjection",
    "SecretTemplate",
    "SessionInfo",
    "SessionListItem",
    "SessionNotInitializedError",
    "ShellMessage",
    "SsoTokenAuth",
    "StepOutput",
    "StepRequest",
    "StepResult",
    "SyncIrohBridge",
    "ToolsConfigMapSource",
    "ToolsImageSource",
    "ToolsSpec",
    "UploadFileResponse",
    "VolumeInjection",
    "WarmPoolManager",
    "create_websocket_proxy",
    "load_config",
    "resolve_from_config",
]
