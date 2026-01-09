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
