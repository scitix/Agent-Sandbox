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

package v1alpha1

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	credNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,62}$`)
	hostnameRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)
	// templateRe matches a credential reference in a header value. The syntax is
	// the E2B one — ${e2b.secrets.<name>} — so a rule written against the E2B
	// SDK (Secret.fill) is stored verbatim, with no rewriting at the API edge.
	templateRe  = regexp.MustCompile(`\$\{e2b\.secrets\.([a-zA-Z0-9_-]+)\}`)
	headerNamRe = regexp.MustCompile(`^[A-Za-z0-9!#$%&'*+\-.^_` + "`" + `|~]+$`)
)

// ValidateSecretInjection checks the credential-injection block resolved for a
// claim against the egress policy resolved for the same claim, for the mistakes
// that would otherwise fail silently or leak a credential.
//
// Both arguments come from one create request, so the two can genuinely
// contradict each other: filtering that excludes a host the rules name leaves
// the rule configured and dead. Either may be nil.
func ValidateSecretInjection(np *SandboxNetworkPolicy, si *SecretInjection) error {
	if si == nil {
		return nil
	}

	if np != nil && np.DisableEgress {
		return fmt.Errorf("credential injection cannot be combined with disabled egress: " +
			"nothing can reach the hosts the rules name")
	}

	creds, err := validateCredentials(si.Credentials)
	if err != nil {
		return err
	}
	if len(si.Rules) == 0 {
		return fmt.Errorf("credential injection declares no rules; nothing would be injected")
	}
	referenced, err := validateRules(si.Rules, creds)
	if err != nil {
		return err
	}
	for name := range creds {
		if !referenced[name] {
			return fmt.Errorf("credential %q is declared but never used by a rule", name)
		}
	}

	// A host that is filtered out never reaches the proxy's L7 path, so the
	// rule would look configured and do nothing.
	if np != nil && np.Egress != nil {
		for i := range si.Rules {
			if !domainAllowed(si.Rules[i].Host, np.Egress.AllowedDomains) {
				return fmt.Errorf("rule host %q is not permitted by the request's allowed domains; "+
					"add it there or the traffic is blocked before injection can run", si.Rules[i].Host)
			}
		}
	}
	return nil
}

// validateCredentials checks each declaration and returns them keyed by name.
func validateCredentials(in []InjectedCredential) (map[string]*InjectedCredential, error) {
	creds := make(map[string]*InjectedCredential, len(in))
	for i := range in {
		c := &in[i]
		if !credNameRe.MatchString(c.Name) {
			return nil, fmt.Errorf("credential name %q must be alphanumeric with - or _", c.Name)
		}
		if _, dup := creds[c.Name]; dup {
			return nil, fmt.Errorf("credential %q is declared twice", c.Name)
		}
		if c.ValueFrom.Name == "" || c.ValueFrom.Key == "" {
			return nil, fmt.Errorf("credential %q needs valueFrom.name and valueFrom.key", c.Name)
		}
		creds[c.Name] = c
	}
	return creds, nil
}

// validateRules checks every rule and returns the set of credentials they use.
func validateRules(rules []InjectionRule, creds map[string]*InjectedCredential) (map[string]bool, error) {
	referenced := make(map[string]bool, len(creds))
	for i := range rules {
		r := &rules[i]
		if err := validateRuleHost(r); err != nil {
			return nil, err
		}
		if len(r.Headers) == 0 {
			return nil, fmt.Errorf("rule %q declares no headers; it would do nothing", r.Host)
		}
		if err := validateRuleHeaders(r, creds, referenced); err != nil {
			return nil, err
		}
	}
	return referenced, nil
}

func validateRuleHost(r *InjectionRule) error {
	if strings.ContainsAny(r.Host, "*?") {
		return fmt.Errorf("rule host %q: wildcards are not allowed for injection — "+
			"anyone controlling a matching subdomain would receive the credential", r.Host)
	}
	if !hostnameRe.MatchString(r.Host) {
		return fmt.Errorf("rule host %q is not a valid hostname", r.Host)
	}
	for _, p := range r.Ports {
		if p < 1 || p > 65535 {
			return fmt.Errorf("rule %q: port %d out of range", r.Host, p)
		}
	}
	return nil
}

func validateRuleHeaders(r *InjectionRule, creds map[string]*InjectedCredential, referenced map[string]bool) error {
	for j := range r.Headers {
		h := &r.Headers[j]
		if !headerNamRe.MatchString(h.Name) {
			return fmt.Errorf("rule %q: %q is not a valid header name", r.Host, h.Name)
		}
		switch h.Mode {
		case "", HeaderInjectionOverride, HeaderInjectionIfAbsent:
		default:
			return fmt.Errorf("rule %q header %q: unknown mode %q", r.Host, h.Name, h.Mode)
		}
		names := templateRe.FindAllStringSubmatch(h.Value, -1)
		if len(names) == 0 {
			return fmt.Errorf("rule %q header %q: value references no credential — use "+
				"${e2b.secrets.<name>}; a literal value would put the credential in the request body",
				r.Host, h.Name)
		}
		for _, m := range names {
			if _, ok := creds[m[1]]; !ok {
				return fmt.Errorf("rule %q header %q references undeclared credential %q", r.Host, h.Name, m[1])
			}
			referenced[m[1]] = true
		}
	}
	return nil
}

// domainAllowed reports whether host is covered by an allowlist entry, using
// the same exact / "*" / "*.suffix" semantics the proxy enforces.
func domainAllowed(host string, allowed []string) bool {
	h := strings.ToLower(host)
	for _, a := range allowed {
		a = strings.ToLower(strings.TrimSpace(a))
		switch {
		case a == "*":
			return true
		case strings.HasPrefix(a, "*."):
			suffix := a[1:] // ".example.com"
			if strings.HasSuffix(h, suffix) || h == a[2:] {
				return true
			}
		case a == h:
			return true
		}
	}
	return false
}
