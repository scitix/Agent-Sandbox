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
{{- /* True of every deployment of this chart, so they are set here rather than
       copied into each one's values. Both describe THE HOST rather than the
       cluster: a sandbox this assistant owns has a browser in front of it and
       a tool to ask a person with, and `abx` renders its hints differently
       because of that. A sandbox someone drives from their own terminal has
       neither, which is why these are not defaults of the CLI itself.
       Overridable through extraEnv below, which is applied last. */ -}}
{{- $_ := set $env "ABX_UI_MODE" "open_page" -}}
{{- $_ := set $env "AGENTBOX_APPROVAL_TOOL" "request_approval" -}}
{{- if $inj.enabled -}}
{{- if $inj.bffEndpoint -}}
{{- /* Routes by cluster in its path, so `abx --cluster X` reaches X through
       this one address. Bearer, not an API key: this is the dashboard's own
       session credential. */ -}}
{{- $_ := set $env "AGENTBOX_ENDPOINT" $inj.bffEndpoint -}}
{{- $_ := set $env "AGENTBOX_AUTH_SCHEME" "bearer" -}}
{{- $_ := set $env "AGENTBOX_API_KEY" ($inj.bffDecoy | default "") -}}
{{- /* Which cluster the sandbox is in, so ordinary commands need no --cluster.
       Without it a path-routing endpoint can reach several clusters and names
       none, so every command — including `whoami` — has to be told which one,
       which reads as the tool being broken rather than unconfigured.
       `--cluster X` still reaches any other. */ -}}
{{- with $inj.clusterID -}}
{{- $_ := set $env "AGENTBOX_CLUSTER" . -}}
{{- end -}}
{{- else if $inj.nativeHost -}}
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
{{- if $inj.bffEndpoint -}}
{{- /* One host and one rule for every cluster — the address routes, so the
       sandbox needs no reachability to any cluster gateway. Derive the host
       from the endpoint when it was not given separately. */ -}}
{{- $host := $inj.bffHost -}}
{{- if not $host -}}
{{- $host = regexReplaceAll "^https?://([^/]+).*$" $inj.bffEndpoint "${1}" -}}
{{- end -}}
{{- $allow = append $allow $host -}}
{{- $hdr := dict "Authorization" (printf "Bearer ${e2b.secrets.%s}" ($inj.bffSecretName | default "abx-jwt")) -}}
{{- $_ := set $rules $host (list (dict "transform" (dict "headers" $hdr))) -}}
{{- else if $inj.nativeHost -}}
{{- $allow = append $allow $inj.nativeHost -}}
{{- $hdr := dict "AGENTBOX-API-KEY" (printf "${e2b.secrets.%s}" ($inj.nativeSecretName | default "abx-key")) -}}
{{- $_ := set $rules $inj.nativeHost (list (dict "transform" (dict "headers" $hdr))) -}}
{{- end -}}
{{- if $inj.e2bHost -}}
{{- $allow = append $allow $inj.e2bHost -}}
{{- $hdr := dict "X-API-Key" (printf "${e2b.secrets.%s}" ($inj.e2bSecretName | default (include "agent-sandbox-hub.assistantNativeSecretName" $))) -}}
{{- $_ := set $rules $inj.e2bHost (list (dict "transform" (dict "headers" $hdr))) -}}
{{- end -}}
{{- if $inj.dataHost -}}
{{- $allow = append $allow $inj.dataHost -}}
{{- end -}}
{{- toJson (dict "allowOut" $allow "rules" $rules) -}}
{{- end -}}
{{- end }}

{{/*
The vault entries the rules above reference, as a comma-separated list.

The daemon writes the acting identity's credential under exactly these names,
and the rules resolve `${e2b.secrets.<name>}` against the vault of whoever
created the sandbox — so the two lists have to be the same list. Rendered from
the same values for that reason: maintained separately they drift, and the
symptom of a drift is a sandbox whose injected requests carry no credential,
visible only as `substituted=0` in a sidecar log.

The BFF-mode entry is included even though its header is a Bearer JWT: what the
daemon has to put there is still the acting identity's platform credential, and
the console's BFF accepts one in that position.
*/}}
{{/*
The vault entry the assistant's sandboxes authenticate from.

ONE entry, not one per header. The two rules differ only in which header they
write; the value they write is the same, and it could not be otherwise — the
daemon has exactly one credential to put there, the acting identity's own, and
it writes that under every name it is given. Two names were two spellings of
one secret, and the second bought nothing but a second write per create.

`e2bSecretName` is still honoured for a deployment whose E2B surface really
does take a different credential. It just no longer defaults to a second name.
*/}}
{{- define "agent-sandbox-hub.assistantNativeSecretName" -}}
{{- $inj := (((.Values.assistant | default dict).sandbox | default dict).injection | default dict) -}}
{{- $inj.nativeSecretName | default "abx-key" -}}
{{- end }}

{{- define "agent-sandbox-hub.assistantSessionSecretNames" -}}
{{- $s := (.Values.assistant | default dict).sandbox | default dict -}}
{{- $inj := $s.injection | default dict -}}
{{- if not $inj.enabled -}}
{{- "" -}}
{{- else -}}
{{- $names := list -}}
{{- if $inj.bffEndpoint -}}
{{- $names = append $names ($inj.bffSecretName | default "abx-jwt") -}}
{{- else if $inj.nativeHost -}}
{{- $names = append $names ($inj.nativeSecretName | default "abx-key") -}}
{{- end -}}
{{- if $inj.e2bHost -}}
{{- $names = append $names ($inj.e2bSecretName | default (include "agent-sandbox-hub.assistantNativeSecretName" $)) -}}
{{- end -}}
{{- /* Deduplicated, because the names now default to ONE name and the daemon
       would otherwise write the same credential to the same entry twice per
       sandbox create. */ -}}
{{- join "," (uniq $names) -}}
{{- end -}}
{{- end }}

{{/*
The console's own public address, e.g. "https://console.example.com/agentbox".

Assembled from the ingress this chart already declares rather than accepted as a
separate value: the host and the base path are both stated here once, and asking
for the whole URL again would let the two drift with no symptom until someone
follows a link that 404s.

Explicit `consoleBaseURL` still wins, for a deployment fronted by something this
chart cannot see (an external gateway, a vanity domain). Empty when there is no
ingress and no override — a refusal then carries no link, which is better than
one that leads nowhere.
*/}}
{{- define "agent-sandbox-hub.consoleBaseURL" -}}
{{- if .Values.consoleBaseURL -}}
{{- .Values.consoleBaseURL | trimSuffix "/" -}}
{{- else if (.Values.ingress).host -}}
{{- $scheme := "https" -}}
{{- if eq (toString ((.Values.ingress).tls | default true)) "false" -}}
{{- $scheme = "http" -}}
{{- end -}}
{{- printf "%s://%s%s" $scheme (.Values.ingress).host (.Values.basePath | default "" | trimSuffix "/") -}}
{{- end -}}
{{- end }}
