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
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Secrets is the injection config the control plane pushes over the exec
// channel. Unlike Policy it carries live credential material, so it is written
// only to the sidecar's own tmpfs (mode 0600) and is never persisted in a Pod
// annotation, an env var, or a log line.
//
// Header values arrive already resolved: the operator expands the CRD's
// "{{ cred }}" templates before pushing, so credential *names* never reach the
// sidecar and the data plane needs no template engine.
type Secrets struct {
	// SandboxID records which sandbox this config was pushed for. Informational.
	SandboxID string `json:"sandboxId,omitempty"`

	// CACertPEM / CAKeyPEM are the per-sandbox CA used to mint leaf
	// certificates for the hosts being intercepted. Absent means TLS
	// interception is impossible and only plaintext :80 rules can apply.
	CACertPEM string `json:"caCertPem,omitempty"`
	CAKeyPEM  string `json:"caKeyPem,omitempty"`

	// Rules is the per-host injection table.
	Rules []InjectRule `json:"rules,omitempty"`

	// Substitutions maps a placeholder handed to the sandbox to the real
	// credential. A placeholder is only ever swapped on a host whose rule
	// lists it, so the real value cannot be steered to another destination.
	Substitutions map[string]string `json:"substitutions,omitempty"`
}

// InjectRule is the per-host injection action.
type InjectRule struct {
	// Host is an exact hostname, lowercase. Wildcards are rejected upstream.
	Host string `json:"host"`

	// Ports the rule covers. Empty means the defaults, 80 and 443.
	Ports []int `json:"ports,omitempty"`

	// Headers to inject.
	Headers []InjectHeader `json:"headers,omitempty"`

	// SubstitutePlaceholders lists the placeholders that may be swapped for
	// their real value on this host.
	SubstitutePlaceholders []string `json:"substitutePlaceholders,omitempty"`

	// PathPrefixes / Methods narrow the rule. Empty means no narrowing.
	PathPrefixes []string `json:"pathPrefixes,omitempty"`
	Methods      []string `json:"methods,omitempty"`
}

// InjectHeader is one header to write onto a matching request.
type InjectHeader struct {
	Name string `json:"name"`
	// Value is the final header value; templates were expanded by the operator.
	Value string `json:"value"`
	// Mode is "Override" (default) or "IfAbsent".
	Mode string `json:"mode,omitempty"`
}

// Injection mode strings, mirroring the CRD enum.
const (
	ModeOverride = "Override"
	ModeIfAbsent = "IfAbsent"
)

var defaultInjectPorts = []int{80, 443}

// MatchAll returns every rule covering host:port, in declaration order.
//
// A host may carry several rules (mirroring E2B's host -> rule-array wire
// shape), and all of them apply: stopping at the first match would silently
// drop the rest. Host comparison is exact and case-insensitive — a wildcard
// would hand the credential to whoever controls a matching subdomain, so the
// API layer refuses to write one and the data plane never interprets one.
func (s *Secrets) MatchAll(host string, port int) []*InjectRule {
	if s == nil || host == "" {
		return nil
	}
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	var out []*InjectRule
	for i := range s.Rules {
		r := &s.Rules[i]
		if !strings.EqualFold(r.Host, h) {
			continue
		}
		ports := r.Ports
		if len(ports) == 0 {
			ports = defaultInjectPorts
		}
		if slices.Contains(ports, port) {
			out = append(out, r)
		}
	}
	return out
}

// Intercepts reports whether host:port has any injection rule, i.e. whether the
// connection must take the TLS-terminating path instead of the plain splice.
func (s *Secrets) Intercepts(host string, port int) bool {
	return len(s.MatchAll(host, port)) > 0
}

// Enabled reports whether any rule is configured.
func (s *Secrets) Enabled() bool { return s != nil && len(s.Rules) > 0 }

// coversRequest reports whether the rule's path/method narrowing admits this
// request.
func (r *InjectRule) coversRequest(method, path string) bool {
	if len(r.Methods) > 0 {
		ok := false
		for _, m := range r.Methods {
			if strings.EqualFold(m, method) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if len(r.PathPrefixes) > 0 {
		ok := false
		for _, p := range r.PathPrefixes {
			if strings.HasPrefix(path, p) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// ApplyOutcome reports what a single Apply did, for metrics. It deliberately
// carries no header values or credential material.
type ApplyOutcome struct {
	Skipped      bool // rule matched the host but not this request's path/method
	HeadersSet   int
	Substituted  int
	SubstitutedK []string // placeholder-bearing header names, for debug logging
}

// Apply rewrites req's headers according to the rule: placeholders are
// substituted first, then declared headers are injected. The order matters and
// is deliberate — an Override header wins over whatever substitution produced,
// while IfAbsent leaves it alone, which is what makes "placeholder first,
// header as fallback" expressible.
//
// Substitution only touches header *values*, never the body or the query
// string: scanning a body would mean buffering the whole request, and a
// credential in a query string ends up in the upstream's access log.
func (s *Secrets) Apply(req *http.Request, rules []*InjectRule) ApplyOutcome {
	var out ApplyOutcome
	if len(rules) == 0 {
		return out
	}
	applied := 0
	for _, r := range rules {
		if !r.coversRequest(req.Method, req.URL.Path) {
			continue
		}
		applied++
		s.applyRule(req, r, &out)
	}
	if applied == 0 {
		out.Skipped = true
	}
	return out
}

// applyRule performs one rule's substitutions and header writes.
func (s *Secrets) applyRule(req *http.Request, r *InjectRule, out *ApplyOutcome) {
	if len(r.SubstitutePlaceholders) > 0 && len(s.Substitutions) > 0 {
		for name, values := range req.Header {
			for i, v := range values {
				nv := v
				for _, ph := range r.SubstitutePlaceholders {
					real, ok := s.Substitutions[ph]
					if !ok || ph == "" || !strings.Contains(nv, ph) {
						continue
					}
					nv = strings.ReplaceAll(nv, ph, real)
					out.Substituted++
				}
				if nv != v {
					values[i] = nv
					out.SubstitutedK = append(out.SubstitutedK, name)
				}
			}
		}
	}

	for i := range r.Headers {
		h := &r.Headers[i]
		if h.Name == "" {
			continue
		}
		if strings.EqualFold(h.Mode, ModeIfAbsent) && req.Header.Get(h.Name) != "" {
			continue
		}
		req.Header.Set(h.Name, h.Value)
		out.HeadersSet++
	}
}

// InterceptHosts returns the lowercase hosts that require TLS termination —
// used to decide, from the SNI alone, whether a connection takes the MITM path
// or stays a byte-for-byte splice.
func (s *Secrets) InterceptHosts() []string {
	if s == nil {
		return nil
	}
	hosts := make([]string, 0, len(s.Rules))
	for i := range s.Rules {
		hosts = append(hosts, strings.ToLower(s.Rules[i].Host))
	}
	return hosts
}

// LoadSecrets reads and parses the secrets file. A missing or empty file yields
// an empty config with no error: that is the steady state before the first push
// and simply means "inject nothing", which is the safe default — the sandbox's
// request then goes out without a credential and the upstream rejects it.
func LoadSecrets(path string) (Secrets, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Secrets{}, nil
		}
		return Secrets{}, fmt.Errorf("read secrets %s: %w", path, err)
	}
	if len(data) == 0 {
		return Secrets{}, nil
	}
	var s Secrets
	if err := json.Unmarshal(data, &s); err != nil {
		return Secrets{}, fmt.Errorf("parse secrets %s: %w", path, err)
	}
	return s, nil
}

// WriteSecrets atomically writes the injection config with owner-only
// permissions. Same temp+rename discipline as WritePolicy so a concurrent
// reader never sees a half-written file.
func WriteSecrets(path string, s Secrets) error {
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal secrets: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".secrets-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", tmpName, path, err)
	}
	return nil
}

// RemoveSecrets deletes the secrets file. Called on release so a recycled pod
// cannot carry the previous sandbox's credentials, CA key, or placeholder map
// into the next claim. A missing file is success.
func RemoveSecrets(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove secrets %s: %w", path, err)
	}
	return nil
}
