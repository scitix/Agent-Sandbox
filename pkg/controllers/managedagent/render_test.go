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

package managedagent

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// fullAgent is a ManagedAgent with every renderable field populated. The env
// assertions below are the executable form of "the Brain is configured almost
// entirely through environment variables": a field that stops rendering turns a
// capability off without failing anything, so each one is pinned here by name.
func fullAgent() *agentsv1alpha1.ManagedAgent {
	size := resource.MustParse("5Gi")
	return &agentsv1alpha1.ManagedAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"},
		Spec: agentsv1alpha1.ManagedAgentSpec{
			Image: agentsv1alpha1.ManagedAgentImage{
				Repository: "registry.example.com/agentbox/brain",
				Tag:        "v1",
				PullPolicy: corev1.PullIfNotPresent,
			},
			Runtime: agentsv1alpha1.ManagedAgentRuntime{
				Default: "claude-code",
				ClaudeCode: &agentsv1alpha1.ClaudeCodeRuntime{
					BaseURL:        "https://models.example.com",
					CredentialsRef: agentsv1alpha1.SecretKeySelector{Name: "model", Key: "API_KEY"},
					Models: []agentsv1alpha1.ManagedAgentModel{
						{ID: "claude-sonnet-5", Name: "Claude Sonnet 5"},
					},
					DefaultModel: "claude-sonnet-5",
					SmallModel:   "claude-haiku-4-5-20251001",
					Effort:       "medium",
					PluginPaths:  []string{"/opt/plugin"},
				},
				OpenCode: &agentsv1alpha1.OpenCodeRuntime{
					Port:            4096,
					ConfigSecretRef: &agentsv1alpha1.SecretKeySelector{Name: "oc", Key: "opencode.json"},
				},
			},
			Classifier: &agentsv1alpha1.ManagedAgentClassifier{
				Wire:            "openai-chat",
				BaseURL:         "https://models.example.com",
				Model:           "fast-model",
				MaxTokens:       512,
				MaxContextChars: 200000,
				TimeoutSeconds:  20,
				CredentialsRef:  &agentsv1alpha1.SecretKeySelector{Name: "model", Key: "API_KEY"},
			},
			Scenarios: []agentsv1alpha1.ManagedAgentScenario{
				{Name: "interactive", Default: true},
				{Name: "batch", Interactive: ptr(false)},
			},
			Hands: agentsv1alpha1.ManagedAgentHands{
				EnvRef: &agentsv1alpha1.HandsEnvRef{
					Name:  "demo-env",
					Image: "registry.example.com/agentbox/sandbox:v1",
				},
				Binding: &agentsv1alpha1.HandsBinding{
					TimeoutSeconds:      3600,
					ReadyTimeoutSeconds: 600,
					AttachmentRoot:      "/opt/attachments",
					MaxAttachmentBytes:  8388608,
					Workspace:           "/home/user/workspace",
				},
				E2B: &agentsv1alpha1.HandsE2B{CredentialsSecret: "sandbox-creds"},
			},
			Session: &agentsv1alpha1.ManagedAgentSession{
				Persistence: &agentsv1alpha1.SessionPersistence{Enabled: ptr(true), Size: &size},
			},
			Observability: &agentsv1alpha1.ManagedAgentObservability{
				Langfuse: &agentsv1alpha1.LangfuseSpec{
					BaseURL:      "https://langfuse.example.com",
					Environment:  "staging",
					PublicKeyRef: &agentsv1alpha1.SecretKeySelector{Name: "lf", Key: "pk"},
					SecretKeyRef: &agentsv1alpha1.SecretKeySelector{Name: "lf", Key: "sk"},
				},
			},
			Brain: &agentsv1alpha1.ManagedAgentBrain{
				ExtraEnv: []corev1.EnvVar{{Name: "TENANT_API_BASE", Value: "https://tenant.example.com"}},
			},
		},
	}
}

func envMap(t *testing.T, dep *appsv1.Deployment) map[string]corev1.EnvVar {
	t.Helper()
	out := map[string]corev1.EnvVar{}
	for _, e := range dep.Spec.Template.Spec.Containers[0].Env {
		if _, dup := out[e.Name]; dup {
			t.Fatalf("env %q rendered twice; the last one silently wins at runtime", e.Name)
		}
		out[e.Name] = e
	}
	return out
}

func TestRenderEnvCoversEveryConfiguredField(t *testing.T) {
	r, err := Render(fullAgent(), "abc123")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	env := envMap(t, r.Deployment)

	wantLiteral := map[string]string{
		"SBX_TIMEOUT":                          "3600",
		"SBX_READY_TIMEOUT":                    "600",
		"SBX_ATTACH_ROOT":                      "/opt/attachments",
		"SBX_MAX_ATTACH_BYTES":                 "8388608",
		"SBX_WORKSPACE":                        "/home/user/workspace",
		"SBX_IMAGE":                            "registry.example.com/agentbox/sandbox:v1",
		"ASSISTANT_BACKEND":                    "claude-code",
		"ASSISTANT_GATEWAY_PORT":               "4099",
		"ASSISTANT_OC_ENABLED":                 "1",
		"OPENCODE_INTERNAL_PORT":               "4096",
		"ANTHROPIC_BASE_URL":                   "https://models.example.com",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":        "claude-haiku-4-5-20251001",
		"ASSISTANT_CC_MODELS":                  `[{"id":"claude-sonnet-5","name":"Claude Sonnet 5"}]`,
		"ASSISTANT_CC_DEFAULT_MODEL":           "claude-sonnet-5",
		"ASSISTANT_CC_EFFORT":                  "medium",
		"ASSISTANT_CC_PLUGIN_PATHS":            "/opt/plugin",
		"CLAUDE_CONFIG_DIR":                    ClaudeConfigDir,
		"ASSISTANT_THREAD_STORE":               ThreadStorePath,
		"ASSISTANT_CLASSIFIER_WIRE":            "openai-chat",
		"ASSISTANT_CLASSIFIER_BASE_URL":        "https://models.example.com",
		"ASSISTANT_CLASSIFIER_MODEL":           "fast-model",
		"ASSISTANT_CLASSIFIER_MAX_TOKENS":      "512",
		"ASSISTANT_CLASSIFIER_MAX_CONTEXT":     "200000",
		"ASSISTANT_CLASSIFIER_TIMEOUT_SECONDS": "20",
		"LANGFUSE_BASEURL":                     "https://langfuse.example.com",
		"LANGFUSE_ENVIRONMENT":                 "staging",
		"TENANT_API_BASE":                      "https://tenant.example.com",
	}
	for name, want := range wantLiteral {
		got, ok := env[name]
		if !ok {
			t.Errorf("env %q missing: a capability configured in the spec would be silently off", name)
			continue
		}
		if got.Value != want {
			t.Errorf("env %q = %q, want %q", name, got.Value, want)
		}
	}

	wantRef := map[string][2]string{
		"ANTHROPIC_AUTH_TOKEN":         {"model", "API_KEY"},
		"ASSISTANT_CLASSIFIER_API_KEY": {"model", "API_KEY"},
		"LANGFUSE_PUBLIC_KEY":          {"lf", "pk"},
		"LANGFUSE_SECRET_KEY":          {"lf", "sk"},
	}
	for name, want := range wantRef {
		got, ok := env[name]
		if !ok {
			t.Errorf("env %q missing", name)
			continue
		}
		var ref *corev1.SecretKeySelector
		if got.ValueFrom != nil {
			ref = got.ValueFrom.SecretKeyRef
		}
		if ref == nil || ref.Name != want[0] || ref.Key != want[1] {
			t.Errorf("env %q secretKeyRef = %+v, want %v/%v", name, ref, want[0], want[1])
		}
	}
}

// A pool's default image does not run the sandbox command endpoint, so an agent
// rendered without an image override produces sandboxes that start and then
// refuse every command — with no error anywhere in the control plane.
func TestRenderSandboxImageComesFromEitherHandsBranch(t *testing.T) {
	ma := fullAgent()
	ma.Spec.Hands.EnvRef = nil
	ma.Spec.Hands.Auto = &agentsv1alpha1.HandsAutoSpec{
		ClusterID:     "c1",
		TemplateRef:   "tmpl",
		Image:         "registry.example.com/agentbox/sandbox:auto",
		InstanceTypes: []agentsv1alpha1.HandsInstanceType{{Name: "1c2gi", Replicas: 1, Default: true}},
	}
	r, err := Render(ma, "")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got := envMap(t, r.Deployment)["SBX_IMAGE"].Value; got != "registry.example.com/agentbox/sandbox:auto" {
		t.Errorf("SBX_IMAGE = %q, want the auto branch's image", got)
	}
}

// The scheme for the external sandbox service travels in its own variable —
// it is not part of the domain, and the sandbox client compares it against the
// literal "true". Getting the name or the spelling wrong is invisible on the
// control plane: sandboxes are still created and still report healthy, and only
// the data-plane calls to them time out.
func TestRenderExternalHandsCarriesTheSchemeFlag(t *testing.T) {
	for _, tc := range []struct {
		name  string
		https *bool
		want  string
	}{
		{"unset defaults to https", nil, "true"},
		{"explicitly on", ptr(true), "true"},
		{"explicitly off", ptr(false), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ma := fullAgent()
			ma.Spec.Hands.EnvRef = nil
			ma.Spec.Hands.Auto = nil
			ma.Spec.Hands.External = &agentsv1alpha1.HandsExternal{
				APIURL:  "https://console.example.com/agent-sandbox/api/e2b",
				Domain:  "console.example.com/agent-sandbox/api/data",
				EnvName: "navix",
				HTTPS:   tc.https,
			}
			r, err := Render(ma, "")
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			env := envMap(t, r.Deployment)
			if got := env["AGBX_HTTPS"].Value; got != tc.want {
				t.Errorf("AGBX_HTTPS = %q, want %q", got, tc.want)
			}
			// E2B_HTTPS belongs to the standalone Python SDK, not to this
			// image. Rendering it here reads as configured while doing nothing.
			if _, ok := env["E2B_HTTPS"]; ok {
				t.Error("E2B_HTTPS is rendered, but the brain image reads AGBX_HTTPS")
			}
		})
	}
}

// The state volume must be mounted at the volume root. The runtime clears a
// volume whose layout marker it cannot see, and a marker written under one
// subPath is invisible to the others — so a split mount discards the session
// history on every restart.
func TestRenderStateVolumeIsMountedAtRootWithoutSubPath(t *testing.T) {
	r, err := Render(fullAgent(), "")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := r.Deployment.Spec.Template.Spec.Containers[0]
	var found bool
	for _, m := range c.VolumeMounts {
		if m.Name != stateVolumeName {
			continue
		}
		found = true
		if m.MountPath != StateRoot {
			t.Errorf("state mountPath = %q, want %q", m.MountPath, StateRoot)
		}
		if m.SubPath != "" {
			t.Errorf("state mount uses subPath %q; that discards history on restart", m.SubPath)
		}
	}
	if !found {
		t.Fatal("no state volume mount rendered")
	}
}

// A second replica does not share work: the session-to-sandbox map is in-process
// and a sandbox handle cannot be adopted by another process, so the second pod
// creates a second sandbox for the same thread.
func TestRenderIsSingleReplicaRecreate(t *testing.T) {
	r, err := Render(fullAgent(), "")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got := *r.Deployment.Spec.Replicas; got != 1 {
		t.Errorf("replicas = %d, want 1", got)
	}
	if got := r.Deployment.Spec.Strategy.Type; got != appsv1.RecreateDeploymentStrategyType {
		t.Errorf("strategy = %s, want Recreate", got)
	}
}

// The workspace-fs port is a separate process from the gateway. Without it the
// file panel and attachment upload have nothing to talk to.
func TestRenderExposesGatewayAndWorkspaceFS(t *testing.T) {
	r, err := Render(fullAgent(), "")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	ports := map[string]int32{}
	for _, p := range r.Deployment.Spec.Template.Spec.Containers[0].Ports {
		ports[p.Name] = p.ContainerPort
	}
	for name, want := range map[string]int32{"gw": 4099, "fs": 8766, "opencode": 4096} {
		if ports[name] != want {
			t.Errorf("container port %q = %d, want %d", name, ports[name], want)
		}
	}
	svcPorts := map[string]int32{}
	for _, p := range r.Service.Spec.Ports {
		svcPorts[p.Name] = p.Port
	}
	for name := range ports {
		if svcPorts[name] != ports[name] {
			t.Errorf("service is missing container port %q; it is unreachable in-cluster", name)
		}
	}
}

func TestRenderExtraPortsReachTheService(t *testing.T) {
	ma := fullAgent()
	ma.Spec.Brain.ExtraPorts = []corev1.ContainerPort{
		{Name: "vendor-mcp", ContainerPort: 4097, Protocol: corev1.ProtocolTCP},
	}
	r, err := Render(ma, "")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, p := range r.Service.Spec.Ports {
		if p.Name == "vendor-mcp" && p.Port == 4097 {
			return
		}
	}
	t.Error("extraPorts did not reach the Service")
}

// The sandbox credential rides envFrom so the API key never appears inline in
// the pod spec.
func TestRenderSandboxCredentialUsesEnvFrom(t *testing.T) {
	r, err := Render(fullAgent(), "")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, ef := range r.Deployment.Spec.Template.Spec.Containers[0].EnvFrom {
		if ef.SecretRef != nil && ef.SecretRef.Name == "sandbox-creds" {
			return
		}
	}
	t.Error("sandbox credential secret not mounted through envFrom")
}

// A credential rotation with no pod-spec change leaves the old value live in a
// running pod, with nothing to indicate it.
func TestRenderChecksumDrivesRollout(t *testing.T) {
	a, err := Render(fullAgent(), "one")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	b, err := Render(fullAgent(), "two")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if a.Deployment.Spec.Template.Annotations[ConfigChecksumAnnotation] ==
		b.Deployment.Spec.Template.Annotations[ConfigChecksumAnnotation] {
		t.Error("checksum annotation did not change; a secret rotation would not restart the pod")
	}
}

func TestRenderPersistence(t *testing.T) {
	t.Run("disabled uses emptyDir and renders no claim", func(t *testing.T) {
		ma := fullAgent()
		ma.Spec.Session.Persistence.Enabled = ptr(false)
		r, err := Render(ma, "")
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		if r.PVC != nil {
			t.Error("PVC rendered while persistence is disabled")
		}
		for _, v := range r.Deployment.Spec.Template.Spec.Volumes {
			if v.Name == stateVolumeName && v.EmptyDir == nil {
				t.Error("state volume is not an emptyDir while persistence is disabled")
			}
		}
	})

	t.Run("existing claim is mounted and not re-created", func(t *testing.T) {
		ma := fullAgent()
		ma.Spec.Session.Persistence.ExistingClaim = "byo"
		r, err := Render(ma, "")
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		if r.PVC != nil {
			t.Error("PVC rendered for an existing claim")
		}
		for _, v := range r.Deployment.Spec.Template.Spec.Volumes {
			if v.Name == stateVolumeName {
				if v.PersistentVolumeClaim == nil || v.PersistentVolumeClaim.ClaimName != "byo" {
					t.Errorf("state volume = %+v, want claim byo", v.VolumeSource)
				}
			}
		}
	})

	t.Run("enabled renders a RWO claim", func(t *testing.T) {
		r, err := Render(fullAgent(), "")
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		if r.PVC == nil {
			t.Fatal("no PVC rendered while persistence is enabled")
		}
		if got := r.PVC.Spec.AccessModes; len(got) != 1 || got[0] != corev1.ReadWriteOnce {
			t.Errorf("accessModes = %v, want [ReadWriteOnce]", got)
		}
	})
}

func TestRenderRejectsAmbiguousScenarioDefault(t *testing.T) {
	for _, tc := range []struct {
		name      string
		scenarios []agentsv1alpha1.ManagedAgentScenario
	}{
		{"no default", []agentsv1alpha1.ManagedAgentScenario{{Name: "a"}, {Name: "b"}}},
		{"two defaults", []agentsv1alpha1.ManagedAgentScenario{{Name: "a", Default: true}, {Name: "b", Default: true}}},
		{"duplicate name", []agentsv1alpha1.ManagedAgentScenario{{Name: "a", Default: true}, {Name: "a"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ma := fullAgent()
			ma.Spec.Scenarios = tc.scenarios
			if _, err := Render(ma, ""); err == nil {
				t.Error("Render accepted an ambiguous scenario set")
			}
		})
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	a, err := Render(fullAgent(), "x")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	b, err := Render(fullAgent(), "x")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	ea, eb := a.Deployment.Spec.Template.Spec.Containers[0].Env, b.Deployment.Spec.Template.Spec.Containers[0].Env
	if len(ea) != len(eb) {
		t.Fatalf("env length differs between renders: %d vs %d", len(ea), len(eb))
	}
	for i := range ea {
		if ea[i].Name != eb[i].Name {
			t.Fatalf("env order differs at %d: %q vs %q; the pod spec would churn every reconcile",
				i, ea[i].Name, eb[i].Name)
		}
	}
}

func TestEndpointIsInCluster(t *testing.T) {
	if got := Endpoint("demo", "agents", 4099, ""); got != "http://agentbox-brain-demo.agents:4099" {
		t.Errorf("Endpoint = %q", got)
	}
}
