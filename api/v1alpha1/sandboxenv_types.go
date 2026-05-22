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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SandboxEnvMode controls how the Env satisfies sandbox-create requests.
// +kubebuilder:validation:Enum=WarmPool;OnDemandJob
type SandboxEnvMode string

const (
	// SandboxEnvModeWarmPool dispatches requests to one of the Env's member SandboxPools.
	// This is the only supported mode in Phase 1.
	SandboxEnvModeWarmPool SandboxEnvMode = "WarmPool"
	// SandboxEnvModeOnDemandJob creates a single-shot SandboxJob per request.
	// Reserved for Phase 3; not implemented yet.
	SandboxEnvModeOnDemandJob SandboxEnvMode = "OnDemandJob"
)

// ObservedMemberState summarises whether a member Pool can serve requests.
// +kubebuilder:validation:Enum=Active;Saturated;Missing;Inconsistent
type ObservedMemberState string

const (
	// ObservedMemberStateActive: member Pool exists and is eligible for routing/scaling.
	ObservedMemberStateActive ObservedMemberState = "Active"
	// ObservedMemberStateSaturated: member hit its maxReplicas or returned InsufficientQuota.
	ObservedMemberStateSaturated ObservedMemberState = "Saturated"
	// ObservedMemberStateMissing: member Pool no longer exists in the cluster.
	ObservedMemberStateMissing ObservedMemberState = "Missing"
	// ObservedMemberStateInconsistent: member's Template or InstanceType drifted from Env's expectation.
	ObservedMemberStateInconsistent ObservedMemberState = "Inconsistent"
)

// SandboxEnvSpec defines the desired state of SandboxEnv.
type SandboxEnvSpec struct {
	// TemplateRef binds this Env to exactly one SandboxTemplate (runtime). All
	// member Pools must reference the same Template.
	// +required
	TemplateRef SandboxEnvTemplateRef `json:"templateRef"`

	// Mode selects between WarmPool (predefined member Pools) and OnDemandJob
	// (per-request SandboxJob).
	// +required
	// +kubebuilder:default=WarmPool
	Mode SandboxEnvMode `json:"mode"`

	// Defaults supplies the InstanceType and multiplier used when a Sandbox.create
	// request does not specify them explicitly. Strongly recommended.
	// +optional
	Defaults *SandboxEnvDefaults `json:"defaults,omitempty"`

	// Clusters is the per-cluster member list. Each segment is owned exclusively
	// by the Worker whose ClusterID matches; foreign segments are read-only to
	// other Workers. Hub merges contributions from all Workers in Phase 2.
	// +optional
	// +listType=map
	// +listMapKey=clusterID
	Clusters []EnvClusterSpec `json:"clusters,omitempty"`

	// Autoscaling configures the Env-level autoscaler. When nil or
	// Autoscaling.Enabled=false, member Pool replicas are managed manually.
	// +optional
	Autoscaling *EnvAutoscalingSpec `json:"autoscaling,omitempty"`
}

// SandboxEnvTemplateRef points at a cluster-scoped SandboxTemplate.
type SandboxEnvTemplateRef struct {
	// Name of the SandboxTemplate (cluster-scoped).
	// +required
	Name string `json:"name"`

	// Version optionally pins the Env to a specific Template version. When
	// empty, the Template's current spec.version is observed and recorded in
	// status.
	// +optional
	Version string `json:"version,omitempty"`
}

// SandboxEnvDefaults captures the default instance shape for Sandbox.create
// requests that don't specify one.
type SandboxEnvDefaults struct {
	// InstanceType references an entry in the cluster-wide InstanceType catalog.
	// May be empty when the Env was migrated from a legacy SandboxPool that did
	// not carry an InstanceType label — in that case members use InlineResources.
	// +optional
	InstanceType string `json:"instanceType,omitempty"`

	// Multiplier scales the InstanceType's base resources. Must fall within the
	// InstanceType's declared [min, max] range; validated by the Env Controller.
	// +optional
	// +kubebuilder:validation:Minimum=1
	Multiplier int32 `json:"multiplier,omitempty"`
}

// EnvClusterSpec is the per-cluster portion of an Env spec.
type EnvClusterSpec struct {
	// ClusterID identifies the cluster that owns this segment. Each Worker
	// only mutates the segment matching its own ClusterID.
	// +required
	ClusterID string `json:"clusterID"`

	// Members is the list of SandboxPool members contributed by this cluster.
	// Phase 1 supports exactly one member per cluster.
	// +optional
	// +listType=map
	// +listMapKey=name
	Members []EnvClusterMember `json:"members,omitempty"`
}

// EnvClusterMember describes one SandboxPool participating in this Env.
type EnvClusterMember struct {
	// Name is the SandboxPool's metadata.name within the Env's namespace.
	// Acts as the list map key for Members.
	// +required
	Name string `json:"name"`

	// InstanceType references an entry in the cluster-wide InstanceType catalog.
	// Mutually informative with InlineResources: if both are set, InstanceType
	// wins and InlineResources serves as a transitional record for migration.
	// +optional
	InstanceType string `json:"instanceType,omitempty"`

	// Multiplier scales InstanceType's resources. Required when InstanceType is set.
	// +optional
	// +kubebuilder:validation:Minimum=0
	Multiplier int32 `json:"multiplier,omitempty"`

	// InlineResources is the Phase 1 migration escape hatch: when the source
	// SandboxPool predates InstanceType labelling, the Pool's PodSpec resources
	// are copied here verbatim. New Envs created via the Dashboard should leave
	// this empty and use InstanceType+Multiplier instead.
	// +optional
	InlineResources *corev1.ResourceRequirements `json:"inlineResources,omitempty"`

	// ScalingGroup names the autoscaling group this member belongs to. Members
	// in the same group must share the same effective resources (= InstanceType
	// × Multiplier or identical InlineResources). Empty means the member is
	// excluded from autoscaling.
	// +optional
	// +kubebuilder:default=default
	ScalingGroup string `json:"scalingGroup,omitempty"`

	// MaxReplicas is the upper bound on this member's spec.replicas. Reserved
	// for Phase 2 multi-member routing — enforced by the Env autoscaler when
	// distributing scale-up delta across members. Phase 1 ignores it.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MaxReplicas *int32 `json:"maxReplicas,omitempty"`

	// Priority controls routing order: lower priority is preferred. Reserved
	// for Phase 2; Phase 1 ignores it.
	// +optional
	Priority int32 `json:"priority,omitempty"`

	// ScaleUpPriority controls scale-up order within a scalingGroup: lower
	// values are scaled up first. Same-value tiebreak: (clusterID, name)
	// lexicographic. Reserved for Phase 2; Phase 1 ignores it.
	// +optional
	ScaleUpPriority int32 `json:"scaleUpPriority,omitempty"`

	// ScaleDownPriority controls scale-down order: lower values shrink first.
	// Reserved for Phase 2; Phase 1 ignores it.
	// +optional
	ScaleDownPriority int32 `json:"scaleDownPriority,omitempty"`
}

// EnvAutoscalingSpec configures the Env-level autoscaler.
type EnvAutoscalingSpec struct {
	// Enabled toggles the autoscaler on/off. When false, member Pool replicas
	// are managed manually.
	// +optional
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// Groups is the list of autoscaling groups. In Phase 1 there is exactly
	// one group named "default" populated from the source Pool's autoscaling.
	// +optional
	// +listType=map
	// +listMapKey=name
	Groups []EnvAutoscalingGroup `json:"groups,omitempty"`
}

// EnvAutoscalingGroup is one Env-level autoscaling unit, applied jointly to
// every member referencing this group.
type EnvAutoscalingGroup struct {
	// Name matches EnvClusterMember.ScalingGroup. Required.
	// +required
	Name string `json:"name"`

	// MinReplicas is the lower bound for the aggregate (group) replica count.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MinReplicas *int32 `json:"minReplicas,omitempty"`

	// MaxReplicas is the upper bound for the aggregate (group) replica count.
	// Phase 1 (single member) treats this as the member's effective ceiling.
	// +optional
	MaxReplicas *int32 `json:"maxReplicas,omitempty"`

	// ScaleUpPolicy reuses the existing SandboxPool scale-up semantics
	// (Conservative/Default/Aggressive modes, cooldown, idle-threshold).
	// +optional
	ScaleUpPolicy *PoolScaleUpPolicy `json:"scaleUpPolicy,omitempty"`

	// ScaleDownPolicy reuses the existing SandboxPool scale-down semantics.
	// +optional
	ScaleDownPolicy *PoolScaleDownPolicy `json:"scaleDownPolicy,omitempty"`
}

// SandboxEnvStatus is the observed state of SandboxEnv.
type SandboxEnvStatus struct {
	// Conditions surfaces high-level Env health signals (Ready,
	// TemplateConsistent, AutoscalingActive, …).
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Clusters carries the per-cluster observed state. Worker writes only the
	// segment with IsLocal=true; other segments are populated by Hub Sync.
	// +optional
	// +listType=map
	// +listMapKey=clusterID
	Clusters []EnvClusterStatus `json:"clusters,omitempty"`

	// ScalingGroups aggregates idle/running counts per scalingGroup across all
	// members (across clusters when remote segments are populated by Sync).
	// +optional
	// +listType=map
	// +listMapKey=name
	ScalingGroups []EnvScalingGroupStatus `json:"scalingGroups,omitempty"`

	// PendingRequests is the number of Sandbox.create requests held by
	// EnvScheduler when no member could accept them (typically because all
	// members hit Saturated). Drives the EnvScaleUpPending signal.
	// +optional
	PendingRequests int32 `json:"pendingRequests,omitempty"`

	// LocalMemberCount caches the length of the local cluster's Members for
	// use in printer columns (which can't easily evaluate nested arrays).
	// +optional
	LocalMemberCount int32 `json:"localMemberCount,omitempty"`
}

// EnvClusterStatus is the per-cluster observed state.
type EnvClusterStatus struct {
	// ClusterID matches the spec's ClusterID for the same segment.
	// +required
	ClusterID string `json:"clusterID"`

	// IsLocal is true on the Worker that owns this cluster's Pools. Used to
	// gate writes: only IsLocal=true segments are mutated by the local Env
	// Reconciler.
	// +optional
	IsLocal bool `json:"isLocal,omitempty"`

	// ObservedMembers reports per-member runtime state (idle/running/desired,
	// effective resources, member state).
	// +optional
	// +listType=map
	// +listMapKey=name
	ObservedMembers []EnvObservedMember `json:"observedMembers,omitempty"`

	// LastScaleUpTime records the latest cluster-local scale-up event. Used
	// by the Env autoscaler for cooldown enforcement.
	// +optional
	LastScaleUpTime *metav1.Time `json:"lastScaleUpTime,omitempty"`

	// LastScaleDownTime records the latest cluster-local scale-down event.
	// +optional
	LastScaleDownTime *metav1.Time `json:"lastScaleDownTime,omitempty"`

	// IdleZeroSince records when the aggregated idle count for this cluster
	// first dropped to zero in the current continuous-zero window. Cleared
	// when idle > 0.
	// +optional
	IdleZeroSince *metav1.Time `json:"idleZeroSince,omitempty"`

	// LastSnapshotTime records when this segment was last updated. For
	// IsLocal=true: write time by the local Reconciler. For IsLocal=false:
	// arrival time of the Hub Sync push.
	// +optional
	LastSnapshotTime *metav1.Time `json:"lastSnapshotTime,omitempty"`
}

// EnvObservedMember reports per-member runtime state.
type EnvObservedMember struct {
	// Name matches the spec member's Name and is the list map key.
	// +required
	Name string `json:"name"`

	// InstanceType / Multiplier are echoed from spec for convenience.
	// +optional
	InstanceType string `json:"instanceType,omitempty"`
	// +optional
	Multiplier int32 `json:"multiplier,omitempty"`

	// EffectiveResources is the resolved resource request/limit per Pod
	// (= InstanceType.resources × Multiplier, or InlineResources verbatim).
	// +optional
	EffectiveResources *corev1.ResourceRequirements `json:"effectiveResources,omitempty"`

	// State summarises whether the member can currently serve requests.
	// +optional
	State ObservedMemberState `json:"state,omitempty"`

	// IdleCount, RunningCount are mirrored from SandboxPool.status to surface
	// a single Env-level view to the Dashboard.
	// +optional
	IdleCount int32 `json:"idleCount,omitempty"`
	// +optional
	RunningCount int32 `json:"runningCount,omitempty"`

	// DesiredReplicas is the most recent value the Env autoscaler patched onto
	// the member Pool's spec.replicas.
	// +optional
	DesiredReplicas int32 `json:"desiredReplicas,omitempty"`

	// CurrentReplicas is the value last observed on the Pool spec.
	// +optional
	CurrentReplicas int32 `json:"currentReplicas,omitempty"`
}

// EnvScalingGroupStatus aggregates a scalingGroup's runtime state across all
// members.
type EnvScalingGroupStatus struct {
	// Name matches the autoscaling group's Name and is the list map key.
	// +required
	Name string `json:"name"`

	// TotalIdle / TotalRunning / TotalDesired aggregate across members.
	// +optional
	TotalIdle int32 `json:"totalIdle,omitempty"`
	// +optional
	TotalRunning int32 `json:"totalRunning,omitempty"`
	// +optional
	TotalDesired int32 `json:"totalDesired,omitempty"`
}

// Condition type constants for SandboxEnv.
const (
	// SandboxEnvConditionReady indicates all members are Active.
	SandboxEnvConditionReady = "Ready"
	// SandboxEnvConditionTemplateConsistent indicates every member Pool
	// references the same Template name (and version, if pinned).
	SandboxEnvConditionTemplateConsistent = "TemplateConsistent"
	// SandboxEnvConditionAutoscalingActive indicates the autoscaler is
	// configured, enabled, and has not stalled due to misconfiguration.
	SandboxEnvConditionAutoscalingActive = "AutoscalingActive"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=sbe
// +kubebuilder:printcolumn:name="Mode",type=string,JSONPath=`.spec.mode`
// +kubebuilder:printcolumn:name="Members",type=integer,JSONPath=`.status.localMemberCount`
// +kubebuilder:printcolumn:name="Pending",type=integer,JSONPath=`.status.pendingRequests`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// SandboxEnv is the Schema for the sandboxenvs API.
type SandboxEnv struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of SandboxEnv
	// +required
	Spec SandboxEnvSpec `json:"spec"`

	// status defines the observed state of SandboxEnv
	// +optional
	Status SandboxEnvStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// SandboxEnvList contains a list of SandboxEnv.
type SandboxEnvList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []SandboxEnv `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SandboxEnv{}, &SandboxEnvList{})
}
