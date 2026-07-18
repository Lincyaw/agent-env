package gateway

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

var ErrNamespaceNotAllowed = errors.New("namespace not allowed")

// RuntimeNotReadyError indicates the sandbox claim exists but is not yet
// ready (e.g., sandbox still binding, WarmPool not found). Callers should
// retry instead of treating this as a permanent failure.
type RuntimeNotReadyError struct {
	SessionID string
	ClaimName string
	Namespace string
}

func (e *RuntimeNotReadyError) Error() string {
	return fmt.Sprintf("session %s sandbox claim %s/%s is not ready", e.SessionID, e.Namespace, e.ClaimName)
}

// httpStatusForError maps common gateway error patterns to HTTP status codes.
func httpStatusForError(err error) int {
	if err == nil {
		return http.StatusOK
	}
	var notReady *RuntimeNotReadyError
	if errors.As(err, &notReady) {
		return http.StatusServiceUnavailable
	}
	msg := err.Error()
	if errors.Is(err, ErrNamespaceNotAllowed) {
		return http.StatusForbidden
	}
	if strings.Contains(msg, "not found") {
		return http.StatusNotFound
	}
	if strings.Contains(msg, "only devbox sessions") ||
		strings.Contains(msg, "invalid session mode") ||
		strings.Contains(msg, "is required") {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}
