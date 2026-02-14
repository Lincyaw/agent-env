package interfaces

import (
	"time"
)

// MetricsCollector defines the interface for collecting metrics
type MetricsCollector interface {
	// RecordPoolUtilization records warm pool utilization
	RecordPoolUtilization(poolName string, ready, allocated int32)

	// RecordSandboxAllocation records sandbox allocation time
	RecordSandboxAllocation(poolName string, duration time.Duration)

	// IncrementReconcileTotal increments reconciliation counter
	IncrementReconcileTotal(controller, result string)

	// IncrementReconcileErrors increments reconciliation error counter
	IncrementReconcileErrors(controller string)

	// RecordSandboxIdleDuration records sandbox idle duration
	RecordSandboxIdleDuration(namespace string, duration time.Duration)

	// RecordAuditWriteError records audit write errors
	RecordAuditWriteError(resourceType string)

	// RecordGatewayStepDuration records gateway step execution duration
	RecordGatewayStepDuration(sessionID, stepType string, duration time.Duration)

	// RecordGatewayStepResult records gateway step execution result
	RecordGatewayStepResult(sessionID, stepType string, exitCode int32)
}

// NoOpMetricsCollector is a no-op implementation for when metrics are disabled
type NoOpMetricsCollector struct{}

func (n *NoOpMetricsCollector) RecordPoolUtilization(poolName string, ready, allocated int32)   {}
func (n *NoOpMetricsCollector) RecordSandboxAllocation(poolName string, duration time.Duration) {}
func (n *NoOpMetricsCollector) IncrementReconcileTotal(controller, result string)               {}
func (n *NoOpMetricsCollector) IncrementReconcileErrors(controller string)                      {}
func (n *NoOpMetricsCollector) RecordSandboxIdleDuration(namespace string, duration time.Duration) {
}
func (n *NoOpMetricsCollector) RecordAuditWriteError(resourceType string) {}
func (n *NoOpMetricsCollector) RecordGatewayStepDuration(sessionID, stepType string, duration time.Duration) {
}
func (n *NoOpMetricsCollector) RecordGatewayStepResult(sessionID, stepType string, exitCode int32) {
}
