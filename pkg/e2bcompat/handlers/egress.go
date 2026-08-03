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
	"net"
	"strings"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	e2bgen "github.com/scitix/agent-sandbox/pkg/e2bcompat/gen"
)

// parseE2BNetworkPolicy maps the E2B create body's network / allow_internet_access
// onto an AgentBox SandboxNetworkPolicy. Returns (nil, nil) when the request
// carries no network intent. Unsupported E2B features are rejected with an
// E2B-shaped error rather than silently dropped.
func parseE2BNetworkPolicy(body *e2bgen.NewSandbox) (*agentsv1alpha1.SandboxNetworkPolicy, *e2bgen.Error) {
	disableAll := body.AllowInternetAccess != nil && !*body.AllowInternetAccess
	ncfg := body.Network
	if ncfg == nil && !disableAll {
		return nil, nil
	}

	np := &agentsv1alpha1.SandboxNetworkPolicy{}
	// allow_internet_access=false is E2B sugar for "deny all egress"; it wins
	// over any allow rules in the same request.
	if disableAll {
		np.DisableEgress = true
		return np, nil
	}

	// ncfg != nil here.
	if ncfg.EgressProxy != nil {
		e := errRespCode(400, "network.egressProxy (SOCKS5 BYOP) is not supported by AgentBox")
		return nil, &e
	}
	if ncfg.Rules != nil && len(*ncfg.Rules) > 0 {
		// AgentBox does support per-host header injection, but only as a
		// SandboxEnv declaration backed by a Secret. E2B's wire shape carries
		// the header value inline, which would put a credential in the request
		// body, the access log, and the caller's source — the exposure the
		// feature exists to eliminate.
		e := errRespCode(400, "network.rules is not accepted per sandbox; declare credential "+
			"injection on the SandboxEnv (overrides.networkPolicy.secretInjection) instead")
		return nil, &e
	}

	var eg agentsv1alpha1.EgressRules
	if ncfg.AllowOut != nil {
		eg.AllowedDomains, eg.AllowedCIDRs = splitAllowOut(*ncfg.AllowOut)
	}
	if ncfg.DenyOut != nil {
		for _, d := range *ncfg.DenyOut {
			if d = strings.TrimSpace(d); d != "" {
				eg.DeniedCIDRs = append(eg.DeniedCIDRs, normalizeCIDR(d))
			}
		}
	}
	if len(eg.AllowedDomains) > 0 || len(eg.AllowedCIDRs) > 0 || len(eg.DeniedCIDRs) > 0 {
		np.Egress = &eg
	}
	// An empty ncfg with no rules leaves np.Egress nil = unrestricted (subject to
	// the anti-SSRF baseline the proxy always enforces).
	return np, nil
}

// splitAllowOut partitions E2B allowOut entries into domains vs CIDRs. An entry
// is a CIDR, a bare IP (promoted to /32 or /128), or a domain name.
func splitAllowOut(entries []string) (domains, cidrs []string) {
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(e); err == nil {
			cidrs = append(cidrs, e)
			continue
		}
		if ip := net.ParseIP(e); ip != nil {
			cidrs = append(cidrs, hostCIDR(ip))
			continue
		}
		domains = append(domains, e)
	}
	return domains, cidrs
}

// normalizeCIDR promotes a bare IP to a host CIDR; passes CIDRs through.
func normalizeCIDR(s string) string {
	if _, _, err := net.ParseCIDR(s); err == nil {
		return s
	}
	if ip := net.ParseIP(s); ip != nil {
		return hostCIDR(ip)
	}
	return s
}

func hostCIDR(ip net.IP) string {
	if ip.To4() != nil {
		return ip.String() + "/32"
	}
	return ip.String() + "/128"
}
