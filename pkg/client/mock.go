package client

import (
	"context"
	"fmt"
	"io"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
)

// MockSidecarClient is a mock implementation for testing
type MockSidecarClient struct {
	ExecuteFunc          func(ctx context.Context, podIP string, req interfaces.ExecRequest) (interfaces.ExecResponse, error)
	ExecuteStreamFunc    func(ctx context.Context, podIP string, req interfaces.ExecRequest) (<-chan interfaces.ExecResponse, error)
	WriteFileFunc        func(ctx context.Context, podIP string, path string, content io.Reader, expectedSHA256 string) (*interfaces.FileWriteResult, error)
	ReadFileFunc         func(ctx context.Context, podIP string, path string, dst io.Writer) (*interfaces.FileReadResult, error)
	InteractiveShellFunc func(ctx context.Context, podIP string) (interfaces.ShellStream, error)
	HealthCheckFunc      func(ctx context.Context, podIP string) error
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

// WriteFile mocks native file upload
func (m *MockSidecarClient) WriteFile(ctx context.Context, podIP string, path string, content io.Reader, expectedSHA256 string) (*interfaces.FileWriteResult, error) {
	if m.WriteFileFunc != nil {
		return m.WriteFileFunc(ctx, podIP, path, content, expectedSHA256)
	}
	return nil, fmt.Errorf("not implemented")
}

// ReadFile mocks native file download
func (m *MockSidecarClient) ReadFile(ctx context.Context, podIP string, path string, dst io.Writer) (*interfaces.FileReadResult, error) {
	if m.ReadFileFunc != nil {
		return m.ReadFileFunc(ctx, podIP, path, dst)
	}
	return nil, fmt.Errorf("not implemented")
}

// GetIrohAddr mocks iroh address retrieval
func (m *MockSidecarClient) GetIrohAddr(_ context.Context, _ string) (string, error) {
	return "", nil
}

// HealthCheck mocks health check
func (m *MockSidecarClient) HealthCheck(ctx context.Context, podIP string) error {
	if m.HealthCheckFunc != nil {
		return m.HealthCheckFunc(ctx, podIP)
	}
	return nil
}

// CloseConnection is a no-op for mock
func (m *MockSidecarClient) CloseConnection(_ string) error {
	return nil
}

// CleanupStale is a no-op for mock
func (m *MockSidecarClient) CleanupStale() int {
	return 0
}

// Close mocks cleanup
func (m *MockSidecarClient) Close() error {
	return nil
}

// InteractiveShell mocks interactive shell (returns error by default)
func (m *MockSidecarClient) InteractiveShell(ctx context.Context, podIP string) (interfaces.ShellStream, error) {
	if m.InteractiveShellFunc != nil {
		return m.InteractiveShellFunc(ctx, podIP)
	}
	return nil, fmt.Errorf("interactive shell not supported in mock")
}

// StreamLogs mocks log streaming
func (m *MockSidecarClient) StreamLogs(_ context.Context, _ string, _ bool, _ int32) (<-chan interfaces.LogEntry, error) {
	ch := make(chan interfaces.LogEntry)
	close(ch)
	return ch, nil
}

// Stat mocks file stat
func (m *MockSidecarClient) Stat(_ context.Context, _ string, _ string) (*interfaces.StatResult, error) {
	return nil, fmt.Errorf("not implemented")
}

// ListDir mocks directory listing
func (m *MockSidecarClient) ListDir(_ context.Context, _ string, _ string, _ bool) ([]interfaces.DirEntry, error) {
	return nil, fmt.Errorf("not implemented")
}

// WriteStdin mocks writing to stdin
func (m *MockSidecarClient) WriteStdin(_ context.Context, _ string, _ string, _ []byte) error {
	return fmt.Errorf("not implemented")
}
