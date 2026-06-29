package interfaces

import "time"

// MetricsCollector defines the gateway metrics used by the current runtime.
type MetricsCollector interface {
	RecordSessionAllocationDuration(poolName string, duration time.Duration)
	SetActiveSessions(count int64)
	RecordGatewayStepDuration(stepType string, duration time.Duration)
	IncrementGatewayStepResult(stepType, result string)
	RecordSidecarCallDuration(method string, duration time.Duration)
	RecordRestoreDuration(duration time.Duration)
	IncrementRestoreResult(result string)
	SetGatewayGoroutines(count int)
	SetGatewaySessionsTotal(count int)
	SetIdleQueueDepth(pool string, count int)
	SetPendingWaiters(pool string, count int)
}

// NoOpMetricsCollector is a no-op implementation for tests or disabled metrics.
type NoOpMetricsCollector struct{}

func (n *NoOpMetricsCollector) RecordSessionAllocationDuration(poolName string, duration time.Duration) {
}
func (n *NoOpMetricsCollector) SetActiveSessions(count int64) {}
func (n *NoOpMetricsCollector) RecordGatewayStepDuration(stepType string, duration time.Duration) {
}
func (n *NoOpMetricsCollector) IncrementGatewayStepResult(stepType, result string) {}
func (n *NoOpMetricsCollector) RecordSidecarCallDuration(method string, duration time.Duration) {
}
func (n *NoOpMetricsCollector) RecordRestoreDuration(duration time.Duration) {}
func (n *NoOpMetricsCollector) IncrementRestoreResult(result string)         {}
func (n *NoOpMetricsCollector) SetGatewayGoroutines(count int)               {}
func (n *NoOpMetricsCollector) SetGatewaySessionsTotal(count int)            {}
func (n *NoOpMetricsCollector) SetIdleQueueDepth(pool string, count int)     {}
func (n *NoOpMetricsCollector) SetPendingWaiters(pool string, count int)     {}
