// Package metrics provides Prometheus metrics collection for the agent-env
// gateway.
//
// The metrics package implements the interfaces.MetricsCollector interface
// using Prometheus as the backend. Metrics are registered with the
// controller-runtime registry and served by the gateway's internal HTTP server
// on /metrics.
//
// Key metrics:
//
//   - arl_session_allocation_seconds: SandboxClaim allocation latency.
//   - arl_sandbox_ready_seconds: SandboxClaim-to-ready sandbox latency.
//   - arl_image_pull_seconds: Image pull latency from Kubernetes events.
//   - arl_gateway_step_duration_seconds: Execute step latency.
//   - arl_gateway_step_result_total: Execute step result counter.
//   - arl_gateway_sidecar_call_seconds: Sidecar gRPC call latency.
//   - arl_gateway_active_sessions: Current session count.
//   - arl_sandbox_pool_saturation: Warm pool allocated/desired ratio.
//   - arl_gateway_admission_queue_depth: Requests waiting for warm capacity.
//
// Example PromQL:
//
//   - histogram_quantile(0.95, rate(arl_session_allocation_seconds_bucket[5m]))
//   - rate(arl_gateway_step_result_total{result="error"}[5m])
//   - arl_gateway_active_sessions
package metrics
