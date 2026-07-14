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
	"encoding/json"
	"fmt"
	"hash/fnv"
)

// ComputeRevisionHash returns a stable fnv32a hash of the identity a freshly
// materialised *idle* Pod would take under this Pool spec. Two Pools that would
// produce byte-identical idle Pods hash equally; any change that alters the idle
// Pod (idle image, sidecars/volumes/affinity in the pod-spec body, egress
// network policy, or the scheduler-facing template metadata) flips the hash and
// drives a rollout. Field *deletion* changes the serialised structure and is
// therefore captured too.
//
// Deliberately excluded from the hash (changing them must NOT roll idle Pods):
//   - Replicas / DefaultStartupTimeout / DefaultIdleTimeout: scale and per-
//     request timeouts, not pod identity.
//   - The main container image under the IdleImage policy: an idle Pod runs
//     spec.idleImage (createPod overrides containers[0].image), and the running
//     image is resolved from the live Pool template at claim time — so changing
//     the running image reaches sandboxes on the next claim without a rebuild.
//     It is normalised to IdleImage before hashing so it does not contribute.
//   - The hash label itself (self-reference).
func ComputeRevisionHash(spec *SandboxPoolSpec) string {
	if spec == nil {
		return ""
	}
	norm := spec.DeepCopy()

	// Excluded: scale / timeouts / rollout throttle / template provenance.
	norm.Replicas = 0
	norm.DefaultStartupTimeout = nil
	norm.DefaultIdleTimeout = nil
	norm.MaxUnavailable = nil
	norm.TemplateName = ""

	// Excluded: running image under the IdleImage policy. Normalise the main
	// container image to the idle image so a running-image-only change does not
	// flip the hash. Under PoolDefaultImage the idle Pod runs the template
	// image, so it is left intact and does contribute.
	if norm.PodCreationImagePolicy == PodCreationImagePolicyIdleImage &&
		norm.IdleImage != "" && len(norm.Template.Spec.Containers) > 0 {
		norm.Template.Spec.Containers[0].Image = norm.IdleImage
	}

	// Excluded: self-reference. The hash is stamped into these maps after it is
	// computed, so it must not feed back into the input.
	delete(norm.Template.Labels, TemplateHashLabelKey)

	// Serialise the normalised spec deterministically. encoding/json sorts map
	// keys, so label/annotation ordering is stable.
	b, err := json.Marshal(norm)
	if err != nil {
		// A PodSpec always marshals; fall back defensively rather than panic.
		b = fmt.Appendf(nil, "%#v", norm)
	}
	h := fnv.New32a()
	_, _ = h.Write(b)
	return fmt.Sprintf("%08x", h.Sum32())
}
