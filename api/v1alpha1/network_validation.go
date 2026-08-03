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

// PlaceholderPrefix marks a generated decoy value. Fixed placeholders supplied
// by the user need not carry it.
const PlaceholderPrefix = "agbx_ph_"

// MinPlaceholderLen is the shortest accepted decoy. Short decoys risk colliding
// with ordinary header content, which would substitute a real credential into
// a request that never asked for one.
const MinPlaceholderLen = 16

var (
	credNameRe  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,62}$`)
	envNameRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	hostnameRe  = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)
	templateRe  = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_-]+)\s*\}\}`)
	headerNamRe = regexp.MustCompile(`^[A-Za-z0-9!#$%&'*+\-.^_` + "`" + `|~]+$`)
)

// ValidateSecretInjection checks a SandboxNetworkPolicy's injection block for
// the mistakes that would otherwise fail silently or leak a credential. It is
// called both when a SandboxEnv is written (so the author sees the error) and
// at claim time (so a hand-edited CRD cannot slip through).
//
// np may be nil or carry no injection block, in which case there is nothing to
// check.
func ValidateSecretInjection(np *SandboxNetworkPolicy) error {
	if np == nil || np.SecretInjection == nil {
		return nil
	}
	si := np.SecretInjection

	if np.DisableEgress {
		return fmt.Errorf("secretInjection cannot be combined with disableEgress: " +
			"nothing can reach the hosts the rules name")
	}

	creds, err := validateCredentials(si.Credentials)
	if err != nil {
		return err
	}
	if err := validatePlaceholderOverlap(si.Credentials); err != nil {
		return err
	}
	if len(si.Rules) == 0 {
		return fmt.Errorf("secretInjection declares no rules; nothing would be injected")
	}
	referenced, err := validateRules(si.Rules, creds)
	if err != nil {
		return err
	}
	for name, c := range creds {
		if !referenced[name] && c.ExposeAs == "" {
			return fmt.Errorf("credential %q is declared but never used by a rule and not exposed", name)
		}
	}

	// A host that is filtered out never reaches the proxy's L7 path, so the
	// rule would look configured and do nothing.
	if np.Egress != nil {
		for i := range si.Rules {
			if !domainAllowed(si.Rules[i].Host, np.Egress.AllowedDomains) {
				return fmt.Errorf("rule host %q is not permitted by egress.allowedDomains; "+
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
		if c.ExposeAs != "" && !envNameRe.MatchString(c.ExposeAs) {
			return nil, fmt.Errorf("credential %q: exposeAs %q is not a valid environment variable name", c.Name, c.ExposeAs)
		}
		if c.Placeholder != "" {
			if len(c.Placeholder) < MinPlaceholderLen {
				return nil, fmt.Errorf("credential %q: placeholder must be at least %d characters so it cannot collide with ordinary header content",
					c.Name, MinPlaceholderLen)
			}
			if c.ExposeAs == "" {
				return nil, fmt.Errorf("credential %q sets a placeholder but no exposeAs, so the sandbox would never receive it", c.Name)
			}
		}
		creds[c.Name] = c
	}
	return creds, nil
}

// validatePlaceholderOverlap rejects decoys that contain one another. An
// overlap means a request meant to carry credential A could leave with B's
// value spliced into it.
func validatePlaceholderOverlap(creds []InjectedCredential) error {
	for i := range creds {
		for j := range creds {
			if i == j {
				continue
			}
			a, b := creds[i].Placeholder, creds[j].Placeholder
			if a == "" || b == "" {
				continue
			}
			if strings.Contains(a, b) {
				return fmt.Errorf("credential %q's placeholder contains credential %q's; overlapping placeholders substitute into each other",
					creds[i].Name, creds[j].Name)
			}
		}
	}
	return nil
}

// validateRules checks every rule and returns the set of credentials they use.
func validateRules(rules []InjectionRule, creds map[string]*InjectedCredential) (map[string]bool, error) {
	referenced := make(map[string]bool, len(creds))
	for i := range rules {
		r := &rules[i]
		if err := validateRuleHost(r); err != nil {
			return nil, err
		}
		if len(r.Headers) == 0 && len(r.Substitute) == 0 {
			return nil, fmt.Errorf("rule %q declares neither headers nor substitute; it would do nothing", r.Host)
		}
		if err := validateRuleHeaders(r, creds, referenced); err != nil {
			return nil, err
		}
		for _, name := range r.Substitute {
			c, ok := creds[name]
			if !ok {
				return nil, fmt.Errorf("rule %q substitutes undeclared credential %q", r.Host, name)
			}
			if c.ExposeAs == "" {
				return nil, fmt.Errorf("rule %q substitutes credential %q, but it has no exposeAs, "+
					"so the sandbox never receives a placeholder to substitute", r.Host, name)
			}
			referenced[name] = true
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
			return fmt.Errorf("rule %q header %q: value references no credential; "+
				"a literal value here would be a plaintext secret in the CRD", r.Host, h.Name)
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
