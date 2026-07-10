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
	"strings"
)

// deniedPrivateCIDRs is the anti-SSRF baseline: sandbox egress to these ranges
// is denied unless Policy.AllowPrivateNetworks is set. Covers RFC1918, loopback,
// link-local (incl. the 169.254.169.254 cloud metadata endpoint), CGNAT, and
// unique-local IPv6. Mirrors e2b-dev/infra's DeniedSandboxSetData.
var deniedPrivateCIDRs = mustCIDRs(
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"127.0.0.0/8",
	"169.254.0.0/16", // link-local + cloud metadata (169.254.169.254)
	"100.64.0.0/10",  // CGNAT
	"::1/128",
	"fc00::/7",  // unique local
	"fe80::/10", // link-local v6
)

func mustCIDRs(cidrs ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic("egressproxy: bad builtin CIDR " + c + ": " + err.Error())
		}
		out = append(out, n)
	}
	return out
}

// MatchType records why a connection was allowed/denied, for metrics and logs.
type MatchType string

const (
	MatchNone   MatchType = "none"
	MatchDomain MatchType = "domain"
	MatchCIDR   MatchType = "cidr"
	MatchSSRF   MatchType = "ssrf"
)

// matchDomain reports whether hostname matches a pattern. Patterns are exact
// (case-insensitive), "*" (match all), or "*.example.com" (suffix). Mirrors
// e2b tcpfirewall's matchDomain.
func matchDomain(hostname, pattern string) bool {
	switch {
	case pattern == "":
		return false
	case pattern == "*":
		return true
	case strings.EqualFold(pattern, hostname):
		return true
	case strings.HasPrefix(pattern, "*."):
		suffix := pattern[1:] // ".example.com"
		return strings.HasSuffix(strings.ToLower(hostname), strings.ToLower(suffix))
	default:
		return false
	}
}

func isPrivateIP(ip net.IP) bool {
	for _, n := range deniedPrivateCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func cidrContains(cidrs []string, ip net.IP) bool {
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			continue // skip malformed entries; validation happens upstream
		}
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// Decision is the outcome of an egress check.
type Decision struct {
	Allow bool
	Match MatchType
}

// Evaluate applies the policy to a connection. hostname is the peeked HTTP Host
// or TLS SNI ("" when unavailable); ip is the original destination IP.
//
// Semantics (allowlist / default-deny, hardened vs e2b's default-allow):
//  1. Not enforcing            -> allow (feature effectively off).
//  2. Anti-SSRF baseline       -> deny private ranges unless AllowPrivateNetworks.
//  3. DisableEgress            -> deny (allowlist empty by construction).
//  4. Allowed domain match     -> allow.
//  5. Allowed CIDR match       -> allow.
//  6. Denied CIDR match        -> deny.
//  7. Default                  -> DENY (this is the key hardening).
func (p Policy) Evaluate(hostname string, ip net.IP) Decision {
	if !p.Enforce {
		return Decision{Allow: true, Match: MatchNone}
	}

	// Anti-SSRF baseline: never let the sandbox reach internal ranges, even if
	// a user rule or a resolved hostname points there. Explicit AllowedCIDRs do
	// not override this — matching e2b's "always denied" set.
	if !p.AllowPrivateNetworks && ip != nil && isPrivateIP(ip) {
		return Decision{Allow: false, Match: MatchSSRF}
	}

	if p.DisableEgress {
		return Decision{Allow: false, Match: MatchNone}
	}

	if hostname != "" {
		for _, d := range p.AllowedDomains {
			if matchDomain(hostname, d) {
				return Decision{Allow: true, Match: MatchDomain}
			}
		}
	}

	if ip != nil {
		if cidrContains(p.AllowedCIDRs, ip) {
			return Decision{Allow: true, Match: MatchCIDR}
		}
		if cidrContains(p.DeniedCIDRs, ip) {
			return Decision{Allow: false, Match: MatchCIDR}
		}
	}

	// Default deny — the allowlist gate. A sandbox with an enforcing policy can
	// only reach what is explicitly permitted.
	return Decision{Allow: false, Match: MatchNone}
}
