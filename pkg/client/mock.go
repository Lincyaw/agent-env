package client

import (
	"context"
	"fmt"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
)

// MockSidecarClient is a mock implementation for testing
type MockSidecarClient struct {
	UpdateFilesFunc   func(ctx context.Context, podIP string, req interfaces.FileUpdateRequest) (interfaces.FileUpdateResponse, error)
	ExecuteFunc       func(ctx context.Context, podIP string, req interfaces.ExecRequest) (interfaces.ExecResponse, error)
	ExecuteStreamFunc func(ctx context.Context, podIP string, req interfaces.ExecRequest) (<-chan interfaces.ExecResponse, error)
	ResetFunc         func(ctx context.Context, podIP string, req interfaces.ResetRequest) (interfaces.ResetResponse, error)
	HealthCheckFunc   func(ctx context.Context, podIP string) error
}

// UpdateFiles mocks file update
func (m *MockSidecarClient) UpdateFiles(ctx context.Context, podIP string, req interfaces.FileUpdateRequest) (interfaces.FileUpdateResponse, error) {
	if m.UpdateFilesFunc != nil {
		return m.UpdateFilesFunc(ctx, podIP, req)
	}
	return nil, fmt.Errorf("not implemented")
}

// Execute mocks command execution
func (m *MockSidecarClient) Execute(ctx context.Context, podIP string, req interfaces.ExecRequest) (interfaces.ExecResponse, error) {
	if m.ExecuteFunc != nil {
		return m.ExecuteFunc(ctx, podIP, req)
	}
	return nil, fmt.Errorf("not implemented")
}

// ExecuteStream mocks streaming command execution
func (m *MockSidecarClient) ExecuteStream(ctx context.Context, podIP string, req interfaces.ExecRequest) (<-chan interfaces.ExecResponse, error) {
	if m.ExecuteStreamFunc != nil {
		return m.ExecuteStreamFunc(ctx, podIP, req)
	}
	return nil, fmt.Errorf("not implemented")
}

// Reset mocks reset
func (m *MockSidecarClient) Reset(ctx context.Context, podIP string, req interfaces.ResetRequest) (interfaces.ResetResponse, error) {
	if m.ResetFunc != nil {
		return m.ResetFunc(ctx, podIP, req)
	}
	return nil, fmt.Errorf("not implemented")
}

// HealthCheck mocks health check
func (m *MockSidecarClient) HealthCheck(ctx context.Context, podIP string) error {
	if m.HealthCheckFunc != nil {
		return m.HealthCheckFunc(ctx, podIP)
	}
	return nil
}

// Close mocks cleanup
func (m *MockSidecarClient) Close() error {
	return nil
}
