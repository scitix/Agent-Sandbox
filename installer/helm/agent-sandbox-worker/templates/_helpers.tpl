{{/*
Expand the name of the chart.
*/}}
{{- define "agent-sandbox-worker.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "agent-sandbox-worker.fullname" -}}
{{- default .Chart.Name .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "agent-sandbox-worker.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{ include "agent-sandbox-worker.controllerSelectorLabels" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels for the controller deployment.
*/}}
{{- define "agent-sandbox-worker.controllerSelectorLabels" -}}
app.kubernetes.io/name: {{ include "agent-sandbox-worker.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
control-plane: controller-manager
{{- end }}

{{/*
Selector labels for the extproc deployment.
*/}}
{{- define "agent-sandbox-worker.extprocSelectorLabels" -}}
app.kubernetes.io/name: {{ include "agent-sandbox-worker.name" . }}-extproc
app.kubernetes.io/instance: {{ .Release.Name }}
control-plane: extproc-manager
{{- end }}

{{/*
Namespace helper — always use Release.Namespace.
*/}}
{{- define "agent-sandbox-worker.namespace" -}}
{{ .Release.Namespace }}
{{- end }}

{{/*
Name for the admin-key Secret.
*/}}
{{- define "agent-sandbox-worker.secretName" -}}
{{- if .Values.controller.secrets.existingSecret }}
{{- .Values.controller.secrets.existingSecret }}
{{- else }}
{{- include "agent-sandbox-worker.fullname" . }}-admin-key
{{- end }}
{{- end }}

{{/*
Name of the cross-cluster routing ConfigMap. Both controller and extproc read
this via --clusters-configmap-name; the chart owns the ConfigMap object itself.
*/}}
{{- define "agent-sandbox-worker.clustersConfigMapName" -}}
{{- printf "%s-clusters-config" (include "agent-sandbox-worker.fullname" .) }}
{{- end }}

{{/*
Resolved extproc internal API URL.
*/}}
{{- define "agent-sandbox-worker.extprocInternalApiUrl" -}}
{{- if .Values.controller.extprocInternalApiUrl }}
{{- .Values.controller.extprocInternalApiUrl }}
{{- else }}
{{- printf "agent-sandbox-data-plane.%s.svc.cluster.local:9003" .Release.Namespace }}
{{- end }}
{{- end }}

{{/*
Resolved envoy gateway base URL.
*/}}
{{- define "agent-sandbox-worker.envoyGatewayBaseUrl" -}}
{{- if .Values.controller.envoyGatewayBaseUrl }}
{{- .Values.controller.envoyGatewayBaseUrl }}
{{- else }}
{{- printf "http://agent-sandbox-data-plane.%s.svc.cluster.local" .Release.Namespace }}
{{- end }}
{{- end }}

{{/*
Resolved E2B domain.
*/}}
{{- define "agent-sandbox-worker.e2bDomain" -}}
{{- if .Values.controller.e2bDomain }}
{{- .Values.controller.e2bDomain }}
{{- else }}
{{- printf "agent-sandbox-data-plane.%s.svc.cluster.local" .Release.Namespace }}
{{- end }}
{{- end }}
