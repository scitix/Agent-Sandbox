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

package instancetype

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// MatchByResources is the canonical implementation of
// Provider.ResolveByResources. Closed-source backends typically delegate to
// this function after exposing their catalog as a []*InstanceType.
//
// Algorithm:
//
//  1. Walk catalog entries in descending order of "specificity" — entries
//     with more numeric keys in BaseResources.Requests come first. This
//     ensures `sci.g11-2` (cpu+memory+gpu) is preferred over `sci.c23-2`
//     (cpu+memory) when both could divide a GPU pool — without this, a
//     CPU-only entry would shadow a GPU entry simply because it appeared
//     earlier in the catalog.
//
//  2. For each candidate, compute the per-key multiplier `observed/base`
//     for every key in observed.Requests. If any key produces a non-integer
//     or zero multiplier, or any two keys disagree, skip the candidate.
//
//  3. Limits, when present on the observed side, must produce the same
//     multiplier as Requests. Absent limits on either side are skipped.
//
//  4. The first candidate that passes wins. Ties (multiple candidates
//     produce the same answer) are broken by catalog order — callers
//     wanting stability should sort their catalog by Name before passing
//     it in.
//
// Returns (nil, 0) when no candidate matches.
func MatchByResources(catalog []*InstanceType, observed corev1.ResourceRequirements) (*InstanceType, int32) {
	if len(catalog) == 0 || len(observed.Requests) == 0 {
		return nil, 0
	}

	// Sort by descending count of numeric keys in BaseResources.Requests for
	// deterministic and "specific-first" matching.
	candidates := make([]*InstanceType, len(catalog))
	copy(candidates, catalog)
	sort.SliceStable(candidates, func(i, j int) bool {
		return len(candidates[i].BaseResources.Requests) > len(candidates[j].BaseResources.Requests)
	})

	for _, it := range candidates {
		m, ok := factorForRequests(it.BaseResources, observed)
		if !ok {
			continue
		}
		if it.MaxMultiplier > 0 && m > it.MaxMultiplier {
			continue
		}
		return it, m
	}
	return nil, 0
}

// factorForRequests returns the integer multiplier that scales base into
// observed exactly, or (0, false) when no such integer exists.
func factorForRequests(base, observed corev1.ResourceRequirements) (int32, bool) {
	if len(base.Requests) == 0 || len(observed.Requests) == 0 {
		return 0, false
	}

	var m int32
	first := true

	// Requests
	for key, observedQ := range observed.Requests {
		baseQ, ok := base.Requests[key]
		if !ok || baseQ.IsZero() {
			return 0, false
		}
		factor, ok := exactIntegerRatio(baseQ, observedQ)
		if !ok {
			return 0, false
		}
		if first {
			m = factor
			first = false
		} else if factor != m {
			return 0, false
		}
	}
	if first || m < 1 {
		return 0, false
	}

	// Limits — only checked when both sides have a value for the key.
	for key, observedQ := range observed.Limits {
		baseQ, ok := base.Limits[key]
		if !ok || baseQ.IsZero() {
			continue
		}
		factor, ok := exactIntegerRatio(baseQ, observedQ)
		if !ok {
			return 0, false
		}
		if factor != m {
			return 0, false
		}
	}

	return m, true
}

// exactIntegerRatio returns observed/base as a positive int32 when the
// division is exact, or (0, false) otherwise.
//
// CPU quantities use milli-precision (1 CPU = 1000m); memory and GPU use
// unit (byte / card) precision. `resource.Quantity.MilliValue()` exposes a
// lossless integer for both: 1 == 1000m for non-CPU resources.
func exactIntegerRatio(base, observed resource.Quantity) (int32, bool) {
	b := base.MilliValue()
	o := observed.MilliValue()
	if b == 0 || o == 0 {
		return 0, false
	}
	if o < b {
		return 0, false
	}
	if o%b != 0 {
		return 0, false
	}
	ratio := o / b
	if ratio < 1 || ratio > int64(int32(^uint32(0)>>1)) {
		return 0, false
	}
	return int32(ratio), true
}

// ApplyMultiplier scales every numeric quantity in base by multiplier and
// returns the result. Closed-source Resolve implementations call this to
// produce Pod ResourceRequirements from (catalog entry, multiplier).
//
// Requests and Limits are scaled symmetrically; missing keys remain missing.
// CPU uses milli-precision; memory / GPU use unit precision.
func ApplyMultiplier(base corev1.ResourceRequirements, multiplier int32) corev1.ResourceRequirements {
	if multiplier < 1 {
		multiplier = 1
	}
	out := corev1.ResourceRequirements{}
	if len(base.Requests) > 0 {
		out.Requests = scaleList(base.Requests, multiplier)
	}
	if len(base.Limits) > 0 {
		out.Limits = scaleList(base.Limits, multiplier)
	}
	return out
}

func scaleList(in corev1.ResourceList, multiplier int32) corev1.ResourceList {
	out := make(corev1.ResourceList, len(in))
	for k, v := range in {
		if k == corev1.ResourceCPU {
			scaled := resource.NewMilliQuantity(v.MilliValue()*int64(multiplier), v.Format)
			out[k] = *scaled
			continue
		}
		scaled := resource.NewQuantity(v.Value()*int64(multiplier), v.Format)
		out[k] = *scaled
	}
	return out
}
