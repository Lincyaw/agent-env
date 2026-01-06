package audit

import (
	"context"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
)

// NoOpWriter is a no-op implementation for when auditing is disabled
type NoOpWriter struct{}

// NewNoOpWriter creates a new no-op audit writer
func NewNoOpWriter() *NoOpWriter {
	return &NoOpWriter{}
}

// WriteTaskCompletion is a no-op
func (n *NoOpWriter) WriteTaskCompletion(_ context.Context, _ interfaces.TaskAuditRecord) error {
	return nil
}

// WriteSandboxEvent is a no-op
func (n *NoOpWriter) WriteSandboxEvent(_ context.Context, _ interfaces.SandboxAuditRecord) error {
	return nil
}

// Flush is a no-op
func (n *NoOpWriter) Flush(_ context.Context) error {
	return nil
}

// Close is a no-op
func (n *NoOpWriter) Close() error {
	return nil
}
