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

package metrics

// Package metrics provides metrics collection for the ARL operator.
//
// The metrics package implements the interfaces.MetricsCollector interface
// using Prometheus as the backend. All metrics are automatically registered
// with the controller-runtime metrics registry and exposed on the metrics
// endpoint (default: :8080/metrics).
//
// Available metrics:
//
// - arl_task_duration_seconds: Histogram of task execution durations
// - arl_task_state_total: Counter of task state transitions
// - arl_pool_utilization: Gauge of warm pool utilization (ready/allocated pods)
// - arl_sandbox_allocation_duration_seconds: Histogram of sandbox allocation times
// - arl_reconcile_total: Counter of reconciliation attempts by controller
// - arl_reconcile_errors_total: Counter of reconciliation errors by controller
//
// Usage in main.go:
//
//   import "github.com/Lincyaw/agent-env/pkg/metrics"
//
//   var metricsCollector interfaces.MetricsCollector
//   if cfg.EnableMetrics {
//       metricsCollector = metrics.NewPrometheusCollector()
//   } else {
//       metricsCollector = &interfaces.NoOpMetricsCollector{}
//   }
//
// To query metrics in Prometheus:
//
//   # Task execution time (95th percentile)
//   histogram_quantile(0.95, rate(arl_task_duration_seconds_bucket[5m]))
//
//   # Pool utilization
//   arl_pool_utilization{status="ready"}
//
//   # Error rate
//   rate(arl_reconcile_errors_total[5m])
