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
	"testing"

	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"k8s.io/utils/ptr"
)

func TestNetworkPolicyFromGen(t *testing.T) {
	if networkPolicyFromGen(nil) != nil {
		t.Fatal("nil in => nil out")
	}
	g := &gen.SandboxNetworkPolicy{
		AllowPrivateNetworks: ptr.To(true),
		Egress: &gen.EgressRules{
			AllowedDomains: ptr.To([]string{"pypi.org", "*.pythonhosted.org"}),
			AllowedCIDRs:   ptr.To([]string{"8.8.8.8/32"}),
		},
	}
	np := networkPolicyFromGen(g)
	if np == nil || !np.AllowPrivateNetworks || np.DisableEgress {
		t.Fatalf("scalars not mapped: %+v", np)
	}
	if np.Egress == nil || len(np.Egress.AllowedDomains) != 2 || len(np.Egress.AllowedCIDRs) != 1 {
		t.Errorf("egress rules not mapped: %+v", np.Egress)
	}
	if len(np.Egress.DeniedCIDRs) != 0 {
		t.Errorf("omitted deniedCIDRs => empty, got %+v", np.Egress.DeniedCIDRs)
	}
}

func TestNetworkPolicyFromGen_DisableEgress(t *testing.T) {
	np := networkPolicyFromGen(&gen.SandboxNetworkPolicy{DisableEgress: ptr.To(true)})
	if np == nil || !np.DisableEgress || np.Egress != nil {
		t.Fatalf("disableEgress-only not mapped: %+v", np)
	}
}
