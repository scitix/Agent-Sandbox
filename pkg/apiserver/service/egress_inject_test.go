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
	"net"
	"strings"
	"testing"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// gatewayPool is an Env-owned Pool whose sandboxes carry the proxy sidecar.
func gatewayPool() *agentsv1alpha1.SandboxPool {
	p := &agentsv1alpha1.SandboxPool{}
	p.Name = "pool-a"
	p.Spec.Gateway = &agentsv1alpha1.GatewaySpec{Enabled: true}
	return p
}

// perSandboxFixture is one create request's rules, already resolved against the
// caller's vault.
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

func TestBuildEgressInjectAnnotation_NoRulesIsNoop(t *testing.T) {
	if _, ok, err := buildEgressInjectAnnotation(nil, gatewayPool(), nil); err != nil || ok {
		t.Fatalf("no rules: ok=%v err=%v", ok, err)
	}
	empty := &perSandboxInjection{}
	if _, ok, err := buildEgressInjectAnnotation(nil, gatewayPool(), empty); err != nil || ok {
		t.Fatalf("empty rules: ok=%v err=%v", ok, err)
	}
}

// The annotation is stored in etcd and echoed by the API. It must carry rule
// shapes and Secret *references* only — never a credential value.
func TestBuildEgressInjectAnnotation_CarriesNoCredentialValue(t *testing.T) {
	value, ok, err := buildEgressInjectAnnotation(nil, gatewayPool(), perSandboxFixture())
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	// The template is preserved verbatim; nothing resolved it here.
	if !strings.Contains(value, "${e2b.secrets.anthropic}") {
		t.Fatalf("annotation lost the unresolved template: %s", value)
	}
	if strings.Contains(value, "sk-") {
		t.Fatalf("annotation appears to contain credential material: %s", value)
	}

	var si agentsv1alpha1.SecretInjection
	if uErr := json.Unmarshal([]byte(value), &si); uErr != nil {
		t.Fatalf("annotation is not decodable: %v", uErr)
	}
	if len(si.Credentials) != 1 {
		t.Fatalf("expected exactly the referenced credential, got %+v", si.Credentials)
	}
	if si.Credentials[0].ValueFrom.Name != "agbx-vault-alice-abc123" ||
		si.Credentials[0].ValueFrom.Key != "anthropic" {
		t.Fatalf("credential must point at the caller's vault Secret, got %+v", si.Credentials[0].ValueFrom)
	}
}

// Identical requests must render an identical annotation: the map of resolved
// references has no order of its own, so the credential list is sorted.
func TestBuildEgressInjectAnnotation_IsDeterministic(t *testing.T) {
	ps := perSandboxFixture()
	ps.refs["openai"] = agentsv1alpha1.SecretKeyRef{Name: "agbx-vault-alice-abc123", Key: "openai"}
	ps.rules[0].Headers = append(ps.rules[0].Headers, agentsv1alpha1.HeaderInjection{
		Name: "X-Other", Value: "${e2b.secrets.openai}",
	})

	first, _, err := buildEgressInjectAnnotation(nil, gatewayPool(), ps)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for range 20 {
		again, _, aErr := buildEgressInjectAnnotation(nil, gatewayPool(), ps)
		if aErr != nil {
			t.Fatalf("err: %v", aErr)
		}
		if again != first {
			t.Fatalf("annotation is not stable:\n%s\n%s", first, again)
		}
	}
}

// Validation runs at claim time as well, so a rule that would silently do
// nothing (or leak) is refused before the sandbox is handed over.
func TestBuildEgressInjectAnnotation_RejectsInvalidRules(t *testing.T) {
	ps := perSandboxFixture()
	ps.rules[0].Host = "*.anthropic.com"
	if _, _, err := buildEgressInjectAnnotation(nil, gatewayPool(), ps); err == nil {
		t.Fatal("wildcard host must be rejected at claim time too")
	}
}

// The filter and the rules come from the same request, so they can contradict
// each other: an allowlist that excludes the rule's host makes the rule dead.
func TestBuildEgressInjectAnnotation_RejectsRuleHostOutsideAllowlist(t *testing.T) {
	np := &agentsv1alpha1.SandboxNetworkPolicy{
		Egress: &agentsv1alpha1.EgressRules{AllowedDomains: []string{"pypi.org"}},
	}
	_, _, err := buildEgressInjectAnnotation(np, gatewayPool(), perSandboxFixture())
	if err == nil {
		t.Fatal("a rule host outside the request's allowlist must be refused")
	}
}

// --------------------------------------------------------------------------
// The gateway switch gates both paths
// --------------------------------------------------------------------------

// Without the sidecar there is no redirect either, so accepting the request
// would leave it entirely unenforced while reporting success.
func TestEgressAnnotations_RefusedWithoutTheGateway(t *testing.T) {
	for name, pool := range map[string]*agentsv1alpha1.SandboxPool{
		"no gateway block": {},
		"gateway off":      {Spec: agentsv1alpha1.SandboxPoolSpec{Gateway: &agentsv1alpha1.GatewaySpec{}}},
	} {
		np := &agentsv1alpha1.SandboxNetworkPolicy{DisableEgress: true}
		if _, _, err := buildEgressPolicyAnnotation(np, pool, "sbx-1"); err == nil {
			t.Errorf("%s: a network policy must be refused", name)
		}
		if _, _, err := buildEgressInjectAnnotation(nil, pool, perSandboxFixture()); err == nil {
			t.Errorf("%s: injection must be refused", name)
		}
	}
}

// A request that asks for nothing against a pool with no gateway is unaffected —
// that is the overwhelming majority of sandboxes.
func TestBuildEgressPolicyAnnotation_NoGatewayNoPolicyIsNoop(t *testing.T) {
	if _, ok, err := buildEgressPolicyAnnotation(nil, &agentsv1alpha1.SandboxPool{}, "sbx-1"); err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

// The sidecar fails closed on a missing policy file, so a gateway-enabled pool
// must be stamped even when the request asked for no filtering — otherwise
// turning the gateway on would cut every sandbox off from the network.
func TestBuildEgressPolicyAnnotation_GatewayOnWithNoRequestIsUnrestricted(t *testing.T) {
	value, ok, err := buildEgressPolicyAnnotation(nil, gatewayPool(), "sbx-1")
	if err != nil || !ok {
		t.Fatalf("a gateway-enabled pool must always be stamped: ok=%v err=%v", ok, err)
	}
	p := decode(t, value)
	if !p.Enforce || p.DisableEgress {
		t.Fatalf("expected an enforcing allow-all policy, got %+v", p)
	}
	if !p.Evaluate("anything.example.com", net.ParseIP("8.8.8.8")).Allow {
		t.Error("a sandbox that asked for nothing must reach the internet")
	}
	// Private ranges included: asking for the gateway must not narrow egress on
	// its own, and split-horizon names resolve inside the cluster.
	if !p.Evaluate("op.example.com", net.ParseIP("10.0.0.1")).Allow {
		t.Error("a sandbox that asked for nothing must reach cluster-internal hosts")
	}
}

func TestBuildEgressPolicyAnnotation_EncodesTheRequestsPolicy(t *testing.T) {
	np := &agentsv1alpha1.SandboxNetworkPolicy{
		Egress: &agentsv1alpha1.EgressRules{AllowedDomains: []string{"pypi.org"}},
	}
	value, ok, err := buildEgressPolicyAnnotation(np, gatewayPool(), "sbx-1")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if !strings.Contains(value, "pypi.org") || !strings.Contains(value, "sbx-1") {
		t.Fatalf("policy did not survive encoding: %s", value)
	}
}
