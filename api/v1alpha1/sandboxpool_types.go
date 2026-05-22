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
	// Can be adjusted by PoolAutoscaler when MinReplicas != MaxReplicas.
	// +kubebuilder:validation:Minimum=0
	Replicas int32 `json:"replicas"`

	// MinReplicas is the lower bound for PoolAutoscaler.
	// When MinReplicas == MaxReplicas, autoscaler is disabled (manual-only scaling).
	// +optional
	// +kubebuilder:validation:Minimum=0
	MinReplicas *int32 `json:"minReplicas,omitempty"`

	// MaxReplicas is the upper bound for PoolAutoscaler.
	// When MinReplicas == MaxReplicas, autoscaler is disabled (manual-only scaling).
	// +optional
	MaxReplicas *int32 `json:"maxReplicas,omitempty"`

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

	// Autoscaling defines the autoscaling configuration for this pool.
	// When nil or autoscaling.enabled=false, the pool is managed manually via spec.replicas.
	//
	// To be deprecated: autoscaling responsibility has moved to SandboxEnv as of the
	// SandboxEnv Phase 1 release. After a Pool is adopted by an Env (label
	// agentbox.navix.sh/owning-env set), the Pool Reconciler no longer runs its
	// own autoscaler — the value here is migrated into the Env's
	// spec.autoscaling.groups[0]. This field will be removed in the next minor
	// release once production migration completes.
	// +optional
	Autoscaling *PoolAutoscalingSpec `json:"autoscaling,omitempty"`

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

	// LastScaleUpTime is the timestamp of the most recent scale-up event triggered
	// by the PoolAutoscaler. Used to enforce scaleUpPolicy.cooldownSeconds.
	// +optional
	LastScaleUpTime *metav1.Time `json:"lastScaleUpTime,omitempty"`

	// LastScaleDownTime is the timestamp of the most recent scale-down event triggered
	// by the PoolAutoscaler. Used to enforce scaleDownPolicy.stabilizationSeconds.
	// +optional
	LastScaleDownTime *metav1.Time `json:"lastScaleDownTime,omitempty"`

	// IdleZeroSince is the timestamp when idleReplicas first dropped to zero in the
	// current continuous zero-idle window. Used to evaluate scaleUpPolicy.idleThresholdSeconds.
	// Cleared when idleReplicas > 0.
	// +optional
	IdleZeroSince *metav1.Time `json:"idleZeroSince,omitempty"`

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
