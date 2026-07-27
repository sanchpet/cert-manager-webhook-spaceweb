{{- define "cert-manager-webhook-spaceweb.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "cert-manager-webhook-spaceweb.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "cert-manager-webhook-spaceweb.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "cert-manager-webhook-spaceweb.labels" -}}
app.kubernetes.io/name: {{ include "cert-manager-webhook-spaceweb.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ include "cert-manager-webhook-spaceweb.chart" . }}
{{- end -}}

{{- define "cert-manager-webhook-spaceweb.selectorLabels" -}}
app: {{ include "cert-manager-webhook-spaceweb.name" . }}
release: {{ .Release.Name }}
{{- end -}}

{{- define "cert-manager-webhook-spaceweb.selfSignedIssuer" -}}
{{ printf "%s-selfsign" (include "cert-manager-webhook-spaceweb.fullname" .) }}
{{- end -}}

{{- define "cert-manager-webhook-spaceweb.rootCAIssuer" -}}
{{ printf "%s-ca" (include "cert-manager-webhook-spaceweb.fullname" .) }}
{{- end -}}

{{- define "cert-manager-webhook-spaceweb.rootCACertificate" -}}
{{ printf "%s-ca" (include "cert-manager-webhook-spaceweb.fullname" .) }}
{{- end -}}

{{- define "cert-manager-webhook-spaceweb.servingCertificate" -}}
{{ printf "%s-webhook-tls" (include "cert-manager-webhook-spaceweb.fullname" .) }}
{{- end -}}

{{/* Resolve the container image tag, defaulting to the chart appVersion. */}}
{{- define "cert-manager-webhook-spaceweb.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end -}}
