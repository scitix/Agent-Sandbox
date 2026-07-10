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

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/egressproxy"
)

// buildEgressPolicyAnnotation resolves the effective egress policy for a claimed
// sandbox and returns its JSON encoding for the egress-policy annotation.
//
// The effective policy is the per-sandbox override (E2B create body) when
// present, else the Pool's Env-default networkPolicy. It returns ok=false (no
// annotation) when the Pool has no policy — unless a per-sandbox override was
// supplied for such a Pool, which is rejected (there is no filter sidecar to
// enforce it, so silently dropping would fail open).
func buildEgressPolicyAnnotation(override *agentsv1alpha1.SandboxNetworkPolicy, pool *agentsv1alpha1.SandboxPool, sandboxID string) (value string, ok bool, err *domain.AppError) {
	poolPolicy := (*agentsv1alpha1.SandboxNetworkPolicy)(nil)
	if pool != nil {
		poolPolicy = pool.Spec.NetworkPolicy
	}
	if poolPolicy == nil {
		if override != nil {
			return "", false, domain.NewBadRequest(
				"network policy was supplied but this pool/env does not have egress filtering enabled " +
					"(set overrides.networkPolicy on the SandboxEnv)")
		}
		return "", false, nil
	}

	src := override
	if src == nil {
		src = poolPolicy
	}
	policy := toProxyPolicy(src, sandboxID)
	data, mErr := json.Marshal(policy)
	if mErr != nil {
		return "", false, domain.NewInternal("failed to encode egress policy", mErr)
	}
	return string(data), true, nil
}

// toProxyPolicy converts the CRD SandboxNetworkPolicy to the on-disk
// egressproxy.Policy the sidecar enforces.
//
// The proxy's default action is deny (allowlist), so the CRD's "Egress==nil
// means unrestricted (except the anti-SSRF baseline)" case is represented with
// an allow-all domain + CIDR. The anti-SSRF baseline is checked before the allow
// rules in Evaluate, so private ranges stay denied unless AllowPrivateNetworks.
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
		// Unrestricted (subject to the anti-SSRF baseline).
		p.AllowedDomains = []string{"*"}
		p.AllowedCIDRs = []string{"0.0.0.0/0"}
		return p
	}
	p.AllowedDomains = np.Egress.AllowedDomains
	p.AllowedCIDRs = np.Egress.AllowedCIDRs
	p.DeniedCIDRs = np.Egress.DeniedCIDRs
	return p
}
