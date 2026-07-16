{{- define "helm-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "helm-operator.webhookServiceName" -}}
{{- printf "%s-webhook" (include "helm-operator.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "helm-operator.webhookCertificateName" -}}
{{- printf "%s-webhook-cert" (include "helm-operator.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "helm-operator.webhookIssuerName" -}}
{{- default (printf "%s-selfsigned" (include "helm-operator.fullname" .) | trunc 63 | trimSuffix "-") .Values.webhook.certManager.issuerRef.name }}
{{- end }}

{{- define "helm-operator.webhookSecretName" -}}
{{- if .Values.webhook.certManager.enabled }}
{{- include "helm-operator.webhookCertificateName" . }}
{{- else }}
{{- required "webhook.existingSecret is required when webhook.certManager.enabled is false" .Values.webhook.existingSecret }}
{{- end }}
{{- end }}

{{- define "helm-operator.fullname" -}}
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

{{- define "helm-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "helm-operator.labels" -}}
helm.sh/chart: {{ include "helm-operator.chart" . }}
{{ include "helm-operator.selectorLabels" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}

{{- define "helm-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "helm-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "helm-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "helm-operator.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- required "serviceAccount.name is required when serviceAccount.create is false" .Values.serviceAccount.name }}
{{- end }}
{{- end }}
