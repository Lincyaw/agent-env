package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExtractBearerTokenRejectsQueryTokenForREST(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/pools?token=secret", nil)

	if got := extractBearerToken(req); got != "" {
		t.Fatalf("extractBearerToken = %q, want empty for non-WebSocket request", got)
	}
}

func TestExtractBearerTokenAllowsQueryTokenForWebSocket(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/sessions/s1/shell?token=secret", nil)
	req.Header.Set("Upgrade", "websocket")

	if got := extractBearerToken(req); got != "secret" {
		t.Fatalf("extractBearerToken = %q, want query token for WebSocket request", got)
	}
}

func TestExtractBearerTokenPrefersAuthorizationHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/sessions/s1/shell?token=query-secret", nil)
	req.Header.Set("Authorization", "Bearer header-secret")
	req.Header.Set("Upgrade", "websocket")

	if got := extractBearerToken(req); got != "header-secret" {
		t.Fatalf("extractBearerToken = %q, want Authorization header token", got)
	}
}
