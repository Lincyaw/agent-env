package metrics

import (
	"time"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// PrometheusCollector implements interfaces.MetricsCollector using Prometheus
type PrometheusCollector struct {
	// Pool / Pod lifecycle
	poolUtilization        *prometheus.GaugeVec
	pendingPods            *prometheus.GaugeVec
	podScheduleDuration    *prometheus.HistogramVec
	firstPodReadyDuration  *prometheus.HistogramVec
	podReadyDuration       *prometheus.HistogramVec
	allPodsReadyDuration   *prometheus.HistogramVec
	containerStartDuration *prometheus.HistogramVec
	imagePullErrors        *prometheus.CounterVec
	podDeleteTotal         *prometheus.CounterVec

	// Sandbox lifecycle
	sandboxE2EReady     *prometheus.HistogramVec
	sandboxIdleDuration *prometheus.HistogramVec
	noIdlePodsTotal     *prometheus.CounterVec

	// Gateway execution
	activeSessions      prometheus.Gauge
	gatewayStepDuration *prometheus.HistogramVec
	gatewayStepResult   *prometheus.CounterVec
	sidecarCallDuration *prometheus.HistogramVec
	restoreDuration     prometheus.Histogram
	restoreResult       *prometheus.CounterVec

	// Controller health
	reconcileTotal   *prometheus.CounterVec
	auditWriteErrors *prometheus.CounterVec
}

// NewPrometheusCollector creates a new Prometheus metrics collector
func NewPrometheusCollector() interfaces.MetricsCollector {
	c := &PrometheusCollector{
		// --- Pool / Pod lifecycle ---

		poolUtilization: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "arl_pool_utilization",
				Help: "Current warm pool utilization: ready idle pods and allocated pods.",
			},
			[]string{"pool", "status"},
		),

		pendingPods: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "arl_warmpool_pending_pods",
				Help: "Pods created but not yet ready (scheduling + image pull + container start).",
			},
			[]string{"pool"},
		),

		podScheduleDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "arl_warmpool_pod_schedule_seconds",
				Help:    "Time from pod creation to the pod being scheduled onto a node.",
				Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 5, 10},
			},
			[]string{"pool"},
		),

		firstPodReadyDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "arl_warmpool_first_pod_ready_seconds",
				Help:    "Time from a scale-out event to the first pod becoming ready.",
				Buckets: []float64{1, 2, 3, 5, 8, 10, 15, 20, 30, 45, 60},
			},
			[]string{"pool"},
		),

		podReadyDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "arl_warmpool_pod_ready_seconds",
				Help:    "Time from pod creation to pod ready, labeled by node (reveals image-locality effects).",
				Buckets: []float64{1, 2, 5, 10, 15, 20, 30, 45, 60, 90, 120},
			},
			[]string{"pool", "node"},
		),

		allPodsReadyDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "arl_warmpool_all_pods_ready_seconds",
				Help:    "Time from a scale-out event until the pool reaches its desired ready pod count.",
				Buckets: []float64{2, 5, 10, 15, 20, 30, 45, 60, 90, 120},
			},
			[]string{"pool"},
		),

		containerStartDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "arl_warmpool_container_start_seconds",
				Help:    "Time from pod creation to a container entering Running state, by container name.",
				Buckets: []float64{1, 2, 5, 10, 15, 20, 30, 45, 60},
			},
			[]string{"pool", "container"},
		),

		imagePullErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "arl_warmpool_image_pull_errors_total",
				Help: "Image pull failures by pool and reason (ImagePullBackOff, ErrImagePull, PullQPSExceeded).",
			},
			[]string{"pool", "reason"},
		),

		podDeleteTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "arl_warmpool_pod_delete_total",
				Help: "Pod deletions by pool and reason (scale_down, sandbox_cleanup).",
			},
			[]string{"pool", "reason"},
		),

		// --- Sandbox lifecycle ---

		sandboxE2EReady: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "arl_sandbox_e2e_ready_seconds",
				Help:    "End-to-end time from sandbox creation to Ready phase (user-visible latency).",
				Buckets: []float64{0.5, 1, 2, 5, 10, 15, 20, 30, 60},
			},
			[]string{"pool"},
		),

		sandboxIdleDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "arl_sandbox_idle_seconds",
				Help:    "How long a sandbox was idle before deletion, by pool and namespace.",
				Buckets: []float64{10, 60, 300, 600, 1800, 3600, 7200},
			},
			[]string{"pool", "namespace"},
		),

		noIdlePodsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "arl_sandbox_no_idle_pods_total",
				Help: "Times a sandbox could not find an idle pod and had to requeue (pool capacity pressure).",
			},
			[]string{"pool"},
		),

		// --- Gateway execution ---

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
				Help: "Step execution results by step type and outcome (success/error).",
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
				Help:    "Total time for a restore operation (new sandbox + replay steps).",
				Buckets: []float64{1, 2, 5, 10, 20, 30, 60, 120, 300},
			},
		),

		restoreResult: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "arl_gateway_restore_result_total",
				Help: "Restore operation outcomes (success/error).",
			},
			[]string{"result"},
		),

		// --- Controller health ---

		reconcileTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "arl_reconcile_total",
				Help: "Total reconciliations by controller and outcome (success/error).",
			},
			[]string{"controller", "result"},
		),

		auditWriteErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "arl_audit_write_errors_total",
				Help: "Total audit write errors by resource type.",
			},
			[]string{"resource_type"},
		),
	}

	metrics.Registry.MustRegister(
		c.poolUtilization,
		c.pendingPods,
		c.podScheduleDuration,
		c.firstPodReadyDuration,
		c.podReadyDuration,
		c.allPodsReadyDuration,
		c.containerStartDuration,
		c.imagePullErrors,
		c.podDeleteTotal,
		c.sandboxE2EReady,
		c.sandboxIdleDuration,
		c.noIdlePodsTotal,
		c.activeSessions,
		c.gatewayStepDuration,
		c.gatewayStepResult,
		c.sidecarCallDuration,
		c.restoreDuration,
		c.restoreResult,
		c.reconcileTotal,
		c.auditWriteErrors,
	)

	return c
}

func (c *PrometheusCollector) RecordPoolUtilization(poolName string, ready, allocated int32) {
	c.poolUtilization.WithLabelValues(poolName, "ready").Set(float64(ready))
	c.poolUtilization.WithLabelValues(poolName, "allocated").Set(float64(allocated))
}

func (c *PrometheusCollector) SetPendingPods(poolName string, count int32) {
	c.pendingPods.WithLabelValues(poolName).Set(float64(count))
}

func (c *PrometheusCollector) RecordPodScheduleDuration(poolName string, duration time.Duration) {
	c.podScheduleDuration.WithLabelValues(poolName).Observe(duration.Seconds())
}

func (c *PrometheusCollector) RecordFirstPodReady(poolName string, duration time.Duration) {
	c.firstPodReadyDuration.WithLabelValues(poolName).Observe(duration.Seconds())
}

func (c *PrometheusCollector) RecordPodReadyDuration(poolName, nodeName string, duration time.Duration) {
	c.podReadyDuration.WithLabelValues(poolName, nodeName).Observe(duration.Seconds())
}

func (c *PrometheusCollector) RecordAllPodsReady(poolName string, duration time.Duration) {
	c.allPodsReadyDuration.WithLabelValues(poolName).Observe(duration.Seconds())
}

func (c *PrometheusCollector) RecordContainerStartDuration(poolName, containerName string, duration time.Duration) {
	c.containerStartDuration.WithLabelValues(poolName, containerName).Observe(duration.Seconds())
}

func (c *PrometheusCollector) IncrementImagePullError(poolName, reason string) {
	c.imagePullErrors.WithLabelValues(poolName, reason).Inc()
}

func (c *PrometheusCollector) IncrementPodDelete(poolName, reason string) {
	c.podDeleteTotal.WithLabelValues(poolName, reason).Inc()
}

func (c *PrometheusCollector) RecordSandboxE2EReady(poolName string, duration time.Duration) {
	c.sandboxE2EReady.WithLabelValues(poolName).Observe(duration.Seconds())
}

func (c *PrometheusCollector) RecordSandboxIdleDuration(poolName, namespace string, duration time.Duration) {
	c.sandboxIdleDuration.WithLabelValues(poolName, namespace).Observe(duration.Seconds())
}

func (c *PrometheusCollector) IncrementNoIdlePods(poolName string) {
	c.noIdlePodsTotal.WithLabelValues(poolName).Inc()
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

func (c *PrometheusCollector) IncrementReconcileTotal(controller, result string) {
	c.reconcileTotal.WithLabelValues(controller, result).Inc()
}

func (c *PrometheusCollector) RecordAuditWriteError(resourceType string) {
	c.auditWriteErrors.WithLabelValues(resourceType).Inc()
}
