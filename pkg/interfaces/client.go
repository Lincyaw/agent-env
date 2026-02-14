package interfaces

import (
	"context"
)

// SidecarClient defines the interface for communicating with sidecar containers
type SidecarClient interface {
	// Execute sends command execution request to sidecar and returns aggregated result
	Execute(ctx context.Context, podIP string, req ExecRequest) (ExecResponse, error)

	// ExecuteStream sends command execution request and streams output via channel
	ExecuteStream(ctx context.Context, podIP string, req ExecRequest) (<-chan ExecResponse, error)

	// InteractiveShell opens a bidirectional shell session
	InteractiveShell(ctx context.Context, podIP string) (ShellStream, error)

	// HealthCheck checks if sidecar is healthy
	HealthCheck(ctx context.Context, podIP string) error

	// Close cleans up any resources (e.g., gRPC connections)
	Close() error
}

// ShellStream represents a bidirectional interactive shell session
type ShellStream interface {
	// Send sends input to the shell
	Send(input ShellInput) error
	// Recv receives output from the shell (blocks until data available)
	Recv() (ShellOutput, error)
	// Close closes the shell stream
	Close() error
}

// ShellInput represents input to a shell session
type ShellInput struct {
	Data   string // stdin data
	Signal string // signal name (e.g., "SIGINT")
	Resize bool   // terminal resize event
	Rows   int32  // terminal rows
	Cols   int32  // terminal columns
}

// ShellOutput represents output from a shell session
type ShellOutput struct {
	Data     string // stdout/stderr data
	ExitCode int32  // exit code (when closed)
	Closed   bool   // true when shell has ended
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
