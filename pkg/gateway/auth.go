package gateway

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Role represents an API key's permission level.
type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

// AuthConfig holds API key authentication and origin-checking configuration.
type AuthConfig struct {
	Enabled        bool
	Keys           map[string]Role
	AllowedOrigins []string

	// KeyFile is an optional path to a file containing API keys (same
	// "key:role" format, one per line). When set, the file is watched and
	// keys are hot-reloaded on change.
	KeyFile string

	mu sync.RWMutex // guards Keys during hot-reload
}

// GetKeys returns a snapshot of the current key map (safe for concurrent reads
// during hot-reload).
func (a *AuthConfig) GetKeys() map[string]Role {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.Keys
}

type contextKey int

const (
	roleCtxKey contextKey = iota
	keyHashCtxKey
)

// RoleFromContext retrieves the authenticated role from the request context.
func RoleFromContext(ctx context.Context) (Role, bool) {
	r, ok := ctx.Value(roleCtxKey).(Role)
	return r, ok
}

// KeyHashFromContext retrieves the SHA-256 hash of the caller's API key.
func KeyHashFromContext(ctx context.Context) (string, bool) {
	h, ok := ctx.Value(keyHashCtxKey).(string)
	return h, ok
}

// requireAuth wraps a handler with Bearer token validation and RBAC.
func requireAuth(authCfg *AuthConfig, minRole Role, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := extractBearerToken(r)
		if token == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="arl-gateway"`)
			writeError(w, http.StatusUnauthorized, "missing or invalid Authorization header")
			return
		}

		keys := authCfg.GetKeys()
		role, ok := matchAPIKey(keys, token)
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid API key")
			return
		}

		if minRole == RoleAdmin && role != RoleAdmin {
			writeError(w, http.StatusForbidden, "admin access required")
			return
		}

		hash := sha256.Sum256([]byte(token))
		ctx := context.WithValue(r.Context(), roleCtxKey, role)
		ctx = context.WithValue(ctx, keyHashCtxKey, hex.EncodeToString(hash[:]))
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// CheckSessionOwnership returns an error if the caller is not the session owner
// and does not have the admin role. Returns nil when auth is disabled (no key
// hash in context) or for recovered sessions with empty ownerKeyHash.
func CheckSessionOwnership(ctx context.Context, ownerKeyHash string) error {
	callerHash, ok := KeyHashFromContext(ctx)
	if !ok {
		return nil
	}
	if ownerKeyHash == "" {
		return nil
	}
	role, _ := RoleFromContext(ctx)
	if role == RoleAdmin {
		return nil
	}
	if subtle.ConstantTimeCompare([]byte(callerHash), []byte(ownerKeyHash)) != 1 {
		return fmt.Errorf("access denied: session owned by another user")
	}
	return nil
}

// extractBearerToken gets the token from the Authorization header. WebSocket
// upgrade requests may also use the "token" query parameter because browser
// clients cannot set custom headers during the upgrade.
func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth != "" {
		const prefix = "Bearer "
		if len(auth) >= len(prefix) && strings.EqualFold(auth[:len(prefix)], prefix) {
			return auth[len(prefix):]
		}
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return ""
	}
	return r.URL.Query().Get("token")
}

// matchAPIKey checks the token against all registered keys using constant-time
// comparison to prevent timing attacks.
func matchAPIKey(keys map[string]Role, token string) (Role, bool) {
	for key, role := range keys {
		if subtle.ConstantTimeCompare([]byte(key), []byte(token)) == 1 {
			return role, true
		}
	}
	return "", false
}

// ParseAPIKeys parses a comma-separated "key1:admin,key2:user" string into a
// map. Keys without a recognised role default to RoleUser.
func ParseAPIKeys(raw string) map[string]Role {
	keys := make(map[string]Role)
	if raw == "" {
		return keys
	}
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		roleStr := strings.TrimSpace(parts[1])
		if key == "" {
			continue
		}
		switch Role(roleStr) {
		case RoleAdmin:
			keys[key] = RoleAdmin
		default:
			keys[key] = RoleUser
		}
	}
	return keys
}

// ParseAPIKeysFile reads a key file (one "key:role" per line, # comments allowed)
// and returns a key map.
func ParseAPIKeysFile(path string) (map[string]Role, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	keys := make(map[string]Role)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		roleStr := strings.TrimSpace(parts[1])
		if key == "" {
			continue
		}
		switch Role(roleStr) {
		case RoleAdmin:
			keys[key] = RoleAdmin
		default:
			keys[key] = RoleUser
		}
	}
	return keys, nil
}

// StartKeyFileWatcher launches a goroutine that reloads API keys from
// authCfg.KeyFile every 30 seconds when the file changes. Returns a stop
// function.
func StartKeyFileWatcher(authCfg *AuthConfig) func() {
	if authCfg.KeyFile == "" {
		return func() {}
	}

	stopCh := make(chan struct{})
	var lastMod time.Time

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				info, err := os.Stat(authCfg.KeyFile)
				if err != nil {
					log.Printf("Warning: cannot stat key file %s: %v", authCfg.KeyFile, err)
					continue
				}
				if !info.ModTime().After(lastMod) {
					continue
				}
				newKeys, err := ParseAPIKeysFile(authCfg.KeyFile)
				if err != nil {
					log.Printf("Warning: failed to reload key file %s: %v", authCfg.KeyFile, err)
					continue
				}
				if len(newKeys) == 0 {
					log.Printf("Warning: key file %s yielded 0 keys, keeping old keys", authCfg.KeyFile)
					continue
				}
				authCfg.mu.Lock()
				authCfg.Keys = newKeys
				authCfg.mu.Unlock()
				lastMod = info.ModTime()
				log.Printf("Reloaded %d API key(s) from %s", len(newKeys), authCfg.KeyFile)
			}
		}
	}()

	return func() { close(stopCh) }
}
