package labels

const (
	PoolLabelKey    = "arl.infra.io/pool"
	SandboxLabelKey = "arl.infra.io/sandbox"
	StatusLabelKey  = "arl.infra.io/status"
	StatusIdle      = "idle"
	StatusAllocated = "allocated"
	StatusRecycling = "recycling"

	// LastActivityAnnotation records the last time a pod was actively used (RFC3339).
	// Used during gateway recovery to distinguish orphaned pods from recently active ones.
	LastActivityAnnotation = "arl.infra.io/last-activity"

	// SessionAnnotation records the session ID that owns this pod.
	// Used during gateway recovery to rebuild in-memory session state.
	SessionAnnotation = "arl.infra.io/session"

	// OwnerKeyHashAnnotation records the hashed API key that owns a session.
	// This preserves ownership checks when sessions are recovered after restart.
	OwnerKeyHashAnnotation = "arl.infra.io/owner-key-hash"

	// ExperimentAnnotation records the managed experiment ID for recovery.
	ExperimentAnnotation = "arl.infra.io/experiment"

	// ManagedAnnotation marks sessions created through the managed-session API.
	ManagedAnnotation = "arl.infra.io/managed"

	// LastReleasedAnnotation records the last time a pod transitioned from
	// allocated back to idle (RFC3339). Used by LRU scale-down to delete the
	// least-recently-used idle pods first.
	LastReleasedAnnotation = "arl.infra.io/last-released"

	RoleLabelKey = "arl.infra.io/role"
	RolePrePull  = "pre-pull"
)
