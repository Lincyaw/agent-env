package interfaces

import (
	"context"
	"time"
)

// AuditWriter defines the interface for writing audit logs
type AuditWriter interface {
	// WriteSessionStep writes a session step audit record (replaces task completion)
	WriteSessionStep(ctx context.Context, record SessionStepAuditRecord) error

	// WriteSandboxEvent writes a sandbox lifecycle event audit record
	WriteSandboxEvent(ctx context.Context, record SandboxAuditRecord) error

	// Flush flushes any buffered audit records
	Flush(ctx context.Context) error

	// Close closes the audit writer
	Close() error
}

// SessionStepAuditRecord represents a session step audit log entry (replaces TaskAuditRecord)
type SessionStepAuditRecord struct {
	SessionID  string
	TraceID    string
	Namespace  string
	StepIndex  int
	StepName   string
	StepType   string
	Input      string
	Stdout     string
	Stderr     string
	ExitCode   int32
	SnapshotID string
	DurationMs int64
	Timestamp  time.Time
}

// SandboxAuditRecord represents a sandbox audit log entry
type SandboxAuditRecord struct {
	TraceID   string
	Namespace string
	Name      string
	PoolRef   string
	Phase     string
	PodName   string
	Event     string
}

// NoOpAuditWriter is a no-op implementation for when auditing is disabled
type NoOpAuditWriter struct{}

func (n *NoOpAuditWriter) WriteSessionStep(_ context.Context, _ SessionStepAuditRecord) error {
	return nil
}

func (n *NoOpAuditWriter) WriteSandboxEvent(_ context.Context, _ SandboxAuditRecord) error {
	return nil
}

func (n *NoOpAuditWriter) Flush(_ context.Context) error {
	return nil
}

func (n *NoOpAuditWriter) Close() error {
	return nil
}
