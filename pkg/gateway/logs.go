package gateway

import (
	"context"
	"fmt"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
)

// StreamSessionLogs is not supported in the direct executor protocol.
// Callers should use Kubernetes pod logs instead.
func (g *Gateway) StreamSessionLogs(_ context.Context, _ string, _ bool, _ int32) (<-chan interfaces.LogEntry, error) {
	return nil, fmt.Errorf("log streaming is not supported; use kubectl logs for pod-level logs")
}

// StreamPoolLogs is not supported in the direct executor protocol.
func (g *Gateway) StreamPoolLogs(_ context.Context, _, _ string, _ bool, _ int32) (<-chan PoolLogEntry, error) {
	return nil, fmt.Errorf("pool log streaming is not supported; use kubectl logs for pod-level logs")
}

// PoolLogEntry wraps a LogEntry with the source pod name.
type PoolLogEntry struct {
	PodName string              `json:"podName"`
	Entry   interfaces.LogEntry `json:"entry"`
}
