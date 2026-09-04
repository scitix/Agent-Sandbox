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
// into sandbox Pods whose Pool declares a SandboxNetworkPolicy. Normally it is a
// native sidecar and lives in Pod.Spec.InitContainers with an Always restart
// policy; on API servers that prune that field the operator can be told to
// inject it as an ordinary container instead, so both lists have to be searched
// for it.
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
	// Both lists must be checked: on API servers without native-sidecar support
	// the proxy is injected as an ordinary container instead. Scanning only
	// InitContainers would make the claim path treat every such Pod as
	// unfiltered and refuse to hand any of them out.
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == EgressProxyContainerName {
			return true
		}
	}
	return false
}

// GatewaySpec is the whole of a SandboxEnv's network configuration: whether its
// sandboxes get the egress gateway.
//
// It is a single switch on purpose. Enabling the gateway adds a sidecar and an
// iptables redirect to the Pod, which is a Pod-spec change and therefore rolls
// the pool — that is the one decision that genuinely belongs to the environment.
// Everything else (what may be reached, what gets injected into which request)
// is a property of one sandbox, arrives on the create call, and is expressed in
// standard E2B terms: network.allowOut / denyOut, network.rules with
// Secret.fill(), and envVars for whatever placeholder the caller wants its tools
// to see.
//
// The Env used to be able to declare egress rules and injection rules of its
// own. That bought nothing: a create request's network config replaced the
// Env's outright, so the Env's rules were a default rather than a floor, and
// Env-level injection applied the same credentials to every tenant sharing the
// environment. What it cost was a second configuration surface, a merge
// semantics between the two (union for injection, replace for filtering), and a
// dialect users had to learn on top of the E2B SDK.
type GatewaySpec struct {
	// Enabled injects the egress gateway sidecar into every sandbox Pod of this
	// environment. Without it, a create request carrying network rules is
	// refused rather than silently unenforced.
	// +optional
	Enabled bool `json:"enabled,omitempty"`
}

// SandboxNetworkPolicy is the per-sandbox egress configuration carried on a
// create request. It is never stored on a SandboxEnv or SandboxPool.
//
// Enforcement is an in-Pod transparent proxy sidecar (not a Kubernetes
// NetworkPolicy), so it can match domains — which the cluster CNIs in use
// (Calico, Aliyun ENI) cannot. The ruleset is pushed at claim time and reset on
// release. Semantics are allowlist / default-deny.
type SandboxNetworkPolicy struct {
	// DisableEgress blocks all outbound traffic (DNS still resolves so lookups
	// do not hang). Takes precedence over Egress. A quick "no internet" switch.
	// +optional
	DisableEgress bool `json:"disableEgress,omitempty"`

	// Egress is the allowlist. When DisableEgress is false and Egress is nil,
	// egress is unrestricted — including private ranges, see
	// AllowPrivateNetworks.
	// +optional
	Egress *EgressRules `json:"egress,omitempty"`

	// AllowPrivateNetworks opens the private ranges (RFC1918, CGNAT, ULA)
	// wholesale for a request that declares filtering. Default false.
	//
	// Usually unnecessary: naming an internal host or CIDR in Egress already
	// reaches it, because a specific allowlist entry lifts the baseline for the
	// destination it names. This is for "allow everything, the cluster network
	// included" — a wildcard entry deliberately does not imply that.
	//
	// It never opens the cloud-metadata and link-local ranges. Those are denied
	// unconditionally: an unauthenticated GET to 169.254.169.254 (or, on this
	// cloud, 100.100.100.200) hands out instance credentials, and no sandbox
	// workload has a legitimate reason to reach them.
	//
	// Implied when Egress is nil and DisableEgress is false: a request that
	// filters nothing (rules only, say) must not be stricter than one that says
	// nothing at all, where private ranges are reachable.
	// +optional
	AllowPrivateNetworks bool `json:"allowPrivateNetworks,omitempty"`
}

// SecretInjection declares the outbound credential broker: the sidecar
// terminates TLS for the listed hosts and sets the configured headers, so the
// sandbox can use a credential without ever being able to read it.
//
// It is assembled at claim time from one create request's network.rules and the
// caller's own vault. It is never stored on a SandboxEnv or SandboxPool — a
// credential shared by an environment would be a credential shared by every
// tenant of it.
//
// It never carries a credential value: values live in Secrets and are resolved
// by the operator at push time, because this struct is serialised into a Pod
// annotation.
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

// Doc comments in this file must never contain a literal doubled curly brace:
// controller-gen copies them verbatim into the CRD field descriptions, and every
// consumer that renders the resulting manifest as a Go template — Helm, because
// the CRDs ship under a chart's templates/, and delivery platforms that parse a
// service YAML for variables — then fails on the unknown action. The credential
// placeholder syntax below is safe to spell out because it uses ${...}, which
// no template engine in that chain interprets.

// InjectedCredential is one named credential.
type InjectedCredential struct {
	// Name is how rules refer to this credential in a header value template,
	// written as ${e2b.secrets.NAME}. This is the same syntax the E2B SDK's
	// Secret.fill() produces, so a rule authored against the SDK and a rule
	// authored here are byte-identical.
	Name string `json:"name"`

	// ValueFrom points at the Secret key holding the credential — for a vault
	// reference, the caller's own vault Secret. It must live in the sandbox's
	// namespace.
	//
	// A credential a tool has to *see* (because it validates the key's shape, or
	// signs with it locally) is not this feature's business: set an ordinary
	// environment variable on the create call to whatever decoy or real value
	// the tool should read, and let the header rule replace what goes on the
	// wire.
	ValueFrom SecretKeyRef `json:"valueFrom"`
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

	// Value is a template that may reference declared credentials by name,
	// written as ${e2b.secrets.NAME}. For an Authorization header, that is a
	// Bearer prefix followed by the reference to the credential holding the key.
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
