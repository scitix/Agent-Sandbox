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

// SandboxNetworkPolicy configures sandbox egress control. It is enforced by an
// in-Pod transparent proxy sidecar (not a Kubernetes NetworkPolicy), so it can
// match domains — which the cluster CNIs in use (Calico, Aliyun ENI) cannot.
//
// A non-nil SandboxNetworkPolicy opts a SandboxEnv (and its member Pools) into
// egress filtering: the operator injects the filter sidecar into every sandbox
// Pod. The effective per-sandbox ruleset is pushed at claim time and reset on
// release/restart. Semantics are allowlist / default-deny.
type SandboxNetworkPolicy struct {
	// DisableEgress blocks all outbound traffic (DNS still resolves so lookups
	// do not hang). Takes precedence over Egress. A quick "no internet" switch.
	// +optional
	DisableEgress bool `json:"disableEgress,omitempty"`

	// Egress is the allowlist. When DisableEgress is false and Egress is nil,
	// egress is unrestricted except for the anti-SSRF baseline below.
	// +optional
	Egress *EgressRules `json:"egress,omitempty"`

	// AllowPrivateNetworks disables the default deny of private / link-local /
	// cloud-metadata ranges (RFC1918, 169.254.0.0/16, etc.). Default false — the
	// anti-SSRF baseline stays on. Enable only for trusted intra-cluster access.
	// +optional
	AllowPrivateNetworks bool `json:"allowPrivateNetworks,omitempty"`
}

// EgressRules is the allow/deny ruleset applied to sandbox outbound traffic.
type EgressRules struct {
	// AllowedDomains permits egress to matching hostnames. Supports exact
	// ("pypi.org"), wildcard-all ("*"), and suffix ("*.pythonhosted.org").
	// Matched via TLS SNI (443) / HTTP Host (80); other ports are IP-only.
	// +optional
	AllowedDomains []string `json:"allowedDomains,omitempty"`

	// AllowedCIDRs permits egress to these CIDR blocks / bare IPs (as /32).
	// +optional
	AllowedCIDRs []string `json:"allowedCIDRs,omitempty"`

	// DeniedCIDRs blocks egress to these CIDR blocks / bare IPs. Domains are not
	// supported for deny (a domain resolves to many changing IPs).
	// +optional
	DeniedCIDRs []string `json:"deniedCIDRs,omitempty"`
}
