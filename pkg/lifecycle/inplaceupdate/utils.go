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

package inplaceupdate

import (
	corev1 "k8s.io/api/core/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

func GetSandboxPhase(pod *corev1.Pod) string {
	phase := pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey]
	switch phase {
	case agentsv1alpha1.SandboxPhaseIdle, agentsv1alpha1.SandboxPhaseRunning,
		agentsv1alpha1.SandboxPhaseStarting, agentsv1alpha1.SandboxPhaseStopping,
		agentsv1alpha1.SandboxPhaseFailed:
		return phase
	}
	if pod.Status.Phase == corev1.PodFailed {
		return agentsv1alpha1.SandboxPhaseFailed
	}
	return agentsv1alpha1.SandboxPhaseIdle
}
