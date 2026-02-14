package metrics

import (
	"time"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// PrometheusCollector implements interfaces.MetricsCollector using Prometheus
type PrometheusCollector struct {
	poolUtilization     *prometheus.GaugeVec
	sandboxAllocation   prometheus.Histogram
	reconcileTotal      *prometheus.CounterVec
	reconcileErrors     *prometheus.CounterVec
	sandboxIdleDuration *prometheus.HistogramVec
	auditWriteErrors    *prometheus.CounterVec
	gatewayStepDuration *prometheus.HistogramVec
	gatewayStepResult   *prometheus.CounterVec
}

// NewPrometheusCollector creates a new Prometheus metrics collector
func NewPrometheusCollector() interfaces.MetricsCollector {
	c := &PrometheusCollector{
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
		sandboxIdleDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "arl_sandbox_idle_duration_seconds",
				Help:    "Duration of sandbox idle time in seconds",
				Buckets: []float64{10, 60, 300, 600, 1800, 3600, 7200},
			},
			[]string{"namespace"},
		),
		auditWriteErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "arl_audit_write_errors_total",
				Help: "Total number of audit write errors",
			},
			[]string{"resource_type"},
		),
		gatewayStepDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "arl_gateway_step_duration_seconds",
				Help:    "Duration of gateway step execution in seconds",
				Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30},
			},
			[]string{"session_id", "step_type"},
		),
		gatewayStepResult: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "arl_gateway_step_result_total",
				Help: "Total number of gateway step executions by result",
			},
			[]string{"session_id", "step_type"},
		),
	}

	metrics.Registry.MustRegister(
		c.poolUtilization,
		c.sandboxAllocation,
		c.reconcileTotal,
		c.reconcileErrors,
		c.sandboxIdleDuration,
		c.auditWriteErrors,
		c.gatewayStepDuration,
		c.gatewayStepResult,
	)

	return c
}

func (c *PrometheusCollector) RecordPoolUtilization(poolName string, ready, allocated int32) {
	c.poolUtilization.WithLabelValues(poolName, "ready").Set(float64(ready))
	c.poolUtilization.WithLabelValues(poolName, "allocated").Set(float64(allocated))
}

func (c *PrometheusCollector) RecordSandboxAllocation(_ string, duration time.Duration) {
	c.sandboxAllocation.Observe(duration.Seconds())
}

func (c *PrometheusCollector) IncrementReconcileTotal(controller, result string) {
	c.reconcileTotal.WithLabelValues(controller, result).Inc()
}

func (c *PrometheusCollector) IncrementReconcileErrors(controller string) {
	c.reconcileErrors.WithLabelValues(controller).Inc()
}

func (c *PrometheusCollector) RecordSandboxIdleDuration(namespace string, duration time.Duration) {
	c.sandboxIdleDuration.WithLabelValues(namespace).Observe(duration.Seconds())
}

func (c *PrometheusCollector) RecordAuditWriteError(resourceType string) {
	c.auditWriteErrors.WithLabelValues(resourceType).Inc()
}

func (c *PrometheusCollector) RecordGatewayStepDuration(sessionID, stepType string, duration time.Duration) {
	c.gatewayStepDuration.WithLabelValues(sessionID, stepType).Observe(duration.Seconds())
}

func (c *PrometheusCollector) RecordGatewayStepResult(sessionID, stepType string, _ int32) {
	c.gatewayStepResult.WithLabelValues(sessionID, stepType).Inc()
}
