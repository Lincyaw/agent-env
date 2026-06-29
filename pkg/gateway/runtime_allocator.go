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
	PoolRef     string
	Namespace   string
	SessionID   string
	SandboxName string
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
	Touch(ctx context.Context, allocation RuntimeAllocation, sessionID string, at time.Time) error
	DiagnosticStats() map[string]AllocatorPoolStats
}
