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
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// GetPodPhaseSince returns the timestamp when the pod entered the requested sandbox phase.
//
// For Starting/Stopping, the timestamp comes from InplaceUpdateState.UpdateTimestamp
// recorded when the transition was triggered. For Running/Idle, it comes from the
// completed InplaceUpdateState timestamp refreshed by MarkUpdateCompleted. When the
// pod has no matching in-place update state (for example, a freshly created idle pod),
// the method falls back to metadata.creationTimestamp.
//
// The returned bool is false when the pod is not currently in the requested phase.
// When phase is empty, the pod's current sandbox phase label is used.
func GetPodPhaseSince(pod *corev1.Pod, phase string) (time.Time, bool, error) {
	if pod == nil {
		return time.Time{}, false, fmt.Errorf("pod is nil")
	}

	currentPhase := pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey]
	if phase == "" {
		phase = currentPhase
	}
	if currentPhase != phase {
		return time.Time{}, false, nil
	}

	state, err := GetInplaceUpdateState(pod)
	if err != nil {
		return time.Time{}, false, err
	}
	if state != nil && !state.UpdateTimestamp.IsZero() {
		switch phase {
		case agentsv1alpha1.SandboxPhaseStarting, agentsv1alpha1.SandboxPhaseStopping:
			if state.Phase == phase {
				return state.UpdateTimestamp.UTC(), true, nil
			}
		case agentsv1alpha1.SandboxPhaseRunning, agentsv1alpha1.SandboxPhaseIdle:
			if state.Phase == InplaceUpdatePhaseCompleted &&
				defaultTargetPodPhase(state.TargetPodPhase, phase) == phase {
				return state.UpdateTimestamp.UTC(), true, nil
			}
		}
	}

	if pod.CreationTimestamp.IsZero() {
		return time.Time{}, true, nil
	}
	return pod.CreationTimestamp.UTC(), true, nil
}

// GetPodPhaseDuration returns how long the pod has been in the requested sandbox phase.
// It uses GetPodPhaseSince and subtracts it from now. When now.IsZero(), time.Now().UTC()
// is used. The returned bool is false when the pod is not currently in that phase.
func GetPodPhaseDuration(pod *corev1.Pod, phase string, now time.Time) (time.Duration, bool, error) {
	since, ok, err := GetPodPhaseSince(pod, phase)
	if err != nil || !ok {
		return 0, ok, err
	}
	if since.IsZero() {
		return 0, true, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if since.After(now) {
		return 0, true, nil
	}
	return now.Sub(since), true, nil
}
