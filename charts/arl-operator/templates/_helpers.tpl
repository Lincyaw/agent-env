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
{{- if .Values.clickhouse.enabled }}
{{- printf "%s-clickhouse" .Release.Name }}
{{- else }}
{{- .Values.clickhouse.external.host }}
{{- end }}
{{- end }}

{{/*
ClickHouse address
*/}}
{{- define "arl-operator.clickhouseAddr" -}}
{{- if .Values.clickhouse.enabled }}
{{- printf "%s-clickhouse:%d" .Release.Name (int .Values.clickhouse.external.port | default 9000) }}
{{- else }}
{{- printf "%s:%d" .Values.clickhouse.external.host (int .Values.clickhouse.external.port | default 9000) }}
{{- end }}
{{- end }}
