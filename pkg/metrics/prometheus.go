package metrics

import (
	"strings"
	"time"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// PrometheusCollector implements interfaces.MetricsCollector using Prometheus.
type PrometheusCollector struct {
	httpRequestDuration       *prometheus.HistogramVec
	sessionAllocationDuration *prometheus.HistogramVec
	podAllocationResult       *prometheus.CounterVec
	sandboxReadyDuration      *prometheus.HistogramVec
	imagePullDuration         *prometheus.HistogramVec

	activeSessions      prometheus.Gauge
	sessionDeletion     *prometheus.CounterVec
	sessionDrop         *prometheus.CounterVec
	executeOperation    *prometheus.CounterVec
	gatewayStepDuration *prometheus.HistogramVec
	gatewayStepResult   *prometheus.CounterVec
	executorCallDuration *prometheus.HistogramVec
	restoreDuration     prometheus.Histogram
	restoreResult       *prometheus.CounterVec

	gatewayGoroutines     prometheus.Gauge
	gatewaySessionsTotal  prometheus.Gauge
	runtimeIdleCapacity   prometheus.Gauge
	runtimePendingWaiters prometheus.Gauge
	admissionQueueDepth   *prometheus.GaugeVec
	poolSaturation        *prometheus.GaugeVec
	poolDesiredReplicas   *prometheus.GaugeVec
	poolReadyReplicas     *prometheus.GaugeVec
	poolAllocatedReplicas *prometheus.GaugeVec
}

// NewPrometheusCollector creates a new Prometheus metrics collector.
func NewPrometheusCollector() interfaces.MetricsCollector {
	c := &PrometheusCollector{
		httpRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "arl_gateway_http_request_seconds",
				Help:    "HTTP request handler duration by method, route pattern, and response status.",
				Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 15, 30},
			},
			[]string{"method", "route", "status"},
		),
		sessionAllocationDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "arl_session_allocation_seconds",
				Help:    "End-to-end time from session creation request to sandbox allocation.",
				Buckets: []float64{0.5, 1, 2, 5, 10, 15, 20, 30, 60},
			},
			[]string{"pool_type"},
		),
		podAllocationResult: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "arl_gateway_pod_allocation_result_total",
				Help: "Sandbox runtime allocation attempts by pool type and result.",
			},
			[]string{"pool_type", "result"},
		),
		sandboxReadyDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "arl_sandbox_ready_seconds",
				Help:    "Time from SandboxClaim creation to a ready sandbox allocation.",
				Buckets: []float64{0.5, 1, 2, 5, 10, 15, 20, 30, 60, 120, 300},
			},
			[]string{"pool_type"},
		),
		imagePullDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "arl_image_pull_seconds",
				Help:    "Best-effort image pull latency derived from Kubernetes Pod events.",
				Buckets: []float64{0.5, 1, 2, 5, 10, 15, 20, 30, 60, 120, 300, 600},
			},
			[]string{"image_class"},
		),
		activeSessions: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "arl_gateway_active_sessions",
				Help: "Number of currently active gateway sessions.",
			},
		),
		sessionDeletion: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "arl_gateway_session_deletion_total",
				Help: "Session deletions by reason.",
			},
			[]string{"reason"},
		),
		sessionDrop: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "arl_gateway_session_drop_total",
				Help: "Sessions dropped due to runtime loss, by deletion reason and container termination reason.",
			},
			[]string{"reason", "termination_reason"},
		),
		executeOperation: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "arl_gateway_execute_operation_result_total",
				Help: "Idempotent execute operation outcomes.",
			},
			[]string{"result"},
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
		executorCallDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "arl_gateway_executor_call_seconds",
				Help:    "Round-trip latency from gateway to executor, by method.",
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
		runtimeIdleCapacity: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "arl_gateway_runtime_idle_capacity",
				Help: "Total ready runtime capacity exposed by the allocator.",
			},
		),
		runtimePendingWaiters: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "arl_gateway_runtime_pending_waiters",
				Help: "Total blocked waiters for runtime allocation.",
			},
		),
		admissionQueueDepth: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "arl_gateway_admission_queue_depth",
				Help: "Number of session requests waiting for warm capacity, by pool profile and state.",
			},
			[]string{"profile", "state"},
		),
		poolSaturation: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "arl_sandbox_pool_saturation",
				Help: "Allocated replicas divided by desired replicas, by pool profile and state.",
			},
			[]string{"profile", "state"},
		),
		poolDesiredReplicas: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "arl_sandbox_pool_desired_replicas",
				Help: "Desired SandboxWarmPool replicas, by pool profile and state.",
			},
			[]string{"profile", "state"},
		),
		poolReadyReplicas: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "arl_sandbox_pool_ready_replicas",
				Help: "Ready SandboxWarmPool replicas, by pool profile and state.",
			},
			[]string{"profile", "state"},
		),
		poolAllocatedReplicas: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "arl_sandbox_pool_allocated_replicas",
				Help: "Active SandboxClaims attached to SandboxWarmPools, by pool profile and state.",
			},
			[]string{"profile", "state"},
		),
	}

	metrics.Registry.MustRegister(
		c.httpRequestDuration,
		c.sessionAllocationDuration,
		c.podAllocationResult,
		c.sandboxReadyDuration,
		c.imagePullDuration,
		c.activeSessions,
		c.sessionDeletion,
		c.sessionDrop,
		c.executeOperation,
		c.gatewayStepDuration,
		c.gatewayStepResult,
		c.executorCallDuration,
		c.restoreDuration,
		c.restoreResult,
		c.gatewayGoroutines,
		c.gatewaySessionsTotal,
		c.runtimeIdleCapacity,
		c.runtimePendingWaiters,
		c.admissionQueueDepth,
		c.poolSaturation,
		c.poolDesiredReplicas,
		c.poolReadyReplicas,
		c.poolAllocatedReplicas,
	)

	return c
}

func (c *PrometheusCollector) RecordHTTPRequestDuration(method, route, status string, duration time.Duration) {
	c.httpRequestDuration.WithLabelValues(metricValue(method, "unknown"), metricValue(route, "unknown"), metricValue(status, "unknown")).Observe(duration.Seconds())
}

func (c *PrometheusCollector) RecordSessionAllocationDuration(poolName string, duration time.Duration) {
	c.sessionAllocationDuration.WithLabelValues(poolMetricType(poolName)).Observe(duration.Seconds())
}

func (c *PrometheusCollector) IncrementPodAllocationResult(poolName, result string) {
	c.podAllocationResult.WithLabelValues(poolMetricType(poolName), metricValue(result, "unknown")).Inc()
}

func (c *PrometheusCollector) RecordSandboxReadyDuration(poolName string, duration time.Duration) {
	c.sandboxReadyDuration.WithLabelValues(poolMetricType(poolName)).Observe(duration.Seconds())
}

func (c *PrometheusCollector) RecordImagePullDuration(image string, duration time.Duration) {
	c.imagePullDuration.WithLabelValues(imageMetricClass(image)).Observe(duration.Seconds())
}

func (c *PrometheusCollector) SetActiveSessions(count int64) {
	c.activeSessions.Set(float64(count))
}

func (c *PrometheusCollector) IncrementSessionDeletion(reason string) {
	c.sessionDeletion.WithLabelValues(reason).Inc()
}

func (c *PrometheusCollector) IncrementSessionDrop(reason, terminationReason string) {
	if terminationReason == "" {
		terminationReason = "unknown"
	}
	c.sessionDrop.WithLabelValues(reason, terminationReason).Inc()
}

func (c *PrometheusCollector) IncrementExecuteOperationResult(result string) {
	c.executeOperation.WithLabelValues(result).Inc()
}

func (c *PrometheusCollector) RecordGatewayStepDuration(stepType string, duration time.Duration) {
	c.gatewayStepDuration.WithLabelValues(stepType).Observe(duration.Seconds())
}

func (c *PrometheusCollector) IncrementGatewayStepResult(stepType, result string) {
	c.gatewayStepResult.WithLabelValues(stepType, result).Inc()
}

func (c *PrometheusCollector) RecordExecutorCallDuration(method string, duration time.Duration) {
	c.executorCallDuration.WithLabelValues(method).Observe(duration.Seconds())
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

func (c *PrometheusCollector) SetRuntimeIdleCapacity(count int) {
	c.runtimeIdleCapacity.Set(float64(count))
}

func (c *PrometheusCollector) SetRuntimePendingWaiters(count int) {
	c.runtimePendingWaiters.Set(float64(count))
}

func (c *PrometheusCollector) ResetPoolAggregateMetrics() {
	c.poolDesiredReplicas.Reset()
	c.poolReadyReplicas.Reset()
	c.poolAllocatedReplicas.Reset()
	c.admissionQueueDepth.Reset()
	c.poolSaturation.Reset()
}

func (c *PrometheusCollector) SetPoolAggregateMetrics(profile, state string, desired, ready, allocated, queued int, saturation float64) {
	profile = metricValue(profile, "default")
	state = metricValue(state, "unknown")
	c.poolDesiredReplicas.WithLabelValues(profile, state).Set(float64(desired))
	c.poolReadyReplicas.WithLabelValues(profile, state).Set(float64(ready))
	c.poolAllocatedReplicas.WithLabelValues(profile, state).Set(float64(allocated))
	c.admissionQueueDepth.WithLabelValues(profile, state).Set(float64(queued))
	c.poolSaturation.WithLabelValues(profile, state).Set(saturation)
}

func poolMetricType(poolName string) string {
	name := strings.ToLower(strings.TrimSpace(poolName))
	if name == "" {
		return "unknown"
	}
	if slash := strings.LastIndex(name, "/"); slash >= 0 {
		name = name[slash+1:]
	}
	if looksLikeManagedPoolName(name) {
		return "managed"
	}
	return "static"
}

func looksLikeManagedPoolName(name string) bool {
	if len(name) < 13 || name[len(name)-13] != '-' {
		return false
	}
	for _, ch := range name[len(name)-12:] {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
			return false
		}
	}
	return true
}

func imageMetricClass(image string) string {
	image = strings.ToLower(strings.TrimSpace(image))
	if image == "" {
		return "unknown"
	}
	switch {
	case strings.Contains(image, "arl-gateway"):
		return "arl-gateway"
	case strings.Contains(image, "arl-executor-agent"):
		return "arl-executor-agent"
	case strings.Contains(image, "arl-image-locality-scheduler"):
		return "arl-image-locality-scheduler"
	case strings.Contains(image, "agent-sandbox-controller"):
		return "agent-sandbox-controller"
	default:
		return "workload"
	}
}

func metricValue(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	if len(value) > 64 {
		return "custom"
	}
	for _, ch := range value {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') ||
			ch == '_' || ch == '-' || ch == '.' || ch == '/' || ch == ' ' || ch == '{' || ch == '}' {
			continue
		}
		return "custom"
	}
	return value
}
