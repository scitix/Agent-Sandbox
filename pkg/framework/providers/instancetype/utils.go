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
