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

{{/*
Fully qualified name for the assistant component.
*/}}
{{- define "agent-sandbox-hub.assistantFullname" -}}
{{- printf "%s-assistant" (include "agent-sandbox-hub.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{- define "agent-sandbox-hub.assistantSelectorLabels" -}}
app.kubernetes.io/name: {{ include "agent-sandbox-hub.name" . }}-assistant
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: assistant
{{- end }}

{{/*
Whether this release renders its own Secret for the assistant.

Only when a key was inlined AND no existing Secret was named. Using `required`
to force one instead would break chart-import tooling, which parses the chart
with default values; a missing credential is a deploy-time failure, not an
import-time one.
*/}}
{{- define "agent-sandbox-hub.assistantInlineModelKey" -}}
{{- $a := .Values.assistant | default dict -}}
{{- $c := $a.claudeCode | default dict -}}
{{- if and $a.modelApiKey (not ($c.credentials | default dict).existingSecret) -}}
true
{{- end -}}
{{- end }}

{{- define "agent-sandbox-hub.assistantInlineSandboxKey" -}}
{{- $s := (.Values.assistant | default dict).sandbox | default dict -}}
{{- if and $s.apiKey (not ($s.credentials | default dict).existingSecret) -}}
true
{{- end -}}
{{- end }}

{{- define "agent-sandbox-hub.assistantSecretName" -}}
{{- printf "%s-assistant" (include "agent-sandbox-hub.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{/*
The environment every one of the assistant's sandboxes is created with.

The DECOY lands here, and it is the half of the injection pair the agent can
read. Its partner is the rule set below; a deployment that renders one without
the other either hands the agent a useless token (no rules) or a real one
(no decoy).
*/}}
{{- define "agent-sandbox-hub.assistantSandboxEnv" -}}
{{- $s := (.Values.assistant | default dict).sandbox | default dict -}}
{{- $inj := $s.injection | default dict -}}
{{- $env := dict -}}
{{- if $inj.enabled -}}
{{- if $inj.nativeHost -}}
{{- $_ := set $env "AGENTBOX_ENDPOINT" (printf "http://%s" $inj.nativeHost) -}}
{{- $_ := set $env "AGENTBOX_API_KEY" ($inj.decoy | default "") -}}
{{- end -}}
{{- if $inj.e2bHost -}}
{{- $_ := set $env "E2B_API_KEY" ($inj.decoy | default "") -}}
{{- /* The sandbox reaches the data plane through the IN-CLUSTER service, which
       is plain HTTP — a different path from the one `sandbox.https` describes,
       which is how the BRAIN reaches the same data plane through the worker's
       ingress. Reusing that value here would tell code inside the sandbox to
       speak TLS to a listener that does not, and the symptom is
       `tls handshake eof` from a URL that looks correct. */ -}}
{{- $_ := set $env "E2B_HTTPS" "false" -}}
{{- end -}}
{{- end -}}
{{- range $k, $v := ($s.extraEnv | default dict) -}}
{{- $_ := set $env $k $v -}}
{{- end -}}
{{- toJson $env -}}
{{- end }}

{{/*
The egress policy every one of the assistant's sandboxes is created with.

Built here rather than hand-written as JSON in values, because the shape has a
trap: rules are per host AND per header. The CLI reaches the native API with
AGENTBOX-API-KEY and the E2B SDK reaches the E2B API with X-API-Key, so a
deployment that declares one host ends up with an assistant that can read the
platform but cannot start a sandbox — reported as "401 invalid api key", which
points at the credential rather than at the missing rule.

The data-plane host is allowed but never injected: envd traffic carries its
own credential.
*/}}
{{- define "agent-sandbox-hub.assistantSandboxNetwork" -}}
{{- $s := (.Values.assistant | default dict).sandbox | default dict -}}
{{- $inj := $s.injection | default dict -}}
{{- if not $inj.enabled -}}
{{- "" -}}
{{- else -}}
{{- $allow := list -}}
{{- $rules := dict -}}
{{- if $inj.nativeHost -}}
{{- $allow = append $allow $inj.nativeHost -}}
{{- $hdr := dict "AGENTBOX-API-KEY" (printf "${e2b.secrets.%s}" ($inj.nativeSecretName | default "abx-key")) -}}
{{- $_ := set $rules $inj.nativeHost (list (dict "transform" (dict "headers" $hdr))) -}}
{{- end -}}
{{- if $inj.e2bHost -}}
{{- $allow = append $allow $inj.e2bHost -}}
{{- $hdr := dict "X-API-Key" (printf "${e2b.secrets.%s}" ($inj.e2bSecretName | default "e2b-key")) -}}
{{- $_ := set $rules $inj.e2bHost (list (dict "transform" (dict "headers" $hdr))) -}}
{{- end -}}
{{- if $inj.dataHost -}}
{{- $allow = append $allow $inj.dataHost -}}
{{- end -}}
{{- toJson (dict "allowOut" $allow "rules" $rules) -}}
{{- end -}}
{{- end }}
