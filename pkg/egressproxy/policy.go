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

// Package egressproxy implements the transparent sandbox egress filter: a
// userspace proxy that all sandbox TCP is iptables-REDIRECT'd through, plus the
// redirect installer and the on-disk policy contract the control plane writes.
//
// The enforcement model mirrors e2b-dev/infra's tcpfirewall, adapted for a
// Kubernetes Pod (init container installs the redirect, a sidecar runs the
// proxy) and hardened for the eval anti-cheat use case: the default action is
// deny (allowlist), not allow.
package egressproxy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Policy is the on-disk egress ruleset the control plane pushes to the proxy.
// It is the single source of truth read by the sidecar; an absent, empty, or
// unparseable file means fail-closed (deny all egress except DNS).
type Policy struct {
	// SandboxID records which sandbox this policy was pushed for. Informational
	// (surfaced in logs/metrics); the proxy enforces whatever is present.
	SandboxID string `json:"sandboxId,omitempty"`

	// Enforce gates the whole filter. When false the policy is treated as
	// fail-closed (deny all). The control plane sets it true only after a
	// concrete ruleset has been resolved for a claimed sandbox.
	Enforce bool `json:"enforce"`

	// DisableEgress blocks all outbound traffic regardless of the allow lists
	// (DNS is still permitted so name resolution does not hang). Equivalent to
	// an empty allowlist.
	DisableEgress bool `json:"disableEgress,omitempty"`

	// AllowedDomains permits egress to matching hostnames (exact, "*", or
	// "*.example.com"). Domains only ever allow; there is no domain deny.
	AllowedDomains []string `json:"allowedDomains,omitempty"`

	// AllowedCIDRs / DeniedCIDRs are IP/CIDR allow/deny for traffic without a
	// usable hostname (non-80/443 TCP, or bare-IP connections).
	AllowedCIDRs []string `json:"allowedCIDRs,omitempty"`
	DeniedCIDRs  []string `json:"deniedCIDRs,omitempty"`

	// AllowPrivateNetworks disables the built-in anti-SSRF deny of private /
	// link-local / cloud-metadata ranges. Default false (baseline stays on).
	AllowPrivateNetworks bool `json:"allowPrivateNetworks,omitempty"`
}

// FailClosed is the policy applied when no valid policy file is present: deny
// everything. DNS egress is permitted separately by the iptables rules, not by
// this policy, so resolution keeps working even while fail-closed.
func FailClosed() Policy {
	return Policy{Enforce: true, DisableEgress: true}
}

// LoadPolicy reads and parses the policy file. A missing file yields FailClosed
// with no error (that is the expected steady state before the first push). A
// present-but-corrupt file yields FailClosed WITH an error so the caller can
// log it — but enforcement still fails closed.
func LoadPolicy(path string) (Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return FailClosed(), nil
		}
		return FailClosed(), fmt.Errorf("read policy %s: %w", path, err)
	}
	if len(data) == 0 {
		return FailClosed(), nil
	}
	var p Policy
	if err := json.Unmarshal(data, &p); err != nil {
		return FailClosed(), fmt.Errorf("parse policy %s: %w", path, err)
	}
	return p, nil
}

// WritePolicy atomically writes the policy to path (write temp + rename) so a
// reader (fsnotify reload) never observes a half-written file.
func WritePolicy(path string, p Policy) error {
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal policy: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".policy-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename
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
