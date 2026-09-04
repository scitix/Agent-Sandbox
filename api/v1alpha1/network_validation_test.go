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
	}
}

func validInjection() *SecretInjection {
	return &SecretInjection{
		Credentials: []InjectedCredential{{
			Name:      "openai",
			ValueFrom: SecretKeyRef{Name: "creds", Key: "openai"},
		}},
		Rules: []InjectionRule{{
			Host:    "api.openai.com",
			Headers: []HeaderInjection{{Name: "Authorization", Value: "Bearer ${e2b.secrets.openai}"}},
		}},
	}
}

func TestValidateSecretInjection_AcceptsValid(t *testing.T) {
	if err := ValidateSecretInjection(validPolicy(), validInjection()); err != nil {
		t.Fatalf("valid injection rejected: %v", err)
	}
}

func TestValidateSecretInjection_NilIsFine(t *testing.T) {
	if err := ValidateSecretInjection(nil, nil); err != nil {
		t.Fatalf("nil injection: %v", err)
	}
	if err := ValidateSecretInjection(validPolicy(), nil); err != nil {
		t.Fatalf("policy without injection: %v", err)
	}
}

func TestValidateSecretInjection_Rejects(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*SandboxNetworkPolicy, *SecretInjection)
		wantSub string
	}{
		{
			// Anyone able to control a matching subdomain would receive the credential.
			name:    "wildcard host",
			mutate:  func(_ *SandboxNetworkPolicy, si *SecretInjection) { si.Rules[0].Host = "*.openai.com" },
			wantSub: "wildcards are not allowed",
		},
		{
			name: "undeclared credential in template",
			mutate: func(_ *SandboxNetworkPolicy, si *SecretInjection) {
				si.Rules[0].Headers[0].Value = "Bearer ${e2b.secrets.nope}"
			},
			wantSub: "undeclared credential",
		},
		{
			// A literal here would put the credential in the request body and log.
			name: "literal header value",
			mutate: func(_ *SandboxNetworkPolicy, si *SecretInjection) {
				si.Rules[0].Headers[0].Value = "Bearer sk-literal"
			},
			wantSub: "references no credential",
		},
		{
			// The traffic would be dropped before the L7 path ever runs.
			name:    "host not in allowlist",
			mutate:  func(np *SandboxNetworkPolicy, _ *SecretInjection) { np.Egress.AllowedDomains = []string{"pypi.org"} },
			wantSub: "not permitted by the request's allowed domains",
		},
		{
			name:    "disableEgress with injection",
			mutate:  func(np *SandboxNetworkPolicy, _ *SecretInjection) { np.DisableEgress = true },
			wantSub: "cannot be combined with disabled egress",
		},
		{
			name:    "rule that does nothing",
			mutate:  func(_ *SandboxNetworkPolicy, si *SecretInjection) { si.Rules[0].Headers = nil },
			wantSub: "declares no headers",
		},
		{
			name: "credential never used",
			mutate: func(_ *SandboxNetworkPolicy, si *SecretInjection) {
				si.Credentials = append(si.Credentials, InjectedCredential{
					Name:      "unused",
					ValueFrom: SecretKeyRef{Name: "creds", Key: "unused"},
				})
			},
			wantSub: "never used",
		},
		{
			name:    "missing secret key",
			mutate:  func(_ *SandboxNetworkPolicy, si *SecretInjection) { si.Credentials[0].ValueFrom.Key = "" },
			wantSub: "valueFrom.name and valueFrom.key",
		},
		{
			name:    "no rules at all",
			mutate:  func(_ *SandboxNetworkPolicy, si *SecretInjection) { si.Rules = nil },
			wantSub: "declares no rules",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			np, si := validPolicy(), validInjection()
			tc.mutate(np, si)
			err := ValidateSecretInjection(np, si)
			if err == nil {
				t.Fatalf("expected rejection containing %q, got nil", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q does not mention %q", err, tc.wantSub)
			}
		})
	}
}

// "Inject but do not filter" is a valid request: there is no allowlist to
// satisfy, and the sidecar is there either way because the Env enabled the
// gateway.
func TestValidateSecretInjection_AllowsInjectionWithoutEgressRules(t *testing.T) {
	if err := ValidateSecretInjection(&SandboxNetworkPolicy{}, validInjection()); err != nil {
		t.Fatalf("injection without an allowlist should be valid: %v", err)
	}
	if err := ValidateSecretInjection(nil, validInjection()); err != nil {
		t.Fatalf("injection with no policy at all should be valid: %v", err)
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
