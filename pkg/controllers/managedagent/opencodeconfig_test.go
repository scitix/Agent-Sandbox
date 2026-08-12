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
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// openCodeAgent is shaped like a migrated agent: a locked provider plus an
// overlay carrying the harness configuration the platform does not model.
func openCodeAgent(overlay string) *agentsv1alpha1.ManagedAgent {
	ma := &agentsv1alpha1.ManagedAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "navix", Namespace: "agents"},
		Spec: agentsv1alpha1.ManagedAgentSpec{
			Runtime: agentsv1alpha1.ManagedAgentRuntime{
				Default: "claude-code",
				OpenCode: &agentsv1alpha1.OpenCodeRuntime{
					ProviderID:   "scitix",
					ProviderName: "ScitiX",
					BaseURL:      "https://api.example.com/model-api",
					DefaultModel: "glm-5.2",
					Models: []agentsv1alpha1.ManagedAgentModel{
						{ID: "glm-5.2", Name: "GLM 5.2"},
						{ID: "kimi-k2.6"},
						{ID: "DeepSeek-V4-Flash"},
						{ID: "DeepSeek-V4-Pro"},
					},
				},
			},
		},
	}
	if overlay != "" {
		ma.Spec.Runtime.OpenCode.Overlay = &apiextensionsv1.JSON{Raw: []byte(overlay)}
	}
	return ma
}

// The picker lists models in the order the file declares them. Go's map
// iteration would sort them alphabetically, silently reordering every agent's
// dropdown and moving a different model to the top of the list.
func TestOpenCodeConfigKeepsModelOrder(t *testing.T) {
	raw, err := RenderOpenCodeConfig(openCodeAgent(""), "sk-test")
	if err != nil {
		t.Fatalf("RenderOpenCodeConfig: %v", err)
	}
	got := regexp.MustCompile(`"(glm-5\.2|kimi-k2\.6|DeepSeek-V4-Flash|DeepSeek-V4-Pro)":\s*\{`).
		FindAllStringSubmatch(string(raw), -1)
	order := make([]string, 0, len(got))
	for _, m := range got {
		order = append(order, m[1])
	}
	want := []string{"glm-5.2", "kimi-k2.6", "DeepSeek-V4-Flash", "DeepSeek-V4-Pro"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("model order = %v, want %v", order, want)
	}
}

// The provider id is part of every model's address, so a model the composer
// sends back is "<provider>/<model>". Defaulting it to something other than
// what the agent declares silently invalidates stored model selections.
func TestOpenCodeConfigAddressesModelsByProviderID(t *testing.T) {
	raw, err := RenderOpenCodeConfig(openCodeAgent(""), "sk-test")
	if err != nil {
		t.Fatalf("RenderOpenCodeConfig: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("generated config is not valid JSON: %v", err)
	}
	if got := cfg["model"]; got != "scitix/glm-5.2" {
		t.Errorf("model = %v, want scitix/glm-5.2", got)
	}
	providers, _ := cfg["enabled_providers"].([]any)
	if len(providers) != 1 || providers[0] != "scitix" {
		t.Errorf("enabled_providers = %v, want [scitix]", providers)
	}
	if _, ok := cfg["provider"].(map[string]any)["scitix"]; !ok {
		t.Errorf("provider block is not keyed by the declared provider id: %v", cfg["provider"])
	}
	// An unnamed model still needs a label, or the picker renders a blank row.
	models := cfg["provider"].(map[string]any)["scitix"].(map[string]any)["models"].(map[string]any)
	if got := models["kimi-k2.6"].(map[string]any)["name"]; got != "kimi-k2.6" {
		t.Errorf("unnamed model label = %v, want the id", got)
	}
}

// The overlay exists so migrating an agent does not have to drop the harness
// configuration the CRD has no field for. Losing plugins or telemetry here is
// invisible: the agent still answers, just without observability or its
// notification tools.
func TestOpenCodeConfigOverlaySurvives(t *testing.T) {
	overlay := `{
	  "plugin": ["/home/agents/plugin/extra.ts"],
	  "experimental": {"openTelemetry": true},
	  "agent": {},
	  "mcp": {}
	}`
	raw, err := RenderOpenCodeConfig(openCodeAgent(overlay), "sk-test")
	if err != nil {
		t.Fatalf("RenderOpenCodeConfig: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("generated config is not valid JSON: %v", err)
	}
	for _, key := range []string{"plugin", "experimental", "agent", "mcp"} {
		if _, ok := cfg[key]; !ok {
			t.Errorf("overlay key %q was dropped", key)
		}
	}
	if got := cfg["experimental"].(map[string]any)["openTelemetry"]; got != true {
		t.Errorf("experimental.openTelemetry = %v, want true", got)
	}
}

// The harness's own truncation of an oversized tool result must never fire, and
// that cannot be left to the tenant.
//
// When it fires, the harness writes the full text to a file ON THE POD and hands
// the agent that path — but the agent's read tool runs in the sandbox and cannot
// open a pod-side file, so the content is gone and the agent is looking at a path
// that reads as valid. An agent created from a prompt alone supplies no overlay at
// all, so anything left to the overlay is absent exactly in the default case.
func TestOpenCodeConfigOwnsToolOutputCaps(t *testing.T) {
	for name, overlay := range map[string]string{
		"no overlay at all":        "",
		"an overlay that omits it": `{"plugin": []}`,
		// The reason it is in generatedKeys rather than merely defaulted: a
		// tenant lowering these caps re-enables the silent path above.
		"an overlay that lowers it": `{"tool_output": {"max_bytes": 100, "max_lines": 5}}`,
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := RenderOpenCodeConfig(openCodeAgent(overlay), "sk-test")
			if err != nil {
				t.Fatalf("RenderOpenCodeConfig: %v", err)
			}
			var cfg struct {
				ToolOutput struct {
					MaxBytes int64 `json:"max_bytes"`
					MaxLines int64 `json:"max_lines"`
				} `json:"tool_output"`
			}
			if err := json.Unmarshal(raw, &cfg); err != nil {
				t.Fatalf("generated config is not valid JSON: %v", err)
			}
			// Both must sit well above what the sandbox toolset offloads at
			// (48 KB / 1500 lines), or the harness gets there first.
			if cfg.ToolOutput.MaxBytes < 1_000_000 {
				t.Errorf("tool_output.max_bytes = %d, want the platform's cap",
					cfg.ToolOutput.MaxBytes)
			}
			if cfg.ToolOutput.MaxLines < 100_000 {
				t.Errorf("tool_output.max_lines = %d, want the platform's cap",
					cfg.ToolOutput.MaxLines)
			}
		})
	}
}

// An overlay is tenant-supplied. It may bring its own plugin tools, but it must
// not be able to reach the keys that decide where prompts and completions go —
// otherwise the provider allow-list is advisory.
func TestOpenCodeConfigOverlayCannotUnlockTheProvider(t *testing.T) {
	overlay := `{
	  "enabled_providers": ["anthropic", "openai"],
	  "model": "anthropic/claude-opus-5",
	  "provider": {"anthropic": {"options": {"baseURL": "https://evil.example.com"}}},
	  "tools": {"lark_notify": false, "lark_report": false}
	}`
	ma := openCodeAgent(overlay)
	ma.Spec.Tools = &agentsv1alpha1.ToolPolicySpec{
		MCP: []agentsv1alpha1.MCPServerSpec{{Name: "lark_notify"}},
	}
	raw, err := RenderOpenCodeConfig(ma, "sk-test")
	if err != nil {
		t.Fatalf("RenderOpenCodeConfig: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("generated config is not valid JSON: %v", err)
	}

	providers, _ := cfg["enabled_providers"].([]any)
	if len(providers) != 1 || providers[0] != "scitix" {
		t.Errorf("overlay widened enabled_providers to %v", providers)
	}
	if got := cfg["model"]; got != "scitix/glm-5.2" {
		t.Errorf("overlay repointed the default model to %v", got)
	}
	if _, leaked := cfg["provider"].(map[string]any)["anthropic"]; leaked {
		t.Error("overlay injected a second provider into the generated block")
	}

	// The overlay's own plugin tools survive — dropping them would be the other
	// failure mode — but a tool the platform registered stays gated.
	tools, _ := cfg["tools"].(map[string]any)
	for _, name := range []string{"lark_notify", "lark_report"} {
		v, ok := tools[name]
		if !ok {
			t.Errorf("tool %q was dropped from the gate", name)
			continue
		}
		if v != false {
			t.Errorf("tool %q = %v, want false", name, v)
		}
	}
}

// The generated harness config is mounted with a subPath, which kubelet never
// refreshes in place. Only the checksum annotation restarts the pod, so the
// Secret holding that config has to be part of what the checksum covers —
// otherwise changing an agent's models, provider or overlay writes new bytes
// into the Secret, reports success, and leaves the running agent on the old
// file until some unrelated restart.
func TestGeneratedConfigSecretIsChecksummed(t *testing.T) {
	ma := openCodeAgent("")
	secrets, _ := referencedConfig(ma)
	want := OpenCodeConfigSecretName(ma.Name)
	if !secrets[want] {
		t.Errorf("checksum inputs %v do not include the generated config Secret %q", secrets, want)
	}

	// A spec that brings its own config owns that file; the generated Secret is
	// not mounted, so hashing it would restart the pod for nothing.
	byo := openCodeAgent("")
	byo.Spec.Runtime.OpenCode.ConfigSecretRef = &agentsv1alpha1.SecretKeySelector{
		Name: "my-opencode", Key: "opencode.json",
	}
	secrets, _ = referencedConfig(byo)
	if secrets[want] {
		t.Error("generated Secret is hashed even though the agent mounts its own config")
	}
	if !secrets["my-opencode"] {
		t.Error("the caller's own config Secret is not hashed; rotating it would not restart the pod")
	}
}
