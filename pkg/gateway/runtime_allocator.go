package gateway

import (
	"context"
	"time"
)

// RuntimeAllocation is the gateway's durable binding to an execution runtime.
// Legacy deployments bind directly to a Pod. SandboxClaim-based deployments
// additionally populate ClaimName and SandboxName.
type RuntimeAllocation struct {
	Backend     string `json:"backend,omitempty"`
	PoolRef     string `json:"poolRef,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
	PodName     string `json:"podName,omitempty"`
	PodIP       string `json:"podIP,omitempty"`
	ClaimName   string `json:"claimName,omitempty"`
	SandboxName string `json:"sandboxName,omitempty"`
}

// RuntimeAllocateRequest describes one session allocation request.
type RuntimeAllocateRequest struct {
	PoolRef              string
	Namespace            string
	SessionID            string
	SandboxName          string
	OwnerKeyHash         string
	Managed              bool
	ExperimentID         string
	Mode                 string
	Lifecycle            RuntimeLifecycle
	Env                  []RuntimeEnvVar
	VolumeClaimTemplates []RuntimeVolumeClaimTemplate
}

// RuntimeEnvVar is a session-scoped environment variable request.
type RuntimeEnvVar struct {
	Name          string
	Value         string
	ContainerName string
}

// RuntimeVolumeClaimTemplate is a per-session PVC request injected into the SandboxClaim.
type RuntimeVolumeClaimTemplate struct {
	Name        string
	StorageSize string
	AccessMode  string
}

// RuntimeLifecycle describes the claim-level lifecycle mirror for a session.
// Redis/session storage is the hot path for activity; this low-frequency
// lifecycle is the Kubernetes-side fallback when the gateway is unavailable.
type RuntimeLifecycle struct {
	CreatedAt      time.Time
	LastActivityAt time.Time
	IdleTimeout    time.Duration
	MaxLifetime    time.Duration
	FinishedTTL    time.Duration
}

// AllocatorPoolStats holds diagnostic statistics for a pool allocator.
type AllocatorPoolStats struct {
	IdleCount   int `json:"idleCount"`
	WaiterCount int `json:"waiterCount"`
}

// RuntimeAllocator binds a session to an agent-sandbox SandboxClaim/Sandbox runtime.
type RuntimeAllocator interface {
	Start(ctx context.Context) error
	Stop()
	Allocate(ctx context.Context, req RuntimeAllocateRequest) (*RuntimeAllocation, error)
	Release(ctx context.Context, allocation RuntimeAllocation) error
	Resolve(ctx context.Context, allocation RuntimeAllocation, sessionID string) (*RuntimeAllocation, error)
	Touch(ctx context.Context, allocation RuntimeAllocation, sessionID string, at time.Time, lifecycle RuntimeLifecycle) error
	DiagnosticStats() map[string]AllocatorPoolStats
}
