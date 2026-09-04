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

// The anti-SSRF baseline is two tiers, because "internal" covers two very
// different things and collapsing them forces a bad trade: a sandbox that needs
// one internal service would otherwise have to be handed the cloud metadata
// endpoint along with it.

// alwaysDeniedCIDRs can never be reached from a sandbox. No policy field lifts
// them, because nothing a sandbox legitimately does requires them and they are
// what an SSRF is aimed at: the instance-metadata services hand out cloud
// credentials to anything that can issue a plain unauthenticated GET.
//
// Loopback is here for the same reason rather than for reachability — `-o lo`
// is excluded from the redirect, so this only catches a destination that
// resolves to loopback while being routed off-box.
var alwaysDeniedCIDRs = mustCIDRs(
	// Link-local, which is where every major cloud puts its metadata service:
	// 169.254.169.254 (AWS IMDS / GCP / Azure / Alibaba / Oracle / DigitalOcean)
	// and 169.254.170.2 (AWS ECS task metadata).
	"169.254.0.0/16",
	"fe80::/10",         // link-local v6
	"fd00:ec2::254/128", // AWS IMDS over IPv6
	// Alibaba Cloud's metadata endpoint sits at 100.100.100.200, inside CGNAT
	// rather than link-local — so denying 169.254.0.0/16 alone would miss it on
	// exactly the cloud this runs on.
	"100.100.100.200/32",
	"127.0.0.0/8",
	"::1/128",
)

// privateCIDRs are denied by default but may be reached when the policy says
// so. These are where internal services legitimately live — an in-cluster
// registry, an internal API, a storage gateway — so a sandbox that names one
// explicitly gets it.
//
// Two things lift the denial: Policy.AllowPrivateNetworks (blanket opt-in), or
// a *specific* allowlist entry matching the destination. A wildcard entry does
// not count: "allow everything" is a statement about the internet, not a
// decision to expose the cluster's own network.
var privateCIDRs = mustCIDRs(
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"100.64.0.0/10", // CGNAT
	"fc00::/7",      // unique local
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

// isAlwaysDenied reports whether ip is in the tier no policy can open.
func isAlwaysDenied(ip net.IP) bool { return inAny(ip, alwaysDeniedCIDRs) }

// isPrivateIP reports whether ip is in the tier a policy may open.
//
// The two tiers overlap by construction — Alibaba's metadata endpoint sits
// inside CGNAT — so the always-denied set is subtracted here rather than left
// to the order of checks at the call site. That keeps the predicates disjoint:
// no address is ever both "re-openable" and "never reachable".
func isPrivateIP(ip net.IP) bool {
	return inAny(ip, privateCIDRs) && !inAny(ip, alwaysDeniedCIDRs)
}

func inAny(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
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

	// AllowsPrivate reports whether this decision permits a destination inside
	// the re-openable private ranges. Carried on the decision rather than
	// recomputed, because the post-resolution rebind guard has to apply the same
	// rule to the address DNS actually returned: a hostname allowed by a
	// specific rule must stay reachable when it resolves internally, while a
	// hostname allowed by a wildcard must not become a path to the cluster.
	AllowsPrivate bool
}

// Evaluate applies the policy to a connection. hostname is the peeked HTTP Host
// or TLS SNI ("" when unavailable); ip is the original destination IP.
//
// Semantics (allowlist / default-deny, hardened vs e2b's default-allow):
//
//  1. Not enforcing        -> allow (feature effectively off).
//  2. Always-denied ranges -> deny. No policy field lifts this.
//  3. DisableEgress        -> deny (allowlist empty by construction).
//  4. Specific allow match -> allow, private ranges included.
//  5. Private range        -> deny unless AllowPrivateNetworks.
//  6. Wildcard allow match -> allow.
//  7. Denied CIDR match    -> deny.
//  8. Default              -> DENY (this is the key hardening).
//
// Step 4 before step 5 is what makes "filter, and also reach this one internal
// service" expressible: allowOut ["harbor.internal", "10.20.0.0/16"] names its
// destinations, so it gets them. Step 6 after step 5 is what keeps allowOut
// ["*"] from quietly meaning "and the whole cluster network too".
func (p Policy) Evaluate(hostname string, ip net.IP) Decision {
	if !p.Enforce {
		return Decision{Allow: true, Match: MatchNone, AllowsPrivate: true}
	}

	// The metadata services and link-local: an unauthenticated GET away from
	// cloud credentials, and never something a sandbox needs.
	if ip != nil && isAlwaysDenied(ip) {
		return Decision{Allow: false, Match: MatchSSRF}
	}

	if p.DisableEgress {
		return Decision{Allow: false, Match: MatchNone}
	}

	match, specific := p.matchAllow(hostname, ip)

	// A caller that wrote down an internal host or CIDR meant it; a caller that
	// wrote "*" was talking about the internet.
	allowsPrivate := p.AllowPrivateNetworks || specific
	if ip != nil && isPrivateIP(ip) && !allowsPrivate {
		return Decision{Allow: false, Match: MatchSSRF}
	}

	if match != MatchNone {
		return Decision{Allow: true, Match: match, AllowsPrivate: allowsPrivate}
	}

	if ip != nil && cidrContains(p.DeniedCIDRs, ip) {
		return Decision{Allow: false, Match: MatchCIDR}
	}

	// Default deny — the allowlist gate. A sandbox with an enforcing policy can
	// only reach what is explicitly permitted.
	return Decision{Allow: false, Match: MatchNone}
}

// matchAllow reports which allowlist entry covers the destination, and whether
// that entry named it specifically rather than matching everything.
//
// "Specific" excludes only the two catch-alls, "*" and a /0 CIDR. A suffix
// pattern like "*.internal.example.com" still counts: it is scoped to a domain
// the caller controls, which is the point.
func (p Policy) matchAllow(hostname string, ip net.IP) (MatchType, bool) {
	if hostname != "" {
		for _, d := range p.AllowedDomains {
			if !matchDomain(hostname, d) {
				continue
			}
			return MatchDomain, d != "*"
		}
	}
	if ip != nil {
		for _, c := range p.AllowedCIDRs {
			_, n, err := net.ParseCIDR(c)
			if err != nil || !n.Contains(ip) {
				continue
			}
			ones, _ := n.Mask.Size()
			return MatchCIDR, ones > 0
		}
	}
	return MatchNone, false
}
