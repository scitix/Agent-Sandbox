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

// Package managedagent renders and reconciles the Brain workload of a
// ManagedAgent.
//
// Rendering is a pure function so the whole object graph a ManagedAgent
// produces can be diffed against a known-good baseline in a unit test. That
// matters more than it sounds: the Brain's behaviour is configured almost
// entirely through environment variables, and a variable that silently fails to
// render does not crash anything — it turns a capability off. A missing sandbox
// image leaves the agent with sandboxes whose command endpoint never answers; a
// missing credentials file variable leaves its CLI unauthenticated; a missing
// telemetry variable simply stops the traces. None of those surface as an error.
package managedagent

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

const (
	// NamePrefix prefixes every object a ManagedAgent owns, so a Brain is
	// never mistaken for a hand-rolled Deployment of the same agent name.
	NamePrefix = "agentbox-brain-"

	// DefaultGatewayPort is the agent gateway: the one port callers use.
	DefaultGatewayPort int32 = 4099
	// DefaultWorkspaceFSPort serves attachment staging and the workspace file
	// browser. It is a separate process from the gateway, and the file panel
	// and attachment upload both stop working if it is not exposed.
	DefaultWorkspaceFSPort int32 = 8766
	// DefaultOpenCodePort is the loopback port for `opencode serve`.
	DefaultOpenCodePort int32 = 4096

	// RuntimeHome is the Brain image's HOME. Every path below is derived from it
	// rather than written out, because the image's runtime user owns these
	// directories and a path that disagrees with HOME is not a crash — it is an
	// unwritable directory the process falls back from silently.
	RuntimeHome = "/home/agents"

	// StateRoot is where the Brain keeps everything a restart must not lose:
	// the thread map, the Claude Code transcripts and the OpenCode session DB.
	//
	// All three live UNDER it, and it is mounted at exactly one path with no
	// subPath. That is the whole point of the layout: the volume used to be
	// mounted on the OpenCode data directory, which left the thread map and the
	// transcripts on the container filesystem — so a restart kept OpenCode's
	// sessions and lost the map naming them, and every conversation came back
	// empty while the transcripts sat there intact.
	StateRoot = RuntimeHome + "/state"
	// OpenCodeDBPath is OpenCode's SQLite file: sessions, messages, parts. Only
	// the database is placed on the volume, not the whole data directory — the
	// rest of it is an unused credential file and a log directory.
	OpenCodeDBPath = StateRoot + "/opencode/opencode.db"
	// ClaudeConfigDir holds one JSONL transcript per session.
	ClaudeConfigDir = StateRoot + "/claude"
	// ThreadStorePath is the gateway's thread map. Every harness builds its
	// history list from this file, so losing it loses the history even when the
	// transcripts survive.
	ThreadStorePath = StateRoot + "/gateway/threads.json"

	// OpenCodeConfigPath is where the OpenCode harness reads its runtime
	// config. The filename is fixed by the harness, and the directory by HOME.
	OpenCodeConfigPath = RuntimeHome + "/.config/opencode/opencode.json"

	labelName      = "app.kubernetes.io/name"
	labelInstance  = "app.kubernetes.io/instance"
	labelManagedBy = "app.kubernetes.io/managed-by"
	labelComponent = "app.kubernetes.io/component"

	// ManagedByValue marks every object this controller owns. The Hands
	// reconciler refuses to update a SandboxEnv that does not carry it, so a
	// user who takes manual ownership of an object keeps it.
	ManagedByValue = "agentbox-managedagent"

	// stateVolumeName is the volume holding everything a restart must not lose.
	stateVolumeName = "state"

	// ConfigChecksumAnnotation carries a hash of everything that is NOT part of
	// the pod spec but still changes the Brain's behaviour — the referenced
	// Secrets and ConfigMaps. Without it a credential rotation leaves the old
	// value live in a running pod with nothing to indicate it.
	ConfigChecksumAnnotation = "agentbox.navix.sh/config-checksum"
)

// Rendered is the full object graph one ManagedAgent produces.
type Rendered struct {
	Deployment *appsv1.Deployment
	Service    *corev1.Service
	// PVC is nil when session persistence is disabled or supplied by an
	// existing claim.
	PVC *corev1.PersistentVolumeClaim
}

// OpenCodeConfigSecretName is the Secret holding the generated opencode.json.
//
// It is generated rather than asked for because the harness must be pinned to a
// single provider: left to its own defaults OpenCode also loads every provider
// reachable without credentials, including its vendor's hosted free models, and
// those become both pickable and reachable by model id.
func OpenCodeConfigSecretName(agent string) string { return BrainName(agent) + "-opencode" }

// BrainName is the name every object of one agent's Brain shares.
func BrainName(agent string) string { return NamePrefix + agent }

// Endpoint is the in-cluster URL callers are given.
//
// It points at the control-plane proxy, not at the Brain's own Service. Both
// are in-cluster, but only one of them checks anything: the Brain takes the
// caller's word for which end user is asking, so handing out its address makes
// every pod in the cluster able to read any tenant's threads. Reporting the
// proxied address instead means the published route and the in-cluster route
// differ only in hostname — same path, same key, same authorization.
//
// proxyService is "<service>.<namespace>:<port>"; empty falls back to the
// Brain's own address, which is the only thing available on a deployment that
// has no proxy.
func Endpoint(agent, namespace string, port int32, proxyService string) string {
	if proxyService != "" {
		return fmt.Sprintf("http://%s/%s", proxyService, agent)
	}
	return fmt.Sprintf("http://%s.%s:%d", BrainName(agent), namespace, port)
}

// Render turns a ManagedAgent into the objects that implement it.
//
// It is pure: same spec in, same bytes out. `checksum` is the caller's hash of
// the referenced Secrets and ConfigMaps; passing "" omits the annotation.
func Render(ma *agentsv1alpha1.ManagedAgent, checksum string) (*Rendered, error) {
	return RenderWithDefaults(ma, checksum, RenderDefaults{})
}

// RenderDefaults supplies values the deployment owns rather than the agent.
//
// Passed in rather than read from a package variable so Render stays pure: the
// golden test and every unit test depend on the same spec producing the same bytes,
// and a default that could be mutated at process scope would make that conditional
// on whatever ran first.
type RenderDefaults struct {
	// BrainImage is used when the agent names no image of its own. Lets a caller
	// create an agent from a prompt alone, which is the point — requiring an image
	// reference means knowing which one carries a compatible gateway, and that is
	// the platform's business, not the tenant's.
	//
	// Empty keeps the image required, which is the correct behaviour for a
	// deployment that has not published one: inventing a reference would fail later
	// as an ImagePullBackOff, a long way from the cause.
	BrainImage agentsv1alpha1.ManagedAgentImage
}

// RenderWithDefaults renders an agent, filling anything it left unset from
// deployment-level defaults.
func RenderWithDefaults(
	ma *agentsv1alpha1.ManagedAgent,
	checksum string,
	defaults RenderDefaults,
) (*Rendered, error) {
	if ma == nil {
		return nil, fmt.Errorf("managedagent is nil")
	}
	if ma.Spec.Image.Repository == "" {
		if defaults.BrainImage.Repository == "" {
			return nil, fmt.Errorf(
				"spec.image.repository is required: this deployment has no default " +
					"Brain image configured")
		}
		// Copied rather than mutated in place: Render must not write to the object
		// its caller handed it, or a reconcile would be observed to change the spec
		// it was reconciling.
		ma = ma.DeepCopy()
		ma.Spec.Image = mergeImageDefaults(ma.Spec.Image, defaults.BrainImage)
	}
	if err := validateScenarios(ma.Spec.Scenarios); err != nil {
		return nil, err
	}

	name := BrainName(ma.Name)
	labels := brainLabels(ma.Name)

	env, err := renderEnv(ma)
	if err != nil {
		return nil, err
	}

	dep := renderDeployment(ma, name, labels, env, checksum)
	svc := renderService(ma, name, labels)
	pvc := renderPVC(ma, name, labels)

	return &Rendered{Deployment: dep, Service: svc, PVC: pvc}, nil
}

func validateScenarios(scenarios []agentsv1alpha1.ManagedAgentScenario) error {
	if len(scenarios) == 0 {
		return nil
	}
	defaults := 0
	seen := map[string]bool{}
	for _, s := range scenarios {
		if s.Name == "" {
			return fmt.Errorf("scenario name must not be empty")
		}
		if seen[s.Name] {
			return fmt.Errorf("duplicate scenario %q", s.Name)
		}
		seen[s.Name] = true
		if s.Default {
			defaults++
		}
	}
	if defaults != 1 {
		return fmt.Errorf("exactly one scenario must set default: true, found %d", defaults)
	}
	return nil
}

func brainLabels(agent string) map[string]string {
	return map[string]string{
		labelName:      "agentbox-brain",
		labelInstance:  agent,
		labelComponent: "brain",
		labelManagedBy: ManagedByValue,
	}
}

// GatewayPort is the port the Brain serves its agent API on. Exported because
// anything proxying to an agent has to address it by the same number the
// Deployment and Service were rendered with.
func GatewayPort(ma *agentsv1alpha1.ManagedAgent) int32 {
	if ma.Spec.Brain != nil && ma.Spec.Brain.GatewayPort != 0 {
		return ma.Spec.Brain.GatewayPort
	}
	return DefaultGatewayPort
}

// WorkspaceFSPort serves attachment staging and the workspace file browser. It
// is exported for the same reason as GatewayPort: a proxy in front of an agent
// must address it by the number the Service was rendered with.
func WorkspaceFSPort(ma *agentsv1alpha1.ManagedAgent) int32 {
	if ma.Spec.Brain != nil && ma.Spec.Brain.WorkspaceFSPort != 0 {
		return ma.Spec.Brain.WorkspaceFSPort
	}
	return DefaultWorkspaceFSPort
}

func openCodePort(ma *agentsv1alpha1.ManagedAgent) int32 {
	if oc := ma.Spec.Runtime.OpenCode; oc != nil && oc.Port != 0 {
		return oc.Port
	}
	return DefaultOpenCodePort
}

func openCodeEnabled(ma *agentsv1alpha1.ManagedAgent) bool {
	oc := ma.Spec.Runtime.OpenCode
	if oc == nil {
		return false
	}
	return oc.Enabled == nil || *oc.Enabled
}

// renderEnv builds the Brain's environment.
//
// Ordering is stable (platform variables first, in a fixed sequence, then the
// tenant's extras in declaration order) so the rendered pod spec does not churn
// between reconciles and a diff against a baseline stays readable.
func renderEnv(ma *agentsv1alpha1.ManagedAgent) ([]corev1.EnvVar, error) {
	var env []corev1.EnvVar
	add := func(name, value string) {
		if value != "" {
			env = append(env, corev1.EnvVar{Name: name, Value: value})
		}
	}
	addRef := func(name string, ref *agentsv1alpha1.SecretKeySelector) {
		if ref == nil || ref.Name == "" || ref.Key == "" {
			return
		}
		env = append(env, corev1.EnvVar{Name: name, ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: ref.Name},
				Key:                  ref.Key,
			},
		}})
	}

	appendSandboxEnv(ma, add)
	appendHandsEnv(ma, add, addRef)

	// --- harness selection ----------------------------------------------
	add("ASSISTANT_BACKEND", ma.Spec.Runtime.Default)
	add("ASSISTANT_GATEWAY_PORT", strconv.Itoa(int(GatewayPort(ma))))
	if openCodeEnabled(ma) {
		add("ASSISTANT_OC_ENABLED", "1")
		add("OPENCODE_INTERNAL_PORT", strconv.Itoa(int(openCodePort(ma))))
	}
	if cc := ma.Spec.Runtime.ClaudeCode; cc != nil {
		if err := appendClaudeCodeEnv(cc, add, addRef); err != nil {
			return nil, err
		}
	}
	if oc := ma.Spec.Runtime.OpenCode; oc != nil && openCodeEnabled(ma) {
		add("ASSISTANT_OC_DEFAULT_MODEL", oc.DefaultModel)
	}

	// --- session storage --------------------------------------------------
	// All three are rendered, not left to the runtime's defaults. A default that
	// resolves outside the mounted volume is the failure this layout exists to
	// prevent, and it is invisible until a restart loses the history.
	add("CLAUDE_CONFIG_DIR", ClaudeConfigDir)
	add("ASSISTANT_THREAD_STORE", ThreadStorePath)
	if openCodeEnabled(ma) {
		add("OPENCODE_DB", OpenCodeDBPath)
	}

	appendClassifierEnv(ma.Spec.Classifier, add, addRef)
	appendTelemetryEnv(ma.Spec.Observability, add, addRef)

	// --- tenant extras ----------------------------------------------------
	if ma.Spec.Brain != nil {
		env = append(env, ma.Spec.Brain.ExtraEnv...)
	}
	return env, nil
}

type envAdder func(name, value string)
type envRefAdder func(name string, ref *agentsv1alpha1.SecretKeySelector)

// appendSandboxEnv carries the sandbox lifecycle knobs.
func appendSandboxEnv(ma *agentsv1alpha1.ManagedAgent, add envAdder) {
	if b := ma.Spec.Hands.Binding; b != nil {
		if b.TimeoutSeconds > 0 {
			add("SBX_TIMEOUT", strconv.Itoa(int(b.TimeoutSeconds)))
		}
		if b.ReadyTimeoutSeconds > 0 {
			add("SBX_READY_TIMEOUT", strconv.Itoa(int(b.ReadyTimeoutSeconds)))
		}
		add("SBX_ATTACH_ROOT", b.AttachmentRoot)
		if b.MaxAttachmentBytes > 0 {
			add("SBX_MAX_ATTACH_BYTES", strconv.FormatInt(b.MaxAttachmentBytes, 10))
		}
		add("SBX_WORKSPACE", b.Workspace)
		if b.SkipSeed {
			add("SBX_SKIP_SEED", "1")
		}
		add("SBX_SEED_REPO", b.SeedRepo)
	}
	// The sandbox image is not an optimisation. A member pool's default image
	// does not run the sandbox command endpoint, so an agent that creates
	// sandboxes without an image override gets sandboxes that come up and then
	// refuse every command.
	if img := handsImage(ma); img != "" {
		add("SBX_IMAGE", img)
	}
}

// appendHandsEnv points the Brain at its sandbox supply.
func appendHandsEnv(ma *agentsv1alpha1.ManagedAgent, add envAdder, addRef envRefAdder) {
	// A sandbox service this control plane does not own is addressed entirely
	// through configuration: there is no SandboxEnv to read, so the endpoint,
	// the environment name and the credential all come from the spec.
	if ext := ma.Spec.Hands.External; ext != nil {
		add("AGBX_ENV_NAME", ext.EnvName)
		add("E2B_API_URL", ext.APIURL)
		add("E2B_DOMAIN", ext.Domain)
		// The scheme is not part of E2B_DOMAIN, and the sandbox client treats
		// anything other than the literal "true" as plain HTTP. Getting this
		// wrong is invisible on the control plane — the sandbox is created and
		// reported healthy, then every data-plane call to it times out.
		if ext.HTTPS == nil || *ext.HTTPS {
			add("AGBX_HTTPS", "true")
		}
		addRef("E2B_API_KEY", ext.CredentialsRef)
	}
	if ref := ma.Spec.Hands.EnvRef; ref != nil {
		add("AGBX_ENV_NAME", ref.Name)
	}
	if sg := handsScalingGroup(ma); sg != "" {
		add("SBX_SCALING_GROUP", sg)
	}
}

// appendClaudeCodeEnv configures the Anthropic-format harness.
func appendClaudeCodeEnv(cc *agentsv1alpha1.ClaudeCodeRuntime, add envAdder, addRef envRefAdder) error {
	add("ANTHROPIC_BASE_URL", cc.BaseURL)
	addRef("ANTHROPIC_AUTH_TOKEN", &cc.CredentialsRef)
	add("ANTHROPIC_DEFAULT_HAIKU_MODEL", cc.SmallModel)
	if len(cc.Models) > 0 {
		raw, err := json.Marshal(toModelWire(cc.Models))
		if err != nil {
			return fmt.Errorf("marshal claudeCode.models: %w", err)
		}
		add("ASSISTANT_CC_MODELS", string(raw))
	}
	add("ASSISTANT_CC_DEFAULT_MODEL", cc.DefaultModel)
	add("ASSISTANT_CC_EFFORT", cc.Effort)
	if len(cc.PluginPaths) > 0 {
		add("ASSISTANT_CC_PLUGIN_PATHS", strings.Join(cc.PluginPaths, ","))
	}
	return nil
}

// appendClassifierEnv configures the topic-switch check.
func appendClassifierEnv(c *agentsv1alpha1.ManagedAgentClassifier, add envAdder, addRef envRefAdder) {
	if c == nil || (c.Enabled != nil && !*c.Enabled) {
		return
	}
	add("ASSISTANT_CLASSIFIER_WIRE", c.Wire)
	add("ASSISTANT_CLASSIFIER_BASE_URL", c.BaseURL)
	add("ASSISTANT_CLASSIFIER_MODEL", c.Model)
	if c.MaxTokens > 0 {
		add("ASSISTANT_CLASSIFIER_MAX_TOKENS", strconv.Itoa(int(c.MaxTokens)))
	}
	if c.MaxContextChars > 0 {
		add("ASSISTANT_CLASSIFIER_MAX_CONTEXT", strconv.Itoa(int(c.MaxContextChars)))
	}
	if c.TimeoutSeconds > 0 {
		add("ASSISTANT_CLASSIFIER_TIMEOUT_SECONDS", strconv.Itoa(int(c.TimeoutSeconds)))
	}
	addRef("ASSISTANT_CLASSIFIER_API_KEY", c.CredentialsRef)
}

// appendTelemetryEnv configures trace export.
func appendTelemetryEnv(o *agentsv1alpha1.ManagedAgentObservability, add envAdder, addRef envRefAdder) {
	if o == nil || o.Langfuse == nil {
		return
	}
	lf := o.Langfuse
	if lf.Enabled != nil && !*lf.Enabled {
		return
	}
	add("LANGFUSE_BASEURL", lf.BaseURL)
	// Changing the environment splits an agent's history from every trace
	// recorded before it, with no way to merge them afterwards.
	add("LANGFUSE_ENVIRONMENT", lf.Environment)
	addRef("LANGFUSE_PUBLIC_KEY", lf.PublicKeyRef)
	addRef("LANGFUSE_SECRET_KEY", lf.SecretKeyRef)
}

type modelWire struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

func toModelWire(models []agentsv1alpha1.ManagedAgentModel) []modelWire {
	out := make([]modelWire, 0, len(models))
	for _, m := range models {
		out = append(out, modelWire{ID: m.ID, Name: m.Name})
	}
	return out
}

// handsImage is the sandbox main-container image the Brain launches with.
func handsImage(ma *agentsv1alpha1.ManagedAgent) string {
	if ref := ma.Spec.Hands.EnvRef; ref != nil && ref.Image != "" {
		return ref.Image
	}
	if auto := ma.Spec.Hands.Auto; auto != nil && auto.Image != "" {
		return auto.Image
	}
	if ext := ma.Spec.Hands.External; ext != nil && ext.Image != "" {
		return ext.Image
	}
	return ""
}

// handsScalingGroup pins sandboxes to one member pool, when asked.
func handsScalingGroup(ma *agentsv1alpha1.ManagedAgent) string {
	if ref := ma.Spec.Hands.EnvRef; ref != nil {
		return ref.ScalingGroup
	}
	if ext := ma.Spec.Hands.External; ext != nil {
		return ext.ScalingGroup
	}
	return ""
}

func renderEnvFrom(ma *agentsv1alpha1.ManagedAgent) []corev1.EnvFromSource {
	var out []corev1.EnvFromSource
	// The sandbox credential arrives through envFrom so the API key never
	// appears inline in the pod spec.
	if e := ma.Spec.Hands.E2B; e != nil && e.CredentialsSecret != "" {
		out = append(out, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: e.CredentialsSecret},
			},
		})
	}
	if ma.Spec.Brain != nil {
		out = append(out, ma.Spec.Brain.ExtraEnvFrom...)
	}
	return out
}

func renderPorts(ma *agentsv1alpha1.ManagedAgent) []corev1.ContainerPort {
	ports := []corev1.ContainerPort{
		{Name: "gw", ContainerPort: GatewayPort(ma), Protocol: corev1.ProtocolTCP},
		{Name: "fs", ContainerPort: WorkspaceFSPort(ma), Protocol: corev1.ProtocolTCP},
	}
	if openCodeEnabled(ma) {
		ports = append(ports, corev1.ContainerPort{
			Name: "opencode", ContainerPort: openCodePort(ma), Protocol: corev1.ProtocolTCP,
		})
	}
	if ma.Spec.Brain != nil {
		ports = append(ports, ma.Spec.Brain.ExtraPorts...)
	}
	return ports
}

func renderVolumes(ma *agentsv1alpha1.ManagedAgent, pvcName string) ([]corev1.Volume, []corev1.VolumeMount) {
	var vols []corev1.Volume
	var mounts []corev1.VolumeMount

	// State volume. Mounted at StateRoot with no subPath, so all three owners
	// land on it — see the note on StateRoot for what mounting it deeper cost.
	stateVol := corev1.Volume{Name: stateVolumeName}
	if claim := persistentClaimName(ma, pvcName); claim != "" {
		stateVol.PersistentVolumeClaim = &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claim}
	} else {
		stateVol.EmptyDir = &corev1.EmptyDirVolumeSource{}
	}
	vols = append(vols, stateVol)
	mounts = append(mounts, corev1.VolumeMount{Name: stateVolumeName, MountPath: StateRoot})

	// OpenCode's runtime config, projected under its fixed filename. A
	// hand-supplied Secret wins; otherwise the generated one is mounted.
	if secretName, key := openCodeConfigSource(ma); secretName != "" {
		vols = append(vols, corev1.Volume{
			Name: "opencode-config",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: secretName,
					Items:      []corev1.KeyToPath{{Key: key, Path: "opencode.json"}},
				},
			},
		})
		mounts = append(mounts, corev1.VolumeMount{
			Name:      "opencode-config",
			MountPath: OpenCodeConfigPath,
			SubPath:   "opencode.json",
		})
	}

	if ma.Spec.Brain != nil {
		vols = append(vols, ma.Spec.Brain.ExtraVolumes...)
		mounts = append(mounts, ma.Spec.Brain.ExtraVolumeMounts...)
	}
	return vols, mounts
}

// openCodeConfigSource names the Secret and key holding opencode.json.
func openCodeConfigSource(ma *agentsv1alpha1.ManagedAgent) (secretName, key string) {
	oc := ma.Spec.Runtime.OpenCode
	if oc == nil || !openCodeEnabled(ma) {
		return "", ""
	}
	if oc.ConfigSecretRef != nil && oc.ConfigSecretRef.Name != "" {
		return oc.ConfigSecretRef.Name, oc.ConfigSecretRef.Key
	}
	return OpenCodeConfigSecretName(ma.Name), "opencode.json"
}

// persistentClaimName returns the claim to mount, or "" for an emptyDir.
func persistentClaimName(ma *agentsv1alpha1.ManagedAgent, pvcName string) string {
	s := ma.Spec.Session
	if s == nil || s.Persistence == nil {
		return ""
	}
	p := s.Persistence
	if p.ExistingClaim != "" {
		return p.ExistingClaim
	}
	if p.Enabled != nil && *p.Enabled {
		return pvcName
	}
	return ""
}

func renderDeployment(
	ma *agentsv1alpha1.ManagedAgent,
	name string,
	labels map[string]string,
	env []corev1.EnvVar,
	checksum string,
) *appsv1.Deployment {
	gw := GatewayPort(ma)
	vols, mounts := renderVolumes(ma, name)

	podAnnotations := map[string]string{}
	if checksum != "" {
		podAnnotations[ConfigChecksumAnnotation] = checksum
	}

	probe := func(failureThreshold, periodSeconds, initialDelay, timeout int32) *corev1.Probe {
		return &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/healthz",
					Port: intstr.FromInt32(gw),
				},
			},
			FailureThreshold:    failureThreshold,
			PeriodSeconds:       periodSeconds,
			InitialDelaySeconds: initialDelay,
			TimeoutSeconds:      timeout,
		}
	}

	container := corev1.Container{
		Name:            "brain",
		Image:           imageRef(ma.Spec.Image),
		ImagePullPolicy: ma.Spec.Image.PullPolicy,
		Env:             env,
		EnvFrom:         renderEnvFrom(ma),
		Ports:           renderPorts(ma),
		VolumeMounts:    mounts,
		// Only the gateway is probed. The other processes in the pod have no
		// health endpoint, and the entrypoint already fails the container when
		// any of them exits.
		StartupProbe:   probe(90, 2, 0, 1),
		LivenessProbe:  probe(3, 30, 30, 5),
		ReadinessProbe: probe(3, 10, 5, 3),
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: ptr(false),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
			SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		},
	}
	if ma.Spec.Brain != nil {
		container.Resources = ma.Spec.Brain.Resources
	}

	podSpec := corev1.PodSpec{
		Containers: []corev1.Container{container},
		Volumes:    vols,
		// The image runs as an unprivileged fixed uid; the sandbox daemon
		// depends on that identity when it writes staged attachments.
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot: ptr(true),
			RunAsUser:    ptr(int64(1000)),
			RunAsGroup:   ptr(int64(1000)),
			FSGroup:      ptr(int64(1000)),
		},
		ImagePullSecrets: ma.Spec.Image.PullSecrets,
	}
	if ma.Spec.Brain != nil {
		podSpec.NodeSelector = ma.Spec.Brain.NodeSelector
		podSpec.Tolerations = ma.Spec.Brain.Tolerations
		podSpec.Affinity = ma.Spec.Brain.Affinity
		podSpec.ServiceAccountName = ma.Spec.Brain.ServiceAccountName
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ma.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			// Exactly one replica, replaced rather than rolled. The
			// session-to-sandbox map lives in the daemon's memory and a sandbox
			// handle cannot be recovered by another process, so a second
			// replica does not share work — it silently creates a second
			// sandbox for the same thread and the user's files vanish.
			Replicas: ptr(int32(1)),
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{
				labelName:     labels[labelName],
				labelInstance: labels[labelInstance],
			}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: podAnnotations,
				},
				Spec: podSpec,
			},
		},
	}
}

func renderService(ma *agentsv1alpha1.ManagedAgent, name string, labels map[string]string) *corev1.Service {
	container := renderPorts(ma)
	ports := make([]corev1.ServicePort, 0, len(container))
	for _, p := range container {
		ports = append(ports, corev1.ServicePort{
			Name:       p.Name,
			Port:       p.ContainerPort,
			TargetPort: intstr.FromInt32(p.ContainerPort),
			Protocol:   corev1.ProtocolTCP,
		})
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ma.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				labelName:     labels[labelName],
				labelInstance: labels[labelInstance],
			},
			Ports: ports,
		},
	}
}

func renderPVC(ma *agentsv1alpha1.ManagedAgent, name string, labels map[string]string) *corev1.PersistentVolumeClaim {
	s := ma.Spec.Session
	if s == nil || s.Persistence == nil {
		return nil
	}
	p := s.Persistence
	if p.Enabled == nil || !*p.Enabled || p.ExistingClaim != "" {
		return nil
	}
	size := resource.MustParse("5Gi")
	if p.Size != nil {
		size = *p.Size
	}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ma.Namespace,
			Labels:    labels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			// ReadWriteOnce is the honest mode: the state directory holds a
			// SQLite database that must never be opened by two processes.
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: size},
			},
		},
	}
	if p.StorageClass != "" {
		pvc.Spec.StorageClassName = &p.StorageClass
	}
	return pvc
}

// mergeImageDefaults fills an agent's image from the deployment default.
//
// Field by field rather than wholesale, so an agent that set only a pull policy or
// only pull secrets keeps them while still getting the default repository and tag.
// Those two travel together: a tag from the agent applied to the default repository
// would name an image nobody published.
func mergeImageDefaults(
	img, def agentsv1alpha1.ManagedAgentImage,
) agentsv1alpha1.ManagedAgentImage {
	img.Repository = def.Repository
	img.Tag = def.Tag
	if img.PullPolicy == "" {
		img.PullPolicy = def.PullPolicy
	}
	if len(img.PullSecrets) == 0 {
		img.PullSecrets = def.PullSecrets
	}
	return img
}

func imageRef(img agentsv1alpha1.ManagedAgentImage) string {
	if img.Tag == "" {
		return img.Repository
	}
	return img.Repository + ":" + img.Tag
}

// ScenarioNames lists the scenarios a Brain serves, sorted for a stable status.
func ScenarioNames(ma *agentsv1alpha1.ManagedAgent) []string {
	out := make([]string, 0, len(ma.Spec.Scenarios))
	for _, s := range ma.Spec.Scenarios {
		out = append(out, s.Name)
	}
	sort.Strings(out)
	return out
}

func ptr[T any](v T) *T { return &v }

// PublicURL is the address external callers use, or "" when the agent is not
// published. base is the shared route this deployment serves agents on.
func PublicURL(ma *agentsv1alpha1.ManagedAgent, base string) string {
	if ma.Spec.Ingress == nil || !ma.Spec.Ingress.Enabled || base == "" {
		return ""
	}
	return strings.TrimSuffix(base, "/") + "/" + ma.Name
}
