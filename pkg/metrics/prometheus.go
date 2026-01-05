package metrics

import (
	"time"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// PrometheusCollector implements interfaces.MetricsCollector using Prometheus
type PrometheusCollector struct {
	taskDuration      *prometheus.HistogramVec
	taskStateCounter  *prometheus.CounterVec
	poolUtilization   *prometheus.GaugeVec
	sandboxAllocation prometheus.Histogram
	reconcileTotal    *prometheus.CounterVec
	reconcileErrors   *prometheus.CounterVec
}

// NewPrometheusCollector creates a new Prometheus metrics collector
func NewPrometheusCollector() interfaces.MetricsCollector {
	c := &PrometheusCollector{
		taskDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "arl_task_duration_seconds",
				Help:    "Duration of task execution in seconds",
				Buckets: []float64{0.1, 0.5, 1, 5, 10, 30, 60, 120, 300},
			},
			[]string{"namespace", "task"},
		),
		taskStateCounter: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "arl_task_state_total",
				Help: "Total number of task state changes",
			},
			[]string{"namespace", "task", "state"},
		),
		poolUtilization: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "arl_pool_utilization",
				Help: "Current pool utilization (ready and allocated pods)",
			},
			[]string{"pool", "status"},
		),
		sandboxAllocation: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "arl_sandbox_allocation_duration_seconds",
				Help:    "Duration of sandbox allocation in seconds",
				Buckets: []float64{0.1, 0.5, 1, 2, 5, 10},
			},
		),
		reconcileTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "arl_reconcile_total",
				Help: "Total number of reconciliations",
			},
			[]string{"controller", "result"},
		),
		reconcileErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "arl_reconcile_errors_total",
				Help: "Total number of reconciliation errors",
			},
			[]string{"controller"},
		),
	}

	// Register metrics with controller-runtime metrics registry
	metrics.Registry.MustRegister(
		c.taskDuration,
		c.taskStateCounter,
		c.poolUtilization,
		c.sandboxAllocation,
		c.reconcileTotal,
		c.reconcileErrors,
	)

	return c
}

// RecordTaskDuration records task execution duration
func (c *PrometheusCollector) RecordTaskDuration(namespace, taskName string, duration time.Duration) {
	c.taskDuration.WithLabelValues(namespace, taskName).Observe(duration.Seconds())
}

// RecordTaskState records task state changes
func (c *PrometheusCollector) RecordTaskState(namespace, taskName, state string) {
	c.taskStateCounter.WithLabelValues(namespace, taskName, state).Inc()
}

// RecordPoolUtilization records warm pool utilization
func (c *PrometheusCollector) RecordPoolUtilization(poolName string, ready, allocated int32) {
	c.poolUtilization.WithLabelValues(poolName, "ready").Set(float64(ready))
	c.poolUtilization.WithLabelValues(poolName, "allocated").Set(float64(allocated))
}

// RecordSandboxAllocation records sandbox allocation time
func (c *PrometheusCollector) RecordSandboxAllocation(poolName string, duration time.Duration) {
	c.sandboxAllocation.Observe(duration.Seconds())
}

// IncrementReconcileTotal increments reconciliation counter
func (c *PrometheusCollector) IncrementReconcileTotal(controller, result string) {
	c.reconcileTotal.WithLabelValues(controller, result).Inc()
}

// IncrementReconcileErrors increments reconciliation error counter
func (c *PrometheusCollector) IncrementReconcileErrors(controller string) {
	c.reconcileErrors.WithLabelValues(controller).Inc()
}
