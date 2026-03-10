package gateway

// SessionStore abstracts session storage so the Gateway can use either
// an in-memory store (default, for dev/testing) or a Redis-backed store
// (for HA deployments with multiple gateway replicas).
type SessionStore interface {
	// Get retrieves a session by ID. Returns nil, false if not found.
	Get(sessionID string) (*session, bool)

	// Set stores a session.
	Set(sessionID string, s *session)

	// Delete removes a session.
	Delete(sessionID string)

	// Range iterates over all sessions. If fn returns false, iteration stops.
	Range(fn func(sessionID string, s *session) bool)

	// Count returns the current session count.
	Count() int64

	// IncrCount atomically increments the session count by delta and returns the new value.
	IncrCount(delta int64) int64

	// Close releases any resources held by the store.
	Close() error
}
