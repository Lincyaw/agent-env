"""Base exception hierarchy for the ARL SDK."""


class ArlError(RuntimeError):
    """Base exception for all ARL SDK errors.

    Derives from :class:`RuntimeError` because the SDK raised bare
    ``RuntimeError`` before this hierarchy existed; callers that still catch
    ``RuntimeError`` keep working.
    """


class SessionNotInitializedError(ArlError):
    """Raised when an operation requires an active session but none exists."""

    def __init__(self, message: str = "No session created. Call create_sandbox() first.") -> None:
        super().__init__(message)
