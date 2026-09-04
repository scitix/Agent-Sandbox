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

package service

import (
	"encoding/json"
	"strings"
	"testing"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// testCredName is the Env-level credential used across these fixtures.
const testCredName = "openai"

func poolWithInjection() *agentsv1alpha1.SandboxPool {
	p := &agentsv1alpha1.SandboxPool{}
	p.Name = "pool-a"
	p.Spec.NetworkPolicy = &agentsv1alpha1.SandboxNetworkPolicy{
		Egress: &agentsv1alpha1.EgressRules{AllowedDomains: []string{"api.openai.com"}},
		SecretInjection: &agentsv1alpha1.SecretInjection{
			Credentials: []agentsv1alpha1.InjectedCredential{{
				Name:      testCredName,
				ValueFrom: agentsv1alpha1.SecretKeyRef{Name: "creds", Key: testCredName},
				ExposeAs:  "OPENAI_API_KEY",
			}},
			Rules: []agentsv1alpha1.InjectionRule{{
				Host:       "api.openai.com",
				Headers:    []agentsv1alpha1.HeaderInjection{{Name: "Authorization", Value: "Bearer ${e2b.secrets.openai}"}},
				Substitute: []string{testCredName},
			}},
		},
	}
	return p
}

func TestBuildEgressInjectAnnotation_NoInjectionIsNoop(t *testing.T) {
	p := &agentsv1alpha1.SandboxPool{}
	_, ok, err := buildEgressInjectAnnotation(p, nil)
	if err != nil || ok {
		t.Fatalf("pool without injection: ok=%v err=%v", ok, err)
	}
}

// The annotation is stored in etcd and echoed by the API. It must carry rule
// shapes and Secret *references* only — never a credential value.
func TestBuildEgressInjectAnnotation_CarriesNoCredentialValue(t *testing.T) {
	value, ok, err := buildEgressInjectAnnotation(poolWithInjection(), nil)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	// The template is preserved verbatim; nothing resolved it here.
	if !strings.Contains(value, "Bearer ${e2b.secrets.openai}") {
		t.Fatalf("annotation lost the unresolved template: %s", value)
	}
	if strings.Contains(value, "sk-") || strings.Contains(value, "real-key") {
		t.Fatalf("annotation appears to contain credential material: %s", value)
	}

	var si agentsv1alpha1.SecretInjection
	if uErr := json.Unmarshal([]byte(value), &si); uErr != nil {
		t.Fatalf("annotation is not decodable: %v", uErr)
	}
	if si.Credentials[0].ValueFrom.Name != "creds" || si.Credentials[0].ValueFrom.Key != testCredName {
		t.Fatalf("Secret reference not preserved: %+v", si.Credentials[0].ValueFrom)
	}
}

// Every claim gets a fresh decoy, so a released pod's decoy is worthless to the
// sandbox that recycles it.
func TestBuildEgressInjectAnnotation_GeneratesUniquePlaceholder(t *testing.T) {
	v1, _, err1 := buildEgressInjectAnnotation(poolWithInjection(), nil)
	v2, _, err2 := buildEgressInjectAnnotation(poolWithInjection(), nil)
	if err1 != nil || err2 != nil {
		t.Fatalf("errs: %v %v", err1, err2)
	}
	var a, b agentsv1alpha1.SecretInjection
	_ = json.Unmarshal([]byte(v1), &a)
	_ = json.Unmarshal([]byte(v2), &b)

	if a.Credentials[0].Placeholder == "" {
		t.Fatal("exposeAs credential got no placeholder")
	}
	if !strings.HasPrefix(a.Credentials[0].Placeholder, agentsv1alpha1.PlaceholderPrefix) {
		t.Fatalf("placeholder %q lacks the generated prefix", a.Credentials[0].Placeholder)
	}
	if len(a.Credentials[0].Placeholder) < agentsv1alpha1.MinPlaceholderLen {
		t.Fatalf("placeholder %q is shorter than the minimum", a.Credentials[0].Placeholder)
	}
	if a.Credentials[0].Placeholder == b.Credentials[0].Placeholder {
		t.Fatal("two claims received the same decoy")
	}
}

// A pinned placeholder survives untouched — that is the point of the field,
// since some SDKs validate credential shape before sending.
func TestBuildEgressInjectAnnotation_KeepsPinnedPlaceholder(t *testing.T) {
	p := poolWithInjection()
	p.Spec.NetworkPolicy.SecretInjection.Credentials[0].Placeholder = "sk-proj-0000000000000000"
	value, _, err := buildEgressInjectAnnotation(p, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	var si agentsv1alpha1.SecretInjection
	_ = json.Unmarshal([]byte(value), &si)
	if si.Credentials[0].Placeholder != "sk-proj-0000000000000000" {
		t.Fatalf("pinned placeholder was replaced with %q", si.Credentials[0].Placeholder)
	}
}

func TestBuildEgressInjectAnnotation_RejectsInvalidConfig(t *testing.T) {
	p := poolWithInjection()
	p.Spec.NetworkPolicy.SecretInjection.Rules[0].Host = "*.openai.com"
	_, _, err := buildEgressInjectAnnotation(p, nil)
	if err == nil {
		t.Fatal("wildcard host must be rejected at claim time too")
	}
}

// Per-sandbox injection is refused: a credential travelling through a create
// request would re-introduce the exposure this feature removes.
func TestBuildEgressPolicyAnnotation_RejectsPerSandboxInjection(t *testing.T) {
	pool := poolWithInjection()
	override := &agentsv1alpha1.SandboxNetworkPolicy{
		SecretInjection: &agentsv1alpha1.SecretInjection{
			Rules: []agentsv1alpha1.InjectionRule{{Host: "api.openai.com"}},
		},
	}
	_, _, err := buildEgressPolicyAnnotation(override, pool, "sbx-1")
	if err == nil {
		t.Fatal("per-sandbox secretInjection must be rejected")
	}
	if !strings.Contains(err.Error(), "SandboxEnv") {
		t.Fatalf("error should point at the Env: %v", err)
	}
}

// --------------------------------------------------------------------------
// Per-sandbox rules merged with the Env's own injection block
// --------------------------------------------------------------------------

func perSandboxFixture() *perSandboxInjection {
	return &perSandboxInjection{
		rules: []agentsv1alpha1.InjectionRule{{
			Host: "api.anthropic.com",
			Headers: []agentsv1alpha1.HeaderInjection{{
				Name:  "X-Api-Key",
				Mode:  agentsv1alpha1.HeaderInjectionOverride,
				Value: "${e2b.secrets.anthropic}",
			}},
		}},
		refs: map[string]agentsv1alpha1.SecretKeyRef{
			"anthropic": {Name: "agbx-vault-alice-abc123", Key: "anthropic"},
		},
	}
}

// Rules are a union: the Env's promise to every sandbox in it must survive a
// create request that adds its own.
func TestBuildEgressInject_MergesPerSandboxRulesWithEnvRules(t *testing.T) {
	value, ok, err := buildEgressInjectAnnotation(poolWithInjection(), perSandboxFixture())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected an injection block")
	}

	var si agentsv1alpha1.SecretInjection
	if uErr := json.Unmarshal([]byte(value), &si); uErr != nil {
		t.Fatalf("decode: %v", uErr)
	}
	if len(si.Rules) != 2 {
		t.Fatalf("expected the Env rule plus the request's, got %d", len(si.Rules))
	}
	// Env rules come first, so their ordering semantics are unchanged.
	if si.Rules[len(si.Rules)-1].Host != "api.anthropic.com" {
		t.Fatalf("per-sandbox rule should be appended last, got %+v", si.Rules)
	}

	var found bool
	for _, c := range si.Credentials {
		if c.Name == "anthropic" {
			found = true
			if c.ValueFrom.Name != "agbx-vault-alice-abc123" || c.ValueFrom.Key != "anthropic" {
				t.Fatalf("credential must point at the vault Secret, got %+v", c.ValueFrom)
			}
			// The annotation carries a reference, never a value.
			if strings.Contains(value, "sk-") {
				t.Fatal("annotation must not contain credential material")
			}
		}
	}
	if !found {
		t.Fatal("expected the vault credential to be added")
	}
}

// With no Env-level injection at all, a request's rules still produce a block.
func TestBuildEgressInject_PerSandboxOnly(t *testing.T) {
	pool := &agentsv1alpha1.SandboxPool{}
	value, ok, err := buildEgressInjectAnnotation(pool, perSandboxFixture())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected an injection block from the request alone")
	}
	var si agentsv1alpha1.SecretInjection
	if uErr := json.Unmarshal([]byte(value), &si); uErr != nil {
		t.Fatalf("decode: %v", uErr)
	}
	if len(si.Rules) != 1 || len(si.Credentials) != 1 {
		t.Fatalf("unexpected block: %+v", si)
	}
}

// A name that exists at both levels is refused rather than shadowed: either
// resolution order surprises somebody, and one clear error costs less.
func TestBuildEgressInject_NameCollisionIsRefused(t *testing.T) {
	ps := perSandboxFixture()
	ps.refs = map[string]agentsv1alpha1.SecretKeyRef{
		testCredName: {Name: "agbx-vault-alice-abc123", Key: testCredName},
	}
	_, _, err := buildEgressInjectAnnotation(poolWithInjection(), ps)
	if err == nil {
		t.Fatal("expected a collision to be refused")
	}
	if !strings.Contains(err.Message, testCredName) {
		t.Fatalf("message must name the clashing secret, got %q", err.Message)
	}
}
