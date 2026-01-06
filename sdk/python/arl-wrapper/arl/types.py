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
