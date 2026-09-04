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

package service

import (
	"encoding/json"
	"fmt"
	"sort"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/egressproxy"
)

// gatewayEnabled reports whether pool's sandboxes carry the egress proxy
// sidecar. Everything egress-related is gated on it: without the sidecar there
// is nothing to enforce a policy or inject a credential, and a Pod that never
// got one also has no iptables redirect, so accepting the request anyway would
// fail open.
func gatewayEnabled(pool *agentsv1alpha1.SandboxPool) bool {
	return pool != nil && pool.Spec.Gateway != nil && pool.Spec.Gateway.Enabled
}

// errGatewayDisabled is the single refusal both egress paths return, so a
// caller who forgot to enable the gateway gets the same sentence whether they
// asked for filtering or for injection.
func errGatewayDisabled(what string) *domain.AppError {
	return domain.NewBadRequest(fmt.Sprintf(
		"%s was requested but this environment has no egress gateway; "+
			"enable it on the SandboxEnv (overrides.gateway.enabled) and let its pools roll", what))
}

// buildEgressPolicyAnnotation encodes the egress policy a create request asked
// for into the egress-policy annotation.
//
// The policy is per-sandbox and only per-sandbox: an environment decides
// whether the gateway exists, never what it permits.
//
// A gateway-enabled pool always gets an annotation, even when the request asked
// for no filtering at all. The sidecar fails closed on a missing policy file —
// that is what keeps an unclaimed or half-armed Pod from reaching the network —
// so leaving it unstamped would silently cut off every sandbox in an
// environment that turned the gateway on and then created a sandbox without a
// network config, which is the common case.
func buildEgressPolicyAnnotation(np *agentsv1alpha1.SandboxNetworkPolicy, pool *agentsv1alpha1.SandboxPool, sandboxID string) (value string, ok bool, err *domain.AppError) {
	if !gatewayEnabled(pool) {
		if np != nil {
			return "", false, errGatewayDisabled("a network policy")
		}
		// No sidecar, nothing asked for: no annotation, and nothing to enforce.
		return "", false, nil
	}
	if np == nil {
		// Unrestricted, private ranges included — exactly what the sandbox would
		// reach with no gateway at all. Asking for the gateway must not narrow
		// egress on its own; that is a decision each create request makes.
		np = &agentsv1alpha1.SandboxNetworkPolicy{}
	}

	return toProxyPolicyJSON(np, sandboxID)
}

// toProxyPolicy converts the request's SandboxNetworkPolicy to the on-disk
// egressproxy.Policy the sidecar enforces.
//
// The proxy's default action is deny (allowlist), so the "Egress==nil means
// unrestricted" case is represented with an allow-all domain + CIDR.
//
// Egress==nil also implies AllowPrivateNetworks: a request that declares no
// filtering must not end up more restricted than one that says nothing at all,
// where private ranges are reachable. Otherwise a request that only wanted a
// credential injected would silently lose every host that resolves inside the
// cluster, including split-horizon public names.
//
// Where filtering *is* declared, the proxy keeps the baseline but lifts it for
// any destination the allowlist names specifically — so "filter, and also reach
// this one internal service" needs no extra flag, while
// Egress{AllowedDomains: ["*"], AllowedCIDRs: ["0.0.0.0/0"]} still means the
// internet rather than the cluster network.
func toProxyPolicy(np *agentsv1alpha1.SandboxNetworkPolicy, sandboxID string) egressproxy.Policy {
	p := egressproxy.Policy{
		SandboxID:            sandboxID,
		Enforce:              true,
		AllowPrivateNetworks: np.AllowPrivateNetworks,
	}
	if np.DisableEgress {
		p.DisableEgress = true
		return p
	}
	if np.Egress == nil {
		// Unrestricted: allow-all, baseline included.
		p.AllowedDomains = []string{"*"}
		p.AllowedCIDRs = []string{"0.0.0.0/0"}
		p.AllowPrivateNetworks = true
		return p
	}
	p.AllowedDomains = np.Egress.AllowedDomains
	p.AllowedCIDRs = np.Egress.AllowedCIDRs
	p.DeniedCIDRs = np.Egress.DeniedCIDRs
	return p
}

// perSandboxInjection is what one create request asked to have injected: the
// rules as written (header values still holding ${e2b.secrets.<name>}) and the
// Secret reference each referenced name resolved to in the caller's own vault.
type perSandboxInjection struct {
	rules []agentsv1alpha1.InjectionRule
	refs  map[string]agentsv1alpha1.SecretKeyRef
}

// buildEgressInjectAnnotation encodes the credential-injection block for a
// claimed sandbox into the egress-inject annotation.
//
// The returned value deliberately carries **no credential material**: it holds
// rule shapes, credential names and Secret references. The operator resolves
// those references at push time and sends the plaintext straight to the sidecar
// over the exec channel, so a credential never reaches etcd, an annotation, or
// an API response.
//
// Returns ok=false when the request declared no rules.
func buildEgressInjectAnnotation(
	np *agentsv1alpha1.SandboxNetworkPolicy,
	pool *agentsv1alpha1.SandboxPool,
	perSandbox *perSandboxInjection,
) (value string, ok bool, err *domain.AppError) {
	if perSandbox == nil || len(perSandbox.rules) == 0 {
		return "", false, nil
	}
	if !gatewayEnabled(pool) {
		return "", false, errGatewayDisabled("credential injection")
	}

	// Names are visited in sorted order so the annotation is byte-stable across
	// identical requests, which is what makes "did this change?" comparisons
	// meaningful further down.
	names := make([]string, 0, len(perSandbox.refs))
	for name := range perSandbox.refs {
		names = append(names, name)
	}
	sort.Strings(names)

	si := &agentsv1alpha1.SecretInjection{Rules: perSandbox.rules}
	for _, name := range names {
		si.Credentials = append(si.Credentials, agentsv1alpha1.InjectedCredential{
			Name:      name,
			ValueFrom: perSandbox.refs[name],
		})
	}

	if vErr := agentsv1alpha1.ValidateSecretInjection(np, si); vErr != nil {
		return "", false, domain.NewBadRequest(fmt.Sprintf("invalid network.rules: %v", vErr))
	}

	data, mErr := json.Marshal(si)
	if mErr != nil {
		return "", false, domain.NewInternal("failed to encode secret injection", mErr)
	}
	return string(data), true, nil
}
