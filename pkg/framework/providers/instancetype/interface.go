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

// Package instancetype defines the catalog abstraction for "named base
// resource shapes that a sandbox can scale up from by an integer multiplier".
//
// Mirrors pkg/framework/providers/quota in shape: a Provider interface, a
// Noop implementation, and a factory registry. Closed-source code (e.g. the
// SciTiX ConfigMap-backed catalog) implements the Provider; open-source
// builds default to Noop.
//
// Concepts:
//
//   - InstanceType: the named base shape, addressable by Name. Has
//     BaseResources (the Pod ResourceRequirements at multiplier=1) and
//     MaxMultiplier (the cap on how large a single sandbox may grow).
//
//   - Multiplier: the integer factor a sandbox applies on top of an
//     InstanceType's BaseResources. Aligns with the existing
//     PoolTemplateOverrides.ResourceMultiplier semantics: scale CPU /
//     memory / GPU element-wise by the multiplier.
//
//   - Provider: read-only catalog access plus the two conversions every
//     consumer needs — Resolve (name + multiplier → resources) and
//     ResolveByResources (resources → name + multiplier).
//
// The Provider is intended to be consumed by:
//
//   - SandboxEnv adoption (round-trip an existing Pool's PodSpec back to
//     name + multiplier when populating EnvClusterMember).
//   - Pool template overrides (set Pod resources from (name, multiplier)
//     when the caller wants catalog-driven sizing).
//   - Dashboard listing (GET /v1/instancetypes — Phase 2).
package instancetype

import (
	"context"

	corev1 "k8s.io/api/core/v1"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
)

// InstanceType is the canonical open-source view of a catalog entry.
//
// Implementations populate this from whatever backend they use (ConfigMap,
// API, etc.). Open-source consumers must NOT interpret values in Extensions —
// they exist solely so closed-source backends can carry hardware-specific
// metadata (e.g. "gpu-type") through the open-source code path for display.
type InstanceType struct {
	// Name is the catalog key. Required; unique within a Provider.
	Name string

	// ShowName is the user-facing label. Falls back to Name when empty.
	ShowName string

	// Description is free-form context for Dashboard tooltips.
	Description string

	// BaseResources are the Pod resource requirements at Multiplier=1.
	// Implementations should populate Requests at minimum; Limits default to
	// Requests when omitted (matches existing PoolTemplateOverrides
	// semantics). Numeric keys (cpu, memory, nvidia.com/gpu, …) scale
	// linearly with the multiplier.
	BaseResources corev1.ResourceRequirements

	// MaxMultiplier caps how large a single sandbox may grow from
	// BaseResources. 0 means "no cap" (used by Noop and tests). Closed-source
	// backends typically derive this from cluster topology (e.g. ConfigMap
	// "instance-quantity" field).
	MaxMultiplier int32

	// Cost is a free-form weight for Dashboard sorting / cost displays.
	Cost string

	// Extensions carries backend-specific key/value pairs (e.g.
	// "gpu-type", "instance-quantity") that don't fit the canonical shape.
	// Open-source code must not interpret values here — only forward them
	// for display.
	Extensions map[string]string
}

// Provider exposes read-only access to the InstanceType catalog plus the
// canonical multiplier conversions.
//
// A nil Provider is invalid; callers should always hold at least a Noop. Use
// Enabled() to decide whether to expose catalog-driven UI / migration paths.
type Provider interface {
	// Enabled reports whether the catalog is available. False means the
	// catalog feature is disabled (no backend configured, ConfigMap missing,
	// etc.) — callers should fall back to legacy paths (PodSpec inline
	// resources, ResourceMultiplier on the raw PodSpec).
	Enabled() bool

	// Get returns the named InstanceType. (nil, false) when not found.
	// Callers must NOT mutate the returned pointer.
	Get(name string) (*InstanceType, bool)

	// List returns the full catalog sorted by Name. Empty when Enabled is
	// false or the catalog has not yet loaded.
	List() []*InstanceType

	// Resolve computes Pod ResourceRequirements for (name, multiplier).
	// Errors:
	//   - NotFound:   name unknown.
	//   - BadRequest: multiplier < 1, or multiplier > MaxMultiplier when
	//                 MaxMultiplier > 0.
	//   - Internal:   provider enabled but catalog still loading.
	Resolve(ctx context.Context, name string, multiplier int32) (corev1.ResourceRequirements, *domain.AppError)

	// ResolveByResources is the inverse of Resolve: round-trip a Pod's
	// observed ResourceRequirements back to (InstanceType, multiplier) when
	// some catalog entry produces them exactly.
	//
	// Returns (nil, 0, nil) when no entry matches — callers fall back to
	// inline resources rather than fail. An *AppError is returned only for
	// transient/system errors.
	//
	// Match rules (see MatchByResources for the canonical implementation):
	//   - Every numeric key in observed.Requests must divide evenly into
	//     the candidate's BaseResources.Requests, yielding the same integer
	//     multiplier.
	//   - Limits, when present on both sides, are checked with the same
	//     algorithm; absent Limits on either side are ignored.
	//   - The resulting multiplier must be ≥ 1 and (when MaxMultiplier > 0)
	//     ≤ MaxMultiplier.
	ResolveByResources(ctx context.Context, observed corev1.ResourceRequirements) (*InstanceType, int32, *domain.AppError)

	// DeriveScalingGroupName returns a stable, human-readable identifier
	// for the given resource shape. Used by SandboxEnv member adoption and
	// the autoscaler to cluster Pools sharing one resource regimen into a
	// single ScalingGroup (e.g. ondemand + spot of the same shape).
	//
	// Implementations MUST return identical names for equivalent resource
	// shapes (same CPU / memory / GPU counts and types). Names should fit
	// DNS-label characters so they can be used as annotation values and
	// map keys.
	//
	// Open-source Noop returns a canonical "Xc{Y}Gi[-Zgpu]" form derived
	// purely from the observed Requests. Closed-source Scitix provider can
	// return catalog-aligned names like "sci.c24-2".
	DeriveScalingGroupName(observed corev1.ResourceRequirements) string
}
