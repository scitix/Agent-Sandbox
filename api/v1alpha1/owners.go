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
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SandboxEnvOwnerKind is the OwnerReference.Kind value for SandboxEnv.
// Exposed so other packages can write owner refs without string literals.
const SandboxEnvOwnerKind = "SandboxEnv"

// HasEnvOwner reports whether obj carries an OwnerReference to a SandboxEnv
// in this API group. Controlling-vs-non-controlling is intentionally ignored;
// Phase 1 adoption stamps a non-controlling reference and we may still want
// to treat hand-edited controlling references the same way.
//
// The check uses APIVersion's group prefix (not exact equality) so future
// minor API revisions (e.g. v1beta1) automatically qualify.
func HasEnvOwner(obj metav1.Object) bool {
	if obj == nil {
		return false
	}
	for _, ref := range obj.GetOwnerReferences() {
		if ref.Kind != SandboxEnvOwnerKind {
			continue
		}
		if !strings.HasPrefix(ref.APIVersion, GroupVersion.Group+"/") {
			continue
		}
		return true
	}
	return false
}
