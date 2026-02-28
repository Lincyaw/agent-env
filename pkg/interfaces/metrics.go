package interfaces

import (
	"time"
)

// MetricsCollector defines the interface for collecting metrics
type MetricsCollector interface {
	// --- Pool / Pod lifecycle ---

	// RecordPoolUtilization records warm pool utilization (ready idle + allocated).
	RecordPoolUtilization(poolName string, ready, allocated int32)

	// SetPendingPods sets the gauge of pods created but not yet ready.
	SetPendingPods(poolName string, count int32)

	// RecordPodScheduleDuration records pod creation → PodScheduled condition.
	RecordPodScheduleDuration(poolName string, duration time.Duration)

	// RecordFirstPodReady records the time from a scale-out event until the
	// first pod in the batch becomes ready.
	RecordFirstPodReady(poolName string, duration time.Duration)

	// RecordPodReadyDuration records pod creation → pod Ready, labeled by node
	// to surface image-locality effects.
	RecordPodReadyDuration(poolName, nodeName string, duration time.Duration)

	// RecordAllPodsReady records the time from a scale-out event until all
	// desired pods are ready.
	RecordAllPodsReady(poolName string, duration time.Duration)

	// RecordContainerStartDuration records pod creation → container Running,
	// labeled by container name (executor / sidecar / init containers).
	RecordContainerStartDuration(poolName, containerName string, duration time.Duration)

	// IncrementImagePullError increments image pull failures by reason
	// (ImagePullBackOff, ErrImagePull, PullQPSExceeded).
	IncrementImagePullError(poolName, reason string)

	// IncrementPodDelete increments pod deletion counter by pool and reason
	// (scale_down, sandbox_cleanup).
	IncrementPodDelete(poolName, reason string)

	// --- Sandbox lifecycle ---

	// RecordSandboxE2EReady records the end-to-end time from sandbox creation
	// until the sandbox reaches Ready phase (user-visible allocation latency).
	RecordSandboxE2EReady(poolName string, duration time.Duration)

	// RecordSandboxIdleDuration records how long a sandbox stayed idle before
	// being deleted (idle timeout or explicit delete).
	RecordSandboxIdleDuration(poolName, namespace string, duration time.Duration)

	// IncrementNoIdlePods increments counter when a sandbox cannot find an
	// idle pod and must requeue (signals pool capacity pressure).
	IncrementNoIdlePods(poolName string)

	// --- Gateway execution ---

	// SetActiveSessions sets the gauge of currently active gateway sessions.
	SetActiveSessions(count int64)

	// RecordGatewayStepDuration records per-step execution latency.
	RecordGatewayStepDuration(stepType string, duration time.Duration)

	// IncrementGatewayStepResult increments step result counter (success/error).
	IncrementGatewayStepResult(stepType, result string)

	// RecordSidecarCallDuration records the gRPC round-trip latency to the sidecar,
	// separate from command execution time inside the container.
	RecordSidecarCallDuration(method string, duration time.Duration)

	// RecordRestoreDuration records the total time for a restore operation.
	RecordRestoreDuration(duration time.Duration)

	// IncrementRestoreResult increments restore outcome counter (success/error).
	IncrementRestoreResult(result string)

	// --- Controller health ---

	// IncrementReconcileTotal increments reconciliation counter by controller and outcome.
	IncrementReconcileTotal(controller, result string)

	// RecordAuditWriteError records audit write errors.
	RecordAuditWriteError(resourceType string)
}

// NoOpMetricsCollector is a no-op implementation for when metrics are disabled
type NoOpMetricsCollector struct{}

func (n *NoOpMetricsCollector) RecordPoolUtilization(poolName string, ready, allocated int32) {}
func (n *NoOpMetricsCollector) SetPendingPods(poolName string, count int32)                   {}
func (n *NoOpMetricsCollector) RecordPodScheduleDuration(poolName string, duration time.Duration) {
}
func (n *NoOpMetricsCollector) RecordFirstPodReady(poolName string, duration time.Duration) {}
func (n *NoOpMetricsCollector) RecordPodReadyDuration(poolName, nodeName string, duration time.Duration) {
}
func (n *NoOpMetricsCollector) RecordAllPodsReady(poolName string, duration time.Duration) {}
func (n *NoOpMetricsCollector) RecordContainerStartDuration(poolName, containerName string, duration time.Duration) {
}
func (n *NoOpMetricsCollector) IncrementImagePullError(poolName, reason string) {}
func (n *NoOpMetricsCollector) IncrementPodDelete(poolName, reason string)      {}
func (n *NoOpMetricsCollector) RecordSandboxE2EReady(poolName string, duration time.Duration) {
}
func (n *NoOpMetricsCollector) RecordSandboxIdleDuration(poolName, namespace string, duration time.Duration) {
}
func (n *NoOpMetricsCollector) IncrementNoIdlePods(poolName string) {}
func (n *NoOpMetricsCollector) SetActiveSessions(count int64)       {}
func (n *NoOpMetricsCollector) RecordGatewayStepDuration(stepType string, duration time.Duration) {
}
func (n *NoOpMetricsCollector) IncrementGatewayStepResult(stepType, result string) {}
func (n *NoOpMetricsCollector) RecordSidecarCallDuration(method string, duration time.Duration) {
}
func (n *NoOpMetricsCollector) RecordRestoreDuration(duration time.Duration)      {}
func (n *NoOpMetricsCollector) IncrementRestoreResult(result string)              {}
func (n *NoOpMetricsCollector) IncrementReconcileTotal(controller, result string) {}
func (n *NoOpMetricsCollector) RecordAuditWriteError(resourceType string)         {}
