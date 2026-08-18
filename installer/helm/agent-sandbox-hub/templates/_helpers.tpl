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

{{/*
Namespace the ManagedAgents (and their Brain pods) run in.
*/}}
{{- define "agent-sandbox-hub.managedAgentNamespace" -}}
{{- .Values.managedAgent.namespace | default .Release.Namespace }}
{{- end }}

{{/*
Whether the default sandbox key is carried in values and must be rendered into
this release's Secret. Non-empty output means yes.

An inline key only works when the agents run in the release namespace: the
reference is resolved by the Brain pod's kubelet, which cannot read a Secret from
another namespace. Failing here is the point — the alternative is a Brain stuck
on a missing secretKeyRef, which says nothing about the value that caused it.
*/}}
{{- define "agent-sandbox-hub.managedAgentInlineSandboxKey" -}}
{{- $d := .Values.managedAgent.hands.default }}
{{- if and $d.apiKey (not $d.existingSecret.name) }}
{{- if ne (include "agent-sandbox-hub.managedAgentNamespace" .) .Release.Namespace }}
{{- fail "managedAgent.hands.default.apiKey cannot be used when managedAgent.namespace differs from the release namespace: the Brain resolves the Secret in its own namespace. Create the Secret there and set managedAgent.hands.default.existingSecret instead." }}
{{- end }}
{{- print "inline" }}
{{- end }}
{{- end }}

{{/*
Secret name + key holding the default sandbox supply's API key. Empty when the
deployment configured neither, which leaves the default without a credential —
reported on the agent rather than guessed at.
*/}}
{{- define "agent-sandbox-hub.managedAgentSandboxSecretName" -}}
{{- $d := .Values.managedAgent.hands.default }}
{{- if $d.existingSecret.name }}
{{- $d.existingSecret.name }}
{{- else if include "agent-sandbox-hub.managedAgentInlineSandboxKey" . }}
{{- include "agent-sandbox-hub.fullname" . }}-secret
{{- end }}
{{- end }}

{{- define "agent-sandbox-hub.managedAgentSandboxSecretKey" -}}
{{- $d := .Values.managedAgent.hands.default }}
{{- if $d.existingSecret.name }}
{{- $d.existingSecret.key | default "E2B_API_KEY" }}
{{- else if include "agent-sandbox-hub.managedAgentInlineSandboxKey" . }}
{{- print "MANAGED_AGENT_SANDBOX_API_KEY" }}
{{- end }}
{{- end }}

{{/*
Whether the model credential is rendered inline into this release's Secret.

Mirrors the sandbox key exactly, including the namespace guard: the Brain's
kubelet resolves a secretKeyRef in the Brain's OWN namespace, so an inline key is
only reachable when the agents run in the release namespace. Failing here beats
rendering a reference that resolves to nothing and surfaces as a harness reporting
itself unavailable.
*/}}
{{- define "agent-sandbox-hub.managedAgentInlineModelKey" -}}
{{- $m := .Values.managedAgent.modelProvider }}
{{- if and $m.apiKey (not $m.existingSecret.name) }}
{{- if ne (include "agent-sandbox-hub.managedAgentNamespace" .) .Release.Namespace }}
{{- fail "managedAgent.modelProvider.apiKey cannot be used when managedAgent.namespace differs from the release namespace: the Brain resolves the Secret in its own namespace. Create the Secret there and set managedAgent.modelProvider.existingSecret instead." }}
{{- end }}
{{- print "inline" }}
{{- end }}
{{- end }}

{{/*
Secret name + key holding the default model credential. Empty when the deployment
configured neither, which leaves agents to bring their own.
*/}}
{{- define "agent-sandbox-hub.managedAgentModelSecretName" -}}
{{- $m := .Values.managedAgent.modelProvider }}
{{- if $m.existingSecret.name }}
{{- $m.existingSecret.name }}
{{- else if include "agent-sandbox-hub.managedAgentInlineModelKey" . }}
{{- include "agent-sandbox-hub.fullname" . }}-secret
{{- end }}
{{- end }}

{{- define "agent-sandbox-hub.managedAgentModelSecretKey" -}}
{{- $m := .Values.managedAgent.modelProvider }}
{{- if $m.existingSecret.name }}
{{- $m.existingSecret.key | default "ANTHROPIC_AUTH_TOKEN" }}
{{- else if include "agent-sandbox-hub.managedAgentInlineModelKey" . }}
{{- print "MANAGED_AGENT_MODEL_API_KEY" }}
{{- end }}
{{- end }}
