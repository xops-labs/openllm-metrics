{{/*
Copyright 2026 Yasvanth Udayakumar
Licensed under the Apache License, Version 2.0.

Common helpers shared across every chart template.
*/}}

{{/*
Expand the chart release fullname. Truncated to 63 chars to stay within
Kubernetes resource-name limits.
*/}}
{{- define "openllm-metrics.fullname" -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "openllm-metrics.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels applied to every Kubernetes object the chart creates.
*/}}
{{- define "openllm-metrics.labels" -}}
helm.sh/chart: {{ include "openllm-metrics.chart" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/part-of: {{ .Values.global.partOf | default "openllm-metrics" }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
{{- end -}}

{{/*
Selector labels — the subset of common labels used by Service selectors
and Deployment selectors. NEVER add labels here that change between
upgrades (or pods become unselectable).
*/}}
{{- define "openllm-metrics.selectorLabels" -}}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
ServiceAccount name resolver — defaults to <fullname> when unset.
*/}}
{{- define "openllm-metrics.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "openllm-metrics.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
Image reference for a given service. Usage:
  {{ include "openllm-metrics.image" (dict "ctx" . "svc" .Values.openaiPoller) }}
Falls back to `.Chart.AppVersion` when `tag` is empty so the chart never
ships a `:latest` ambiguity. Pull policy defaults to the global value.
*/}}
{{- define "openllm-metrics.image" -}}
{{- $ctx := .ctx -}}
{{- $svc := .svc -}}
{{- $registry := $ctx.Values.global.imageRegistry -}}
{{- $tag := default $ctx.Chart.AppVersion $svc.image.tag -}}
{{- printf "%s/%s:%s" $registry $svc.image.repository $tag -}}
{{- end -}}

{{- define "openllm-metrics.pullPolicy" -}}
{{- $svc := .svc -}}
{{- $ctx := .ctx -}}
{{- default $ctx.Values.global.imagePullPolicy $svc.image.pullPolicy -}}
{{- end -}}
