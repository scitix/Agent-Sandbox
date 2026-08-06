{{/*
Expand the name of the chart.
*/}}
{{- define "agent-sandbox-hub.name" -}}
{{- .Chart.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.

The release name is the canonical prefix. fullnameOverride is the escape hatch
when you need a name that doesn't match the release. Examples:
  helm install agent-sandbox-hub ./...    → agent-sandbox-hub-*
  helm install agentbox-dashboard ./...   → agentbox-dashboard-*
  helm install foo ./... --set fullnameOverride=bar → bar-*
*/}}
{{- define "agent-sandbox-hub.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end }}

{{/*
Common labels
*/}}
{{- define "agent-sandbox-hub.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{ include "agent-sandbox-hub.selectorLabels" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "agent-sandbox-hub.selectorLabels" -}}
app.kubernetes.io/name: {{ include "agent-sandbox-hub.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Fully qualified name for the ws-proxy component.
*/}}
{{- define "agent-sandbox-hub.proxyFullname" -}}
{{- printf "%s-proxy" (include "agent-sandbox-hub.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Name of the images-catalog ConfigMap. The chart owns the object; ws-proxy reads
and writes it via the AGENTBOX_IMAGES_CATALOG_CONFIGMAP env var.
*/}}
{{- define "agent-sandbox-hub.imagesCatalogConfigMapName" -}}
{{- printf "%s-images-catalog" (include "agent-sandbox-hub.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Name of the notification ConfigMap. The chart does not render the object —
ws-proxy owns it entirely, bootstrapping it on first run and rewriting it on
every config change and every send, so a chart-managed copy would clobber
runtime state on upgrade.
*/}}
{{- define "agent-sandbox-hub.notificationConfigMapName" -}}
{{- printf "%s-notifications" (include "agent-sandbox-hub.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Selector labels for the ws-proxy Deployment / Service.
*/}}
{{- define "agent-sandbox-hub.proxySelectorLabels" -}}
app.kubernetes.io/name: {{ printf "%s-proxy" (include "agent-sandbox-hub.name" .) }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
