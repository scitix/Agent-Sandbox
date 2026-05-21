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
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
)

// overridesFromGen projects the caller-supplied gen.PoolTemplateOverrides into the
// internal annotation-storage shape. Returns nil when the gen value has no effect
// (no image, no resource multiplier > 1).
func overridesFromGen(g *gen.PoolTemplateOverrides) *PoolTemplateOverrides {
	if g == nil {
		return nil
	}
	out := &PoolTemplateOverrides{}
	if g.Image != nil {
		out.Image = *g.Image
	}
	if g.ResourceMultiplier != nil {
		out.ResourceMultiplier = *g.ResourceMultiplier
	}
	if out.Image == "" && out.ResourceMultiplier <= 1 {
		return nil
	}
	return out
}

// PoolTemplateOverrides holds per-pool overrides applied on top of the referenced template.
// Applied AFTER copying EmbeddedSandboxTemplate from the source template. The effective
// computed values are stored in the pool spec, while the override intent is persisted in
// the pool's agentbox.navix.sh/overrides annotation so SyncTemplate can re-apply it against
// newer template versions.
//
// This is an internal annotation-storage shape, not a wire type. The native API exposes
// gen.PoolTemplateOverrides (which carries only Image and ResourceMultiplier); the extra
// ImagePullSecretName field tracked here is server-derived and never round-trips through
// the API.
type PoolTemplateOverrides struct {
	// Image overrides containers[0].Image; empty = no-op.
	Image string `json:"image,omitempty"`
	// ResourceMultiplier uniformly scales all container CPU and memory requests+limits,
	// and all reservation.replicaQuota values. Must be >= 1; 1 = no change.
	ResourceMultiplier int32 `json:"resourceMultiplier,omitempty"`
	// ImagePullSecretName is the deterministic Secret name injected into
	// spec.template.spec.imagePullSecrets; empty = no-op.
	ImagePullSecretName string `json:"imagePullSecretName,omitempty"`
}
