// Copyright 2024 ARL-Infra Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package interfaces

import (
	"context"
	"time"
)

// AuditWriter defines the interface for writing audit logs
type AuditWriter interface {
	// WriteTaskCompletion writes a task completion audit record
	WriteTaskCompletion(ctx context.Context, record TaskAuditRecord) error

	// WriteSandboxEvent writes a sandbox lifecycle event audit record
	WriteSandboxEvent(ctx context.Context, record SandboxAuditRecord) error

	// Flush flushes any buffered audit records
	Flush(ctx context.Context) error

	// Close closes the audit writer
	Close() error
}

// TaskAuditRecord represents a task audit log entry
type TaskAuditRecord struct {
	TraceID        string
	Namespace      string
	Name           string
	SandboxRef     string
	State          string
	ExitCode       int32
	Duration       string
	StartTime      time.Time
	CompletionTime time.Time
	StepCount      int
	// Input is the JSON-serialized task steps (input commands/content)
	Input string
	// Stdout is the standard output from task execution
	Stdout string
	// Stderr is the standard error output from task execution
	Stderr string
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

func (n *NoOpAuditWriter) WriteTaskCompletion(_ context.Context, _ TaskAuditRecord) error {
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
