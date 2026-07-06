package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
)

const (
	SessionModeDefault = ""
	SessionModeDevbox  = "devbox"
)

func validSessionMode(mode string) bool {
	switch mode {
	case SessionModeDefault, SessionModeDevbox:
		return true
	}
	return false
}

// ManagedSessionInfo extends SessionInfo with experiment metadata.
type ManagedSessionInfo struct {
	SessionInfo
	ExperimentID string `json:"experimentId"`
	Managed      bool   `json:"managed"`
}

// DevboxConfig holds devbox-specific session configuration.
type DevboxConfig struct {
	Ports         []DevboxPort `json:"ports,omitempty"`
	SSHPublicKeys []string     `json:"sshPublicKeys,omitempty"`
	GitConfig     *GitConfig   `json:"gitConfig,omitempty"`
	StorageSize   string       `json:"storageSize,omitempty"`
}

// DevboxPort describes a port to expose on the devbox container.
type DevboxPort struct {
	Port     int32  `json:"port"`
	Protocol string `json:"protocol,omitempty"`
	Name     string `json:"name,omitempty"`
}

// GitConfig holds git identity configuration for devbox sessions.
type GitConfig struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
}

// ConnectionInfo describes how to connect to a devbox session.
type ConnectionInfo struct {
	Shell string     `json:"shell"`
	SSH   *SSHInfo   `json:"ssh,omitempty"`
	Ports []PortInfo `json:"ports,omitempty"`
}

// SSHInfo describes SSH connection details.
type SSHInfo struct {
	Host string `json:"host"`
	Port int32  `json:"port"`
}

// PortInfo describes an exposed port on a session pod.
type PortInfo struct {
	Name          string `json:"name"`
	ContainerPort int32  `json:"containerPort"`
	Protocol      string `json:"protocol"`
}

// --- Request types ---

// CreateSessionRequest is the body for POST /v1/sessions
type CreateSessionRequest struct {
	Image              string                 `json:"image,omitempty"`
	Profile            string                 `json:"profile,omitempty"`
	Namespace          string                 `json:"namespace,omitempty"`
	Mode               string                 `json:"mode,omitempty"`
	Devbox             *DevboxConfig          `json:"devbox,omitempty"`
	ConfigEnv          json.RawMessage        `json:"configEnv,omitempty"`
	IdleTimeoutSeconds int                    `json:"idleTimeoutSeconds,omitempty"`
	PrivateContainers  []PrivateContainerSpec `json:"privateContainers,omitempty"`
	PoolName           string                 `json:"-"` // internal pinned SandboxWarmPool, not part of the public API
	ExtraLabels        map[string]string      `json:"-"` // internal use only, not exposed via JSON
	Managed            bool                   `json:"-"`
	ExperimentID       string                 `json:"-"`
}

func hasJSONPayload(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func decodeConfigEnv(configEnv json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(configEnv)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}

	var spec any
	if err := json.Unmarshal(trimmed, &spec); err != nil {
		return nil, fmt.Errorf("decode configEnv: %w", err)
	}
	normalized, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("normalize configEnv: %w", err)
	}
	return normalized, nil
}

// CreateManagedSessionRequest is the body for POST /v1/managed/sessions
type CreateManagedSessionRequest struct {
	Image              string                       `json:"image"`
	Profile            string                       `json:"profile,omitempty"`
	ExperimentID       string                       `json:"experimentId"`
	Namespace          string                       `json:"namespace,omitempty"`
	Mode               string                       `json:"mode,omitempty"`
	Devbox             *DevboxConfig                `json:"devbox,omitempty"`
	ConfigEnv          json.RawMessage              `json:"configEnv,omitempty"`
	Resources          *corev1.ResourceRequirements `json:"resources,omitempty"`
	Tools              json.RawMessage              `json:"tools,omitempty"`
	WorkspaceDir       string                       `json:"workspaceDir,omitempty"`
	IdleTimeoutSeconds int                          `json:"idleTimeoutSeconds,omitempty"`
	PrivateContainers  []PrivateContainerSpec       `json:"privateContainers,omitempty"`
}

// ExecuteRequest is the body for POST /v1/sessions/{id}/execute
type ExecuteRequest struct {
	Steps       []StepRequest `json:"steps"`
	TraceID     string        `json:"traceID,omitempty"`
	OperationID string        `json:"operationID,omitempty"`
}

// StepRequest describes a single execution step
type StepRequest struct {
	Name           string            `json:"name"`
	Command        []string          `json:"command,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	WorkDir        string            `json:"workDir,omitempty"`
	TimeoutSeconds int32             `json:"timeoutSeconds,omitempty"`
	Timeout        int32             `json:"timeout,omitempty"`
}

// PrivateContainerSpec describes a gateway-managed container that is not part
// of the agent-facing executor environment.
type PrivateContainerSpec struct {
	Name               string                       `json:"name"`
	Image              string                       `json:"image"`
	MountWorkspace     bool                         `json:"mountWorkspace,omitempty"`
	WorkspaceMountPath string                       `json:"workspaceMountPath,omitempty"`
	WorkspaceAccess    string                       `json:"workspaceAccess,omitempty"`
	Command            []string                     `json:"command,omitempty"`
	Args               []string                     `json:"args,omitempty"`
	Env                map[string]string            `json:"env,omitempty"`
	Resources          *corev1.ResourceRequirements `json:"resources,omitempty"`
	ImagePullPolicy    string                       `json:"imagePullPolicy,omitempty"`
}

// ContainerExecuteRequest is the body for running steps in a private container.
type ContainerExecuteRequest struct {
	Steps []StepRequest `json:"steps"`
}

// UploadFileResponse is the response for PUT /v1/sessions/{id}/files/{path...}
type UploadFileResponse struct {
	Path         string `json:"path"`
	BytesWritten int    `json:"bytesWritten"`
	SHA256       string `json:"sha256,omitempty"`
}

// RestoreRequest is the body for POST /v1/sessions/{id}/restore
type RestoreRequest struct {
	SnapshotID string `json:"snapshotID"`
}

// ReplayRequest is the body for POST /v1/sessions/{id}/replay
type ReplayRequest struct {
	SourceSessionID string `json:"sourceSessionID"`
	UpToStep        *int   `json:"upToStep,omitempty"`
}

// ReplayResponse is the response for POST /v1/sessions/{id}/replay
type ReplayResponse struct {
	StepsReplayed int `json:"stepsReplayed"`
	Errors        int `json:"errors"`
}

// CreatePoolRequest is the body for POST /v1/pools
type CreatePoolRequest struct {
	Name              string                       `json:"name"`
	Image             string                       `json:"image"`
	Profile           string                       `json:"profile,omitempty"`
	Replicas          int32                        `json:"replicas,omitempty"`
	Namespace         string                       `json:"namespace,omitempty"`
	ConfigEnv         json.RawMessage              `json:"configEnv,omitempty"`
	Tools             json.RawMessage              `json:"tools,omitempty"`
	Resources         *corev1.ResourceRequirements `json:"resources,omitempty"`
	WorkspaceDir      string                       `json:"workspaceDir,omitempty"`
	ImageLocality     json.RawMessage              `json:"imageLocality,omitempty"`
	PrivateContainers []PrivateContainerSpec       `json:"privateContainers,omitempty"`
	Prefetch          bool                         `json:"prefetch,omitempty"`
	Managed           bool                         `json:"-"`
}

// PrefetchPoolRequest is the body for POST /v1/pools/{name}/prefetch
type PrefetchPoolRequest struct {
	Namespace string `json:"namespace,omitempty"`
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
	ID             string          `json:"id"`
	SandboxName    string          `json:"sandboxName"`
	Namespace      string          `json:"namespace"`
	Image          string          `json:"image,omitempty"`
	Profile        string          `json:"profile,omitempty"`
	Mode           string          `json:"mode,omitempty"`
	PoolRef        string          `json:"-"`
	PodIP          string          `json:"podIP"`
	PodName        string          `json:"podName"`
	CreatedAt      time.Time       `json:"createdAt"`
	Status         string          `json:"status,omitempty"`
	DeletedAt      *time.Time      `json:"deletedAt,omitempty"`
	DeletionReason string          `json:"deletionReason,omitempty"`
	ConnectionInfo *ConnectionInfo `json:"connectionInfo,omitempty"`
}

// ExecuteResponse is the response for POST /v1/sessions/{id}/execute
type ExecuteResponse struct {
	SessionID       string       `json:"sessionID"`
	Results         []StepResult `json:"results"`
	TotalDurationMs int64        `json:"totalDurationMs"`
	OperationID     string       `json:"operationID,omitempty"`
}

// ContainerExecuteResponse is returned from private container execution.
type ContainerExecuteResponse struct {
	SessionID       string       `json:"sessionID"`
	Container       string       `json:"container"`
	Results         []StepResult `json:"results"`
	TotalDurationMs int64        `json:"totalDurationMs"`
}

// ExecuteOperationInfo describes an idempotent execute operation.
type ExecuteOperationInfo struct {
	OperationID string           `json:"operationID"`
	SessionID   string           `json:"sessionID"`
	Status      string           `json:"status"`
	Result      *ExecuteResponse `json:"result,omitempty"`
	Error       string           `json:"error,omitempty"`
	CreatedAt   time.Time        `json:"createdAt"`
	StartedAt   time.Time        `json:"startedAt,omitempty"`
	FinishedAt  *time.Time       `json:"finishedAt,omitempty"`
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
	Profile           string          `json:"profile,omitempty"`
	Image             string          `json:"image,omitempty"`
	Replicas          int32           `json:"replicas"`
	ReadyReplicas     int32           `json:"readyReplicas"`
	AllocatedReplicas int32           `json:"allocatedReplicas"`
	State             string          `json:"state,omitempty"`
	CreatedAt         time.Time       `json:"createdAt,omitempty"`
	Conditions        []PoolCondition `json:"conditions,omitempty"`
}

// PoolCondition is a simplified condition for API consumers
type PoolCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// SessionListItem extends SessionInfo for list responses.
type SessionListItem struct {
	SessionInfo
	Managed      bool   `json:"managed,omitempty"`
	ExperimentID string `json:"experimentId,omitempty"`
}

// ExperimentSummary describes an experiment with aggregate info.
type ExperimentSummary struct {
	ExperimentID string `json:"experimentId"`
	SessionCount int    `json:"sessionCount"`
	Image        string `json:"image,omitempty"`
	Profile      string `json:"profile,omitempty"`
	Namespace    string `json:"namespace,omitempty"`
}

// ErrorResponse is a generic error response
type ErrorResponse struct {
	Error  string `json:"error"`
	Detail string `json:"detail,omitempty"`
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
