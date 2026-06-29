{{/*
Expand the name of the chart.
*/}}
{{- define "agent-env.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "agent-env.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "agent-env.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "agent-env.labels" -}}
helm.sh/chart: {{ include "agent-env.chart" . }}
{{ include "agent-env.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "agent-env.selectorLabels" -}}
app.kubernetes.io/name: {{ include "agent-env.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
ClickHouse host
*/}}
{{- define "agent-env.clickhouseHost" -}}
{{- .Values.clickhouse.host | default (printf "%s-clickhouse" (include "agent-env.fullname" .)) }}
{{- end }}

{{/*
ClickHouse address (host:port)
*/}}
{{- define "agent-env.clickhouseAddr" -}}
{{- printf "%s:%d" (include "agent-env.clickhouseHost" .) (int .Values.clickhouse.port | default 9000) }}
{{- end }}

{{/*
OpenTelemetry environment block. Emits OTEL_ENABLED + OTLP/gRPC exporter
config when otel.enabled is true. Intended to be included verbatim inside a
container env: list — pass the top-level context, e.g.
    {{- include "agent-env.otelEnv" . | nindent 12 }}
*/}}
{{- define "agent-env.otelEnv" -}}
{{- if .Values.otel.enabled }}
- name: OTEL_ENABLED
  value: "true"
- name: OTEL_EXPORTER_OTLP_ENDPOINT
  value: {{ .Values.otel.endpoint | quote }}
- name: OTEL_EXPORTER_OTLP_INSECURE
  value: "{{ .Values.otel.insecure }}"
{{- if .Values.otel.sampleRatio }}
- name: OTEL_TRACES_SAMPLER_ARG
  value: "{{ .Values.otel.sampleRatio }}"
{{- end }}
{{- if .Values.otel.resourceAttributes }}
- name: OTEL_RESOURCE_ATTRIBUTES
  value: {{ .Values.otel.resourceAttributes | quote }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Redis address (host:port) — auto-resolves to the in-cluster Service when deploy is true.
*/}}
{{- define "agent-env.redisAddr" -}}
{{- if .Values.redis.deploy }}
{{- printf "%s-redis:6379" (include "agent-env.fullname" .) }}
{{- else }}
{{- .Values.redis.addr }}
{{- end }}
{{- end }}

{{/*
Resolve the shared gRPC auth token. The sidecar refuses to start without one,
so a token must always exist. Precedence: explicit value -> existing secret
(preserved across upgrades) -> freshly generated random token.
*/}}
{{- define "agent-env.grpcTokenSecretName" -}}
{{ include "agent-env.fullname" . }}-grpc-token
{{- end }}

{{- define "agent-env.grpcToken" -}}
{{- if .Values.auth.grpcToken -}}
{{- .Values.auth.grpcToken -}}
{{- else -}}
{{- $existing := lookup "v1" "Secret" .Release.Namespace (include "agent-env.grpcTokenSecretName" .) -}}
{{- $legacy := lookup "v1" "Secret" .Release.Namespace "arl-grpc-token" -}}
{{- if and $existing $existing.data.token -}}
{{- $existing.data.token | b64dec -}}
{{- else if and $legacy $legacy.data.token -}}
{{- $legacy.data.token | b64dec -}}
{{- else -}}
{{- randAlphaNum 48 -}}
{{- end -}}
{{- end -}}
{{- end }}
