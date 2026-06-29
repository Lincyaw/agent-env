package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireAuthAcceptsBearerToken(t *testing.T) {
	authCfg := &AuthConfig{
		Enabled: true,
		Keys:    map[string]Role{"good-token": RoleUser},
	}

	req := httptest.NewRequest(http.MethodGet, "/demo", nil)
	req.Header.Set("Authorization", "Bearer good-token")
	rr := httptest.NewRecorder()

	requireAuth(authCfg, RoleUser, func(w http.ResponseWriter, r *http.Request) {
		role, ok := RoleFromContext(r.Context())
		if !ok || role != RoleUser {
			t.Fatalf("role = %q, ok=%t; want user", role, ok)
		}
		if _, ok := KeyHashFromContext(r.Context()); !ok {
			t.Fatal("missing key hash in context")
		}
		w.WriteHeader(http.StatusNoContent)
	})(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d, body=%s", rr.Code, http.StatusNoContent, rr.Body.String())
	}
}

func TestRequireAuthAcceptsForwardUserFromTrustedProxy(t *testing.T) {
	trustedNets, err := ParseTrustedProxies("10.0.0.0/8")
	if err != nil {
		t.Fatalf("ParseTrustedProxies: %v", err)
	}
	authCfg := &AuthConfig{
		Enabled:            true,
		ForwardAuthEnabled: true,
		ForwardUserHeader:  "Remote-User",
		ForwardTrustedNets: trustedNets,
	}

	req := httptest.NewRequest(http.MethodGet, "/demo", nil)
	req.RemoteAddr = "10.2.3.4:5678"
	req.Header.Set("Remote-User", "alice")
	rr := httptest.NewRecorder()

	requireAuth(authCfg, RoleUser, func(w http.ResponseWriter, r *http.Request) {
		role, ok := RoleFromContext(r.Context())
		if !ok || role != RoleUser {
			t.Fatalf("role = %q, ok=%t; want user", role, ok)
		}
		if _, ok := KeyHashFromContext(r.Context()); !ok {
			t.Fatal("missing forwarded identity hash in context")
		}
		w.WriteHeader(http.StatusNoContent)
	})(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d, body=%s", rr.Code, http.StatusNoContent, rr.Body.String())
	}
}

func TestRequireAuthRejectsForwardUserFromUntrustedProxy(t *testing.T) {
	trustedNets, err := ParseTrustedProxies("10.0.0.0/8")
	if err != nil {
		t.Fatalf("ParseTrustedProxies: %v", err)
	}
	authCfg := &AuthConfig{
		Enabled:            true,
		ForwardAuthEnabled: true,
		ForwardUserHeader:  "Remote-User",
		ForwardTrustedNets: trustedNets,
	}

	req := httptest.NewRequest(http.MethodGet, "/demo", nil)
	req.RemoteAddr = "192.168.1.10:5678"
	req.Header.Set("Remote-User", "alice")
	rr := httptest.NewRecorder()

	requireAuth(authCfg, RoleUser, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestRequireAuthForwardAdminUsers(t *testing.T) {
	trustedNets, err := ParseTrustedProxies("10.0.0.1")
	if err != nil {
		t.Fatalf("ParseTrustedProxies: %v", err)
	}
	authCfg := &AuthConfig{
		Enabled:            true,
		ForwardAuthEnabled: true,
		ForwardUserHeader:  "Remote-User",
		ForwardAdminUsers:  ParseForwardAdminUsers("alice"),
		ForwardTrustedNets: trustedNets,
	}

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.RemoteAddr = "10.0.0.1:5678"
	req.Header.Set("Remote-User", "alice")
	rr := httptest.NewRecorder()

	requireAuth(authCfg, RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		role, ok := RoleFromContext(r.Context())
		if !ok || role != RoleAdmin {
			t.Fatalf("role = %q, ok=%t; want admin", role, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	})(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d, body=%s", rr.Code, http.StatusNoContent, rr.Body.String())
	}
}

func TestRequireAuthInvalidBearerDoesNotFallBackToForwardUser(t *testing.T) {
	trustedNets, err := ParseTrustedProxies("10.0.0.0/8")
	if err != nil {
		t.Fatalf("ParseTrustedProxies: %v", err)
	}
	authCfg := &AuthConfig{
		Enabled:            true,
		Keys:               map[string]Role{"good-token": RoleUser},
		ForwardAuthEnabled: true,
		ForwardUserHeader:  "Remote-User",
		ForwardTrustedNets: trustedNets,
	}

	req := httptest.NewRequest(http.MethodGet, "/demo", nil)
	req.RemoteAddr = "10.2.3.4:5678"
	req.Header.Set("Authorization", "Bearer bad-token")
	req.Header.Set("Remote-User", "alice")
	rr := httptest.NewRecorder()

	requireAuth(authCfg, RoleUser, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestParseTrustedProxiesRejectsInvalidEntry(t *testing.T) {
	if _, err := ParseTrustedProxies("10.0.0.0/8,not-an-ip"); err == nil {
		t.Fatal("ParseTrustedProxies accepted invalid entry")
	}
}
