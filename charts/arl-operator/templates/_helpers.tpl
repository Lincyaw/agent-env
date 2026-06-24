{{/*
Expand the name of the chart.
*/}}
{{- define "arl-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "arl-operator.fullname" -}}
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
{{- define "arl-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "arl-operator.labels" -}}
helm.sh/chart: {{ include "arl-operator.chart" . }}
{{ include "arl-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "arl-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "arl-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "arl-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "arl-operator.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
ClickHouse host
*/}}
{{- define "arl-operator.clickhouseHost" -}}
{{- .Values.clickhouse.host | default (printf "%s-clickhouse" (include "arl-operator.fullname" .)) }}
{{- end }}

{{/*
ClickHouse address (host:port)
*/}}
{{- define "arl-operator.clickhouseAddr" -}}
{{- printf "%s:%d" (include "arl-operator.clickhouseHost" .) (int .Values.clickhouse.port | default 9000) }}
{{- end }}

{{/*
OpenTelemetry environment block. Emits OTEL_ENABLED + OTLP/gRPC exporter
config when otel.enabled is true. Intended to be included verbatim inside a
container env: list — pass the top-level context, e.g.
    {{- include "arl-operator.otelEnv" . | nindent 12 }}
*/}}
{{- define "arl-operator.otelEnv" -}}
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
{{- define "arl-operator.redisAddr" -}}
{{- if .Values.redis.deploy }}
{{- printf "%s-redis:6379" (include "arl-operator.fullname" .) }}
{{- else }}
{{- .Values.redis.addr }}
{{- end }}
{{- end }}

{{/*
Resolve the shared gRPC auth token. The sidecar refuses to start without one,
so a token must always exist. Precedence: explicit value -> existing secret
(preserved across upgrades) -> freshly generated random token.
*/}}
{{- define "arl-operator.grpcToken" -}}
{{- if .Values.auth.grpcToken -}}
{{- .Values.auth.grpcToken -}}
{{- else -}}
{{- $existing := lookup "v1" "Secret" .Release.Namespace "arl-grpc-token" -}}
{{- if and $existing $existing.data.token -}}
{{- $existing.data.token | b64dec -}}
{{- else -}}
{{- randAlphaNum 48 -}}
{{- end -}}
{{- end -}}
{{- end }}
