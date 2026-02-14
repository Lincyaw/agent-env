"""Type definitions for ARL SDK using Pydantic models."""

from __future__ import annotations

from datetime import datetime
from typing import Annotated, Literal

from pydantic import BaseModel, Field, field_validator


class StepRequest(BaseModel):
    """A single execution step request.

    Attributes:
        name: Step identifier (must be unique within a batch)
        command: Shell command with arguments, e.g. ["echo", "hello"]
        env: Environment variables to set for this step
        work_dir: Working directory (default: /workspace)
        timeout: Timeout in seconds (None = no timeout)
    """

    name: str
    command: list[str] | None = None
    env: dict[str, str] | None = None
    work_dir: str | None = Field(None, alias="workDir")
    timeout: Annotated[int | None, Field(gt=0)] = None  # Must be positive if specified


class StepOutput(BaseModel):
    """Output of a single execution step.

    Attributes:
        stdout: Standard output from the command
        stderr: Standard error from the command
        exit_code: Exit code (0 = success, non-zero = error)
    """

    stdout: str = ""
    stderr: str = ""
    exit_code: int = 0


class StepResult(BaseModel):
    """Result of a single execution step including metadata.

    Attributes:
        index: Zero-based step index in the batch
        name: Step identifier
        output: Command output (stdout/stderr/exit_code)
        snapshot_id: Git snapshot ID for workspace state after this step
        duration_ms: Execution duration in milliseconds
        timestamp: Execution timestamp (ISO 8601)
    """

    index: Annotated[int, Field(ge=0)]
    name: str
    output: StepOutput
    snapshot_id: str = ""
    duration_ms: Annotated[int, Field(ge=0)] = 0
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
    """A condition on a warm pool (from Kubernetes status).

    Attributes:
        type: Condition type (e.g. "Ready", "PodsReady")
        status: Condition status ("True", "False", "Unknown")
        reason: Machine-readable reason code
        message: Human-readable explanation
    """

    type: str
    status: Literal["True", "False", "Unknown"]
    reason: str = ""
    message: str = ""


class PoolInfo(BaseModel):
    """Information about a warm pool.

    Attributes:
        name: WarmPool name
        namespace: Kubernetes namespace
        replicas: Desired number of warm pods
        ready_replicas: Number of ready idle pods
        allocated_replicas: Number of pods currently allocated to sessions
        conditions: Kubernetes status conditions
    """

    name: str
    namespace: str
    replicas: Annotated[int, Field(ge=0)] = 0
    ready_replicas: Annotated[int, Field(ge=0)] = Field(0, alias="readyReplicas")
    allocated_replicas: Annotated[int, Field(ge=0)] = Field(0, alias="allocatedReplicas")
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
    """A single tool manifest from registry.json.

    Attributes:
        name: Tool name (alphanumeric with _.-)
        description: Human-readable tool description
        parameters: JSON Schema defining tool input parameters
        entrypoint: Entry script filename (e.g. "run.sh", "main.py")
        runtime: Tool runtime environment
        timeout: Execution timeout (e.g. "30s", "1m")
    """

    name: str
    description: str = ""
    parameters: dict[str, object] = {}  # JSON Schema format
    entrypoint: str
    runtime: Literal["bash", "python", "binary"]
    timeout: str = ""  # Go duration format


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
    """Inline tool definition for WarmPool creation.

    Defines a small tool directly in code/YAML without needing a container image.
    The controller auto-generates manifest.json and writes files during pod init.

    Attributes:
        name: Tool name (alphanumeric with _.- only, max 63 chars)
        description: Human-readable description
        parameters: JSON Schema for tool input (passed as JSON stdin)
        runtime: Tool runtime (bash = shell script, python = Python script, binary = executable)
        entrypoint: Entry script filename that must exist in `files`
        timeout: Max execution time in Go duration format (e.g. "30s", "5m")
        files: Map of filename -> file content (all files written to /opt/arl/tools/<name>/)

    Example:
        ```python
        InlineToolSpec(
            name="greet",
            runtime="bash",
            entrypoint="run.sh",
            timeout="10s",
            parameters={"type": "object", "properties": {"name": {"type": "string"}}},
            files={"run.sh": "#!/bin/sh\\nread input\\necho hello"}
        )
        ```
    """

    name: Annotated[str, Field(pattern=r"^[a-zA-Z0-9][a-zA-Z0-9_.-]*$", max_length=63)]
    description: str = ""
    parameters: dict[str, object] = {}  # JSON Schema format
    runtime: Literal["bash", "python", "binary"]
    entrypoint: Annotated[str, Field(pattern=r"^[a-zA-Z0-9][a-zA-Z0-9_.-]*$", max_length=255)]
    timeout: str = ""  # Go duration format: "30s", "1m", "1h"
    files: dict[str, str]  # filename -> content

    @field_validator("entrypoint")
    @classmethod
    def validate_entrypoint_in_files(cls, v: str, info) -> str:
        """Ensure entrypoint exists in files dict."""
        if info.data.get("files") and v not in info.data["files"]:
            raise ValueError(f"entrypoint '{v}' must exist in files dict")
        return v


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


class ShellMessage(BaseModel):
    """A message received from the interactive shell WebSocket.

    Message types:
      - input: Send stdin data to shell (client → server)
      - output: Shell stdout/stderr data (server → client)
      - signal: Send signal to shell process (client → server, e.g. "SIGINT")
      - resize: Terminal resize event (client → server)
      - exit: Shell process exited (server → client)
      - error: Server-side error (server → client)

    Attributes:
        type: Message type (see above)
        data: Stdin data (input) or stdout/stderr (output) or error message (error)
        signal: Signal name for signal messages ("SIGINT", "SIGTERM", etc.)
        rows: Terminal rows for resize messages
        cols: Terminal columns for resize messages
        exit_code: Exit code for exit messages
    """

    type: Literal["input", "output", "signal", "resize", "exit", "error"]
    data: str = ""
    signal: str = ""
    rows: Annotated[int, Field(ge=0)] = 0
    cols: Annotated[int, Field(ge=0)] = 0
    exit_code: int = 0
