"""Base exception hierarchy for the ARL SDK."""


class ArlError(Exception):
    """Base exception for all ARL SDK errors."""


class SessionNotInitializedError(ArlError):
    """Raised when an operation requires an active session but none exists."""

    def __init__(self, message: str = "No session created. Call create_sandbox() first.") -> None:
        super().__init__(message)
