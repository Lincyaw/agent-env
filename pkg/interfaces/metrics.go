package interfaces

import "time"

// MetricsCollector defines the gateway metrics used by the current runtime.
type MetricsCollector interface {
	RecordHTTPRequestDuration(method, route, status string, duration time.Duration)
	RecordSessionAllocationDuration(poolName string, duration time.Duration)
	IncrementPodAllocationResult(poolName, result string)
	RecordSandboxReadyDuration(poolName string, duration time.Duration)
	RecordImagePullDuration(image string, duration time.Duration)
	SetActiveSessions(count int64)
	IncrementSessionDeletion(reason string)
	IncrementExecuteOperationResult(result string)
	RecordGatewayStepDuration(stepType string, duration time.Duration)
	IncrementGatewayStepResult(stepType, result string)
	RecordSidecarCallDuration(method string, duration time.Duration)
	RecordRestoreDuration(duration time.Duration)
	IncrementRestoreResult(result string)
	SetGatewayGoroutines(count int)
	SetGatewaySessionsTotal(count int)
	SetRuntimeIdleCapacity(count int)
	SetRuntimePendingWaiters(count int)
	ResetPoolAggregateMetrics()
	SetPoolAggregateMetrics(profile, state string, desired, ready, allocated, queued int, saturation float64)
}

// NoOpMetricsCollector is a no-op implementation for tests or disabled metrics.
type NoOpMetricsCollector struct{}

func (n *NoOpMetricsCollector) RecordHTTPRequestDuration(method, route, status string, duration time.Duration) {
}
func (n *NoOpMetricsCollector) RecordSessionAllocationDuration(poolName string, duration time.Duration) {
}
func (n *NoOpMetricsCollector) IncrementPodAllocationResult(poolName, result string)               {}
func (n *NoOpMetricsCollector) RecordSandboxReadyDuration(poolName string, duration time.Duration) {}
func (n *NoOpMetricsCollector) RecordImagePullDuration(image string, duration time.Duration)       {}
func (n *NoOpMetricsCollector) SetActiveSessions(count int64)                                      {}
func (n *NoOpMetricsCollector) IncrementSessionDeletion(reason string)                             {}
func (n *NoOpMetricsCollector) IncrementExecuteOperationResult(result string)                      {}
func (n *NoOpMetricsCollector) RecordGatewayStepDuration(stepType string, duration time.Duration) {
}
func (n *NoOpMetricsCollector) IncrementGatewayStepResult(stepType, result string) {}
func (n *NoOpMetricsCollector) RecordSidecarCallDuration(method string, duration time.Duration) {
}
func (n *NoOpMetricsCollector) RecordRestoreDuration(duration time.Duration) {}
func (n *NoOpMetricsCollector) IncrementRestoreResult(result string)         {}
func (n *NoOpMetricsCollector) SetGatewayGoroutines(count int)               {}
func (n *NoOpMetricsCollector) SetGatewaySessionsTotal(count int)            {}
func (n *NoOpMetricsCollector) SetRuntimeIdleCapacity(count int)             {}
func (n *NoOpMetricsCollector) SetRuntimePendingWaiters(count int)           {}
func (n *NoOpMetricsCollector) ResetPoolAggregateMetrics()                   {}
func (n *NoOpMetricsCollector) SetPoolAggregateMetrics(profile, state string, desired, ready, allocated, queued int, saturation float64) {
}
