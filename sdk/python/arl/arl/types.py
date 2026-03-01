"""Type definitions for ARL SDK using Pydantic models."""

from __future__ import annotations

import re
from datetime import datetime
from typing import Annotated, Literal

from pydantic import BaseModel, Field, field_validator, model_validator


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

    @model_validator(mode="after")
    def validate_entrypoint_in_files(self) -> InlineToolSpec:
        """Ensure entrypoint exists in files dict."""
        if self.files and self.entrypoint not in self.files:
            raise ValueError(
                f"entrypoint '{self.entrypoint}' must be a key in files"
            )
        return self


class ToolsImageSource(BaseModel):
    """Reference to a container image containing tools."""

    image: str


class ToolsConfigMapSource(BaseModel):
    """Reference to a ConfigMap containing tools."""

    name: str


class ResourceRequirements(BaseModel):
    """Kubernetes resource requirements (CPU/memory requests and limits).

    Attributes:
        requests: Minimum resources required (e.g. {"cpu": "100m", "memory": "128Mi"})
        limits: Maximum resources allowed (e.g. {"cpu": "1", "memory": "1Gi"})

    Supported resource types:
        - cpu: CPU cores (e.g., "100m" = 0.1 cores, "1" = 1 core, "2.5" = 2.5 cores)
        - memory: Memory bytes (e.g., "128Mi", "1Gi", "512M")
        - ephemeral-storage: Ephemeral storage (e.g., "1Gi", "10Gi")
        - Custom resources: vendor-specific (e.g., "nvidia.com/gpu": "1")

    Quantity format:
        - Integer: "1", "2"
        - Decimal: "0.5", "2.5"
        - Milliunits (cpu): "100m", "500m"
        - Binary (memory): "128Mi", "1Gi", "1Ti"
        - Decimal (memory): "128M", "1G", "1T"

    Examples:
        >>> # Basic usage with CPU and memory
        >>> ResourceRequirements(
        ...     requests={"cpu": "100m", "memory": "128Mi"},
        ...     limits={"cpu": "1", "memory": "1Gi"}
        ... )
        >>> # Requests only (no hard limits)
        >>> ResourceRequirements(requests={"cpu": "500m", "memory": "512Mi"})
        >>> # With ephemeral storage
        >>> ResourceRequirements(
        ...     requests={"cpu": "1", "memory": "1Gi", "ephemeral-storage": "10Gi"}
        ... )
        >>> # With custom GPU resource
        >>> ResourceRequirements(
        ...     limits={"nvidia.com/gpu": "2"}
        ... )
    """

    requests: dict[str, str] = Field(default_factory=dict)
    limits: dict[str, str] = Field(default_factory=dict)

    @field_validator("requests", "limits")
    @classmethod
    def validate_resource_quantities(cls, v: dict[str, str]) -> dict[str, str]:
        """Validate that resource quantities follow Kubernetes format.

        Valid formats:
        - Integer: 1, 2, 100
        - Decimal: 0.5, 2.5
        - Milliunits: 100m, 500m (for CPU)
        - Binary suffixes: Ki, Mi, Gi, Ti, Pi, Ei
        - Decimal suffixes: k, M, G, T, P, E
        """
        # Kubernetes quantity regex pattern
        # Matches: integers, decimals, milliunits (m), and suffixes (Ki, Mi, Gi, k, M, G, etc.)
        quantity_pattern = re.compile(r"^([+-]?)(\d+(\.\d+)?|\.\d+)([eE][+-]?\d+)?[numkKMGTPE]?i?$")

        for resource_name, quantity in v.items():
            if not quantity:
                raise ValueError(f"Resource '{resource_name}' has empty quantity")

            if not quantity_pattern.match(quantity):
                raise ValueError(
                    f"Invalid quantity format for '{resource_name}': '{quantity}'. "
                    f"Expected Kubernetes quantity format (e.g., '100m', '1', '128Mi', '1Gi')"
                )

            # Additional validation for common resources
            if resource_name == "cpu":
                # CPU should be reasonable (not more than 1000 cores typically)
                if quantity.endswith("m"):
                    try:
                        millicores = int(quantity[:-1])
                    except ValueError:
                        raise ValueError(f"Invalid cpu millicore quantity: {quantity}")
                    if millicores <= 0 or millicores > 1_000_000:
                        raise ValueError(
                            f"CPU millicores must be between 1 and 1000000, got {millicores}"
                        )

            elif resource_name == "memory":
                # Memory should have a suffix (Mi, Gi, M, G, etc.)
                if not re.search(r"[KMGTPE]i?$", quantity):
                    # Allow plain numbers but warn they might be interpreted as bytes
                    if not quantity.isdigit():
                        raise ValueError(
                            f"Memory quantity '{quantity}' should include a unit suffix "
                            f"(e.g., 'Mi', 'Gi', 'M', 'G')"
                        )

        return v


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
