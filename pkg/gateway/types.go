package gateway

import (
	"encoding/json"
	"time"

	corev1 "k8s.io/api/core/v1"

	arlv1alpha1 "github.com/Lincyaw/agent-env/api/v1alpha1"
)

// --- Request types ---

// CreateSessionRequest is the body for POST /v1/sessions
type CreateSessionRequest struct {
	PoolRef            string `json:"poolRef"`
	Namespace          string `json:"namespace,omitempty"`
	KeepAlive          bool   `json:"keepAlive,omitempty"`
	IdleTimeoutSeconds int    `json:"idleTimeoutSeconds,omitempty"`
}

// ExecuteRequest is the body for POST /v1/sessions/{id}/execute
type ExecuteRequest struct {
	Steps   []StepRequest `json:"steps"`
	TraceID string        `json:"traceID,omitempty"`
}

// StepRequest describes a single execution step
type StepRequest struct {
	Name    string            `json:"name"`
	Command []string          `json:"command,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	WorkDir string            `json:"workDir,omitempty"`
}

// RestoreRequest is the body for POST /v1/sessions/{id}/restore
type RestoreRequest struct {
	SnapshotID string `json:"snapshotID"`
}

// CreatePoolRequest is the body for POST /v1/pools
type CreatePoolRequest struct {
	Name         string                       `json:"name"`
	Image        string                       `json:"image"`
	Replicas     int32                        `json:"replicas,omitempty"`
	Namespace    string                       `json:"namespace,omitempty"`
	Tools        *arlv1alpha1.ToolsSpec       `json:"tools,omitempty"`
	Resources    *corev1.ResourceRequirements `json:"resources,omitempty"`
	WorkspaceDir string                       `json:"workspaceDir,omitempty"`
}

// ScalePoolRequest is the body for PATCH /v1/pools/{name}
type ScalePoolRequest struct {
	Replicas  int32                        `json:"replicas"`
	Namespace string                       `json:"namespace,omitempty"`
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// --- Response types ---

// SessionInfo describes a session
type SessionInfo struct {
	ID          string    `json:"id"`
	SandboxName string    `json:"sandboxName"`
	Namespace   string    `json:"namespace"`
	PoolRef     string    `json:"poolRef"`
	PodIP       string    `json:"podIP"`
	PodName     string    `json:"podName"`
	CreatedAt   time.Time `json:"createdAt"`
}

// ExecuteResponse is the response for POST /v1/sessions/{id}/execute
type ExecuteResponse struct {
	SessionID       string       `json:"sessionID"`
	Results         []StepResult `json:"results"`
	TotalDurationMs int64        `json:"totalDurationMs"`
}

// StepOutput is the output of an execution step
type StepOutput struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int32  `json:"exit_code"`
}

// StepResult describes the result of one step
type StepResult struct {
	Index      int             `json:"index"`
	Name       string          `json:"name"`
	Output     StepOutput      `json:"output"`
	SnapshotID string          `json:"snapshot_id"`
	DurationMs int64           `json:"duration_ms"`
	Timestamp  time.Time       `json:"timestamp"`
	Input      json.RawMessage `json:"input"`
}

// PoolInfo describes a warm pool
type PoolInfo struct {
	Name              string          `json:"name"`
	Namespace         string          `json:"namespace"`
	Replicas          int32           `json:"replicas"`
	ReadyReplicas     int32           `json:"readyReplicas"`
	AllocatedReplicas int32           `json:"allocatedReplicas"`
	Conditions        []PoolCondition `json:"conditions,omitempty"`
}

// PoolCondition is a simplified condition for API consumers
type PoolCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// ErrorResponse is a generic error response
type ErrorResponse struct {
	Error string `json:"error"`
}

// TrajectoryEntry is a single entry in JSONL trajectory export
type TrajectoryEntry struct {
	SessionID   string          `json:"session_id"`
	Step        int             `json:"step"`
	Action      json.RawMessage `json:"action"`
	Observation json.RawMessage `json:"observation"`
	SnapshotID  string          `json:"snapshot_id"`
	Timestamp   time.Time       `json:"timestamp"`
}
