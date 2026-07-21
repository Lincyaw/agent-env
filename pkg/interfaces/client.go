package interfaces

import (
	"context"
	"io"
)

// FileTransferChunkSize is the standard chunk size for streaming file operations.
const FileTransferChunkSize = 1024 * 1024

// FileWriteResult describes a streamed file upload result.
type FileWriteResult struct {
	Path         string
	BytesWritten int64
	SHA256       string
}

// FileReadResult describes a streamed file download result.
type FileReadResult struct {
	Path      string
	SizeBytes int64
	SHA256    string
}

// StatResult describes file metadata returned by Stat.
type StatResult struct {
	Exists   bool
	IsDir    bool
	Size     uint64
	Mode     string
	Modified string
}

// DirEntry describes a single directory entry.
type DirEntry struct {
	Name  string
	IsDir bool
	Size  uint64
}

// LogEntry represents a single log line.
type LogEntry struct {
	Timestamp string
	Level     string
	Message   string
	Source    string
}

// ExecutorClient defines the interface for communicating with executor agents.
type ExecutorClient interface {
	// Execute sends command execution request to executor and returns aggregated result
	Execute(ctx context.Context, podIP string, req *ExecRequest) (*ExecResponse, error)

	// ExecuteStream sends command execution request and streams output via channel
	ExecuteStream(ctx context.Context, podIP string, req *ExecRequest) (<-chan ExecResponse, error)

	// WriteFile streams one file into the container filesystem.
	WriteFile(ctx context.Context, podIP string, path string, content io.Reader, expectedSHA256 string) (*FileWriteResult, error)

	// ReadFile streams one file from the container filesystem.
	ReadFile(ctx context.Context, podIP string, path string, dst io.Writer) (*FileReadResult, error)

	// InteractiveShell opens a bidirectional shell session
	InteractiveShell(ctx context.Context, podIP string) (ShellStream, error)

	// GetIrohAddr returns the iroh endpoint address from the executor.
	// Returns empty string if iroh is not configured.
	GetIrohAddr(ctx context.Context, podIP string) (string, error)

	// DownloadCheckpoint downloads a combined checkpoint tar for steps 1..through.
	DownloadCheckpoint(ctx context.Context, podIP string, through int, dst io.Writer) error

	// ListCheckpointSteps lists available checkpoint step numbers.
	ListCheckpointSteps(ctx context.Context, podIP string) ([]int, error)

	// HealthCheck checks if executor is healthy
	HealthCheck(ctx context.Context, podIP string) error

	// CloseConnection closes and removes a single connection by pod IP
	CloseConnection(podIP string) error

	// CleanupStale removes connections that are no longer usable
	CleanupStale() int

	// Close cleans up any resources (e.g., TCP connections)
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

// ExecRequest represents a command execution request.
type ExecRequest struct {
	Command        []string
	Env            map[string]string
	WorkingDir     string
	TimeoutSeconds int32
}

// ExecResponse represents the response from command execution.
type ExecResponse struct {
	Stdout   string
	Stderr   string
	ExitCode int32
	Done     bool
}
