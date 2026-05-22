{{/*
Expand the name of the chart.
*/}}
{{- define "agent-sandbox-worker.name" -}}
{{- .Chart.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.

The release name is the canonical prefix. fullnameOverride is the escape hatch
when you need a name that doesn't match the release.
*/}}
{{- define "agent-sandbox-worker.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end }}

{{/*
Component Service names. These DON'T follow fullname because they have been the
stable, externally-referenced DNS names since the chart's inception (referenced
by Hub clusters[].url, SDK E2B_DOMAIN, custom Ingresses, etc.). To keep upgrades
safe for existing installs, the literal historical names are the defaults.

Override values:
  controller.services.apiName       (default: agent-sandbox-api)
  controller.services.metricsName   (default: agent-sandbox-controller-metrics)
  controller.services.e2bName       (default: agent-sandbox-e2b-api)
  extproc.services.dataPlaneName    (default: agent-sandbox-data-plane)
*/}}
{{- define "agent-sandbox-worker.apiServiceName" -}}
{{- default "agent-sandbox-api" .Values.controller.services.apiName -}}
{{- end }}

{{- define "agent-sandbox-worker.metricsServiceName" -}}
{{- default "agent-sandbox-controller-metrics" .Values.controller.services.metricsName -}}
{{- end }}

{{- define "agent-sandbox-worker.e2bServiceName" -}}
{{- default "agent-sandbox-e2b-api" .Values.controller.services.e2bName -}}
{{- end }}

{{- define "agent-sandbox-worker.dataplaneServiceName" -}}
{{- default "agent-sandbox-data-plane" .Values.extproc.services.dataPlaneName -}}
{{- end }}

{{/*
Component "name" values — what goes into app.kubernetes.io/name.
Both default to deriving from the chart name (`name` for controller,
`name-extproc` for extproc), but each can be overridden so adopters
migrating from non-Helm installs can pin to whatever literal value their
existing Deployment selectors already use (which is immutable).
*/}}
{{- define "agent-sandbox-worker.controllerAppName" -}}
{{- if .Values.controller.appName -}}
{{- .Values.controller.appName -}}
{{- else -}}
{{- include "agent-sandbox-worker.name" . -}}
{{- end -}}
{{- end }}

{{- define "agent-sandbox-worker.extprocAppName" -}}
{{- if .Values.extproc.appName -}}
{{- .Values.extproc.appName -}}
{{- else -}}
{{- printf "%s-extproc" (include "agent-sandbox-worker.name" .) -}}
{{- end -}}
{{- end }}

{{/*
Common labels — applied to metadata.labels of every resource.
Deliberately does NOT include a control-plane label (that's component-specific
and is added in each Deployment via its own *SelectorLabels helper) or an
app.kubernetes.io/name (also component-specific). Do NOT use this for selector
matchLabels; use *SelectorLabels helpers, which return only the minimal stable
subset (Deployment.spec.selector is immutable, so churn breaks `kubectl apply`).
*/}}
{{- define "agent-sandbox-worker.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels — minimal stable subset used in:
  - Deployment.spec.selector.matchLabels (immutable)
  - Service.spec.selector
  - ServiceMonitor.spec.selector.matchLabels
  - Pod template metadata.labels (must be a superset of matchLabels)
Deliberately excludes app.kubernetes.io/instance: if you ever rename the
Helm release, the instance label would flip and the immutable matchLabels
would conflict — breaking upgrades.
*/}}
{{- define "agent-sandbox-worker.controllerSelectorLabels" -}}
app.kubernetes.io/name: {{ include "agent-sandbox-worker.controllerAppName" . }}
control-plane: controller-manager
{{- end }}

{{- define "agent-sandbox-worker.extprocSelectorLabels" -}}
app.kubernetes.io/name: {{ include "agent-sandbox-worker.extprocAppName" . }}
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
{{- printf "%s.%s.svc.cluster.local:9003" (include "agent-sandbox-worker.dataplaneServiceName" .) .Release.Namespace }}
{{- end }}
{{- end }}

{{/*
Resolved envoy gateway base URL.
*/}}
{{- define "agent-sandbox-worker.envoyGatewayBaseUrl" -}}
{{- if .Values.controller.envoyGatewayBaseUrl }}
{{- .Values.controller.envoyGatewayBaseUrl }}
{{- else }}
{{- printf "http://%s.%s.svc.cluster.local" (include "agent-sandbox-worker.dataplaneServiceName" .) .Release.Namespace }}
{{- end }}
{{- end }}

{{/*
Resolved E2B domain.
*/}}
{{- define "agent-sandbox-worker.e2bDomain" -}}
{{- if .Values.controller.e2bDomain }}
{{- .Values.controller.e2bDomain }}
{{- else }}
{{- printf "%s.%s.svc.cluster.local" (include "agent-sandbox-worker.dataplaneServiceName" .) .Release.Namespace }}
{{- end }}
{{- end }}
