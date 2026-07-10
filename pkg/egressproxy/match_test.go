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

package egressproxy

import (
	"net"
	"testing"
)

func TestMatchDomain(t *testing.T) {
	cases := []struct {
		host, pattern string
		want          bool
	}{
		{"pypi.org", "pypi.org", true},
		{"PyPI.ORG", "pypi.org", true}, // case-insensitive
		{"pypi.org", "*", true},        // wildcard-all
		{"files.pythonhosted.org", "*.pythonhosted.org", true},
		{"a.b.pythonhosted.org", "*.pythonhosted.org", true}, // multi-label suffix
		{"pythonhosted.org", "*.pythonhosted.org", false},    // bare apex not covered by *.
		{"evil-pythonhosted.org", "*.pythonhosted.org", false},
		{"github.com", "pypi.org", false},
		{"pypi.org", "", false}, // empty pattern never matches
	}
	for _, c := range cases {
		if got := matchDomain(c.host, c.pattern); got != c.want {
			t.Errorf("matchDomain(%q,%q)=%v want %v", c.host, c.pattern, got, c.want)
		}
	}
}

func TestEvaluate_AllowlistDefaultDeny(t *testing.T) {
	p := Policy{Enforce: true, AllowedDomains: []string{"pypi.org", "*.pythonhosted.org"}}
	pub := net.ParseIP("8.8.8.8")

	if d := p.Evaluate("pypi.org", pub); !d.Allow || d.Match != MatchDomain {
		t.Errorf("allowed domain: got %+v", d)
	}
	if d := p.Evaluate("files.pythonhosted.org", pub); !d.Allow {
		t.Errorf("wildcard domain should allow: got %+v", d)
	}
	// Not on the allowlist -> default deny (the anti-cheat gate).
	if d := p.Evaluate("github.com", pub); d.Allow {
		t.Errorf("github.com must be denied by default-deny: got %+v", d)
	}
	// No hostname, public IP, not in allowed CIDRs -> deny.
	if d := p.Evaluate("", pub); d.Allow {
		t.Errorf("bare public IP must be denied when only domains allowed: got %+v", d)
	}
}

func TestEvaluate_DisableEgress(t *testing.T) {
	p := Policy{Enforce: true, DisableEgress: true, AllowedDomains: []string{"*"}}
	if d := p.Evaluate("pypi.org", net.ParseIP("8.8.8.8")); d.Allow {
		t.Errorf("DisableEgress must deny even with allow-all domain: got %+v", d)
	}
}

func TestEvaluate_NotEnforcing(t *testing.T) {
	p := Policy{Enforce: false}
	if d := p.Evaluate("github.com", net.ParseIP("8.8.8.8")); !d.Allow {
		t.Errorf("non-enforcing policy allows all: got %+v", d)
	}
}

func TestEvaluate_AntiSSRF(t *testing.T) {
	p := Policy{Enforce: true, AllowedCIDRs: []string{"169.254.169.254/32"}, AllowedDomains: []string{"*"}}
	// Even explicitly allowed, private/metadata ranges are denied by baseline.
	if d := p.Evaluate("", net.ParseIP("169.254.169.254")); d.Allow || d.Match != MatchSSRF {
		t.Errorf("metadata IP must be SSRF-denied despite allow rule: got %+v", d)
	}
	if d := p.Evaluate("", net.ParseIP("10.1.2.3")); d.Allow {
		t.Errorf("RFC1918 must be denied by baseline: got %+v", d)
	}
	// Opt-out lifts the baseline; an explicit allow rule then takes effect.
	p.AllowPrivateNetworks = true
	p.AllowedCIDRs = []string{"10.0.0.0/8"}
	if d := p.Evaluate("", net.ParseIP("10.1.2.3")); !d.Allow || d.Match != MatchCIDR {
		t.Errorf("AllowPrivateNetworks + allowed CIDR should permit RFC1918: got %+v", d)
	}
	// But without an allow rule it still default-denies (allowlist model).
	p.AllowedCIDRs = nil
	if d := p.Evaluate("", net.ParseIP("10.1.2.3")); d.Allow {
		t.Errorf("AllowPrivateNetworks alone must not allow (default-deny): got %+v", d)
	}
}

func TestEvaluate_CIDRAllowDeny(t *testing.T) {
	p := Policy{Enforce: true, AllowedCIDRs: []string{"8.8.0.0/16"}, DeniedCIDRs: []string{"1.1.1.1/32"}}
	if d := p.Evaluate("", net.ParseIP("8.8.8.8")); !d.Allow || d.Match != MatchCIDR {
		t.Errorf("allowed cidr: got %+v", d)
	}
	if d := p.Evaluate("", net.ParseIP("1.1.1.1")); d.Allow {
		t.Errorf("denied cidr must deny: got %+v", d)
	}
	if d := p.Evaluate("", net.ParseIP("9.9.9.9")); d.Allow {
		t.Errorf("unlisted public IP default-deny: got %+v", d)
	}
}

func TestIsPrivateIP(t *testing.T) {
	priv := []string{"10.0.0.1", "172.16.5.5", "192.168.1.1", "127.0.0.1", "169.254.169.254", "100.64.0.1"}
	for _, s := range priv {
		if !isPrivateIP(net.ParseIP(s)) {
			t.Errorf("%s should be private", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "93.184.216.34"} {
		if isPrivateIP(net.ParseIP(s)) {
			t.Errorf("%s should be public", s)
		}
	}
}
