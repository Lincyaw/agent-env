"""Type definitions for ARL SDK using Pydantic models."""

from __future__ import annotations

from datetime import datetime

from pydantic import BaseModel, Field


class StepRequest(BaseModel):
    """A single execution step request."""

    name: str
    command: list[str] | None = None
    env: dict[str, str] | None = None
    work_dir: str | None = Field(None, alias="workDir")
    timeout: int | None = None


class StepOutput(BaseModel):
    """Output of a single execution step."""

    stdout: str = ""
    stderr: str = ""
    exit_code: int = 0


class StepResult(BaseModel):
    """Result of a single execution step including metadata."""

    index: int
    name: str
    output: StepOutput
    snapshot_id: str = ""
    duration_ms: int = 0
    timestamp: datetime | None = None


class SessionInfo(BaseModel):
    """Information about an active session."""

    id: str
    sandbox_name: str = Field(alias="sandboxName")
    namespace: str
    pool_ref: str = Field(alias="poolRef")
    pod_ip: str = Field("", alias="podIP")
    pod_name: str = Field("", alias="podName")
    created_at: datetime | None = Field(None, alias="createdAt")

    model_config = {"populate_by_name": True}


class ExecuteResponse(BaseModel):
    """Response from executing steps."""

    session_id: str = Field(alias="sessionID")
    results: list[StepResult] = []
    total_duration_ms: int = Field(0, alias="totalDurationMs")

    model_config = {"populate_by_name": True}


class PoolCondition(BaseModel):
    """A condition on a warm pool."""

    type: str
    status: str
    reason: str = ""
    message: str = ""


class PoolInfo(BaseModel):
    """Information about a warm pool."""

    name: str
    namespace: str
    replicas: int = 0
    ready_replicas: int = Field(0, alias="readyReplicas")
    allocated_replicas: int = Field(0, alias="allocatedReplicas")
    conditions: list[PoolCondition] = []

    model_config = {"populate_by_name": True}


class TrajectoryEntry(BaseModel):
    """A single trajectory entry for RL/SFT export."""

    session_id: str
    step: int
    action: dict[str, object]
    observation: dict[str, object]
    snapshot_id: str = ""
    timestamp: datetime | None = None


class ErrorResponse(BaseModel):
    """Error response from the gateway."""

    error: str
    detail: str = ""


# --- Tool types ---


class ToolManifest(BaseModel):
    """A single tool manifest from registry.json."""

    name: str
    description: str = ""
    parameters: dict[str, object] = {}
    entrypoint: str
    runtime: str
    timeout: str = ""


class ToolsRegistry(BaseModel):
    """Registry of all available tools in a sandbox."""

    tools: list[ToolManifest] = []


class ToolResult(BaseModel):
    """Result of a tool call (parsed JSON stdout)."""

    raw_output: str = ""
    parsed: dict[str, object] = {}
    exit_code: int = 0
    stderr: str = ""


class InlineToolSpec(BaseModel):
    """Inline tool definition for WarmPool creation."""

    name: str
    description: str = ""
    parameters: dict[str, object] = {}
    runtime: str
    entrypoint: str
    timeout: str = ""
    files: dict[str, str]


class ToolsImageSource(BaseModel):
    """Reference to a container image containing tools."""

    image: str


class ToolsConfigMapSource(BaseModel):
    """Reference to a ConfigMap containing tools."""

    name: str


class ToolsSpec(BaseModel):
    """Tools specification for a WarmPool."""

    images: list[ToolsImageSource] = []
    config_maps: list[ToolsConfigMapSource] = Field(default=[], alias="configMaps")
    inline: list[InlineToolSpec] = []

    model_config = {"populate_by_name": True}
