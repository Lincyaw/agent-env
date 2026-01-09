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
	"time"
)

// MetricsCollector defines the interface for collecting metrics
type MetricsCollector interface {
	// RecordTaskDuration records task execution duration
	RecordTaskDuration(namespace, taskName string, duration time.Duration)

	// RecordTaskState records task state changes
	RecordTaskState(namespace, taskName, state string)

	// RecordPoolUtilization records warm pool utilization
	RecordPoolUtilization(poolName string, ready, allocated int32)

	// RecordSandboxAllocation records sandbox allocation time
	RecordSandboxAllocation(poolName string, duration time.Duration)

	// IncrementReconcileTotal increments reconciliation counter
	IncrementReconcileTotal(controller, result string)

	// IncrementReconcileErrors increments reconciliation error counter
	IncrementReconcileErrors(controller string)

	// RecordTaskCleanup records task cleanup events
	RecordTaskCleanup(namespace, state string)

	// RecordSandboxIdleDuration records sandbox idle duration
	RecordSandboxIdleDuration(namespace string, duration time.Duration)

	// RecordAuditWriteError records audit write errors
	RecordAuditWriteError(resourceType string)

	// RecordResourceAge records resource age
	RecordResourceAge(resourceType, namespace string, age time.Duration)
}

// NoOpMetricsCollector is a no-op implementation for when metrics are disabled
type NoOpMetricsCollector struct{}

func (n *NoOpMetricsCollector) RecordTaskDuration(namespace, taskName string, duration time.Duration) {
}
func (n *NoOpMetricsCollector) RecordTaskState(namespace, taskName, state string)               {}
func (n *NoOpMetricsCollector) RecordPoolUtilization(poolName string, ready, allocated int32)   {}
func (n *NoOpMetricsCollector) RecordSandboxAllocation(poolName string, duration time.Duration) {}
func (n *NoOpMetricsCollector) IncrementReconcileTotal(controller, result string)               {}
func (n *NoOpMetricsCollector) IncrementReconcileErrors(controller string)                      {}
func (n *NoOpMetricsCollector) RecordTaskCleanup(namespace, state string)                       {}
func (n *NoOpMetricsCollector) RecordSandboxIdleDuration(namespace string, duration time.Duration) {
}
func (n *NoOpMetricsCollector) RecordAuditWriteError(resourceType string) {}
func (n *NoOpMetricsCollector) RecordResourceAge(resourceType, namespace string, age time.Duration) {
}
