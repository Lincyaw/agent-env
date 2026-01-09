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
