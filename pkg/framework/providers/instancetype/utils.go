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
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// DeriveResourceKey renders a resource shape as a lowercase DNS-label-safe key
// used for both the ScalingGroup name and the PoolName suffix.
//
// Each dimension is rendered in its largest lossless unit, so whole-unit shapes
// stay compact while sub-unit shapes keep their precision:
//
//	{cpu:1,    mem:16Gi}   → "1c16gi"
//	{cpu:500m, mem:2Gi}    → "500mc2gi"
//	{cpu:20m,  mem:128Mi}  → "20mc128mi"
//	{cpu:2,    mem:1536Mi} → "2c1536mi"
//	{}                     → "default"
//
// Rendering sub-unit shapes losslessly is what keeps them in distinct scaling
// groups: collapsing them to whole cores/GiB would map every shape under 1Gi
// onto the same key, and two differently-sized Pools would then collide on
// PoolName. Exact nvidia.com/gpu quantities add -Ngpu; other nonzero plugin
// resource names and quantities add a stable resource-identity digest.
func DeriveResourceKey(observed corev1.ResourceRequirements) string {
	reqs := observed.Requests
	if len(reqs) == 0 {
		reqs = observed.Limits
	}

	cpuMilli := reqs.Cpu().MilliValue()
	memBytes := reqs.Memory().Value()

	suffix := extendedResourceSuffix(observed)
	if cpuMilli == 0 && memBytes == 0 && suffix == "" {
		return "default"
	}

	key := formatCPUKey(cpuMilli, "c", "mc") + formatMemoryKey(memBytes, "gi", "mi") + suffix
	return CompactResourceName(key, 63)
}

// CompactResourceName keeps a DNS-label-safe generated name within maxLength.
// The digest covers the entire name so truncation does not merge distinct shapes.
func CompactResourceName(name string, maxLength int) string {
	if len(name) <= maxLength {
		return name
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(name)))[:16]
	return strings.TrimRight(name[:maxLength-len(digest)-1], "-") + "-" + digest
}

// extendedResourceSuffix distinguishes all device/plugin resource names without
// classifying every extended resource as a GPU. Only the exact NVIDIA resource
// has the familiar gpu suffix; custom accelerators and RDMA use opaque resource
// identity. Native storage and hugepage dimensions retain existing naming.
func extendedResourceSuffix(observed corev1.ResourceRequirements) string {
	resources := extendedResources(observed)

	suffix := ""
	if quantity, ok := resources["nvidia.com/gpu"]; ok && quantity.Sign() > 0 {
		suffix = fmt.Sprintf("-%dgpu", quantity.Value())
	}
	delete(resources, "nvidia.com/gpu")
	var names []string
	for name, quantity := range resources {
		if quantity.Sign() > 0 {
			names = append(names, string(name))
		}
	}
	if len(names) == 0 {
		return suffix
	}
	sort.Strings(names)
	var shape strings.Builder
	for _, name := range names {
		quantity := resources[corev1.ResourceName(name)]
		fmt.Fprintf(&shape, "%s=%s\n", name, quantity.String())
	}
	return suffix + fmt.Sprintf("-r%x", sha256.Sum256([]byte(shape.String())))[:18]
}

// ExtendedResourceIdentity is the lossless identity of nonzero plugin resource
// names and quantities. It is used to reject legacy-group device mixing without
// renaming an existing group or inferring a GPU model from resource counts.
func ExtendedResourceIdentity(observed corev1.ResourceRequirements) string {
	resources := extendedResources(observed)
	var names []string
	for name, quantity := range resources {
		if quantity.Sign() > 0 {
			names = append(names, string(name))
		}
	}
	sort.Strings(names)
	var identity strings.Builder
	for _, name := range names {
		quantity := resources[corev1.ResourceName(name)]
		fmt.Fprintf(&identity, "%s=%s\n", name, quantity.String())
	}
	return identity.String()
}

func extendedResources(observed corev1.ResourceRequirements) corev1.ResourceList {
	resources := corev1.ResourceList{}
	for _, source := range []corev1.ResourceList{observed.Requests, observed.Limits} {
		for name, quantity := range source {
			if !strings.Contains(string(name), "/") || strings.HasPrefix(string(name), "kubernetes.io/") {
				continue
			}
			// Extended resources specified only in limits receive matching requests
			// from Kubernetes. Preserve that device identity before API defaulting.
			if current, exists := resources[name]; !exists || quantity.Cmp(current) > 0 {
				resources[name] = quantity.DeepCopy()
			}
		}
	}
	return resources
}

// formatCPUKey renders a milli-core count as "<cores>"+coreUnit for whole
// cores, else "<milli>"+milliUnit. Callers supply the unit spelling so the
// lowercase resource key and the mixed-case scaling-group name can share the
// rounding logic.
func formatCPUKey(milli int64, coreUnit, milliUnit string) string {
	if milli%1000 == 0 {
		return fmt.Sprintf("%d%s", milli/1000, coreUnit)
	}
	return fmt.Sprintf("%d%s", milli, milliUnit)
}

// formatMemoryKey renders a byte count as "<gib>"+gibUnit for whole GiB, else
// "<mib>"+mibUnit rounded up to the next whole MiB.
func formatMemoryKey(bytes int64, gibUnit, mibUnit string) string {
	const (
		mib = 1 << 20
		gib = 1 << 30
	)
	if bytes%gib == 0 {
		return fmt.Sprintf("%d%s", bytes/gib, gibUnit)
	}
	return fmt.Sprintf("%d%s", (bytes+mib-1)/mib, mibUnit)
}

// FitsWithin reports whether every resource dimension in pod is ≤ the
// corresponding dimension in capacity (the reservation envelope, typically an
// InstanceType's BaseResources × multiplier). This is the "round-down"
// contract: a Pod may request less than the reserved instance in any
// dimension, but never more.
//
// It returns the first offending dimension when pod exceeds capacity — either
// a dimension whose value is larger, or a non-zero dimension absent from
// capacity (e.g. a GPU request against a CPU-only instance). Comparison uses
// MilliValue() (lossless for cpu / memory / gpu), matching the precision used
// by the matching helpers elsewhere.
func FitsWithin(pod, capacity corev1.ResourceList) (exceeded corev1.ResourceName, ok bool) {
	for name, q := range pod {
		if q.IsZero() {
			continue
		}
		capQ, has := capacity[name]
		if !has {
			return name, false
		}
		if q.MilliValue() > capQ.MilliValue() {
			return name, false
		}
	}
	return "", true
}
