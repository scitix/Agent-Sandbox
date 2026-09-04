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

func decode(t *testing.T, s string) egressproxy.Policy {
	t.Helper()
	var p egressproxy.Policy
	if err := json.Unmarshal([]byte(s), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return p
}

func TestBuildEgressPolicyAnnotation_EncodesWhatTheRequestAsked(t *testing.T) {
	np := &agentsv1alpha1.SandboxNetworkPolicy{
		Egress: &agentsv1alpha1.EgressRules{AllowedDomains: []string{"pypi.org"}},
	}
	v, ok, err := buildEgressPolicyAnnotation(np, gatewayPool(), "s1")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	p := decode(t, v)
	if !p.Enforce || p.SandboxID != "s1" || len(p.AllowedDomains) != 1 || p.AllowedDomains[0] != "pypi.org" {
		t.Errorf("unexpected policy: %+v", p)
	}
}

func TestToProxyPolicy_UnrestrictedRepresentation(t *testing.T) {
	// Egress nil, not disabled => allow-all, private ranges included: a policy
	// that filters nothing must not be stricter than having no policy at all.
	p := toProxyPolicy(&agentsv1alpha1.SandboxNetworkPolicy{}, "s1")
	if p.DisableEgress || len(p.AllowedDomains) != 1 || p.AllowedDomains[0] != "*" || len(p.AllowedCIDRs) != 1 {
		t.Errorf("nil Egress should be allow-all: %+v", p)
	}
	if !p.AllowPrivateNetworks {
		t.Errorf("nil Egress must imply AllowPrivateNetworks: %+v", p)
	}
	if !p.Evaluate("anything.example.com", net.ParseIP("8.8.8.8")).Allow {
		t.Error("allow-all should permit public host")
	}
	// A split-horizon public name resolving inside the cluster: reachable with
	// no policy at all, so it must stay reachable under injection-only too.
	if !p.Evaluate("op.example.com", net.ParseIP("10.0.0.1")).Allow {
		t.Error("unrestricted must permit a host that resolves to a private IP")
	}
}

func TestToProxyPolicy_NamingAnInternalHostReachesIt(t *testing.T) {
	// The whole reason the baseline has two tiers: a request that filters and
	// also names an internal service gets that service. Requiring an extra flag
	// would push callers toward opening the entire cluster network instead.
	np := &agentsv1alpha1.SandboxNetworkPolicy{
		Egress: &agentsv1alpha1.EgressRules{AllowedDomains: []string{"op.example.com"}},
	}
	p := toProxyPolicy(np, "s1")
	if p.AllowPrivateNetworks {
		t.Errorf("declared filtering must not flip the blanket opt-in: %+v", p)
	}
	if !p.Evaluate("op.example.com", net.ParseIP("10.0.0.1")).Allow {
		t.Error("a specifically allowlisted host must be reachable where it resolves")
	}
	// Anything the request did not name stays denied.
	if d := p.Evaluate("other.example.com", net.ParseIP("10.0.0.1")); d.Allow {
		t.Error("an unnamed internal host must stay denied")
	}
	// And the metadata endpoint is not reachable by any route.
	if p.Evaluate("", net.ParseIP("169.254.169.254")).Allow {
		t.Error("metadata IP must be denied under a filtering policy")
	}
	if d := p.Evaluate("", net.ParseIP("100.100.100.200")); d.Allow {
		t.Errorf("this cloud's metadata endpoint must be denied too, got %+v", d)
	}
}

func TestToProxyPolicy_AllowAllWithBaselineIsExpressible(t *testing.T) {
	// "Allow everything but keep the baseline": declare the allow-all rules
	// explicitly instead of leaving Egress nil. A wildcard is deliberately not
	// a specific match, so it reaches the internet without exposing the cluster.
	p := toProxyPolicy(&agentsv1alpha1.SandboxNetworkPolicy{
		Egress: &agentsv1alpha1.EgressRules{
			AllowedDomains: []string{"*"},
			AllowedCIDRs:   []string{"0.0.0.0/0"},
		},
	}, "s1")
	if !p.Evaluate("anything.example.com", net.ParseIP("8.8.8.8")).Allow {
		t.Error("explicit allow-all should permit a public host")
	}
	if p.Evaluate("", net.ParseIP("169.254.169.254")).Allow {
		t.Error("explicit allow-all must still SSRF-deny metadata IP")
	}
	if p.Evaluate("internal.example.com", net.ParseIP("10.0.0.1")).Allow {
		t.Error("explicit allow-all must not reach the cluster network")
	}
}
