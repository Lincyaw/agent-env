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

	// IdleTimeoutAnnotation records the per-session idle timeout in seconds.
	// It is stored on SandboxClaims as low-frequency lifecycle metadata; Redis
	// remains the hot-path source for frequent activity updates.
	IdleTimeoutAnnotation = "arl.infra.io/idle-timeout-seconds"

	// MaxLifetimeAnnotation records the per-session maximum lifetime in seconds.
	MaxLifetimeAnnotation = "arl.infra.io/max-lifetime-seconds"

	// FinishedTTLAnnotation records how long terminal runtimes should be kept
	// before they are eligible for deletion.
	FinishedTTLAnnotation = "arl.infra.io/finished-ttl-seconds"

	// ManagedPoolAnnotation marks gateway-created image-backed pools that may
	// be deleted automatically once no session or claim references them.
	ManagedPoolAnnotation = "arl.infra.io/managed-pool"

	RoleLabelKey = "arl.infra.io/role"
	RolePrePull  = "pre-pull"
)
