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
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	e2bgen "github.com/scitix/agent-sandbox/pkg/e2bcompat/gen"
)

// parseE2BNetworkPolicy maps the E2B create body's network / allow_internet_access
// onto an AgentBox SandboxNetworkPolicy, and extracts any per-host injection
// rules. Returns (nil, nil, nil) when the request carries no network intent.
// Unsupported E2B features are rejected with an E2B-shaped error rather than
// silently dropped.
func parseE2BNetworkPolicy(body *e2bgen.NewSandbox) (*agentsv1alpha1.SandboxNetworkPolicy, []agentsv1alpha1.InjectionRule, *e2bgen.Error) {
	disableAll := body.AllowInternetAccess != nil && !*body.AllowInternetAccess
	ncfg := body.Network
	if ncfg == nil && !disableAll {
		return nil, nil, nil
	}

	np := &agentsv1alpha1.SandboxNetworkPolicy{}
	// allow_internet_access=false is E2B sugar for "deny all egress"; it wins
	// over any allow rules in the same request. Transform rules are refused
	// rather than dropped alongside them: nothing can reach the hosts they name,
	// so a silently ignored rule would look configured and never fire.
	if disableAll {
		if ncfg != nil && ncfg.Rules != nil && len(*ncfg.Rules) > 0 {
			e := errRespCode(400, "network.rules cannot be combined with allow_internet_access=false: "+
				"nothing can reach the hosts the rules name.")
			return nil, nil, &e
		}
		np.DisableEgress = true
		return np, nil, nil
	}

	// ncfg != nil here.
	if ncfg.EgressProxy != nil {
		e := errRespCode(400, "network.egressProxy (SOCKS5 BYOP) is not supported: AgentBox filters "+
			"egress in an in-Pod sidecar, not through an external proxy.")
		return nil, nil, &e
	}

	rules, ruleErr := parseInjectionRules(ncfg.Rules)
	if ruleErr != nil {
		return nil, nil, ruleErr
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
	return np, rules, nil
}

// vaultRefRe matches a vault reference in a header value. It is the syntax the
// E2B SDK's Secret.fill() produces, and the same syntax the CRD stores, so a
// rule crosses the API boundary unrewritten.
var vaultRefRe = regexp.MustCompile(`\$\{e2b\.secrets\.([a-zA-Z0-9_-]+)\}`)

// identityRefRe matches the workload-identity placeholder, which we do not
// serve. Recognised only so it can be refused by name instead of falling into
// the generic "references no credential" error, which would send the caller
// looking for a typo that is not there.
var identityRefRe = regexp.MustCompile(`\$\{e2b\.identity\.[^}]*\}`)

// parseInjectionRules converts E2B's per-host transform rules into the CRD
// shape, accepting only header values built from vault references.
//
// A literal value is refused, and that is the whole point: a credential written
// inline would land in the request body, the access log, and the caller's
// source — exactly the exposure that injecting it server-side exists to remove.
// A reference carries only a name, and names are not secret.
func parseInjectionRules(in *map[string][]e2bgen.SandboxNetworkRule) ([]agentsv1alpha1.InjectionRule, *e2bgen.Error) {
	if in == nil || len(*in) == 0 {
		return nil, nil
	}

	// Hosts are visited in sorted order so a rejection is deterministic: the
	// same request must not blame a different host on a retry.
	hosts := make([]string, 0, len(*in))
	for host := range *in {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)

	var out []agentsv1alpha1.InjectionRule
	for _, host := range hosts {
		if strings.Contains(host, "*") {
			e := errRespCode(400, fmt.Sprintf("network.rules host %q: wildcard hosts are not accepted, "+
				"because anyone able to control a matching subdomain would receive the injected "+
				"credential. Use an exact hostname.", host))
			return nil, &e
		}
		for _, rule := range (*in)[host] {
			if rule.Transform == nil || rule.Transform.Headers == nil || len(*rule.Transform.Headers) == 0 {
				continue
			}
			converted, convErr := convertTransformHeaders(host, *rule.Transform.Headers)
			if convErr != nil {
				return nil, convErr
			}
			out = append(out, agentsv1alpha1.InjectionRule{Host: host, Headers: converted})
		}
	}
	return out, nil
}

// convertTransformHeaders validates and converts one rule's headers.
func convertTransformHeaders(host string, headers map[string]string) ([]agentsv1alpha1.HeaderInjection, *e2bgen.Error) {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]agentsv1alpha1.HeaderInjection, 0, len(names))
	for _, name := range names {
		value := headers[name]

		if identityRefRe.MatchString(value) {
			e := errRespCode(400, fmt.Sprintf("network.rules host %q header %q references a workload "+
				"identity token, which AgentBox does not issue. Store the credential with POST /secrets "+
				"and reference it as ${e2b.secrets.<name>} instead.", host, name))
			return nil, &e
		}
		if strings.Contains(value, "{{") || strings.Contains(value, "}}") {
			e := errRespCode(400, fmt.Sprintf("network.rules host %q header %q uses a doubled-curly "+
				"template, which is no longer the placeholder syntax. Use ${e2b.secrets.<name>}.", host, name))
			return nil, &e
		}
		if !vaultRefRe.MatchString(value) {
			e := errRespCode(400, fmt.Sprintf("network.rules host %q header %q must reference a stored "+
				"secret: build the value with Secret.fill(\"name\") so it reads ${e2b.secrets.name}. "+
				"A literal value would put the credential in the request body and the access log.", host, name))
			return nil, &e
		}

		out = append(out, agentsv1alpha1.HeaderInjection{
			Name: name,
			// E2B's transform semantics are "an existing header with the same
			// name is replaced", which is Override. The wire shape cannot
			// express IfAbsent, so nothing produces it here.
			Mode:  agentsv1alpha1.HeaderInjectionOverride,
			Value: value,
		})
	}
	return out, nil
}

// VaultRefsIn returns every vault entry name referenced by these rules.
func VaultRefsIn(rules []agentsv1alpha1.InjectionRule) []string {
	seen := map[string]struct{}{}
	for i := range rules {
		for j := range rules[i].Headers {
			for _, m := range vaultRefRe.FindAllStringSubmatch(rules[i].Headers[j].Value, -1) {
				seen[m[1]] = struct{}{}
			}
		}
	}
	return sortedNames(seen)
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

// parseE2BNetworkUpdate maps a live network update onto a SandboxNetworkPolicy.
//
// The body is a full replacement per E2B's contract ("Omitting a field clears
// it"), so a nil or empty body legitimately means "unrestricted" — this is the
// one place where saying nothing is a decision rather than an absence.
//
// Transform rules are refused rather than applied: the CA the gateway uses to
// intercept TLS is minted per claim and installed into the sandbox's trust
// store while it is being armed, so a rule added later would have nothing to
// sign with. Saying so beats accepting the field and injecting nothing.
func parseE2BNetworkUpdate(body *e2bgen.SandboxNetworkUpdateConfig) (*agentsv1alpha1.SandboxNetworkPolicy, *e2bgen.Error) {
	np := &agentsv1alpha1.SandboxNetworkPolicy{}
	if body == nil {
		return np, nil
	}

	if body.Rules != nil && len(*body.Rules) > 0 {
		e := errRespCode(400, "network.rules cannot be changed on a running sandbox: the certificate "+
			"authority the gateway uses to intercept TLS is minted when the sandbox is created and "+
			"installed into its trust store then. Pass the rules on create, or create a new sandbox.")
		return nil, &e
	}

	if body.AllowInternetAccess != nil && !*body.AllowInternetAccess {
		np.DisableEgress = true
		return np, nil
	}

	var eg agentsv1alpha1.EgressRules
	if body.AllowOut != nil {
		eg.AllowedDomains, eg.AllowedCIDRs = splitAllowOut(*body.AllowOut)
	}
	if body.DenyOut != nil {
		for _, d := range *body.DenyOut {
			if d = strings.TrimSpace(d); d != "" {
				eg.DeniedCIDRs = append(eg.DeniedCIDRs, normalizeCIDR(d))
			}
		}
	}
	if len(eg.AllowedDomains) > 0 || len(eg.AllowedCIDRs) > 0 || len(eg.DeniedCIDRs) > 0 {
		np.Egress = &eg
	}
	return np, nil
}
