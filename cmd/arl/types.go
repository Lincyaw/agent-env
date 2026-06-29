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

type ExecuteRequest struct {
	Steps []StepRequest `json:"steps"`
}

type StepRequest struct {
	Name    string            `json:"name"`
	Command []string          `json:"command,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	WorkDir string            `json:"workDir,omitempty"`
}

type ExecuteResponse struct {
	SessionID       string       `json:"sessionID"`
	Results         []StepResult `json:"results"`
	TotalDurationMs int64        `json:"totalDurationMs"`
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

type ScalePoolRequest struct {
	Replicas  int32  `json:"replicas"`
	Namespace string `json:"namespace,omitempty"`
}

type CreatePoolRequest struct {
	Name      string `json:"name"`
	Image     string `json:"image"`
	Profile   string `json:"profile,omitempty"`
	Replicas  int32  `json:"replicas,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

type CreateManagedSessionRequest struct {
	Image        string `json:"image"`
	Profile      string `json:"profile,omitempty"`
	ExperimentID string `json:"experimentId"`
	Namespace    string `json:"namespace,omitempty"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
