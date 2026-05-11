package controller

import (
	"os"

	corev1 "k8s.io/api/core/v1"
)

// otelPropagatedVars are forwarded from the operator container to each
// sidecar container so a single Helm-level OTEL config reaches every workload.
// Only non-empty values are emitted; this keeps pods unaffected when tracing
// is disabled.
var otelPropagatedVars = []string{
	"OTEL_ENABLED",
	"OTEL_EXPORTER_OTLP_ENDPOINT",
	"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
	"OTEL_EXPORTER_OTLP_INSECURE",
	"OTEL_TRACES_SAMPLER_ARG",
	"OTEL_RESOURCE_ATTRIBUTES",
	"OTEL_SDK_DISABLED",
}

func otelEnvFromOperator() []corev1.EnvVar {
	envs := make([]corev1.EnvVar, 0, len(otelPropagatedVars)+1)
	for _, name := range otelPropagatedVars {
		if v := os.Getenv(name); v != "" {
			envs = append(envs, corev1.EnvVar{Name: name, Value: v})
		}
	}
	// Override service name per-component when tracing is on.
	if os.Getenv("OTEL_ENABLED") != "" {
		envs = append(envs, corev1.EnvVar{Name: "OTEL_SERVICE_NAME", Value: "arl-sidecar"})
	}
	return envs
}
