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

func poolWithInjection() *agentsv1alpha1.SandboxPool {
	p := &agentsv1alpha1.SandboxPool{}
	p.Name = "pool-a"
	p.Spec.NetworkPolicy = &agentsv1alpha1.SandboxNetworkPolicy{
		Egress: &agentsv1alpha1.EgressRules{AllowedDomains: []string{"api.openai.com"}},
		SecretInjection: &agentsv1alpha1.SecretInjection{
			Credentials: []agentsv1alpha1.InjectedCredential{{
				Name:      "openai",
				ValueFrom: agentsv1alpha1.SecretKeyRef{Name: "creds", Key: "openai"},
				ExposeAs:  "OPENAI_API_KEY",
			}},
			Rules: []agentsv1alpha1.InjectionRule{{
				Host:       "api.openai.com",
				Headers:    []agentsv1alpha1.HeaderInjection{{Name: "Authorization", Value: "Bearer {{ openai }}"}},
				Substitute: []string{"openai"},
			}},
		},
	}
	return p
}

func TestBuildEgressInjectAnnotation_NoInjectionIsNoop(t *testing.T) {
	p := &agentsv1alpha1.SandboxPool{}
	_, ok, err := buildEgressInjectAnnotation(p)
	if err != nil || ok {
		t.Fatalf("pool without injection: ok=%v err=%v", ok, err)
	}
}

// The annotation is stored in etcd and echoed by the API. It must carry rule
// shapes and Secret *references* only — never a credential value.
func TestBuildEgressInjectAnnotation_CarriesNoCredentialValue(t *testing.T) {
	value, ok, err := buildEgressInjectAnnotation(poolWithInjection())
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	// The template is preserved verbatim; nothing resolved it here.
	if !strings.Contains(value, "Bearer {{ openai }}") {
		t.Fatalf("annotation lost the unresolved template: %s", value)
	}
	if strings.Contains(value, "sk-") || strings.Contains(value, "real-key") {
		t.Fatalf("annotation appears to contain credential material: %s", value)
	}

	var si agentsv1alpha1.SecretInjection
	if uErr := json.Unmarshal([]byte(value), &si); uErr != nil {
		t.Fatalf("annotation is not decodable: %v", uErr)
	}
	if si.Credentials[0].ValueFrom.Name != "creds" || si.Credentials[0].ValueFrom.Key != "openai" {
		t.Fatalf("Secret reference not preserved: %+v", si.Credentials[0].ValueFrom)
	}
}

// Every claim gets a fresh decoy, so a released pod's decoy is worthless to the
// sandbox that recycles it.
func TestBuildEgressInjectAnnotation_GeneratesUniquePlaceholder(t *testing.T) {
	v1, _, err1 := buildEgressInjectAnnotation(poolWithInjection())
	v2, _, err2 := buildEgressInjectAnnotation(poolWithInjection())
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
	value, _, err := buildEgressInjectAnnotation(p)
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
	_, _, err := buildEgressInjectAnnotation(p)
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
