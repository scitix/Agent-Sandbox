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

	e2bgen "github.com/scitix/agent-sandbox/pkg/e2bcompat/gen"
)

func boolp(b bool) *bool { return &b }

func TestParseE2BNetworkPolicy_None(t *testing.T) {
	np, err := parseE2BNetworkPolicy(&e2bgen.NewSandbox{})
	if err != nil || np != nil {
		t.Fatalf("no network intent => nil policy; got np=%+v err=%+v", np, err)
	}
}

func TestParseE2BNetworkPolicy_DisableInternet(t *testing.T) {
	np, err := parseE2BNetworkPolicy(&e2bgen.NewSandbox{AllowInternetAccess: boolp(false)})
	if err != nil || np == nil || !np.DisableEgress {
		t.Fatalf("allow_internet_access=false => DisableEgress; got np=%+v err=%+v", np, err)
	}
}

func TestParseE2BNetworkPolicy_AllowOutSplit(t *testing.T) {
	allow := []string{"pypi.org", "*.pythonhosted.org", "8.8.8.8", "10.0.0.0/8"}
	deny := []string{"1.2.3.4"}
	np, err := parseE2BNetworkPolicy(&e2bgen.NewSandbox{
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
	if _, err := parseE2BNetworkPolicy(&e2bgen.NewSandbox{
		Network: &e2bgen.SandboxNetworkConfig{EgressProxy: &e2bgen.SandboxEgressProxyConfig{Address: "p:1080"}},
	}); err == nil {
		t.Error("egressProxy must be rejected")
	}
	rules := map[string][]e2bgen.SandboxNetworkRule{"api.example.com": {}}
	if _, err := parseE2BNetworkPolicy(&e2bgen.NewSandbox{
		Network: &e2bgen.SandboxNetworkConfig{Rules: &rules},
	}); err == nil {
		t.Error("rules must be rejected")
	}
}
