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

// PoolScaleUpPolicy controls scale-up behavior.
type PoolScaleUpPolicy struct {
	// Mode controls how aggressively the pool grows on each scale-up decision.
	//   - Conservative: +1 per decision
	//   - Default:      +max(1, ceil(currentReplicas/2))
	//   - Aggressive:   scale to min(currentReplicas*2, maxReplicas)
	// Defaults to Default.
	// +optional
	// +kubebuilder:validation:Enum=Conservative;Default;Aggressive
	// +kubebuilder:default=Default
	Mode PoolScaleUpMode `json:"mode,omitempty"`

	// CooldownSeconds is the minimum number of seconds between two consecutive
	// scale-up events. Prevents scale-up storms when many requests arrive simultaneously.
	// Defaults to 30.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=30
	CooldownSeconds int32 `json:"cooldownSeconds,omitempty"`

	// IdleThresholdSeconds triggers a proactive scale-up when idleReplicas == 0
	// has persisted for this many seconds. Set to 0 to disable proactive scale-up.
	// Defaults to 30.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=30
	IdleThresholdSeconds int32 `json:"idleThresholdSeconds,omitempty"`

	// IdleZeroQuietWindowSeconds suppresses the proactive idleZero
	// scale-up trigger when no Sandbox.Create request has been observed
	// for this Pool within the most recent window. Concretely: if
	// `now - LastSandboxCreateTime > idleZeroQuietWindowSeconds` AND
	// `IdleThresholdSeconds` has elapsed since `idleReplicas` last hit
	// zero, the autoscaler will NOT fire a proactive scale-up. Reactive
	// scale-ups (queue length > 0 with no idle Pod) ignore this window
	// — a real waiter always wins. Set to 0 to disable the quiet-window
	// gate (proactive scale-up fires the moment IdleThresholdSeconds
	// elapses, restoring the pre-redesign behaviour).
	// Defaults to 300 (5 minutes).
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=300
	IdleZeroQuietWindowSeconds int32 `json:"idleZeroQuietWindowSeconds,omitempty"`

	// SaturationCooldownSeconds is the duration the Env autoscaler keeps
	// a member marked saturated after a probe returned InsufficientResources
	// (or InvalidSpec). Subsequent reconciles skip the member's expensive
	// PreUpdatePool probe until the cooldown elapses; the router also
	// deprioritises saturated members. Defaults to 60.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=60
	SaturationCooldownSeconds int32 `json:"saturationCooldownSeconds,omitempty"`
}

// PoolScaleDownPolicy controls scale-down behavior.
type PoolScaleDownPolicy struct {
	// IdleTimeoutSeconds is the minimum duration (in seconds) a Pod must remain
	// in Idle state before it becomes a candidate for scale-down.
	// Defaults to 300 (5 minutes).
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=300
	IdleTimeoutSeconds int32 `json:"idleTimeoutSeconds,omitempty"`

	// StabilizationSeconds is the minimum number of seconds between two consecutive
	// scale-down events. Prevents thrashing when load fluctuates around the threshold.
	// Defaults to 60.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=60
	StabilizationSeconds int32 `json:"stabilizationSeconds,omitempty"`

	// ProtectionWindowSeconds is the time window after a Pod is marked for
	// scale-down (via the scale-down-protected annotation) during which a new
	// Create Sandbox request can still claim it, cancelling the scale-down intent.
	// Defaults to 10.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=10
	ProtectionWindowSeconds int32 `json:"protectionWindowSeconds,omitempty"`
}

// PoolScaleUpMode defines how aggressively a pool scales up.
// +kubebuilder:validation:Enum=Conservative;Default;Aggressive
type PoolScaleUpMode string

const (
	// PoolScaleUpModeConservative adds one Pod per scale-up decision.
	PoolScaleUpModeConservative PoolScaleUpMode = "Conservative"
	// PoolScaleUpModeDefault adds max(1, ceil(currentReplicas/2)) Pods per decision.
	PoolScaleUpModeDefault PoolScaleUpMode = "Default"
	// PoolScaleUpModeAggressive doubles the replica count up to maxReplicas per decision.
	PoolScaleUpModeAggressive PoolScaleUpMode = "Aggressive"
)

// PodCreationImagePolicy defines which image createPod should use.
// +kubebuilder:validation:Enum=PoolDefaultImage;IdleImage
type PodCreationImagePolicy string

const (
	// PodCreationImagePolicyPoolDefaultImage preserves the template container image.
	// This matches the current createPod behavior and enables the same-image fast path.
	PodCreationImagePolicyPoolDefaultImage PodCreationImagePolicy = "PoolDefaultImage"
	// PodCreationImagePolicyIdleImage replaces the first container image with
	// spec.idleImage when a Pod is created, so Pods enter Idle faster.
	PodCreationImagePolicyIdleImage PodCreationImagePolicy = "IdleImage"
)
