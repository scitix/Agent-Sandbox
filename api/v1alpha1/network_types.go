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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EgressProxyContainerName is the egress filter sidecar the operator injects
// into sandbox Pods whose Pool declares a SandboxNetworkPolicy. It runs as a
// native sidecar, so it lives in Pod.Spec.InitContainers with an Always restart
// policy.
const EgressProxyContainerName = "egress-proxy"

// PodHasEgressProxy reports whether pod carries the egress filter sidecar.
//
// A Pod materialised before its Pool gained a SandboxNetworkPolicy does not
// have it — and since the same injection also installs the iptables redirect,
// such a Pod has *no* egress enforcement whatsoever. Handing one to a claim
// that expects enforcement would be fail-open (the policy annotation would be
// stamped, the API would report success, and traffic would leave unfiltered),
// so the scheduler refuses those Pods and waits for a rolled one instead.
func PodHasEgressProxy(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	for i := range pod.Spec.InitContainers {
		if pod.Spec.InitContainers[i].Name == EgressProxyContainerName {
			return true
		}
	}
	return false
}

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

	// SecretInjection brokers credentials on the way out: the sidecar terminates
	// TLS for the listed hosts and adds (or substitutes) the configured headers,
	// so the sandbox can use a credential without ever being able to read it.
	// nil disables it.
	//
	// Setting this alone (with Egress nil) is valid and means "inject, but do
	// not filter": the sidecar is still injected — which is what makes the
	// interception possible — while egress stays unrestricted apart from the
	// anti-SSRF baseline.
	//
	// Declarable only on a SandboxEnv. Sandbox-create requests carrying it are
	// rejected, because a credential travelling through a create request would
	// re-introduce exactly the exposure this feature removes.
	// +optional
	SecretInjection *SecretInjection `json:"secretInjection,omitempty"`
}

// SecretInjection declares the outbound credential broker. It never carries a
// credential value: values live in Secrets and are resolved by the operator at
// push time, because this struct is serialised into a Pod annotation and into
// API responses.
type SecretInjection struct {
	// Credentials are the named secrets rules may reference.
	// +optional
	Credentials []InjectedCredential `json:"credentials,omitempty"`

	// Rules declare what to inject, per host.
	// +optional
	Rules []InjectionRule `json:"rules,omitempty"`

	// CACertTTL bounds the lifetime of the per-sandbox CA minted for TLS
	// interception. Defaults to 24h.
	// +optional
	CACertTTL *metav1.Duration `json:"caCertTTL,omitempty"`
}

// InjectedCredential is one named credential.
type InjectedCredential struct {
	// Name is how rules refer to this credential in a value template
	// ("{{ name }}").
	Name string `json:"name"`

	// ValueFrom points at the Secret key holding the credential. The Secret
	// must live in the SandboxEnv's namespace.
	ValueFrom SecretKeyRef `json:"valueFrom"`

	// ExposeAs turns on placeholder mode: the sandbox gets an environment
	// variable of this name whose value is a decoy (see Placeholder), and the
	// proxy swaps the decoy for the real value on hosts that allow it. Leave
	// empty to use the credential through header injection only.
	// +optional
	ExposeAs string `json:"exposeAs,omitempty"`

	// Placeholder is the decoy value handed to the sandbox. Empty means a fresh
	// random "agbx_ph_<32 hex>" per claim.
	//
	// Set it when a client validates credential shape before sending — several
	// SDKs reject a key that lacks the expected prefix or length, so a random
	// decoy would fail inside the sandbox and never reach the proxy. A fixed
	// decoy costs nothing in secrecy (it already lives in the sandbox's
	// environment); it is merely identical across sandboxes.
	//
	// Must be at least 16 characters, and no two credentials may share a
	// placeholder or have one be a substring of another — overlapping decoys
	// would substitute into each other.
	// +optional
	Placeholder string `json:"placeholder,omitempty"`
}

// SecretKeyRef selects one key of one Secret.
type SecretKeyRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// InjectionRule declares the injection applied to one host.
type InjectionRule struct {
	// Host must be an exact hostname. Wildcards are rejected: anyone able to
	// control a matching subdomain would receive the credential.
	Host string `json:"host"`

	// Ports narrows which destination ports the rule covers. Defaults to
	// [80, 443]; other ports get no L7 handling at all.
	// +optional
	Ports []int32 `json:"ports,omitempty"`

	// Headers are the headers to inject.
	// +optional
	Headers []HeaderInjection `json:"headers,omitempty"`

	// Substitute lists credentials whose placeholder may be swapped for the
	// real value on this host.
	// +optional
	Substitute []string `json:"substitute,omitempty"`

	// PathPrefixes narrows the rule to matching request paths. Empty means all.
	// +optional
	PathPrefixes []string `json:"pathPrefixes,omitempty"`

	// Methods narrows the rule to these HTTP methods. Empty means all.
	// +optional
	Methods []string `json:"methods,omitempty"`
}

// HeaderInjectionMode selects how an injected header interacts with one the
// sandbox already set.
// +kubebuilder:validation:Enum=Override;IfAbsent
type HeaderInjectionMode string

const (
	// HeaderInjectionOverride replaces whatever the sandbox sent. The default.
	HeaderInjectionOverride HeaderInjectionMode = "Override"
	// HeaderInjectionIfAbsent injects only when the sandbox sent no such header,
	// so an agent that supplies its own credential keeps it.
	HeaderInjectionIfAbsent HeaderInjectionMode = "IfAbsent"
)

// HeaderInjection is one header to add to matching requests.
type HeaderInjection struct {
	// Name is the header name, compared case-insensitively.
	Name string `json:"name"`

	// Value is a template that may reference declared credentials as
	// "{{ credName }}", e.g. "Bearer {{ openai }}".
	Value string `json:"value"`

	// Mode defaults to Override.
	// +optional
	Mode HeaderInjectionMode `json:"mode,omitempty"`
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
