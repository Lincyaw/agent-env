package client

import (
	"context"
	"fmt"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
)

// MockSidecarClient is a mock implementation for testing
type MockSidecarClient struct {
	ExecuteFunc       func(ctx context.Context, podIP string, req interfaces.ExecRequest) (interfaces.ExecResponse, error)
	ExecuteStreamFunc func(ctx context.Context, podIP string, req interfaces.ExecRequest) (<-chan interfaces.ExecResponse, error)
	HealthCheckFunc   func(ctx context.Context, podIP string) error
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

// InteractiveShell mocks interactive shell (returns error by default)
func (m *MockSidecarClient) InteractiveShell(_ context.Context, _ string) (interfaces.ShellStream, error) {
	return nil, fmt.Errorf("interactive shell not supported in mock")
}
