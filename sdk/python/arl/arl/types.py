"""Type definitions for ARL wrapper."""

from typing import TypedDict


class TaskStep(TypedDict, total=False):
    """Task step configuration.

    Attributes:
        name: Step identifier
        type: Step type - "Command" or "FilePatch"
        command: Command and arguments (for Command type)
        env: Environment variables (optional)
        workDir: Working directory (optional)
        path: File path (for FilePatch type)
        content: File content (for FilePatch type)
        traceID: Optional trace ID for distributed tracing
    """

    name: str
    type: str  # "Command" or "FilePatch"
    # For Command steps
    command: list[str]
    env: dict[str, str]
    workDir: str
    # For FilePatch steps
    path: str
    content: str
    # For distributed tracing
    traceID: str


class KubernetesMetadata(TypedDict, total=False):
    """Kubernetes resource metadata.

    Attributes:
        name: Resource name
        namespace: Kubernetes namespace
        labels: Resource labels
        annotations: Resource annotations
        uid: Resource UID
        resourceVersion: Resource version
        creationTimestamp: Creation timestamp
    """

    name: str
    namespace: str
    labels: dict[str, str]
    annotations: dict[str, str]
    uid: str
    resourceVersion: str
    creationTimestamp: str


class SandboxSpec(TypedDict, total=False):
    """Sandbox specification.

    Attributes:
        poolRef: Name of the WarmPool to allocate from
        keepAlive: Whether sandbox should persist
    """

    poolRef: str
    keepAlive: bool


class SandboxCondition(TypedDict, total=False):
    """Sandbox status condition.

    Attributes:
        type: Condition type
        status: Condition status (True/False)
        reason: Reason for condition
        message: Human-readable message
        lastTransitionTime: Last transition timestamp
    """

    type: str
    status: str
    reason: str
    message: str
    lastTransitionTime: str


class SandboxStatus(TypedDict, total=False):
    """Sandbox status.

    Attributes:
        phase: Current phase (Pending/Ready/Failed)
        podName: Name of the backing pod
        conditions: List of status conditions
        message: Status message
    """

    phase: str
    podName: str
    conditions: list[SandboxCondition]
    message: str


class SandboxResource(TypedDict, total=False):
    """Kubernetes Sandbox resource.

    Attributes:
        apiVersion: API version
        kind: Resource kind
        metadata: Resource metadata
        spec: Sandbox specification
        status: Sandbox status
    """

    apiVersion: str
    kind: str
    metadata: KubernetesMetadata
    spec: SandboxSpec
    status: SandboxStatus


class TaskSpec(TypedDict, total=False):
    """Task specification.

    Attributes:
        sandboxRef: Reference to sandbox
        steps: List of task steps
        traceID: Optional trace ID
    """

    sandboxRef: str
    steps: list[TaskStep]
    traceID: str


class TaskStepResult(TypedDict, total=False):
    """Result of a single task step.

    Attributes:
        name: Step name
        exitCode: Exit code
        stdout: Standard output
        stderr: Standard error
        error: Error message if step failed
    """

    name: str
    exitCode: int
    stdout: str
    stderr: str
    error: str


class TaskStatus(TypedDict, total=False):
    """Task status.

    Attributes:
        state: Current state (Pending/Running/Succeeded/Failed)
        startTime: Task start time
        completionTime: Task completion time
        steps: Results of individual steps
        stdout: Combined stdout (for backward compatibility)
        stderr: Combined stderr (for backward compatibility)
        exitCode: Exit code (for backward compatibility)
        message: Status message
    """

    state: str
    startTime: str
    completionTime: str
    steps: list[TaskStepResult]
    stdout: str
    stderr: str
    exitCode: int
    message: str


class TaskResource(TypedDict, total=False):
    """Kubernetes Task resource.

    Attributes:
        apiVersion: API version
        kind: Resource kind
        metadata: Resource metadata
        spec: Task specification
        status: Task status
        callback_result: Optional callback execution result
    """

    apiVersion: str
    kind: str
    metadata: KubernetesMetadata
    spec: TaskSpec
    status: TaskStatus
    # For execute_with_callback
    callback_result: "TaskResource"
