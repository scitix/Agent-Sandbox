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

package handlers

import (
	"strings"
	"testing"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	e2bgen "github.com/scitix/agent-sandbox/pkg/e2bcompat/gen"
)

func boolp(b bool) *bool { return &b }

func TestParseE2BNetworkPolicy_None(t *testing.T) {
	np, _, err := parseE2BNetworkPolicy(&e2bgen.NewSandbox{})
	if err != nil || np != nil {
		t.Fatalf("no network intent => nil policy; got np=%+v err=%+v", np, err)
	}
}

func TestParseE2BNetworkPolicy_DisableInternet(t *testing.T) {
	np, _, err := parseE2BNetworkPolicy(&e2bgen.NewSandbox{AllowInternetAccess: boolp(false)})
	if err != nil || np == nil || !np.DisableEgress {
		t.Fatalf("allow_internet_access=false => DisableEgress; got np=%+v err=%+v", np, err)
	}
}

func TestParseE2BNetworkPolicy_AllowOutSplit(t *testing.T) {
	allow := []string{"pypi.org", "*.pythonhosted.org", "8.8.8.8", "10.0.0.0/8"}
	deny := []string{"1.2.3.4"}
	np, _, err := parseE2BNetworkPolicy(&e2bgen.NewSandbox{
		Network: &e2bgen.SandboxNetworkConfig{AllowOut: &allow, DenyOut: &deny},
	})
	if err != nil || np == nil || np.Egress == nil {
		t.Fatalf("expected egress rules; got np=%+v err=%+v", np, err)
	}
	if len(np.Egress.AllowedDomains) != 2 {
		t.Errorf("want 2 domains, got %v", np.Egress.AllowedDomains)
	}
	if len(np.Egress.AllowedCIDRs) != 2 { // 8.8.8.8 -> /32, 10.0.0.0/8
		t.Errorf("want 2 cidrs, got %v", np.Egress.AllowedCIDRs)
	}
	if len(np.Egress.DeniedCIDRs) != 1 || np.Egress.DeniedCIDRs[0] != "1.2.3.4/32" {
		t.Errorf("deny bare IP should promote to /32, got %v", np.Egress.DeniedCIDRs)
	}
}

func TestParseE2BNetworkPolicy_RejectsUnsupported(t *testing.T) {
	if _, _, err := parseE2BNetworkPolicy(&e2bgen.NewSandbox{
		Network: &e2bgen.SandboxNetworkConfig{EgressProxy: &e2bgen.SandboxEgressProxyConfig{Address: "p:1080"}},
	}); err == nil {
		t.Error("egressProxy must be rejected")
	}
}

// --------------------------------------------------------------------------
// Per-host injection rules
//
// Rules used to be refused outright, because E2B's wire shape put the header
// value inline and accepting it would have written a credential into the
// request body and the access log. A value built from Secret.fill() carries
// only a name, so the rule is now accepted — and a literal one still is not.
// --------------------------------------------------------------------------

func rulesBody(host string, headers map[string]string) *e2bgen.NewSandbox {
	rules := map[string][]e2bgen.SandboxNetworkRule{
		host: {{Transform: &e2bgen.SandboxNetworkTransform{Headers: &headers}}},
	}
	return &e2bgen.NewSandbox{Network: &e2bgen.SandboxNetworkConfig{Rules: &rules}}
}

func TestParseInjectionRules_AcceptsVaultReference(t *testing.T) {
	_, rules, err := parseE2BNetworkPolicy(rulesBody("api.openai.com", map[string]string{
		"Authorization": "Bearer ${e2b.secrets.openai}",
	}))
	if err != nil {
		t.Fatalf("unexpected rejection: %s", err.Message)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	r := rules[0]
	if r.Host != "api.openai.com" || len(r.Headers) != 1 {
		t.Fatalf("unexpected rule: %+v", r)
	}
	// The value crosses the boundary unrewritten: wire syntax and stored syntax
	// are the same, so there is no translation step to get wrong.
	if r.Headers[0].Value != "Bearer ${e2b.secrets.openai}" {
		t.Fatalf("value should be stored verbatim, got %q", r.Headers[0].Value)
	}
	// E2B transform semantics replace an existing header.
	if r.Headers[0].Mode != agentsv1alpha1.HeaderInjectionOverride {
		t.Fatalf("expected Override mode, got %q", r.Headers[0].Mode)
	}
	if got := VaultRefsIn(rules); len(got) != 1 || got[0] != "openai" {
		t.Fatalf("unexpected refs: %v", got)
	}
}

func TestParseInjectionRules_RejectsLiteralValue(t *testing.T) {
	_, _, err := parseE2BNetworkPolicy(rulesBody("api.openai.com", map[string]string{
		"Authorization": "Bearer sk-literal-secret",
	}))
	if err == nil {
		t.Fatal("a plaintext credential in the request body must be refused")
	}
	if !strings.Contains(err.Message, "Secret.fill") {
		t.Fatalf("message should point at the fix, got %q", err.Message)
	}
}

func TestParseInjectionRules_RejectsWildcardHost(t *testing.T) {
	_, _, err := parseE2BNetworkPolicy(rulesBody("*.openai.com", map[string]string{
		"Authorization": "Bearer ${e2b.secrets.openai}",
	}))
	if err == nil {
		t.Fatal("a wildcard host would hand the credential to any matching subdomain")
	}
}

func TestParseInjectionRules_RejectsWorkloadIdentityPlaceholder(t *testing.T) {
	_, _, err := parseE2BNetworkPolicy(rulesBody("api.internal.example.com", map[string]string{
		"Authorization": "Bearer ${e2b.identity.tokens.aws}",
	}))
	if err == nil {
		t.Fatal("workload identity tokens are not issued here")
	}
	// Named explicitly rather than falling into "references no credential",
	// which would send the caller hunting for a typo that is not there.
	if !strings.Contains(err.Message, "workload identity") {
		t.Fatalf("message should name the feature, got %q", err.Message)
	}
}

func TestParseInjectionRules_RejectsRetiredTemplateSyntax(t *testing.T) {
	_, _, err := parseE2BNetworkPolicy(rulesBody("api.openai.com", map[string]string{
		"Authorization": "Bearer {{ openai }}",
	}))
	if err == nil {
		t.Fatal("the retired doubled-curly syntax must be refused, not silently treated as a literal")
	}
	if !strings.Contains(err.Message, "${e2b.secrets.") {
		t.Fatalf("message should give the current syntax, got %q", err.Message)
	}
}

// A host entry with no transform asks for nothing; it is not an error.
func TestParseInjectionRules_EmptyRuleListIsNoRules(t *testing.T) {
	rules := map[string][]e2bgen.SandboxNetworkRule{"api.example.com": {}}
	_, got, err := parseE2BNetworkPolicy(&e2bgen.NewSandbox{
		Network: &e2bgen.SandboxNetworkConfig{Rules: &rules},
	})
	if err != nil {
		t.Fatalf("unexpected rejection: %s", err.Message)
	}
	if len(got) != 0 {
		t.Fatalf("expected no rules, got %+v", got)
	}
}

// Rejections must not depend on Go's map iteration order, or the same request
// would blame a different host on a retry.
func TestParseInjectionRules_RejectionIsDeterministic(t *testing.T) {
	rules := map[string][]e2bgen.SandboxNetworkRule{}
	for _, h := range []string{"z.example.com", "a.example.com", "m.example.com"} {
		headers := map[string]string{"Authorization": "Bearer literal"}
		rules[h] = []e2bgen.SandboxNetworkRule{{Transform: &e2bgen.SandboxNetworkTransform{Headers: &headers}}}
	}
	body := &e2bgen.NewSandbox{Network: &e2bgen.SandboxNetworkConfig{Rules: &rules}}

	first := ""
	for range 20 {
		_, _, err := parseE2BNetworkPolicy(body)
		if err == nil {
			t.Fatal("expected a rejection")
		}
		if first == "" {
			first = err.Message
			continue
		}
		if err.Message != first {
			t.Fatalf("rejection is order-dependent:\n%s\n%s", first, err.Message)
		}
	}
	if !strings.Contains(first, "a.example.com") {
		t.Fatalf("expected the first host in sorted order, got %q", first)
	}
}
