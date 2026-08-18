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

// What a deployment supplies on behalf of an agent that names none.
//
// These three defaults are the difference between "create an agent" and "know
// which Brain image ships a compatible gateway, which sandbox environment exists
// on which cluster, and which image inside it actually runs commands". They are
// assembled here, from flags, rather than in the bootstrap so the parsing has
// somewhere to be tested.

package config

import (
	"strings"

	corev1 "k8s.io/api/core/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// ModelProviderDefaults is the model endpoint a new agent starts with.
//
// There is no credential here, and there is not meant to be: publishing an
// address and a model list saves a caller from knowing them, while a key that the
// platform handed to every agent would make one agent's quota and one agent's
// revocation everyone's.
type ModelProviderDefaults struct {
	BaseURL      string
	Models       []agentsv1alpha1.ManagedAgentModel
	DefaultModel string
	SmallModel   string
}

// DefaultBrainImage is the Brain image an agent gets when it names none.
//
// An empty repository is passed through as empty rather than defaulted to
// something: the renderer treats that as "this deployment has no default" and
// keeps spec.image.repository required, which fails with a readable message.
// Substituting a guess here would instead produce a Deployment that reconciles
// cleanly and then sits in ImagePullBackOff.
func (c *Config) DefaultBrainImage() agentsv1alpha1.ManagedAgentImage {
	img := agentsv1alpha1.ManagedAgentImage{
		Repository: strings.TrimSpace(c.ManagedAgentBrainImage),
		Tag:        strings.TrimSpace(c.ManagedAgentBrainImageTag),
	}
	for name := range strings.SplitSeq(c.ManagedAgentBrainPullSecrets, ",") {
		if name = strings.TrimSpace(name); name != "" {
			img.PullSecrets = append(img.PullSecrets,
				corev1.LocalObjectReference{Name: name})
		}
	}
	return img
}

// DefaultHands is the sandbox supply an agent gets when it declares no branch.
//
// Nil when no API URL is configured. That is the whole switch: a deployment
// either names a sandbox service its agents may use by default or it does not,
// and a half-configured default — an endpoint with no environment name, say —
// would create sandboxes that fail at the first tool call, so the missing pieces
// are reported by the agent's HandsReady condition rather than guessed at here.
//
// The credential is a reference, never a value. The pod that reads it is the
// Brain, in the agents' namespace, and this process only has to render the
// pointer; the key itself is not held here and does not pass through the API.
func (c *Config) DefaultHands() *agentsv1alpha1.ManagedAgentHands {
	apiURL := strings.TrimSpace(c.ManagedAgentHandsAPIURL)
	if apiURL == "" {
		return nil
	}
	// Copied, not pointed at the Config field: the result outlives this call and
	// must not change under its holder if the config is ever reloaded.
	https := c.ManagedAgentHandsHTTPS
	ext := &agentsv1alpha1.HandsExternal{
		APIURL:       apiURL,
		Domain:       strings.TrimSpace(c.ManagedAgentHandsDomain),
		HTTPS:        &https,
		EnvName:      strings.TrimSpace(c.ManagedAgentHandsEnvName),
		Image:        strings.TrimSpace(c.ManagedAgentHandsImage),
		ScalingGroup: strings.TrimSpace(c.ManagedAgentHandsScalingGroup),
	}
	name := strings.TrimSpace(c.ManagedAgentHandsSecretName)
	key := strings.TrimSpace(c.ManagedAgentHandsSecretKey)
	if name != "" && key != "" {
		ext.CredentialsRef = &agentsv1alpha1.SecretKeySelector{Name: name, Key: key}
	}
	return &agentsv1alpha1.ManagedAgentHands{External: ext}
}

// DefaultModelProvider is the model endpoint and dropdown a new agent starts with.
func (c *Config) DefaultModelProvider() ModelProviderDefaults {
	return ModelProviderDefaults{
		BaseURL:      strings.TrimSpace(c.ManagedAgentModelBaseURL),
		Models:       ParseModelList(c.ManagedAgentModels),
		DefaultModel: strings.TrimSpace(c.ManagedAgentModelDefault),
		SmallModel:   strings.TrimSpace(c.ManagedAgentModelSmall),
	}
}

// ParseModelList reads a model dropdown from one flag value.
//
// Entries are comma-separated and each is "id", "id|Display Name" or
// "id|Display Name|nonreasoning". A model id may not contain a comma, so the two
// separators cannot collide.
//
// The nonreasoning marker is not decoration: it is what makes a model eligible to
// back the topic classifier, and a reasoning model there spends its whole
// completion budget on the chain of thought and returns empty content, which
// reads as "same topic" every single time.
func ParseModelList(spec string) []agentsv1alpha1.ManagedAgentModel {
	var out []agentsv1alpha1.ManagedAgentModel
	for entry := range strings.SplitSeq(spec, ",") {
		parts := strings.Split(entry, "|")
		id := strings.TrimSpace(parts[0])
		if id == "" {
			continue
		}
		model := agentsv1alpha1.ManagedAgentModel{ID: id}
		if len(parts) > 1 {
			model.Name = strings.TrimSpace(parts[1])
		}
		if len(parts) > 2 && strings.EqualFold(strings.TrimSpace(parts[2]), "nonreasoning") {
			model.NonReasoning = true
		}
		out = append(out, model)
	}
	return out
}

// DefaultRuntime is the harness configuration an agent gets when it declares
// none of its own.
//
// Nil unless BOTH an endpoint and a credential reference are configured. A
// half-configured default is worse than none: an endpoint with no key produces an
// agent whose Brain is healthy and whose every harness reports itself
// unavailable, which reads as a broken platform rather than as configuration a
// deployment chose not to supply.
//
// Rendered as Claude Code because it is the harness whose sandbox confinement is
// verified by a driven session; the OpenCode override mechanism is still an open
// question against 1.18.16. An agent that wants OpenCode configures its own
// runtime, which suppresses this default entirely.
func (c *Config) DefaultRuntime() *agentsv1alpha1.ManagedAgentRuntime {
	mp := c.DefaultModelProvider()
	name := strings.TrimSpace(c.ManagedAgentModelSecretName)
	key := strings.TrimSpace(c.ManagedAgentModelSecretKey)
	if mp.BaseURL == "" || name == "" || key == "" {
		return nil
	}
	return &agentsv1alpha1.ManagedAgentRuntime{
		ClaudeCode: &agentsv1alpha1.ClaudeCodeRuntime{
			BaseURL:        mp.BaseURL,
			CredentialsRef: agentsv1alpha1.SecretKeySelector{Name: name, Key: key},
			Models:         mp.Models,
			DefaultModel:   mp.DefaultModel,
			SmallModel:     mp.SmallModel,
		},
	}
}
