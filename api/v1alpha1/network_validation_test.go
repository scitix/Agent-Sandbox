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

package v1alpha1

import (
	"strings"
	"testing"
)

func validPolicy() *SandboxNetworkPolicy {
	return &SandboxNetworkPolicy{
		Egress: &EgressRules{AllowedDomains: []string{"api.openai.com"}},
		SecretInjection: &SecretInjection{
			Credentials: []InjectedCredential{{
				Name:      "openai",
				ValueFrom: SecretKeyRef{Name: "creds", Key: "openai"},
			}},
			Rules: []InjectionRule{{
				Host:    "api.openai.com",
				Headers: []HeaderInjection{{Name: "Authorization", Value: "Bearer ${e2b.secrets.openai}"}},
			}},
		},
	}
}

func TestValidateSecretInjection_AcceptsValid(t *testing.T) {
	if err := ValidateSecretInjection(validPolicy()); err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}
}

func TestValidateSecretInjection_NilIsFine(t *testing.T) {
	if err := ValidateSecretInjection(nil); err != nil {
		t.Fatalf("nil policy: %v", err)
	}
	if err := ValidateSecretInjection(&SandboxNetworkPolicy{}); err != nil {
		t.Fatalf("policy without injection: %v", err)
	}
}

func TestValidateSecretInjection_Rejects(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*SandboxNetworkPolicy)
		wantSub string
	}{
		{
			// Anyone able to control a matching subdomain would receive the credential.
			name:    "wildcard host",
			mutate:  func(p *SandboxNetworkPolicy) { p.SecretInjection.Rules[0].Host = "*.openai.com" },
			wantSub: "wildcards are not allowed",
		},
		{
			name: "undeclared credential in template",
			mutate: func(p *SandboxNetworkPolicy) {
				p.SecretInjection.Rules[0].Headers[0].Value = "Bearer ${e2b.secrets.nope}"
			},
			wantSub: "undeclared credential",
		},
		{
			// A literal value here would be a plaintext secret living in the CRD.
			name:    "literal header value",
			mutate:  func(p *SandboxNetworkPolicy) { p.SecretInjection.Rules[0].Headers[0].Value = "Bearer sk-literal" },
			wantSub: "references no credential",
		},
		{
			// The traffic would be dropped before the L7 path ever runs.
			name:    "host not in allowlist",
			mutate:  func(p *SandboxNetworkPolicy) { p.Egress.AllowedDomains = []string{"pypi.org"} },
			wantSub: "not permitted by egress.allowedDomains",
		},
		{
			name:    "disableEgress with injection",
			mutate:  func(p *SandboxNetworkPolicy) { p.DisableEgress = true },
			wantSub: "cannot be combined with disableEgress",
		},
		{
			name: "short placeholder",
			mutate: func(p *SandboxNetworkPolicy) {
				p.SecretInjection.Credentials[0].ExposeAs = "OPENAI_API_KEY"
				p.SecretInjection.Credentials[0].Placeholder = "short"
			},
			wantSub: "at least 16 characters",
		},
		{
			name: "placeholder without exposeAs",
			mutate: func(p *SandboxNetworkPolicy) {
				p.SecretInjection.Credentials[0].Placeholder = "0123456789abcdef0123"
			},
			wantSub: "no exposeAs",
		},
		{
			// Overlapping decoys substitute into each other.
			name: "placeholder is substring of another",
			mutate: func(p *SandboxNetworkPolicy) {
				si := p.SecretInjection
				si.Credentials[0].ExposeAs = "A_KEY"
				si.Credentials[0].Placeholder = "agbx_ph_0123456789abcdef"
				si.Credentials = append(si.Credentials, InjectedCredential{
					Name:        "other",
					ValueFrom:   SecretKeyRef{Name: "creds", Key: "other"},
					ExposeAs:    "B_KEY",
					Placeholder: "agbx_ph_0123456789abcdef_more",
				})
				si.Rules[0].Substitute = []string{"openai", "other"}
			},
			wantSub: "overlapping placeholders",
		},
		{
			name: "substitute without exposeAs",
			mutate: func(p *SandboxNetworkPolicy) {
				p.SecretInjection.Rules[0].Substitute = []string{"openai"}
			},
			wantSub: "no exposeAs",
		},
		{
			name:    "rule that does nothing",
			mutate:  func(p *SandboxNetworkPolicy) { p.SecretInjection.Rules[0].Headers = nil },
			wantSub: "neither headers nor substitute",
		},
		{
			name: "credential never used",
			mutate: func(p *SandboxNetworkPolicy) {
				p.SecretInjection.Credentials = append(p.SecretInjection.Credentials, InjectedCredential{
					Name:      "unused",
					ValueFrom: SecretKeyRef{Name: "creds", Key: "unused"},
				})
			},
			wantSub: "never used",
		},
		{
			name:    "missing secret key",
			mutate:  func(p *SandboxNetworkPolicy) { p.SecretInjection.Credentials[0].ValueFrom.Key = "" },
			wantSub: "valueFrom.name and valueFrom.key",
		},
		{
			name:    "no rules at all",
			mutate:  func(p *SandboxNetworkPolicy) { p.SecretInjection.Rules = nil },
			wantSub: "declares no rules",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := validPolicy()
			tc.mutate(p)
			err := ValidateSecretInjection(p)
			if err == nil {
				t.Fatalf("expected rejection containing %q, got nil", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q does not mention %q", err, tc.wantSub)
			}
		})
	}
}

// "Inject but do not filter" is a valid configuration: no egress allowlist to
// satisfy, and the sidecar still gets injected because NetworkPolicy is set.
func TestValidateSecretInjection_AllowsInjectionWithoutEgressRules(t *testing.T) {
	p := validPolicy()
	p.Egress = nil
	if err := ValidateSecretInjection(p); err != nil {
		t.Fatalf("injection without an allowlist should be valid: %v", err)
	}
}

func TestDomainAllowed(t *testing.T) {
	for _, tc := range []struct {
		host    string
		allowed []string
		want    bool
	}{
		{"api.openai.com", []string{"api.openai.com"}, true},
		{"api.openai.com", []string{"*"}, true},
		{"api.openai.com", []string{"*.openai.com"}, true},
		{"openai.com", []string{"*.openai.com"}, true},
		{"api.openai.com", []string{"pypi.org"}, false},
		{"API.OPENAI.COM", []string{"api.openai.com"}, true},
	} {
		if got := domainAllowed(tc.host, tc.allowed); got != tc.want {
			t.Errorf("domainAllowed(%q,%v)=%v, want %v", tc.host, tc.allowed, got, tc.want)
		}
	}
}

func TestPodHasEgressProxy(t *testing.T) {
	if PodHasEgressProxy(nil) {
		t.Fatal("nil pod must not report a sidecar")
	}
}
