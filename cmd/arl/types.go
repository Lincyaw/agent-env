package main

import (
	"encoding/json"
	"time"
)

type SessionInfo struct {
	ID          string    `json:"id"`
	SandboxName string    `json:"sandboxName"`
	Namespace   string    `json:"namespace"`
	Image       string    `json:"image,omitempty"`
	Profile     string    `json:"profile,omitempty"`
	PodIP       string    `json:"podIP"`
	PodName     string    `json:"podName"`
	CreatedAt   time.Time `json:"createdAt"`
}

type SessionListItem struct {
	SessionInfo
	Managed      bool   `json:"managed,omitempty"`
	ExperimentID string `json:"experimentId,omitempty"`
}

type CreateSessionRequest struct {
	Image              string                 `json:"image,omitempty"`
	Profile            string                 `json:"profile,omitempty"`
	IdleTimeoutSeconds int                    `json:"idleTimeoutSeconds,omitempty"`
	MaxLifetimeSeconds int                    `json:"maxLifetimeSeconds,omitempty"`
	PrivateContainers  []PrivateContainerSpec `json:"privateContainers,omitempty"`
}

type ManagedSessionInfo struct {
	SessionInfo
	ExperimentID string `json:"experimentId"`
	Managed      bool   `json:"managed"`
}

type PoolInfo struct {
	Name              string          `json:"name"`
	Namespace         string          `json:"namespace"`
	Image             string          `json:"image,omitempty"`
	Profile           string          `json:"profile,omitempty"`
	Replicas          int32           `json:"replicas"`
	ReadyReplicas     int32           `json:"readyReplicas"`
	AllocatedReplicas int32           `json:"allocatedReplicas"`
	CreatedAt         time.Time       `json:"createdAt,omitempty"`
	Conditions        []PoolCondition `json:"conditions,omitempty"`
}

type PoolCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

type ExperimentSummary struct {
	ExperimentID string `json:"experimentId"`
	SessionCount int    `json:"sessionCount"`
	Image        string `json:"image,omitempty"`
	Profile      string `json:"profile,omitempty"`
	Namespace    string `json:"namespace,omitempty"`
}

type StepRecord struct {
	Index      int             `json:"index"`
	Name       string          `json:"name"`
	Input      json.RawMessage `json:"input"`
	Output     StepOutput      `json:"output"`
	DurationMs int64           `json:"duration_ms"`
	Timestamp  time.Time       `json:"timestamp"`
}

type StepOutput struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int32  `json:"exit_code"`
}

type UploadFileResponse struct {
	Path         string `json:"path"`
	BytesWritten int    `json:"bytesWritten"`
	SHA256       string `json:"sha256,omitempty"`
}

type ExecuteRequest struct {
	Steps       []StepRequest `json:"steps"`
	OperationID string        `json:"operationID,omitempty"`
}

type StepRequest struct {
	Name           string            `json:"name"`
	Command        []string          `json:"command,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	WorkDir        string            `json:"workDir,omitempty"`
	TimeoutSeconds int32             `json:"timeoutSeconds,omitempty"`
}

type ExecuteResponse struct {
	SessionID       string       `json:"sessionID"`
	Results         []StepResult `json:"results"`
	TotalDurationMs int64        `json:"totalDurationMs"`
	OperationID     string       `json:"operationID,omitempty"`
}

type ContainerExecuteRequest struct {
	Steps []StepRequest `json:"steps"`
}

type ContainerExecuteResponse struct {
	SessionID       string       `json:"sessionID"`
	Container       string       `json:"container"`
	Results         []StepResult `json:"results"`
	TotalDurationMs int64        `json:"totalDurationMs"`
}

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

type StepResult struct {
	Index      int             `json:"index"`
	Name       string          `json:"name"`
	Output     StepOutput      `json:"output"`
	SnapshotID string          `json:"snapshot_id"`
	DurationMs int64           `json:"duration_ms"`
	Timestamp  time.Time       `json:"timestamp"`
	Input      json.RawMessage `json:"input"`
}

type RestoreRequest struct {
	SnapshotID string `json:"snapshotID"`
}

type ReplayRequest struct {
	SourceSessionID string `json:"sourceSessionID"`
	UpToStep        *int   `json:"upToStep,omitempty"`
}

type ReplayResponse struct {
	StepsReplayed int `json:"stepsReplayed"`
	Errors        int `json:"errors"`
}

type ScalePoolRequest struct {
	Replicas int32 `json:"replicas"`
}

type CreatePoolRequest struct {
	Name              string                 `json:"name"`
	Image             string                 `json:"image"`
	Profile           string                 `json:"profile,omitempty"`
	Replicas          int32                  `json:"replicas,omitempty"`
	WorkspaceDir      string                 `json:"workspaceDir,omitempty"`
	PrivateContainers []PrivateContainerSpec `json:"privateContainers,omitempty"`
}

type CreateManagedSessionRequest struct {
	Image              string                 `json:"image"`
	Profile            string                 `json:"profile,omitempty"`
	ExperimentID       string                 `json:"experimentId"`
	WorkspaceDir       string                 `json:"workspaceDir,omitempty"`
	IdleTimeoutSeconds int                    `json:"idleTimeoutSeconds,omitempty"`
	MaxLifetimeSeconds int                    `json:"maxLifetimeSeconds,omitempty"`
	PrivateContainers  []PrivateContainerSpec `json:"privateContainers,omitempty"`
}

type PrivateContainerSpec struct {
	Name               string            `json:"name"`
	Image              string            `json:"image"`
	MountWorkspace     bool              `json:"mountWorkspace,omitempty"`
	WorkspaceMountPath string            `json:"workspaceMountPath,omitempty"`
	WorkspaceAccess    string            `json:"workspaceAccess,omitempty"`
	Command            []string          `json:"command,omitempty"`
	Args               []string          `json:"args,omitempty"`
	Env                map[string]string `json:"env,omitempty"`
	Resources          json.RawMessage   `json:"resources,omitempty"`
	ImagePullPolicy    string            `json:"imagePullPolicy,omitempty"`
}

type ErrorResponse struct {
	Error  string `json:"error"`
	Detail string `json:"detail,omitempty"`
}
