package interfaces

import (
	"context"
	"io"
	"time"
)

// FileTransferChunkSize is the standard chunk size for streaming file operations.
const FileTransferChunkSize = 1024 * 1024

// LogEntry represents a single log line from the sidecar.
type LogEntry struct {
	Timestamp string
	Level     string
	Message   string
	Source    string
}

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
	Size     int64
	Mode     string
	Modified time.Time
}

// DirEntry describes a single entry in a directory listing.
type DirEntry struct {
	Name  string
	IsDir bool
	Size  int64
}

// ListDirResult describes the result of a directory listing.
type ListDirResult struct {
	Entries []DirEntry
}

// SidecarClient defines the interface for communicating with sidecar containers
type SidecarClient interface {
	// Execute sends command execution request to sidecar and returns aggregated result
	Execute(ctx context.Context, podIP string, req ExecRequest) (ExecResponse, error)

	// ExecuteStream sends command execution request and streams output via channel
	ExecuteStream(ctx context.Context, podIP string, req ExecRequest) (<-chan ExecResponse, error)

	// WriteFile streams one file into the session workspace.
	WriteFile(ctx context.Context, podIP string, path string, content io.Reader, expectedSHA256 string) (*FileWriteResult, error)

	// ReadFile streams one file from the session workspace.
	ReadFile(ctx context.Context, podIP string, path string, dst io.Writer) (*FileReadResult, error)

	// Stat returns file metadata without downloading the file content.
	Stat(ctx context.Context, podIP string, path string) (*StatResult, error)

	// ListDir lists directory contents. When recursive is true, entries
	// include paths relative to the listed directory.
	ListDir(ctx context.Context, podIP string, path string, recursive bool) (*ListDirResult, error)

	// WriteStdin sends data to the stdin of a running process identified by handle.
	WriteStdin(ctx context.Context, podIP string, handle string, data string) error

	// InteractiveShell opens a bidirectional shell session
	InteractiveShell(ctx context.Context, podIP string) (ShellStream, error)

	// StreamLogs streams log entries from the sidecar ring buffer.
	StreamLogs(ctx context.Context, podIP string, follow bool, tailLines int32) (<-chan LogEntry, error)

	// HealthCheck checks if sidecar is healthy
	HealthCheck(ctx context.Context, podIP string) error

	// CloseConnection closes and removes a single gRPC connection by pod IP
	CloseConnection(podIP string) error

	// CleanupStale removes connections in Shutdown or TransientFailure state
	CleanupStale() int

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
