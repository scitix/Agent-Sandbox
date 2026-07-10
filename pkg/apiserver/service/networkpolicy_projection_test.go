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
	"testing"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

func TestNetworkPolicyToGen(t *testing.T) {
	if networkPolicyToGen(nil) != nil {
		t.Fatal("nil in => nil out")
	}
	np := &agentsv1alpha1.SandboxNetworkPolicy{
		AllowPrivateNetworks: true,
		Egress: &agentsv1alpha1.EgressRules{
			AllowedDomains: []string{"pypi.org", "*.pythonhosted.org"},
			DeniedCIDRs:    []string{"1.2.3.4/32"},
		},
	}
	g := networkPolicyToGen(np)
	if g == nil || g.AllowPrivateNetworks == nil || !*g.AllowPrivateNetworks {
		t.Fatalf("allowPrivateNetworks not mapped: %+v", g)
	}
	if g.DisableEgress != nil {
		t.Errorf("disableEgress false should be omitted, got %+v", g.DisableEgress)
	}
	if g.Egress == nil || g.Egress.AllowedDomains == nil || len(*g.Egress.AllowedDomains) != 2 {
		t.Errorf("allowedDomains not mapped: %+v", g.Egress)
	}
	if g.Egress.AllowedCIDRs != nil {
		t.Errorf("empty allowedCIDRs should be omitted, got %+v", g.Egress.AllowedCIDRs)
	}
	if g.Egress.DeniedCIDRs == nil || (*g.Egress.DeniedCIDRs)[0] != "1.2.3.4/32" {
		t.Errorf("deniedCIDRs not mapped: %+v", g.Egress.DeniedCIDRs)
	}
}

func TestNetworkPolicyToGen_DisableEgress(t *testing.T) {
	g := networkPolicyToGen(&agentsv1alpha1.SandboxNetworkPolicy{DisableEgress: true})
	if g == nil || g.DisableEgress == nil || !*g.DisableEgress {
		t.Fatalf("disableEgress not mapped: %+v", g)
	}
	if g.Egress != nil {
		t.Errorf("no egress rules => nil egress, got %+v", g.Egress)
	}
}
