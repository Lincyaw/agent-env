package labels

const (
	PoolLabelKey    = "arl.infra.io/pool"
	SandboxLabelKey = "arl.infra.io/sandbox"
	StatusLabelKey  = "arl.infra.io/status"
	StatusIdle      = "idle"
	StatusAllocated = "allocated"

	// LastActivityAnnotation records the last time a pod was actively used (RFC3339).
	// Used during gateway recovery to distinguish orphaned pods from recently active ones.
	LastActivityAnnotation = "arl.infra.io/last-activity"

	// SessionAnnotation records the session ID that owns this pod.
	// Used during gateway recovery to rebuild in-memory session state.
	SessionAnnotation = "arl.infra.io/session"

	// LastReleasedAnnotation records the last time a pod transitioned from
	// allocated back to idle (RFC3339). Used by LRU scale-down to delete the
	// least-recently-used idle pods first.
	LastReleasedAnnotation = "arl.infra.io/last-released"
)
