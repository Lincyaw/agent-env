package interfaces

import (
	"context"
)

// SidecarClient defines the interface for communicating with sidecar containers
type SidecarClient interface {
	// UpdateFiles sends file update request to sidecar
	UpdateFiles(ctx context.Context, podIP string, req FileUpdateRequest) (FileUpdateResponse, error)

	// Execute sends command execution request to sidecar
	Execute(ctx context.Context, podIP string, req ExecRequest) (ExecResponse, error)

	// Reset sends reset request to sidecar
	Reset(ctx context.Context, podIP string, req ResetRequest) (ResetResponse, error)

	// HealthCheck checks if sidecar is healthy
	HealthCheck(ctx context.Context, podIP string) error
}

// FileUpdateRequest represents a file update request
type FileUpdateRequest interface {
	GetBasePath() string
	GetFiles() map[string]string
	GetPatch() string
}

// FileUpdateResponse represents the response from file update
type FileUpdateResponse interface {
	IsSuccess() bool
	GetMessage() string
}

// ExecRequest represents a command execution request
type ExecRequest interface {
	GetCommand() []string
	GetEnv() map[string]string
	GetWorkingDir() string
	GetTimeout() int32
}

// ExecResponse represents the response from command execution
type ExecResponse interface {
	GetStdout() string
	GetStderr() string
	GetExitCode() int32
	IsDone() bool
}

// ResetRequest represents a reset request
type ResetRequest interface {
	ShouldPreserveFiles() bool
}

// ResetResponse represents the response from reset
type ResetResponse interface {
	IsSuccess() bool
	GetMessage() string
}
