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

func DeriveResourceKey(observed corev1.ResourceRequirements) string {
	reqs := observed.Requests
	if len(reqs) == 0 {
		reqs = observed.Limits
	}

	cpu := reqs.Cpu().Value()
	memBytes := reqs.Memory().Value()
	memGi := memBytes / (1 << 30)

	if cpu == 0 && memGi == 0 {
		return "default"
	}

	return fmt.Sprintf("%dc%dgi", cpu, memGi)
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
