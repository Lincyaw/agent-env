package metrics

import (
	"time"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// PrometheusCollector implements interfaces.MetricsCollector using Prometheus.
type PrometheusCollector struct {
	sessionAllocationDuration *prometheus.HistogramVec

	activeSessions      prometheus.Gauge
	gatewayStepDuration *prometheus.HistogramVec
	gatewayStepResult   *prometheus.CounterVec
	sidecarCallDuration *prometheus.HistogramVec
	restoreDuration     prometheus.Histogram
	restoreResult       *prometheus.CounterVec

	gatewayGoroutines    prometheus.Gauge
	gatewaySessionsTotal prometheus.Gauge
	idleQueueDepth       *prometheus.GaugeVec
	pendingWaiters       *prometheus.GaugeVec
}

// NewPrometheusCollector creates a new Prometheus metrics collector.
func NewPrometheusCollector() interfaces.MetricsCollector {
	c := &PrometheusCollector{
		sessionAllocationDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "arl_session_allocation_seconds",
				Help:    "End-to-end time from session creation request to sandbox allocation.",
				Buckets: []float64{0.5, 1, 2, 5, 10, 15, 20, 30, 60},
			},
			[]string{"pool"},
		),
		activeSessions: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "arl_gateway_active_sessions",
				Help: "Number of currently active gateway sessions.",
			},
		),
		gatewayStepDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "arl_gateway_step_duration_seconds",
				Help:    "Per-step execution latency in the gateway, by step type.",
				Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60},
			},
			[]string{"step_type"},
		),
		gatewayStepResult: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "arl_gateway_step_result_total",
				Help: "Step execution results by step type and outcome.",
			},
			[]string{"step_type", "result"},
		),
		sidecarCallDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "arl_gateway_sidecar_call_seconds",
				Help:    "gRPC round-trip latency from gateway to sidecar, by method.",
				Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5},
			},
			[]string{"method"},
		),
		restoreDuration: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "arl_gateway_restore_duration_seconds",
				Help:    "Total time for a restore operation.",
				Buckets: []float64{1, 2, 5, 10, 20, 30, 60, 120, 300},
			},
		),
		restoreResult: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "arl_gateway_restore_result_total",
				Help: "Restore operation outcomes.",
			},
			[]string{"result"},
		),
		gatewayGoroutines: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "arl_gateway_goroutines",
				Help: "Current number of goroutines in the gateway process.",
			},
		),
		gatewaySessionsTotal: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "arl_gateway_sessions_total",
				Help: "Actual session count from SessionStore traversal.",
			},
		),
		idleQueueDepth: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "arl_gateway_idle_queue_depth",
				Help: "Ready runtime capacity exposed by the allocator, by pool.",
			},
			[]string{"pool"},
		),
		pendingWaiters: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "arl_gateway_pending_waiters",
				Help: "Number of blocked waiters for runtime allocation, by pool.",
			},
			[]string{"pool"},
		),
	}

	metrics.Registry.MustRegister(
		c.sessionAllocationDuration,
		c.activeSessions,
		c.gatewayStepDuration,
		c.gatewayStepResult,
		c.sidecarCallDuration,
		c.restoreDuration,
		c.restoreResult,
		c.gatewayGoroutines,
		c.gatewaySessionsTotal,
		c.idleQueueDepth,
		c.pendingWaiters,
	)

	return c
}

func (c *PrometheusCollector) RecordSessionAllocationDuration(poolName string, duration time.Duration) {
	c.sessionAllocationDuration.WithLabelValues(poolName).Observe(duration.Seconds())
}

func (c *PrometheusCollector) SetActiveSessions(count int64) {
	c.activeSessions.Set(float64(count))
}

func (c *PrometheusCollector) RecordGatewayStepDuration(stepType string, duration time.Duration) {
	c.gatewayStepDuration.WithLabelValues(stepType).Observe(duration.Seconds())
}

func (c *PrometheusCollector) IncrementGatewayStepResult(stepType, result string) {
	c.gatewayStepResult.WithLabelValues(stepType, result).Inc()
}

func (c *PrometheusCollector) RecordSidecarCallDuration(method string, duration time.Duration) {
	c.sidecarCallDuration.WithLabelValues(method).Observe(duration.Seconds())
}

func (c *PrometheusCollector) RecordRestoreDuration(duration time.Duration) {
	c.restoreDuration.Observe(duration.Seconds())
}

func (c *PrometheusCollector) IncrementRestoreResult(result string) {
	c.restoreResult.WithLabelValues(result).Inc()
}

func (c *PrometheusCollector) SetGatewayGoroutines(count int) {
	c.gatewayGoroutines.Set(float64(count))
}

func (c *PrometheusCollector) SetGatewaySessionsTotal(count int) {
	c.gatewaySessionsTotal.Set(float64(count))
}

func (c *PrometheusCollector) SetIdleQueueDepth(pool string, count int) {
	c.idleQueueDepth.WithLabelValues(pool).Set(float64(count))
}

func (c *PrometheusCollector) SetPendingWaiters(pool string, count int) {
	c.pendingWaiters.WithLabelValues(pool).Set(float64(count))
}
