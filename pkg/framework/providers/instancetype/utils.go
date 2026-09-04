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
	"fmt"

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
// PoolName.
func DeriveResourceKey(observed corev1.ResourceRequirements) string {
	reqs := observed.Requests
	if len(reqs) == 0 {
		reqs = observed.Limits
	}

	cpuMilli := reqs.Cpu().MilliValue()
	memBytes := reqs.Memory().Value()

	if cpuMilli == 0 && memBytes == 0 {
		return "default"
	}

	return formatCPUKey(cpuMilli, "c", "mc") + formatMemoryKey(memBytes, "gi", "mi")
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
