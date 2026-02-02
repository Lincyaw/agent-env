package interfaces

import (
	"context"
	"io"
)

// SidecarClient defines the interface for communicating with sidecar containers
type SidecarClient interface {
	// UpdateFiles sends file update request to sidecar
	UpdateFiles(ctx context.Context, podIP string, req FileUpdateRequest) (FileUpdateResponse, error)

	// Execute sends command execution request to sidecar and returns aggregated result
	Execute(ctx context.Context, podIP string, req ExecRequest) (ExecResponse, error)

	// ExecuteStream sends command execution request and streams output via channel
	ExecuteStream(ctx context.Context, podIP string, req ExecRequest) (<-chan ExecResponse, error)

	// Reset sends reset request to sidecar
	Reset(ctx context.Context, podIP string, req ResetRequest) (ResetResponse, error)

	// HealthCheck checks if sidecar is healthy
	HealthCheck(ctx context.Context, podIP string) error

	// Close cleans up any resources (e.g., gRPC connections)
	Close() error
}

// PodExecClient defines the interface for executing commands in pod containers via kubectl exec
type PodExecClient interface {
	// Execute runs a command in the specified container of a pod
	Execute(ctx context.Context, namespace, podName, container string, req ExecRequest) (ExecResponse, error)

	// InteractiveShell starts an interactive shell session in the specified container
	// stdin: input stream to send to the shell
	// stdout: output stream to receive from the shell
	// stderr: error stream to receive from the shell
	// resize: optional channel to send terminal size changes
	InteractiveShell(ctx context.Context, namespace, podName, container string,
		stdin io.Reader, stdout, stderr io.Writer,
		resize <-chan TerminalSize) error
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

// TerminalSize represents the dimensions of a terminal
type TerminalSize struct {
	Width  uint16
	Height uint16
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
