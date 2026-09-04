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

package sandboxenv

import (
	"testing"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

func policyWith(values ...string) *agentsv1alpha1.SandboxNetworkPolicy {
	headers := make([]agentsv1alpha1.HeaderInjection, 0, len(values))
	for _, v := range values {
		headers = append(headers, agentsv1alpha1.HeaderInjection{Name: "Authorization", Value: v})
	}
	return &agentsv1alpha1.SandboxNetworkPolicy{
		SecretInjection: &agentsv1alpha1.SecretInjection{
			Rules: []agentsv1alpha1.InjectionRule{{Host: "api.example.com", Headers: headers}},
		},
	}
}

func TestNormalizeInjectionPlaceholders_RewritesLegacySyntax(t *testing.T) {
	np := policyWith("Bearer {{ navix }}", "Basic {{tok}}")
	if !agentsv1alpha1.NormalizeInjectionPlaceholders(np) {
		t.Fatal("expected a rewrite")
	}
	got := np.SecretInjection.Rules[0].Headers
	if got[0].Value != "Bearer ${e2b.secrets.navix}" {
		t.Fatalf("spaced form not rewritten: %q", got[0].Value)
	}
	if got[1].Value != "Basic ${e2b.secrets.tok}" {
		t.Fatalf("tight form not rewritten: %q", got[1].Value)
	}
}

// Idempotence is what lets both the API and the controller call it freely.
func TestNormalizeInjectionPlaceholders_IsIdempotent(t *testing.T) {
	np := policyWith("Bearer ${e2b.secrets.navix}")
	if agentsv1alpha1.NormalizeInjectionPlaceholders(np) {
		t.Fatal("already-current values must not be reported as changed")
	}
	if np.SecretInjection.Rules[0].Headers[0].Value != "Bearer ${e2b.secrets.navix}" {
		t.Fatal("value must be untouched")
	}
}

// The migrated value must pass the validation that rejects the legacy form,
// otherwise the migration would leave the Env exactly as unusable as before.
func TestNormalizedValuePassesValidation(t *testing.T) {
	np := policyWith("Bearer {{ navix }}")
	np.SecretInjection.Credentials = []agentsv1alpha1.InjectedCredential{{
		Name:      "navix",
		ValueFrom: agentsv1alpha1.SecretKeyRef{Name: "eis-navix", Key: "navix"},
	}}

	if err := agentsv1alpha1.ValidateSecretInjection(np); err == nil {
		t.Fatal("the legacy form should not validate; that is why migration exists")
	}
	agentsv1alpha1.NormalizeInjectionPlaceholders(np)
	if err := agentsv1alpha1.ValidateSecretInjection(np); err != nil {
		t.Fatalf("migrated policy must validate: %v", err)
	}
}

func TestNormalizeInjectionPlaceholders_NilSafe(t *testing.T) {
	if agentsv1alpha1.NormalizeInjectionPlaceholders(nil) {
		t.Fatal("nil policy must report no change")
	}
	if agentsv1alpha1.NormalizeInjectionPlaceholders(&agentsv1alpha1.SandboxNetworkPolicy{}) {
		t.Fatal("a policy without injection must report no change")
	}
}
