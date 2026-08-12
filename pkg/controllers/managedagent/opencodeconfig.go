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
	"bytes"
	"encoding/json"
	"fmt"
	"maps"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// DefaultProviderID is used when the agent does not name one. The provider id
// is part of every model's address ("<provider>/<model>"), so it is a spec
// field rather than a constant here.
const DefaultProviderID = "platform"

// Keys the generated config owns outright. An overlay may supply anything
// else, but these decide which endpoint the harness may reach and with whose
// credential — and whether the sandbox confinement holds — so an overlay never
// gets to influence them.
var generatedKeys = []string{
	"$schema", "enabled_providers", "model", "provider", "tool_output",
}

// Caps for the harness's own truncation of a tool result, raised far out of the
// way so that it never fires.
//
// This is platform-owned rather than tenant-configurable because lowering it
// breaks the sandbox guarantee, silently. When the harness decides a result is
// too large it writes the full text to a file ON THE POD and hands the agent that
// path — but the agent's `read` runs in the sandbox and cannot open a pod-side
// file, so the content is simply gone, reported as a path that looks valid.
//
// The sandbox toolset does its own offload first (at 48 KB or 1500 lines) and
// writes the spill file INTO the sandbox, where `read` can reach it. These caps
// exist only to keep the harness from getting there first, so they are set well
// above any figure the toolset could hand over.
var generatedToolOutput = map[string]int64{
	"max_bytes": 50_000_000,
	"max_lines": 1_000_000,
}

// toolsKey is merged per entry rather than replaced. Tools arrive from two
// places — the platform's registry and whatever the overlay's plugins bring —
// and dropping either set is wrong: replacing loses the plugin tools, and
// deferring loses the platform's gate. Generated entries win on collision, so
// the gate still cannot be widened from an overlay.
const toolsKey = "tools"

// RenderOpenCodeConfig produces the opencode.json for one agent.
//
// The result is the agent's overlay with the platform-owned keys stamped on
// top. apiKey is read from the agent's credential Secret by the caller; it is
// embedded here because the harness reads its provider credential from this
// file rather than from the environment.
func RenderOpenCodeConfig(ma *agentsv1alpha1.ManagedAgent, apiKey string) ([]byte, error) {
	oc := ma.Spec.Runtime.OpenCode
	if oc == nil {
		return nil, fmt.Errorf("opencode runtime is not configured")
	}

	providerID := oc.ProviderID
	if providerID == "" {
		providerID = DefaultProviderID
	}
	providerName := oc.ProviderName
	if providerName == "" {
		providerName = providerID
	}

	// Model order is the picker's order, so it follows the spec rather than
	// Go's map iteration, which would sort the list alphabetically and quietly
	// reorder every agent's dropdown.
	models := orderedObject{}
	for _, m := range oc.Models {
		name := m.Name
		if name == "" {
			name = m.ID
		}
		models.set(m.ID, map[string]string{"name": name})
	}

	generated := map[string]any{
		"$schema": "https://opencode.ai/config.json",
		// An allow-list enforced by the harness, not a display filter. Without
		// it the harness also loads every provider it can reach without
		// credentials — including its vendor's hosted free models — which
		// would put them in the picker AND make them reachable by model id,
		// sending this deployment's data to a third party. Naming the single
		// provider makes anything else unusable rather than merely hidden.
		"enabled_providers": []string{providerID},
		"tool_output":       generatedToolOutput,
		"provider": map[string]any{
			providerID: map[string]any{
				"name": providerName,
				"npm":  "@ai-sdk/openai-compatible",
				"options": map[string]string{
					"baseURL": oc.BaseURL,
					"apiKey":  apiKey,
				},
				"models": models,
			},
		},
	}
	if oc.DefaultModel != "" {
		generated["model"] = providerID + "/" + oc.DefaultModel
	}
	merged, err := mergeUnderGenerated(oc.Overlay, generated, openCodeToolGate(ma))
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(merged, "", "  ")
}

// mergeUnderGenerated lays the overlay down first and stamps the generated keys
// on top.
//
// The nesting is deliberate at exactly one level: an overlay may add sibling
// keys inside "provider" or extra entries inside "tools", but a generated key
// that exists replaces the overlay's version outright rather than being merged
// into. A deep merge here would let an overlay reach into
// provider.<id>.options.baseURL and repoint the endpoint while still looking
// like the platform generated it.
func mergeUnderGenerated(
	overlay *apiextensionsv1.JSON,
	generated map[string]any,
	gate map[string]bool,
) (map[string]any, error) {
	out := map[string]any{}
	if overlay != nil && len(overlay.Raw) > 0 {
		if err := json.Unmarshal(overlay.Raw, &out); err != nil {
			return nil, fmt.Errorf("opencode.overlay is not a JSON object: %w", err)
		}
		for _, k := range generatedKeys {
			delete(out, k)
		}
	}
	maps.Copy(out, generated)

	tools, _ := out[toolsKey].(map[string]any)
	if tools == nil && len(gate) > 0 {
		tools = map[string]any{}
	}
	for name, enabled := range gate {
		tools[name] = enabled
	}
	if len(tools) > 0 {
		out[toolsKey] = tools
	}
	return out, nil
}

// openCodeToolGate turns every registered MCP tool off globally.
//
// Scenario allow-lists re-enable them one at a time. The gate has to be global
// and default-closed: a tool that is registered but merely unlisted is still
// reachable, and the failure direction of forgetting to configure a scenario
// must be "the tool is invisible", never "the agent can act on the outside
// world".
//
// Client-side tools are absent on purpose. They are executed by the caller's
// own frontend and forwarded by the gateway, so they are not names this
// harness can call; listing them here would gate something that does not exist
// while implying it had been secured.
func openCodeToolGate(ma *agentsv1alpha1.ManagedAgent) map[string]bool {
	if ma.Spec.Tools == nil || len(ma.Spec.Tools.MCP) == 0 {
		return nil
	}
	gate := map[string]bool{}
	for _, m := range ma.Spec.Tools.MCP {
		gate[m.Name] = false
	}
	return gate
}

// orderedObject marshals as a JSON object with its keys in insertion order.
type orderedObject struct {
	keys   []string
	values map[string]any
}

func (o *orderedObject) set(key string, value any) {
	if o.values == nil {
		o.values = map[string]any{}
	}
	if _, seen := o.values[key]; !seen {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

func (o orderedObject) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		val, err := json.Marshal(o.values[k])
		if err != nil {
			return nil, err
		}
		buf.Write(val)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
