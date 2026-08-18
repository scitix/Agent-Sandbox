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

package config_test

import (
	"testing"

	"github.com/scitix/agent-sandbox/pkg/wsproxy/config"
)

func TestDefaultHands_UnsetPublishesNothing(t *testing.T) {
	cfg := &config.Config{}
	if got := cfg.DefaultHands(); got != nil {
		t.Errorf("DefaultHands() = %+v, want nil when no endpoint is configured", got)
	}
}

func TestDefaultHands_CarriesEveryFieldTheBrainNeeds(t *testing.T) {
	cfg := &config.Config{
		ManagedAgentHandsAPIURL:       " https://console.example.com/api/e2b ",
		ManagedAgentHandsDomain:       "console.example.com/api/data",
		ManagedAgentHandsHTTPS:        true,
		ManagedAgentHandsEnvName:      "navix",
		ManagedAgentHandsImage:        "reg/e2b-sandbox:v1",
		ManagedAgentHandsScalingGroup: "1c2gi",
		ManagedAgentHandsSecretName:   "platform-sandbox",
		ManagedAgentHandsSecretKey:    "E2B_API_KEY",
	}
	hands := cfg.DefaultHands()
	if hands == nil || hands.External == nil {
		t.Fatal("DefaultHands() = nil, want an external supply")
	}
	ext := hands.External
	// Trimmed: a value pasted into a values file with a trailing space would
	// otherwise become part of the URL.
	if ext.APIURL != "https://console.example.com/api/e2b" {
		t.Errorf("apiURL = %q, want it trimmed", ext.APIURL)
	}
	if ext.Domain != "console.example.com/api/data" || ext.EnvName != "navix" {
		t.Errorf("domain/envName = %q/%q", ext.Domain, ext.EnvName)
	}
	// The image is load-bearing: without it sandboxes start from the pool's
	// default image and then refuse every command.
	if ext.Image != "reg/e2b-sandbox:v1" {
		t.Errorf("image = %q, want the configured sandbox image", ext.Image)
	}
	if ext.ScalingGroup != "1c2gi" {
		t.Errorf("scalingGroup = %q", ext.ScalingGroup)
	}
	if ext.HTTPS == nil || !*ext.HTTPS {
		t.Error("https not carried; the data plane would be addressed over http")
	}
	if ext.CredentialsRef == nil || ext.CredentialsRef.Name != "platform-sandbox" ||
		ext.CredentialsRef.Key != "E2B_API_KEY" {
		t.Errorf("credentialsRef = %+v, want the platform's Secret", ext.CredentialsRef)
	}
}

// Half a credential is no credential: a Secret name with no key renders an env
// var that cannot resolve, which stops the pod rather than the sandbox call.
func TestDefaultHands_IgnoresAHalfConfiguredCredential(t *testing.T) {
	cfg := &config.Config{
		ManagedAgentHandsAPIURL:     "https://console.example.com/api/e2b",
		ManagedAgentHandsSecretName: "platform-sandbox",
	}
	if ref := cfg.DefaultHands().External.CredentialsRef; ref != nil {
		t.Errorf("credentialsRef = %+v, want nil when no key is named", ref)
	}
}

func TestDefaultHands_DoesNotAliasTheConfig(t *testing.T) {
	cfg := &config.Config{
		ManagedAgentHandsAPIURL: "https://console.example.com/api/e2b",
		ManagedAgentHandsHTTPS:  true,
	}
	hands := cfg.DefaultHands()
	cfg.ManagedAgentHandsHTTPS = false
	if hands.External.HTTPS == nil || !*hands.External.HTTPS {
		t.Error("the returned default changed with the config it was built from")
	}
}

func TestParseModelList(t *testing.T) {
	models := config.ParseModelList(
		" claude-sonnet-5 | Claude Sonnet 5 , glm-5.2|GLM 5.2|nonreasoning ,, kimi-k2.6 ")
	if len(models) != 3 {
		t.Fatalf("got %d models, want 3 (blank entries dropped): %+v", len(models), models)
	}
	if models[0].ID != "claude-sonnet-5" || models[0].Name != "Claude Sonnet 5" {
		t.Errorf("first = %+v", models[0])
	}
	if models[0].NonReasoning {
		t.Error("first marked non-reasoning; nothing said so")
	}
	// The marker decides which model may back the topic classifier. A reasoning
	// model there returns empty content, which reads as "same topic" every turn.
	if !models[1].NonReasoning {
		t.Error("second not marked non-reasoning")
	}
	if models[2].ID != "kimi-k2.6" || models[2].Name != "" {
		t.Errorf("third = %+v, want an id with no display name", models[2])
	}
}

func TestParseModelList_EmptyIsNoModels(t *testing.T) {
	if got := config.ParseModelList(""); got != nil {
		t.Errorf("ParseModelList(\"\") = %+v, want nil", got)
	}
}

func TestDefaultBrainImage_SplitsPullSecrets(t *testing.T) {
	cfg := &config.Config{
		ManagedAgentBrainImage:       "reg/brain",
		ManagedAgentBrainImageTag:    "v2",
		ManagedAgentBrainPullSecrets: "regcred, other ,",
	}
	img := cfg.DefaultBrainImage()
	if img.Repository != "reg/brain" || img.Tag != "v2" {
		t.Errorf("image = %+v", img)
	}
	if len(img.PullSecrets) != 2 ||
		img.PullSecrets[0].Name != "regcred" || img.PullSecrets[1].Name != "other" {
		t.Errorf("pullSecrets = %+v, want two trimmed names", img.PullSecrets)
	}
}
