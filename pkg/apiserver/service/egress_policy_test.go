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
	"testing"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/egressproxy"
)

func poolWith(np *agentsv1alpha1.SandboxNetworkPolicy) *agentsv1alpha1.SandboxPool {
	return &agentsv1alpha1.SandboxPool{Spec: agentsv1alpha1.SandboxPoolSpec{NetworkPolicy: np}}
}

func decode(t *testing.T, s string) egressproxy.Policy {
	t.Helper()
	var p egressproxy.Policy
	if err := json.Unmarshal([]byte(s), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return p
}

func TestBuildEgressPolicyAnnotation_NoPolicyPool(t *testing.T) {
	// Pool without a policy + no override => no annotation, no error.
	v, ok, err := buildEgressPolicyAnnotation(nil, poolWith(nil), "s1")
	if err != nil || ok || v != "" {
		t.Fatalf("expected no annotation; got v=%q ok=%v err=%v", v, ok, err)
	}
	// Pool without a policy + a per-sandbox override => rejected (fail-closed,
	// no sidecar to enforce it).
	_, _, err = buildEgressPolicyAnnotation(&agentsv1alpha1.SandboxNetworkPolicy{}, poolWith(nil), "s1")
	if err == nil {
		t.Fatal("override on a non-enabled pool must be rejected")
	}
}

func TestBuildEgressPolicyAnnotation_EnvDefault(t *testing.T) {
	env := &agentsv1alpha1.SandboxNetworkPolicy{Egress: &agentsv1alpha1.EgressRules{AllowedDomains: []string{"pypi.org"}}}
	v, ok, err := buildEgressPolicyAnnotation(nil, poolWith(env), "s1")
	if err != nil || !ok {
		t.Fatalf("env default should produce annotation: ok=%v err=%v", ok, err)
	}
	p := decode(t, v)
	if !p.Enforce || p.SandboxID != "s1" || len(p.AllowedDomains) != 1 || p.AllowedDomains[0] != "pypi.org" {
		t.Errorf("unexpected policy: %+v", p)
	}
}

func TestBuildEgressPolicyAnnotation_OverrideWins(t *testing.T) {
	env := &agentsv1alpha1.SandboxNetworkPolicy{Egress: &agentsv1alpha1.EgressRules{AllowedDomains: []string{"pypi.org"}}}
	override := &agentsv1alpha1.SandboxNetworkPolicy{DisableEgress: true}
	v, ok, err := buildEgressPolicyAnnotation(override, poolWith(env), "s1")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if p := decode(t, v); !p.DisableEgress {
		t.Errorf("override (DisableEgress) must win over env default: %+v", p)
	}
}

func TestToProxyPolicy_UnrestrictedRepresentation(t *testing.T) {
	// Egress nil, not disabled => allow-all with SSRF baseline intact.
	p := toProxyPolicy(&agentsv1alpha1.SandboxNetworkPolicy{}, "s1")
	if p.DisableEgress || len(p.AllowedDomains) != 1 || p.AllowedDomains[0] != "*" || len(p.AllowedCIDRs) != 1 {
		t.Errorf("nil Egress should be allow-all: %+v", p)
	}
	// Sanity: a public host is allowed, a metadata IP is still SSRF-denied.
	if !p.Evaluate("anything.example.com", net.ParseIP("8.8.8.8")).Allow {
		t.Error("allow-all should permit public host")
	}
	if p.Evaluate("", net.ParseIP("169.254.169.254")).Allow {
		t.Error("allow-all must still SSRF-deny metadata IP")
	}
}
