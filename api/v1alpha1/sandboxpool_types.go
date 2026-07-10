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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// SandboxPoolPhase is the high-level phase of a SandboxPool.
// +kubebuilder:validation:Enum=Pending;Ready;ScalingUp;ScalingDown;Degraded;Terminating
type SandboxPoolPhase string

const (
	// SandboxPoolPhasePending indicates the pool has no pods yet (spec.replicas == 0 and no pods exist).
	SandboxPoolPhasePending SandboxPoolPhase = "Pending"
	// SandboxPoolPhaseReady indicates the pool has reached the desired replica count and all pods are healthy.
	SandboxPoolPhaseReady SandboxPoolPhase = "Ready"
	// SandboxPoolPhaseScalingUp indicates the pool is scaling up (current < desired replicas).
	SandboxPoolPhaseScalingUp SandboxPoolPhase = "ScalingUp"
	// SandboxPoolPhaseScalingDown indicates the pool is scaling down (current > desired replicas).
	// This can persist if running pods cannot be deleted immediately.
	SandboxPoolPhaseScalingDown SandboxPoolPhase = "ScalingDown"
	// SandboxPoolPhaseDegraded indicates the pool has reached the desired replica count but
	// some idle pods are unavailable (NotReady) or some pods are in failed state.
	SandboxPoolPhaseDegraded SandboxPoolPhase = "Degraded"
	// SandboxPoolPhaseTerminating indicates the pool is being deleted.
	SandboxPoolPhaseTerminating SandboxPoolPhase = "Terminating"
)

// Condition type constants for SandboxPool.
const (
	// SandboxPoolConditionAvailable indicates whether the pool has idle pods ready to accept sandbox requests.
	SandboxPoolConditionAvailable = "Available"
	// SandboxPoolConditionScaling indicates whether the pool is currently scaling up or down.
	SandboxPoolConditionScaling = "Scaling"
	// SandboxPoolConditionDegraded indicates whether the pool has unhealthy or failed pods.
	SandboxPoolConditionDegraded = "Degraded"
)

// Condition reason constants for SandboxPool.
const (
	// Available condition reasons
	SandboxPoolReasonIdlePodsAvailable   = "IdlePodsAvailable"   // healthy idle pods are available
	SandboxPoolReasonNoIdlePodsAvailable = "NoIdlePodsAvailable" // no idle pods can accept requests

	// Scaling condition reasons
	SandboxPoolReasonScalingUp     = "ScalingUp"     // pool is scaling up
	SandboxPoolReasonScalingDown   = "ScalingDown"   // pool is scaling down
	SandboxPoolReasonReplicasReady = "ReplicasReady" // all replicas are up-to-date

	// Degraded condition reasons
	SandboxPoolReasonAllPodsHealthy     = "AllPodsHealthy"         // no unhealthy or failed pods
	SandboxPoolReasonUnhealthyIdlePods  = "UnhealthyIdlePods"      // idle pods are NotReady
	SandboxPoolReasonFailedPodsPresent  = "FailedPodsPresent"      // failed pods exist
	SandboxPoolReasonUnhealthyAndFailed = "UnhealthyAndFailedPods" // both unhealthy idle and failed pods
)

// SandboxPoolSpec defines the desired state of SandboxPool
type SandboxPoolSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	// Replicas is the total desired number of Pods (Idle + Running + Starting + Stopping).
	// Adjusted by the SandboxEnv autoscaler (when the Pool is owned by an Env) or
	// directly by the operator for unmanaged Pools.
	// +kubebuilder:validation:Minimum=0
	Replicas int32 `json:"replicas"`

	// TemplateName references a cluster-scoped SandboxTemplate to use as the base
	// configuration. When set, the template's EmbeddedSandboxTemplate is copied at
	// creation time. Inline fields in SandboxPoolSpec override template fields.
	// +optional
	TemplateName string `json:"templateName,omitempty"`

	// DefaultStartupTimeout is the default startup timeout applied to sandbox create
	// requests in this pool when the CreateSandbox request does not specify a startupTimeout.
	// It also serves as the upper bound for the Starting phase: the controller deletes any pod
	// that has been in Starting phase longer than this value.
	//
	// When nil, the controller does not enforce an upper bound on the Starting phase
	// (pods with a per-pod agentbox.navix.sh/startup-timeout annotation are still cleaned up),
	// and create requests without an explicit startupTimeout use the internal default (2 minutes).
	// +optional
	DefaultStartupTimeout *metav1.Duration `json:"defaultStartupTimeout,omitempty"`

	// DefaultIdleTimeout is the default idle timeout applied to sandboxes created
	// in this pool when the CreateSandbox request does not specify an idleTimeout.
	// If nil, sandboxes have no idle timeout by default (they run until explicitly released).
	// +optional
	DefaultIdleTimeout *metav1.Duration `json:"defaultIdleTimeout,omitempty"`

	// PodCreationImagePolicy controls which image newly created Pods start with,
	// regardless of whether replicas are increased manually or by autoscaling.
	//   - PoolDefaultImage: preserve template container image (current behavior)
	//   - IdleImage:        override the first container image with spec.idleImage
	// +optional
	// +kubebuilder:validation:Enum=PoolDefaultImage;IdleImage
	// +kubebuilder:default=IdleImage
	PodCreationImagePolicy PodCreationImagePolicy `json:"podCreationImagePolicy,omitempty"`

	// NetworkPolicy, when set, enables sandbox egress filtering for Pods in this
	// Pool: the operator injects a transparent filter sidecar. For Env-owned
	// Pools this is projected from the Env's overrides.networkPolicy and serves
	// as the default ruleset; per-sandbox create requests may override it.
	// +optional
	NetworkPolicy *SandboxNetworkPolicy `json:"networkPolicy,omitempty"`

	EmbeddedSandboxTemplate `json:",inline"`
}

// SandboxPoolStatus defines the observed state of SandboxPool.
type SandboxPoolStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// Phase is a high-level summary of the pool's current state.
	// Possible values: Pending, Ready, ScalingUp, ScalingDown, Degraded, Terminating.
	//
	// Phase is determined by the following priority rules:
	//   - Terminating: DeletionTimestamp is set
	//   - Pending:     spec.replicas == 0 and no pods exist
	//   - ScalingUp:   current pod count < spec.replicas
	//   - ScalingDown: current pod count > spec.replicas (may persist while running pods cannot be deleted)
	//   - Degraded:    replica count is stable but unavailableIdleReplicas > 0 or failedReplicas > 0
	//   - Ready:       all replicas present and all pods are healthy
	// +optional
	Phase SandboxPoolPhase `json:"phase,omitempty"`

	// IdleReplicas is the number of Pods in idle state
	// +optional
	IdleReplicas int32 `json:"idleReplicas,omitempty"`

	// UnavailableIdleReplicas is the number of Pods in idle phase whose Kubernetes PodReady
	// condition is not True (e.g. Pending, CrashLoopBackOff, ErrImagePull).
	// These Pods are counted in IdleReplicas but cannot accept sandbox requests.
	// A non-zero value causes the pool to enter the Degraded phase.
	// +optional
	UnavailableIdleReplicas int32 `json:"unavailableIdleReplicas,omitempty"`

	// RunningReplicas is the number of Pods in running state
	// +optional
	RunningReplicas int32 `json:"runningReplicas,omitempty"`

	// StartingReplicas is the number of Pods being activated (Idle → Running)
	// +optional
	StartingReplicas int32 `json:"startingReplicas,omitempty"`

	// StoppingReplicas is the number of Pods being recycled (Running → Idle)
	// +optional
	StoppingReplicas int32 `json:"stoppingReplicas,omitempty"`

	// FailedReplicas is the number of Pods in failed state
	// +optional
	FailedReplicas int32 `json:"failedReplicas,omitempty"`

	// PendingRequests is the throttled mirror of the in-process PoolScheduler
	// claim queue depth. Patched every ~3 s when the queue length changes by
	// at least 20 % or crosses the 0/>0 boundary. Used by Dashboard for
	// real-time backlog observability; the Env autoscaler reads the live
	// in-process Snapshot instead and does not depend on this field.
	// +optional
	PendingRequests int32 `json:"pendingRequests,omitempty"`

	// Selector is the label selector string used to identify Pods managed by this Pool.
	// Deprecated: Use LabelSelector for structured access or PhaseSelectors for per-phase filtering.
	// This field is retained for kubectl scale / HPA compatibility (subresource:scale selectorpath).
	// +optional
	Selector string `json:"selector,omitempty"`

	// LabelSelector is the structured label selector matching all Pods managed by this Pool.
	// Equivalent to the Selector field but in structured metav1.LabelSelector form.
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`

	// PhaseSelectors contains pre-computed label selector strings for filtering Pods by phase,
	// suitable for direct use with `kubectl get pods -l <selector>`.
	// Keys: "all", "idle", "running", "starting", "stopping", "failed".
	// Example: kubectl get pods -l <phaseSelectors.running>
	// +optional
	PhaseSelectors map[string]string `json:"phaseSelectors,omitempty"`

	// conditions represent the current state of the SandboxPool resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types:
	// - "Available":  True when healthy idle pods are available to accept new sandbox requests.
	// - "Scaling":    True when the pool is actively scaling up or down.
	// - "Degraded":   True when unavailable idle pods or failed pods are present.
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// AutoScaling persists the Pool autoscaler's decision-time bookkeeping
	// (last scale-up/down timestamps, idle-zero window start, saturation
	// cooldown, last probe outcome). The Pool reconciler is the only writer.
	// Nil when autoscaling is disabled on this Pool's owning Env group.
	// +optional
	AutoScaling *PoolAutoScalingStatus `json:"autoscaling,omitempty"`
}

// PoolAutoScalingStatus carries the autoscaler's per-Pool decision state.
// Every field is set/read exclusively by the SandboxPool reconciler running
// the autoscaling decision pipeline; the SandboxEnv reconciler must never
// write these fields.
type PoolAutoScalingStatus struct {
	// LastScaleUpTime is the wall-clock time of the most recent
	// scale-up that actually increased spec.replicas (the probe
	// accepted at least one additional replica). Drives the success
	// cooldown gate (scaleUpPolicy.cooldownSeconds).
	// +optional
	LastScaleUpTime *metav1.Time `json:"lastScaleUpTime,omitempty"`

	// LastScaleDownTime is the wall-clock time of the most recent
	// successful scale-down (spec.replicas decreased) on this Pool. Drives
	// scaleDownPolicy.stabilizationSeconds.
	// +optional
	LastScaleDownTime *metav1.Time `json:"lastScaleDownTime,omitempty"`

	// IdleZeroSince is the wall-clock time at which this Pool's idle
	// replica count first dropped to zero in the current continuous-zero
	// window. Cleared the instant idle > 0 is observed. Drives the
	// proactive scaleUpPolicy.idleThresholdSeconds trigger.
	// +optional
	IdleZeroSince *metav1.Time `json:"idleZeroSince,omitempty"`

	// LastScaleUpAttemptTime records when the autoscaler last invoked
	// the admission probe for a scale-up, regardless of whether the
	// probe accepted the target. Together with LastScaleUpAttemptResult
	// and the group's SaturationCooldownSeconds it drives the saturation
	// cooldown: when the last attempt was Insufficient / JustRight /
	// Failed, the autoscaler and router treat the Pool as saturated
	// until SaturationCooldownSeconds has elapsed past this timestamp.
	// +optional
	LastScaleUpAttemptTime *metav1.Time `json:"lastScaleUpAttemptTime,omitempty"`

	// LastScaleUpAttemptResult records the outcome of the most recent
	// scale-up admission probe. Empty before the first attempt; one of
	// the PoolScaleUpAttemptResult enum values otherwise.
	// +optional
	LastScaleUpAttemptResult PoolScaleUpAttemptResult `json:"lastScaleUpAttemptResult,omitempty"`

	// ScaleUpErrorMessage is a short single-line description of the most
	// recent non-Enough scale-up result, suitable for surfacing to the
	// dashboard. Empty when LastScaleUpAttemptResult is Enough.
	// +optional
	ScaleUpErrorMessage string `json:"scaleUpErrorMessage,omitempty"`

	// ObservedGeneration is the metadata.generation observed when the
	// autoscaler last wrote this block. Clients may use it to confirm the
	// status is current with respect to the spec they care about.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=sbp
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.runningReplicas,selectorpath=.status.selector
// +kubebuilder:printcolumn:name="Phase",type=string,description="Overall pool phase",JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Replicas",type=integer,description="Total desired replicas",JSONPath=`.spec.replicas`
// +kubebuilder:printcolumn:name="Idle",type=integer,description="Number of idle sandboxes",JSONPath=`.status.idleReplicas`
// +kubebuilder:printcolumn:name="Unavail",type=integer,description="Unavailable idle sandboxes (NotReady)",JSONPath=`.status.unavailableIdleReplicas`
// +kubebuilder:printcolumn:name="Running",type=integer,description="Number of running sandboxes",JSONPath=`.status.runningReplicas`
// +kubebuilder:printcolumn:name="Starting",type=integer,description="Number of starting sandboxes",JSONPath=`.status.startingReplicas`
// +kubebuilder:printcolumn:name="Stopping",type=integer,description="Number of stopping sandboxes",JSONPath=`.status.stoppingReplicas`
// +kubebuilder:printcolumn:name="Failed",type=integer,description="Number of failed sandboxes",JSONPath=`.status.failedReplicas`
// +kubebuilder:printcolumn:name="Age",type=date,description="Age of the resource",JSONPath=`.metadata.creationTimestamp`

// SandboxPool is the Schema for the sandboxpools API
type SandboxPool struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of SandboxPool
	// +required
	Spec SandboxPoolSpec `json:"spec"`

	// status defines the observed state of SandboxPool
	// +optional
	Status SandboxPoolStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// SandboxPoolList contains a list of SandboxPool
type SandboxPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []SandboxPool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SandboxPool{}, &SandboxPoolList{})
}
