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
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"

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

	// Credential injection is an Env-level declaration only. Accepting it from a
	// create request would put a credential (or a reference resolved with the
	// caller's authority) into a request body, request log, and SDK call site —
	// precisely the exposure the feature exists to remove.
	if override != nil && override.SecretInjection != nil {
		return "", false, domain.NewBadRequest(
			"secretInjection cannot be set per sandbox; declare it on the SandboxEnv " +
				"(overrides.networkPolicy.secretInjection)")
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
// means unrestricted" case is represented with an allow-all domain + CIDR.
//
// Egress==nil also implies AllowPrivateNetworks: a policy that declares no
// filtering must not be more restrictive than having no SandboxNetworkPolicy at
// all, and without one there is no sidecar and private ranges are reachable.
// Otherwise merely turning on SecretInjection — which requires a non-nil policy
// to get the sidecar injected — would silently cut the sandbox off from every
// host that resolves inside the cluster, including split-horizon public names.
// The anti-SSRF baseline stays on wherever filtering *is* declared (Egress set,
// or DisableEgress), and "allow everything but keep the baseline" remains
// expressible as Egress{AllowedDomains: ["*"], AllowedCIDRs: ["0.0.0.0/0"]}.
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

// buildEgressInjectAnnotation resolves the credential-injection block for a
// claimed sandbox and returns its JSON encoding for the egress-inject
// annotation.
//
// The returned value deliberately carries **no credential material**: it holds
// rule shapes, credential names, Secret references, and the per-claim
// placeholders. The operator resolves the Secret references at push time and
// sends the plaintext straight to the sidecar over the exec channel, so a
// credential never reaches etcd, an annotation, or an API response.
//
// Returns ok=false when the Pool declares no injection.
func buildEgressInjectAnnotation(pool *agentsv1alpha1.SandboxPool) (value string, ok bool, err *domain.AppError) {
	if pool == nil || pool.Spec.NetworkPolicy == nil || pool.Spec.NetworkPolicy.SecretInjection == nil {
		return "", false, nil
	}
	if vErr := agentsv1alpha1.ValidateSecretInjection(pool.Spec.NetworkPolicy); vErr != nil {
		return "", false, domain.NewBadRequest(fmt.Sprintf("invalid secretInjection on pool %s: %v", pool.Name, vErr))
	}

	si := pool.Spec.NetworkPolicy.SecretInjection.DeepCopy()

	// Fill in a fresh decoy for every credential that did not pin one, so two
	// sandboxes never share a generated placeholder and a released pod's decoy
	// is worthless to its successor.
	for i := range si.Credentials {
		if si.Credentials[i].ExposeAs == "" || si.Credentials[i].Placeholder != "" {
			continue
		}
		ph, genErr := generatePlaceholder()
		if genErr != nil {
			return "", false, domain.NewInternal("failed to generate credential placeholder", genErr)
		}
		si.Credentials[i].Placeholder = ph
	}

	data, mErr := json.Marshal(si)
	if mErr != nil {
		return "", false, domain.NewInternal("failed to encode secret injection", mErr)
	}
	return string(data), true, nil
}

// generatePlaceholder returns a decoy value with enough entropy that it cannot
// collide with ordinary header content.
func generatePlaceholder() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return agentsv1alpha1.PlaceholderPrefix + hex.EncodeToString(buf), nil
}
