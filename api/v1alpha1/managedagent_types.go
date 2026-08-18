// Copyright 2026 ScitiX
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ManagedAgentSpec declares one hosted agent: a Brain (the harness-independent
// agent runtime, one Deployment per agent) plus the Hands it reaches for
// (a SandboxEnv, either referenced or derived).
//
// The field layout deliberately splits two kinds of configuration:
//
//   - PLATFORM INVARIANTS get first-class fields. Anything whose misconfiguration
//     would break an isolation guarantee or fail silently — identity, tool
//     visibility, sandbox binding, session storage — is modelled here so the
//     controller, not the tenant, owns it.
//   - TENANT-SPECIFIC configuration rides `brain.extraEnv` / `extraVolumes` /
//     `extraPorts`. These are a deliberate escape hatch, not a dumping ground:
//     anything that turns out to be common across agents should graduate into a
//     first-class field. They exist because a hosted agent inevitably carries
//     business configuration the platform has no business understanding (an
//     upstream API base, a credentials file, a vendor MCP port).
type ManagedAgentSpec struct {
	// DisplayName is shown in the console. Defaults to metadata.name.
	// +optional
	DisplayName string `json:"displayName,omitempty"`

	// Description is free-form text for the console.
	// +optional
	Description string `json:"description,omitempty"`

	// Owner is stamped from the authenticated caller when the agent is created
	// and is not settable through the console. An agent belongs to one person,
	// not to their whole team: a teammate listing agents does not see it, and a
	// request for it is answered 404 rather than 403 — whether someone else's
	// agent exists is itself not disclosed.
	// +optional
	Owner *ManagedAgentOwner `json:"owner,omitempty"`

	// Image is the Brain image. It must ship every harness listed under
	// `runtime`, plus the sandbox daemon and the workspace-fs server.
	//
	// Optional: omitted, the agent runs the deployment's default Brain image, which
	// is what lets an agent be created from a prompt alone. Set it to pin a specific
	// build, or when the deployment publishes no default.
	// +optional
	Image ManagedAgentImage `json:"image,omitempty"`

	// Runtime declares which harnesses this agent can serve and how each reaches
	// its model endpoint. At least one must be configured, otherwise the Brain
	// starts and reports every harness unavailable through GET /backends.
	// +required
	Runtime ManagedAgentRuntime `json:"runtime"`

	// Classifier configures the one-shot "is this a new topic?" check. It is
	// deliberately NOT part of `runtime`: reusing a harness's model would make
	// the check's cost and behaviour change whenever a user switches harness.
	// +optional
	Classifier *ManagedAgentClassifier `json:"classifier,omitempty"`

	// Prompt is the base system prompt shared by every scenario — the sandbox
	// contract (what exists in the image, what the cwd is, what gets reclaimed).
	// Scenario prompts are APPENDED to it, never substituted: a statement that
	// stops being true when the scenario changes does not belong here.
	// +optional
	Prompt *ManagedAgentPrompt `json:"prompt,omitempty"`

	// Scenarios are the agent's slices. Each bundles a prompt addendum, a tool
	// allow-list, an optional harness pin and sandbox env. Exactly one entry
	// must set `default: true`.
	// +optional
	// +listType=map
	// +listMapKey=name
	Scenarios []ManagedAgentScenario `json:"scenarios,omitempty"`

	// Tools is the registry: which MCP servers and client-side tools exist, how
	// to reach them and where their credentials live.
	//
	// Registration is not visibility. A registered tool is invisible until some
	// scenario names it in `allow`, and visibility is computed server-side into
	// the tool set the harness is given — an unlisted tool is absent from the
	// model's tool list rather than refused at call time, which would burn turns
	// and disclose that it exists.
	// +optional
	Tools *ToolPolicySpec `json:"tools,omitempty"`

	// Ingress publishes this agent outside the cluster.
	//
	// Traffic never reaches the Brain directly. The Brain authenticates nothing
	// — it takes the caller's word for which end user is asking, which is safe
	// only because it is unreachable from outside — so exposing it as-is would
	// let anyone read every tenant's threads and drive the agent's tools.
	// External callers arrive through the control-plane proxy, which checks the
	// API key and that the key's owner may use this agent before forwarding.
	// +optional
	Ingress *ManagedAgentIngress `json:"ingress,omitempty"`

	// Hands declares sandbox supply: reference an existing SandboxEnv, derive
	// one, or point at an external service. At most one branch may be set.
	//
	// Declaring none is how an agent takes the deployment's default sandbox
	// supply, which is what lets one be created from a prompt alone. The default
	// is resolved on every reconcile rather than copied in at creation, so the
	// deployment can re-point it — a sandbox image tag that rolls with its own
	// build would otherwise leave every agent pinned to whatever was current the
	// day it was created. A deployment that publishes no default answers an agent
	// without a branch with HandsReady=False.
	// +optional
	Hands ManagedAgentHands `json:"hands,omitzero"`

	// Session covers thread persistence.
	// +optional
	Session *ManagedAgentSession `json:"session,omitempty"`

	// Observability is Langfuse today.
	// +optional
	Observability *ManagedAgentObservability `json:"observability,omitempty"`

	// Docs is operator-supplied Markdown shown as the agent's landing tab in the
	// console: how to reach this agent, what its scenarios mean, which tools they
	// expose. It travels with the object rather than being compiled into the
	// console so a deployment can write it in its own language and keep it
	// accurate for its own conventions.
	// +optional
	Docs string `json:"docs,omitempty"`

	// Brain tunes the Deployment. Replicas are always 1 with strategy Recreate,
	// because the session-to-sandbox map lives in the daemon's memory: a second
	// replica that has not served a thread would build it a second sandbox, and the
	// thread's files would appear and disappear depending on which replica answered.
	//
	// This is a property of the daemon, not of the platform. Re-attaching to a
	// running sandbox by id works, so lifting the constraint is a matter of moving
	// that map somewhere both replicas can read.
	// +optional
	Brain *ManagedAgentBrain `json:"brain,omitempty"`
}

// ManagedAgentOwner identifies who the agent belongs to.
//
// Both fields come from the caller's credentials at creation time. They are
// recorded in the spec so the object is self-describing (and re-appliable), but
// the API overwrites them on create and rejects changes on update.
type ManagedAgentOwner struct {
	// +optional
	Team string `json:"team,omitempty"`
	// +optional
	User string `json:"user,omitempty"`
}

// ManagedAgentImage is the Brain container image.
type ManagedAgentImage struct {
	// Repository is the image to run. Optional: left empty, the agent gets the
	// deployment's default Brain image, so an agent can be created from a prompt
	// alone. A deployment that publishes no default rejects an agent without one,
	// rather than guessing a reference that would surface later as a pull failure.
	// +optional
	Repository string `json:"repository,omitempty"`
	// +optional
	Tag string `json:"tag,omitempty"`
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	// +optional
	PullPolicy corev1.PullPolicy `json:"pullPolicy,omitempty"`
	// +optional
	PullSecrets []corev1.LocalObjectReference `json:"pullSecrets,omitempty"`
}

// ManagedAgentRuntime declares the harnesses served by this agent.
type ManagedAgentRuntime struct {
	// Default is the harness a conversation starts under when the caller picks
	// none. Every CONFIGURED harness is served regardless — the browser has a
	// picker — so this is a default, not a restriction.
	// +kubebuilder:validation:Enum=claude-code;opencode
	// +required
	Default string `json:"default"`

	// ClaudeCode speaks the Anthropic Messages API. Anthropic does not support
	// routing Claude Code at non-Claude models, so baseURL must serve them.
	// +optional
	ClaudeCode *ClaudeCodeRuntime `json:"claudeCode,omitempty"`

	// OpenCode speaks an OpenAI-compatible API.
	// +optional
	OpenCode *OpenCodeRuntime `json:"opencode,omitempty"`
}

// ClaudeCodeRuntime configures the Claude Agent SDK harness.
type ClaudeCodeRuntime struct {
	// BaseURL is an Anthropic-format endpoint. Empty means api.anthropic.com.
	// +optional
	BaseURL string `json:"baseURL,omitempty"`

	// CredentialsRef supplies ANTHROPIC_AUTH_TOKEN.
	// +required
	CredentialsRef SecretKeySelector `json:"credentialsRef"`

	// Models is the ONLY source of the in-composer dropdown. The Claude Agent
	// SDK's supportedModels() is a static table compiled into the SDK and never
	// queries the endpoint, so a configured list is the only correct answer.
	// +optional
	Models []ManagedAgentModel `json:"models,omitempty"`

	// +optional
	DefaultModel string `json:"defaultModel,omitempty"`

	// SmallModel backs the harness's own side tasks (titles and the like).
	// +optional
	SmallModel string `json:"smallModel,omitempty"`

	// +kubebuilder:validation:Enum=low;medium;high;xhigh;max
	// +optional
	Effort string `json:"effort,omitempty"`

	// PluginPaths are in-image plugin directories carrying skills, sub-agents,
	// hooks and .mcp.json. They coexist with an empty settingSources, which is
	// how product assets load without reading a developer's own ~/.claude.
	// +optional
	PluginPaths []string `json:"pluginPaths,omitempty"`
}

// OpenCodeRuntime configures the OpenCode harness.
//
// OpenCode is pinned to exactly one OpenAI-compatible provider. Left to itself
// it also loads every provider it can reach without credentials — including its
// vendor's own hosted free models — which would put both in the model picker
// and reachable by model id, i.e. this deployment's data leaving for a third
// party. The rendered config therefore carries a server-side allow-list naming
// only the provider below, so an unlisted provider is not merely hidden but
// unusable.
type OpenCodeRuntime struct {
	// Enabled withdraws the harness from the picker AND stops the entrypoint
	// from launching its server — one value drives both, so the picker can never
	// offer a harness whose server nobody started.
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Port is the loopback port for `opencode serve`.
	// +kubebuilder:default=4096
	// +optional
	Port int32 `json:"port,omitempty"`

	// BaseURL is the OpenAI-compatible endpoint this harness may reach.
	// +optional
	BaseURL string `json:"baseURL,omitempty"`

	// CredentialsRef supplies the provider API key. The console collects the key
	// itself and the platform materialises this reference; it can also be set
	// directly to an existing Secret.
	// +optional
	CredentialsRef *SecretKeySelector `json:"credentialsRef,omitempty"`

	// Models is the exact in-composer dropdown for this harness.
	// +optional
	Models []ManagedAgentModel `json:"models,omitempty"`

	// +optional
	DefaultModel string `json:"defaultModel,omitempty"`

	// ProviderID names the single provider in the generated config. It is not
	// cosmetic: every model is addressed as "<providerID>/<model-id>", so this
	// string is part of the model identity the composer sends back. Changing it
	// on a live agent invalidates whatever model ids callers have stored.
	// +kubebuilder:default=platform
	// +optional
	ProviderID string `json:"providerID,omitempty"`

	// ProviderName is the provider's display label in the model picker.
	// +optional
	ProviderName string `json:"providerName,omitempty"`

	// Overlay is harness configuration the platform does not model: plugins,
	// telemetry switches, tool output limits. It is merged *underneath* the
	// generated keys, so an overlay cannot widen the provider allow-list,
	// repoint the endpoint, or re-enable a gated tool — the keys that decide
	// where data goes stay generated.
	//
	// This is the seam that keeps the CRD from growing a field per harness
	// knob. Use ConfigSecretRef instead only when you want no generated config
	// at all.
	// +optional
	Overlay *apiextensionsv1.JSON `json:"overlay,omitempty"`

	// ConfigSecretRef brings your own opencode.json instead of the generated
	// one. It bypasses the provider allow-list above, so a hand-managed config
	// must set `enabled_providers` itself.
	// +optional
	ConfigSecretRef *SecretKeySelector `json:"configSecretRef,omitempty"`
}

// ManagedAgentModel is one entry of a harness's model dropdown.
type ManagedAgentModel struct {
	// +required
	ID string `json:"id"`
	// +optional
	Name string `json:"name,omitempty"`
	// NonReasoning marks a model eligible to back the topic classifier. A
	// reasoning model spends its completion budget on the chain of thought and
	// returns empty content, which reads as "same topic" every time.
	// +optional
	NonReasoning bool `json:"nonReasoning,omitempty"`
}

// ManagedAgentClassifier configures the topic-switch check.
type ManagedAgentClassifier struct {
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
	// +kubebuilder:validation:Enum=anthropic-messages;openai-chat
	// +optional
	Wire string `json:"wire,omitempty"`
	// +optional
	BaseURL string `json:"baseURL,omitempty"`
	// +optional
	CredentialsRef *SecretKeySelector `json:"credentialsRef,omitempty"`
	// Model must be a non-reasoning id (see ManagedAgentModel.NonReasoning).
	// +optional
	Model string `json:"model,omitempty"`
	// +optional
	MaxTokens int32 `json:"maxTokens,omitempty"`
	// +optional
	MaxContextChars int32 `json:"maxContextChars,omitempty"`
	// TimeoutSeconds must stay below any reverse proxy read timeout in front of
	// the Brain; a classifier timeout fails safe to "same topic".
	// +optional
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`
}

// ManagedAgentPrompt is the shared base system prompt.
type ManagedAgentPrompt struct {
	// Inline is the prompt text.
	// +optional
	Inline string `json:"inline,omitempty"`
	// From reads the prompt from a ConfigMap key. Takes precedence over Inline.
	// +optional
	From *ConfigMapKeySelector `json:"from,omitempty"`
	// Append is added after whatever the harness's own preset contributes.
	// +optional
	Append string `json:"append,omitempty"`
}

// ManagedAgentScenario is one slice of an agent: same image, same base prompt,
// different persona and different visible tools.
type ManagedAgentScenario struct {
	// Name is the identifier callers pass on /run. DNS-1123 label.
	// +required
	Name string `json:"name"`

	// +optional
	DisplayName string `json:"displayName,omitempty"`

	// Default marks the scenario used when a caller names none. Exactly one.
	// +optional
	Default bool `json:"default,omitempty"`

	// Prompt is APPENDED to spec.prompt, never substituted.
	// +optional
	Prompt *ManagedAgentPrompt `json:"prompt,omitempty"`

	// Runtime pins this scenario to one harness. Unattended flows should pin,
	// so a user switching harness in the console does not drag background
	// analysis onto an unverified model configuration.
	// +kubebuilder:validation:Enum=claude-code;opencode
	// +optional
	Runtime string `json:"runtime,omitempty"`

	// Model overrides the harness default for this scenario.
	// +optional
	Model string `json:"model,omitempty"`

	// Allow is the tool allow-list: which registered MCP servers and
	// client-side tools this scenario may see. The sandbox toolset and
	// AskUserQuestion are not listed here — they are on by default.
	//
	// Registration (spec.tools) and visibility (this field) are separate on
	// purpose, and visibility is DENY BY DEFAULT: forgetting to configure a
	// scenario must fail towards "the tool is invisible", never towards "the
	// agent can post to a chat group". Visibility is computed server-side and
	// decides which tools are registered with the harness at all — an unlisted
	// tool is absent from the model's tool list rather than rejected at call
	// time, which would burn turns and leak the tool's existence.
	// +optional
	Allow []string `json:"allow,omitempty"`

	// Disable removes individual sandbox tools (bash, read, write, edit, grep,
	// glob, apply_patch) from this scenario.
	// +optional
	Disable []string `json:"disable,omitempty"`

	// Interactive false means the agent cannot ask the user a question: its
	// prompts degrade to plain text instead of rendering a card nobody can
	// click. Unattended flows must set this false.
	// +kubebuilder:default=true
	// +optional
	Interactive *bool `json:"interactive,omitempty"`

	// SandboxEnv is injected into the sandbox at CREATE time and is immutable
	// for that sandbox's life. That is why a thread is bound to one scenario
	// when it is created and cannot switch afterwards.
	// +optional
	SandboxEnv []corev1.EnvVar `json:"sandboxEnv,omitempty"`

	// ScalingGroup routes this scenario's sandboxes to one member pool
	// (rendered as the reserved metadata key agentbox.scitix.ai/scaling-group).
	// +optional
	ScalingGroup string `json:"scalingGroup,omitempty"`

	// Image overrides the sandbox image for this scenario while still reusing
	// the same SandboxEnv and warm pool.
	// +optional
	Image string `json:"image,omitempty"`

	// Exposed false hides the scenario from the console picker and the public
	// scenario list; callers may still name it explicitly.
	// +kubebuilder:default=true
	// +optional
	Exposed *bool `json:"exposed,omitempty"`
}

// ToolPolicySpec registers the tools an agent's scenarios may draw from.
type ToolPolicySpec struct {
	// MCP servers reachable by the harness.
	// +optional
	// +listType=map
	// +listMapKey=name
	MCP []MCPServerSpec `json:"mcp,omitempty"`

	// ClientSide tools are executed by the CALLER, not by the sandbox: the
	// platform only forwards the call and waits for a result. This is how an
	// agent drives its caller's own surface — navigating a page, opening a
	// panel — which no hosted runtime could do on the caller's behalf.
	// +optional
	// +listType=map
	// +listMapKey=name
	ClientSide []ClientToolSpec `json:"clientSide,omitempty"`

	// Approval gates individual tools behind a human decision. Patterns are
	// matched in order and the first hit wins; anything unmatched is allowed.
	// +optional
	Approval []ToolApproval `json:"approval,omitempty"`
}

// MCPServerSpec is one registered MCP server.
type MCPServerSpec struct {
	// +required
	Name string `json:"name"`
	// +kubebuilder:validation:Enum=http;sse;stdio
	// +required
	Transport string `json:"transport"`
	// +optional
	URL string `json:"url,omitempty"`
	// Command runs a stdio server inside the Brain container.
	// +optional
	Command []string `json:"command,omitempty"`
	// HeadersFrom supplies auth headers from a Secret.
	// +optional
	HeadersFrom *SecretKeySelector `json:"headersFrom,omitempty"`
}

// ClientToolSpec declares a tool the caller executes.
type ClientToolSpec struct {
	// +required
	Name string `json:"name"`
	// +required
	Description string `json:"description"`
	// InputSchema is JSON Schema, passed to the harness unchanged.
	// +optional
	// +kubebuilder:pruning:PreserveUnknownFields
	InputSchema *apiextensionsv1.JSON `json:"inputSchema,omitempty"`
	// TimeoutSeconds bounds how long the platform waits for the caller.
	// +optional
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`
}

// ToolApproval is one rule of the approval policy.
type ToolApproval struct {
	// Pattern matches a tool name; a trailing "*" matches a prefix.
	// +required
	Pattern string `json:"pattern"`
	// +kubebuilder:validation:Enum=allow;ask;deny
	// +required
	Action string `json:"action"`
	// Prompt is shown to whoever is asked to approve.
	// +optional
	Prompt string `json:"prompt,omitempty"`
}

// ManagedAgentHands declares sandbox supply.
type ManagedAgentHands struct {
	// EnvRef reuses an existing SandboxEnv. The controller only probes it; it
	// writes nothing on the worker side.
	// +optional
	EnvRef *HandsEnvRef `json:"envRef,omitempty"`

	// Auto derives a SandboxEnv named "<agent>-hands" with one member pool per
	// instance type, named "<agent>-hands-<instanceType>".
	// +optional
	Auto *HandsAutoSpec `json:"auto,omitempty"`

	// External points at an E2B-compatible sandbox service this control plane
	// does not own — another AgentBox installation, or a managed offering. The
	// platform never inspects or reconciles it; the Brain simply speaks the API.
	// +optional
	External *HandsExternal `json:"external,omitempty"`

	// Binding covers how a thread is tied to a sandbox.
	// +optional
	Binding *HandsBinding `json:"binding,omitempty"`

	// E2B is the endpoint and credential the Brain uses to reach the sandbox
	// API. When omitted, the Brain falls back to in-cluster service DNS, which
	// only works on a cluster that runs the worker chart.
	// +optional
	E2B *HandsE2B `json:"e2b,omitempty"`
}

// HandsEnvRef references an existing SandboxEnv.
type HandsEnvRef struct {
	// ClusterID scopes the env to one cluster. Empty means the local cluster.
	// +optional
	ClusterID string `json:"clusterID,omitempty"`
	// +required
	Name string `json:"name"`
	// Namespace of the SandboxEnv; empty means the agent's namespace.
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// ScalingGroup pins every sandbox to one member pool unless a scenario
	// overrides it.
	// +optional
	ScalingGroup string `json:"scalingGroup,omitempty"`
	// Image overrides the sandbox main container image.
	//
	// Leaving this empty is not equivalent to "use a sensible default": a pool's
	// default image does not run envd, so sandboxes created without an override
	// come up and then answer every command with a 502. Either set it here or on
	// every scenario.
	// +optional
	Image string `json:"image,omitempty"`
}

// HandsExternal describes a sandbox service owned by someone else.
//
// This is what a deployment pointing at a different installation looks like:
// there is no SandboxEnv object to read, so nothing is reconciled and readiness
// is whatever the remote API reports at call time.
type HandsExternal struct {
	// APIURL is the E2B-compatible control endpoint.
	// +required
	APIURL string `json:"apiURL"`

	// Domain is the data-plane gateway, host plus any ingress path. Omitting the
	// path is the usual cause of "the sandbox exists but no port answers".
	// +required
	Domain string `json:"domain"`

	// +kubebuilder:default=true
	// +optional
	HTTPS *bool `json:"https,omitempty"`

	// EnvName is the name to launch from at the remote, written verbatim: a bare
	// environment ("navix"), or one scoped to a cluster ("cluster::navix").
	// +required
	EnvName string `json:"envName"`

	// Image overrides the sandbox main container image.
	//
	// A pool's default image does not run the sandbox command endpoint, so
	// leaving this empty yields sandboxes that start and then refuse every
	// command, with no error on the control plane.
	// +optional
	Image string `json:"image,omitempty"`

	// ScalingGroup pins every sandbox to one member pool at the remote.
	// +optional
	ScalingGroup string `json:"scalingGroup,omitempty"`

	// CredentialsRef supplies the remote's API key.
	// +optional
	CredentialsRef *SecretKeySelector `json:"credentialsRef,omitempty"`
}

// HandsAutoSpec derives a SandboxEnv and its member pools.
type HandsAutoSpec struct {
	// +required
	ClusterID string `json:"clusterID"`
	// +required
	TemplateRef string `json:"templateRef"`
	// +optional
	Image string `json:"image,omitempty"`
	// +required
	InstanceTypes []HandsInstanceType `json:"instanceTypes"`
	// +optional
	IdleTimeoutSeconds int32 `json:"idleTimeoutSeconds,omitempty"`
	// +optional
	StartupTimeoutSeconds int32 `json:"startupTimeoutSeconds,omitempty"`
}

// HandsInstanceType is one member pool of a derived SandboxEnv.
type HandsInstanceType struct {
	// Name is the member's identity. It is the instance-type catalog entry on
	// a cluster that has the catalog enabled, and the scaling group otherwise;
	// either way it is the name sandboxes are routed to.
	// +required
	Name string `json:"name"`

	// Resources sizes the member directly, for clusters with no instance-type
	// catalog. Setting it switches the member from a catalog lookup to an
	// inline size — on a cluster without a catalog, a member that only names
	// an instance type is rejected, and on one with a catalog these two ways
	// of sizing are mutually exclusive.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// +optional
	Replicas int32 `json:"replicas,omitempty"`
	// +optional
	MinReplicas *int32 `json:"minReplicas,omitempty"`
	// +optional
	MaxReplicas *int32 `json:"maxReplicas,omitempty"`
	// +optional
	Default bool `json:"default,omitempty"`
}

// ManagedAgentIngress publishes one agent to callers outside the cluster.
//
// There is no host or path here: publishing is a single shared route on the
// control-plane proxy, and the agent's name in that route is what selects it.
// One route means one hostname, one certificate and one authenticating hop for
// every agent, so a caller holds a single base URL and a single key for the
// whole platform.
type ManagedAgentIngress struct {
	// Enabled publishes the agent. While false the shared route answers 404 for
	// this agent's name, so an agent is never reachable from outside by
	// accident — being deployed is not the same as being published.
	// +kubebuilder:default=false
	// +optional
	Enabled bool `json:"enabled,omitempty"`
}

// HandsBinding covers thread-to-sandbox lifecycle.
type HandsBinding struct {
	// Scope is the granularity a sandbox is bound at.
	// +kubebuilder:validation:Enum=thread;user
	// +kubebuilder:default=thread
	// +optional
	Scope string `json:"scope,omitempty"`

	// TimeoutSeconds is the sandbox IDLE timeout.
	// +optional
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`

	// ReadyTimeoutSeconds bounds how long a cold image pull may take.
	//
	// Note that readiness is not the same as usability: the sandbox API reports
	// a sandbox running before envd accepts commands, so callers must retry the
	// first command rather than trust the readiness flag alone.
	// +optional
	ReadyTimeoutSeconds int32 `json:"readyTimeoutSeconds,omitempty"`

	// AttachmentRoot is where staged attachments are flushed inside the sandbox,
	// written as root and world-readable so the agent can read but not alter
	// them.
	// +optional
	AttachmentRoot string `json:"attachmentRoot,omitempty"`

	// +optional
	MaxAttachmentBytes int64 `json:"maxAttachmentBytes,omitempty"`

	// Workspace is the FALLBACK sandbox working directory, used only when the
	// per-user directory cannot be resolved.
	// +optional
	Workspace string `json:"workspace,omitempty"`

	// SkipSeed suppresses the daemon's workspace seeding for images that already
	// carry it.
	// +optional
	SkipSeed bool `json:"skipSeed,omitempty"`

	// SeedRepo is cloned into the workspace on sandbox creation. Ignored when
	// SkipSeed is set.
	// +optional
	SeedRepo string `json:"seedRepo,omitempty"`
}

// HandsE2B is the sandbox API endpoint and credential.
type HandsE2B struct {
	// CredentialsSecret carries E2B_API_KEY and AGBX_ENV_NAME, optionally
	// AGBX_HTTPS / E2B_DOMAIN / E2B_API_URL. It is consumed with envFrom, so
	// the Brain never has these values inlined in its pod spec.
	// +optional
	CredentialsSecret string `json:"credentialsSecret,omitempty"`
	// +optional
	APIURL string `json:"apiURL,omitempty"`
	// +optional
	Domain string `json:"domain,omitempty"`
	// +optional
	HTTPS *bool `json:"https,omitempty"`
}

// ManagedAgentSession covers thread persistence.
type ManagedAgentSession struct {
	// +optional
	Persistence *SessionPersistence `json:"persistence,omitempty"`
	// RetentionDays prunes threads older than this. Zero means keep forever.
	// +optional
	RetentionDays int32 `json:"retentionDays,omitempty"`
}

// SessionPersistence backs the state directory.
//
// The state directory is mounted at ONE path, at the volume root, and must not
// be split into per-owner subPath mounts: the runtime converts a volume it does
// not recognise by clearing its root, and a marker written under one subPath is
// invisible to the others, so every restart would look like a fresh volume.
type SessionPersistence struct {
	// Enabled false uses an emptyDir — history is lost on restart.
	// +kubebuilder:default=false
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
	// ExistingClaim mounts a claim this controller does not manage.
	// +optional
	ExistingClaim string `json:"existingClaim,omitempty"`
	// +optional
	Size *resource.Quantity `json:"size,omitempty"`
	// +optional
	StorageClass string `json:"storageClass,omitempty"`
}

// ManagedAgentObservability is telemetry configuration.
type ManagedAgentObservability struct {
	// +optional
	Langfuse *LangfuseSpec `json:"langfuse,omitempty"`
}

// LangfuseSpec configures trace export.
type LangfuseSpec struct {
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
	// +optional
	BaseURL string `json:"baseURL,omitempty"`
	// +optional
	PublicKeyRef *SecretKeySelector `json:"publicKeyRef,omitempty"`
	// +optional
	SecretKeyRef *SecretKeySelector `json:"secretKeyRef,omitempty"`
	// Environment separates deployments inside one Langfuse project. Changing it
	// on a live agent splits its history from every trace recorded before, with
	// no way to merge the two afterwards.
	// +optional
	Environment string `json:"environment,omitempty"`
}

// ManagedAgentBrain tunes the Brain Deployment.
type ManagedAgentBrain struct {
	// GatewayPort is the agent gateway's port — the one contract callers use.
	// +kubebuilder:default=4099
	// +optional
	GatewayPort int32 `json:"gatewayPort,omitempty"`

	// WorkspaceFSPort serves attachment staging and the workspace file browser.
	// It is a separate process from the gateway and must be exposed for the
	// file panel and attachment upload to work at all.
	// +kubebuilder:default=8766
	// +optional
	WorkspaceFSPort int32 `json:"workspaceFSPort,omitempty"`

	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitzero"`
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// ExtraEnv is passed to the Brain verbatim, secretKeyRef included. See the
	// note on ManagedAgentSpec: this is where tenant-specific configuration
	// lives until (and unless) it earns a first-class field.
	// +optional
	ExtraEnv []corev1.EnvVar `json:"extraEnv,omitempty"`

	// +optional
	ExtraEnvFrom []corev1.EnvFromSource `json:"extraEnvFrom,omitempty"`
	// +optional
	ExtraVolumes []corev1.Volume `json:"extraVolumes,omitempty"`
	// +optional
	ExtraVolumeMounts []corev1.VolumeMount `json:"extraVolumeMounts,omitempty"`
	// ExtraPorts exposes additional in-pod processes (a vendor MCP server, for
	// instance) on the Brain Service.
	// +optional
	ExtraPorts []corev1.ContainerPort `json:"extraPorts,omitempty"`
}

// SecretKeySelector points at one key of a Secret.
type SecretKeySelector struct {
	// +required
	Name string `json:"name"`
	// +required
	Key string `json:"key"`
}

// ConfigMapKeySelector points at one key of a ConfigMap.
type ConfigMapKeySelector struct {
	// +required
	Name string `json:"name"`
	// +required
	Key string `json:"key"`
}

// ManagedAgentStatus is the observed state.
type ManagedAgentStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Phase is a coarse rollup: Pending, Provisioning, Ready, Degraded, Failed.
	// +optional
	Phase string `json:"phase,omitempty"`

	// Endpoint is the in-cluster URL of the agent gateway. It reaches the Brain
	// directly and is therefore unauthenticated — only ever hand it to callers
	// already inside the cluster.
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// PublicURL is the address external callers use, set only while the agent
	// is published. Unlike Endpoint it goes through the authenticating proxy,
	// so a caller needs an API key whose owner may use this agent.
	// +optional
	PublicURL string `json:"publicURL,omitempty"`

	// Hands reports the resolved sandbox supply.
	// +optional
	Hands *ResolvedHands `json:"hands,omitempty"`

	// Backends mirrors the Brain's own view of each harness, including why one
	// is unavailable. A harness failing preflight is reported here rather than
	// taking the pod down — a bad model credential must not stop the other
	// processes in the pod from serving.
	// +optional
	Backends []BackendStatus `json:"backends,omitempty"`

	// Scenarios lists the scenario names currently served.
	// +optional
	Scenarios []string `json:"scenarios,omitempty"`

	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ResolvedHands is the sandbox supply the Brain will actually use.
type ResolvedHands struct {
	// Source says where the supply came from: "agent" when the spec declares a
	// branch, "platformDefault" when it declares none and the deployment's default
	// answered instead. Without it the two are indistinguishable on the object,
	// since the default is applied per reconcile and never written to the spec.
	// +kubebuilder:validation:Enum=agent;platformDefault
	// +optional
	Source string `json:"source,omitempty"`

	// +optional
	ClusterID string `json:"clusterID,omitempty"`
	// +optional
	EnvName string `json:"envName,omitempty"`
	// +optional
	Pools []string `json:"pools,omitempty"`
	// +optional
	Ready bool `json:"ready,omitempty"`
}

// BackendStatus is one harness's availability.
type BackendStatus struct {
	// +required
	ID string `json:"id"`
	// +optional
	Available bool `json:"available,omitempty"`
	// +optional
	Reason string `json:"reason,omitempty"`
}

// ManagedAgent condition types.
const (
	// ManagedAgentConditionBrainReady is true when the Deployment is available
	// and the gateway answers its health probe.
	ManagedAgentConditionBrainReady = "BrainReady"
	// ManagedAgentConditionBackendsAvailable is true when at least one harness
	// passed preflight.
	ManagedAgentConditionBackendsAvailable = "BackendsAvailable"
	// ManagedAgentConditionHandsReady is true when the referenced or derived
	// SandboxEnv exists and can serve.
	ManagedAgentConditionHandsReady = "HandsReady"
	// ManagedAgentConditionSandboxReachable is true when the configured sandbox
	// credential and endpoint answer.
	ManagedAgentConditionSandboxReachable = "SandboxReachable"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=mag
// +kubebuilder:printcolumn:name="Runtime",type=string,JSONPath=`.spec.runtime.default`
// +kubebuilder:printcolumn:name="Hands",type=string,JSONPath=`.status.hands.envName`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Endpoint",type=string,JSONPath=`.status.endpoint`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ManagedAgent is the Schema for the managedagents API.
type ManagedAgent struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ManagedAgent
	// +required
	Spec ManagedAgentSpec `json:"spec"`

	// status defines the observed state of ManagedAgent
	// +optional
	Status ManagedAgentStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ManagedAgentList contains a list of ManagedAgent.
type ManagedAgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ManagedAgent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ManagedAgent{}, &ManagedAgentList{})
}
