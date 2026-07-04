package gateway

import (
	"errors"
	"net/http"
	"strings"
)

var ErrNamespaceNotAllowed = errors.New("namespace not allowed")

// httpStatusForError maps common gateway error patterns to HTTP status codes.
func httpStatusForError(err error) int {
	if err == nil {
		return http.StatusOK
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
