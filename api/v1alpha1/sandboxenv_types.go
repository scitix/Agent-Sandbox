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
	"k8s.io/apimachinery/pkg/util/intstr"
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
	// ObservedMemberStateInconsistent: member Pool references a different
	// SandboxTemplate than the Env's templateRef.name. Template *version* is
	// not part of this judgement — the cluster holds a single mutable
	// SandboxTemplate per name, so spec.version is a human-maintained label
	// rather than a resolvable revision. Whether a member has converged onto
	// the Template's current body is answered by the revision hash
	// (status.updateRevision vs the Pool's currentRevision), surfaced through
	// the TemplateConsistent condition's RolloutInProgress reason.
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

	// Overrides carries the Env-wide overrides that uniformly replace
	// fields of the referenced SandboxTemplate for every member Pool.
	// Per-Pool variations (resource multiplier, replicas, plugin metadata
	// like quota URLs) live on each EnvClusterMember instead.
	// +optional
	Overrides *EnvOverridesSpec `json:"overrides,omitempty"`
}

// EnvOverridesSpec captures the SandboxTemplate fields this Env replaces
// uniformly across every member Pool. The Env represents a single class of
// sandbox runtime (e.g. an E2B-compatible sandbox or a SWE-ReX sandbox), so
// image / startup / idle / image-creation policy are expected to be shared;
// only per-Pool resource sizing and plugin metadata vary on the Member.
type EnvOverridesSpec struct {
	// Image overrides the main container (containers[0]) image of the
	// rendered Template. Applied before any per-Member overrides.
	// +optional
	Image string `json:"image,omitempty"`

	// PodCreationImagePolicy overrides the Template's
	// spec.podCreationImagePolicy. Applied to every member Pool.
	// +optional
	// +kubebuilder:validation:Enum=PoolDefaultImage;IdleImage
	PodCreationImagePolicy PodCreationImagePolicy `json:"podCreationImagePolicy,omitempty"`

	// DefaultStartupTimeout overrides the Template's
	// spec.defaultStartupTimeout. Applied to Sandbox.Create requests that
	// don't carry an explicit startupTimeout.
	// +optional
	DefaultStartupTimeout *metav1.Duration `json:"defaultStartupTimeout,omitempty"`

	// DefaultIdleTimeout overrides the Template's spec.defaultIdleTimeout.
	// Applied to Sandboxes that don't carry an explicit idleTimeout.
	// +optional
	DefaultIdleTimeout *metav1.Duration `json:"defaultIdleTimeout,omitempty"`

	// NetworkPolicy, when set, enables sandbox egress filtering for every member
	// Pool of this Env. The operator injects a transparent filter sidecar into
	// each sandbox Pod; this policy is the Env-wide default, overridable per
	// sandbox at create time. Nil disables egress filtering (no sidecar).
	// +optional
	NetworkPolicy *SandboxNetworkPolicy `json:"networkPolicy,omitempty"`

	// UpdateStrategy is the Env-wide default rollout policy: when a member's
	// effective idle-Pod identity changes (Template edit, image / networkPolicy
	// override), whether and how fast its idle Pods are rebuilt. Per-member
	// EnvClusterMemberConfig.UpdateStrategy overrides this; see ResolveAutoUpdate
	// / ResolveMaxUnavailable for the resolution order.
	// +optional
	UpdateStrategy *EnvUpdateStrategy `json:"updateStrategy,omitempty"`
}

// EnvUpdateStrategy controls automatic rollout of member Pools when their
// rendered idle-Pod identity (revision hash) changes. The only rollout mode is
// Recreate: stale idle Pods are deleted and re-created from the new spec; Pods
// that have been claimed (Running/Starting) are never disrupted — they roll on
// the next reconcile after they return to Idle.
type EnvUpdateStrategy struct {
	// AutoUpdate toggles automatic rollout. When nil the value is inherited
	// (member → env → default true). Set false to freeze a member on its
	// current revision (e.g. to pin a fleet during an incident).
	// +optional
	AutoUpdate *bool `json:"autoUpdate,omitempty"`

	// MaxUnavailable bounds how many of a member Pool's desired idle Pods may be
	// unavailable at once during a rollout, as an absolute number or a
	// percentage of desired replicas (e.g. "20%"). Rounded down, floored at 1 so
	// small pools still make progress. When nil the value is inherited
	// (member → env → default "20%").
	// +optional
	// +kubebuilder:validation:XIntOrString
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`
}

// SandboxEnvTemplateRef points at a cluster-scoped SandboxTemplate.
type SandboxEnvTemplateRef struct {
	// Name of the SandboxTemplate (cluster-scoped).
	// +required
	Name string `json:"name"`

	// Version records the SandboxTemplate spec.version this Env was created
	// against.
	//
	// Legacy: it does not pin anything. A SandboxTemplate is a single mutable
	// cluster-scoped object — there is no version history and no way to
	// resolve an older spec.version — so every consumer resolves the Template
	// by Name alone and members always converge onto its current body. The
	// version actually in effect is reported per member in
	// status.clusters[].observedMembers[].templateVersion; read that instead
	// of this field. Retained so existing objects and manifests keep
	// round-tripping; real version pinning needs immutable per-version
	// Template objects first.
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
//
// The shape is a three-bucket split:
//
//   - Name: identity within the Env (the list map key).
//   - Metadata + Spec: server-managed snapshot of the materialised SandboxPool,
//     captured AFTER PreCreatePool admission ran at the API layer. The Env
//     Reconciler stamps these onto the live Pool verbatim — it never re-runs
//     plugin admission, so plugin side-effects (Reservation submit, scheduling
//     labels, NodeAffinity, …) survive Pool recreate / Env re-apply without
//     redoing the side-effect. **Not exposed through the REST API.** Template
//     upgrades do NOT auto-propagate into Spec; an explicit RefreshMember API
//     (Phase 2 TODO) is the way to align an existing member with a newer
//     Template revision.
//   - Config: user-declared intent (sizing, scaling-group bookkeeping, routing
//     priorities). This is the only bucket exposed through the REST API.
//     Plugins do not mutate Config — it stays equal to whatever the caller
//     supplied at AddMember/UpdateMember time so it remains a faithful
//     description of the request shape.
type EnvClusterMember struct {
	// Name is the SandboxPool's metadata.name within the Env's namespace.
	// Acts as the list map key for Members. Must equal Metadata.Name once
	// the Reconciler materialises the Pool; the Reconciler overwrites
	// Metadata.Name with Name at stamp time if they disagree.
	// +required
	Name string `json:"name"`

	// Metadata is the snapshot of the candidate Pool's mutable ObjectMeta
	// subset (Labels + Annotations) after PreCreatePool. The Reconciler
	// propagates these onto the live Pool when materialising it.
	//
	// Finalizers are intentionally NOT stored here — `SandboxPoolReconciler`
	// owns the Pool's finalizer lifecycle. Name/Namespace/UID/etc. are server
	// or Env-owned and don't belong on a per-member snapshot. Using a
	// dedicated struct (instead of metav1.ObjectMeta) avoids controller-gen
	// emitting a degenerate `type: object` schema, which K8s API server would
	// otherwise prune in admission.
	// +optional
	Metadata MemberMetadata `json:"metadata,omitempty"`

	// Spec is the snapshot of the candidate SandboxPoolSpec after
	// PreCreatePool. The Reconciler stamps the whole Spec verbatim when
	// creating the live Pool and uses equality.Semantic.DeepEqual to
	// detect drift between Spec and the live Pool on subsequent
	// reconciles, including Spec.Replicas. The Env Reconciler is the
	// sole writer of the live Pool's Replicas — both the API
	// (UpdateMember) and the Env autoscaler express their intent by
	// patching Member.Spec.Replicas here and let the Reconciler
	// propagate it.
	// +optional
	Spec SandboxPoolSpec `json:"spec,omitempty"`

	// Config carries user-declared intent: sizing (InstanceType/Multiplier
	// or InlineResources), autoscaling bookkeeping (ScalingGroup,
	// MaxReplicas), and routing priorities. Plugins do not mutate Config,
	// so it remains a faithful description of the caller's request.
	// +optional
	Config EnvClusterMemberConfig `json:"config,omitempty"`
}

// MemberMetadata is the mutable subset of a candidate SandboxPool's ObjectMeta
// that the Env Reconciler propagates onto the live Pool. It exists as a
// dedicated type (not metav1.ObjectMeta) because controller-gen emits only a
// degenerate `type: object` schema for an embedded ObjectMeta inside a
// non-root CRD field, and the K8s API server then prunes every sub-field at
// admission time — silently dropping Labels/Annotations the AddMember flow
// just wrote.
//
// Fields are deliberately limited to what survives the round-trip from
// RenderSandboxPool + PreCreatePool back onto the live Pool:
//   - Labels/Annotations: identity (team/user) + plugin-added routing keys.
//   - Finalizers are intentionally absent — SandboxPoolReconciler manages the
//     Pool's finalizer lifecycle directly.
//   - Name/Namespace/UID/ResourceVersion/etc. are server- or Env-owned and
//     don't belong on a per-member snapshot.
type MemberMetadata struct {
	// Labels are the candidate Pool's metadata.labels post-PreCreatePool.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations are the candidate Pool's metadata.annotations post-PreCreatePool.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// EnvClusterMemberConfig captures the user-declared intent for one member.
// Plugins never write to this — it stays equal to the caller-supplied value
// across the lifetime of the member.
type EnvClusterMemberConfig struct {
	// Labels are caller-supplied SandboxPool metadata.labels stamped onto
	// the rendered candidate Pool BEFORE PreCreatePool runs. Plugins
	// typically consume these for routing decisions (e.g. the
	// "quota.scitix.ai/url" label selects which ScitixQuota CR backs the
	// member). The plugin output — original + any plugin-added labels —
	// lands in Member.Metadata.Labels; Config.Labels stays equal to the
	// caller's input.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations are caller-supplied SandboxPool metadata.annotations,
	// same propagation rules as Labels.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// InstanceType references an entry in the cluster-wide InstanceType
	// catalog. Mutually informative with InlineResources: if both are set,
	// InstanceType wins and InlineResources serves as a transitional
	// record for migration.
	// +optional
	InstanceType string `json:"instanceType,omitempty"`

	// Multiplier scales InstanceType's resources. Required when
	// InstanceType is set.
	// +optional
	// +kubebuilder:validation:Minimum=0
	Multiplier int32 `json:"multiplier,omitempty"`

	// InlineResources is the Phase 1 migration escape hatch (legacy Pools
	// without an InstanceType label) AND the source of truth used by a
	// future RefreshMember API to keep resource sizing stable when the
	// underlying Template is upgraded. New Envs created via the Dashboard
	// should leave this empty and use InstanceType+Multiplier instead.
	// +optional
	InlineResources *corev1.ResourceRequirements `json:"inlineResources,omitempty"`

	// ScalingGroup names the autoscaling group this member belongs to.
	// Members in the same group must share the same effective resources
	// (= InstanceType × Multiplier or identical InlineResources). Empty
	// means the member is excluded from autoscaling.
	// +optional
	// +kubebuilder:default=default
	ScalingGroup string `json:"scalingGroup,omitempty"`

	// MinReplicas is the lower bound on this member's spec.replicas.
	// Enforced by the Env autoscaler: scale-down never shrinks this member
	// below MinReplicas. nil/0 means no per-member floor (only the group's
	// aggregate MinReplicas applies).
	// +optional
	// +kubebuilder:validation:Minimum=0
	MinReplicas *int32 `json:"minReplicas,omitempty"`

	// MaxReplicas is the upper bound on this member's spec.replicas.
	// Enforced by the Env autoscaler when distributing scale-up delta
	// across members.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MaxReplicas *int32 `json:"maxReplicas,omitempty"`

	// Priority is the canonical routing/scaling preference: lower wins.
	// Also acts as the default for ScaleUpPriority / ScaleDownPriority
	// when those are unset.
	// +optional
	Priority int32 `json:"priority,omitempty"`

	// ScaleUpPriority overrides Priority for scale-up ordering within a
	// scalingGroup. Same-value tiebreak: (clusterID, name) lexicographic.
	// When nil, EffectiveScaleUpPriority falls back to Priority.
	// Reserved for Phase 2; Phase 1 ignores it.
	// +optional
	ScaleUpPriority *int32 `json:"scaleUpPriority,omitempty"`

	// ScaleDownPriority overrides Priority for scale-down ordering: lower
	// values are retained, higher values shrink first. The value direction
	// is intentionally inverted from ScaleUpPriority so that a single
	// Priority value (lower wins) means "preferred member" in both
	// directions — preferred members scale up first AND scale down last.
	// Same-value tiebreak: oldest idle Pod first, then name lexicographic.
	// When nil, EffectiveScaleDownPriority falls back to Priority.
	// +optional
	ScaleDownPriority *int32 `json:"scaleDownPriority,omitempty"`

	// UpdateStrategy overrides the Env-wide overrides.updateStrategy for this
	// member only. Unset fields inherit from the Env default, then from the
	// hard-coded default (autoUpdate=true, maxUnavailable="20%"). See
	// ResolveAutoUpdate / ResolveMaxUnavailable.
	// +optional
	UpdateStrategy *EnvUpdateStrategy `json:"updateStrategy,omitempty"`
}

// EffectiveScaleUpPriority returns ScaleUpPriority when set, otherwise
// Priority. Use this when picking which member in a scalingGroup gets
// scale-up traffic first.
func (c EnvClusterMemberConfig) EffectiveScaleUpPriority() int32 {
	if c.ScaleUpPriority != nil {
		return *c.ScaleUpPriority
	}
	return c.Priority
}

// EffectiveScaleDownPriority returns ScaleDownPriority when set, otherwise
// Priority. Use this when picking which member in a scalingGroup shrinks
// first: HIGHER values are scaled down first (inverse of scale-up's
// "lower wins"), so that a shared Priority field expresses "preferred to
// retain" symmetrically across both directions.
func (c EnvClusterMemberConfig) EffectiveScaleDownPriority() int32 {
	if c.ScaleDownPriority != nil {
		return *c.ScaleDownPriority
	}
	return c.Priority
}

// DefaultMaxUnavailable is the rollout unavailability budget applied when
// neither the member nor the Env overrides specify one.
var DefaultMaxUnavailable = intstr.FromString("20%")

// ResolveAutoUpdate returns whether the given member auto-rolls when its
// revision changes. Resolution order: member.Config.UpdateStrategy →
// env.Spec.Overrides.UpdateStrategy → default true. A cross-field default like
// this cannot be expressed with kubebuilder markers, so it lives in code
// alongside EffectiveScaleUpPriority.
func ResolveAutoUpdate(env *SandboxEnv, member EnvClusterMember) bool {
	if s := member.Config.UpdateStrategy; s != nil && s.AutoUpdate != nil {
		return *s.AutoUpdate
	}
	if env != nil && env.Spec.Overrides != nil {
		if s := env.Spec.Overrides.UpdateStrategy; s != nil && s.AutoUpdate != nil {
			return *s.AutoUpdate
		}
	}
	return true
}

// ResolveMaxUnavailable returns the rollout unavailability budget for the given
// member. Resolution order: member.Config.UpdateStrategy →
// env.Spec.Overrides.UpdateStrategy → DefaultMaxUnavailable ("20%").
func ResolveMaxUnavailable(env *SandboxEnv, member EnvClusterMember) intstr.IntOrString {
	if s := member.Config.UpdateStrategy; s != nil && s.MaxUnavailable != nil {
		return *s.MaxUnavailable
	}
	if env != nil && env.Spec.Overrides != nil {
		if s := env.Spec.Overrides.UpdateStrategy; s != nil && s.MaxUnavailable != nil {
			return *s.MaxUnavailable
		}
	}
	return DefaultMaxUnavailable
}

// EnvAutoscalingSpec configures the Env-level autoscaler. The Enabled
// switch lives on each EnvAutoscalingGroup so groups can be toggled
// independently — a group with Enabled=false is dormant; its members'
// Pool replicas stay where the user (or other actors) put them.
type EnvAutoscalingSpec struct {
	// Groups is the list of autoscaling groups. Each group is keyed by Name
	// and toggles its own Enabled bit independently.
	// +optional
	// +listType=map
	// +listMapKey=name
	Groups []EnvAutoscalingGroup `json:"groups,omitempty"`
}

// EnvAutoscalingGroup is one Env-level autoscaling unit, applied jointly to
// every member referencing this group.
//
// +kubebuilder:validation:XValidation:rule="self.scaleUpPolicy.mode != 'Aggressive' || has(self.maxReplicas)",message="Aggressive scaleUpPolicy.mode requires maxReplicas to be set on the group — Aggressive doubles the replica count each cooldown and would otherwise grow without bound"
type EnvAutoscalingGroup struct {
	// Name matches EnvClusterMember.ScalingGroup. Required. The Env
	// rejects groups whose Name does not match the ScalingGroup of at
	// least one member — empty-group policies have no effect and would
	// confuse the autoscaler's per-group iteration.
	// +required
	Name string `json:"name"`

	// Enabled toggles the autoscaler on/off for this group. When false,
	// member Pool replicas in this scaling group are managed manually.
	// +optional
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// MinReplicas is the lower bound for the aggregate (group) replica
	// count. Defaults to 0 — set explicitly so kubectl get sbe surfaces
	// the floor instead of leaving it implicit.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=0
	MinReplicas *int32 `json:"minReplicas,omitempty"`

	// MaxReplicas is the upper bound for the aggregate (group) replica
	// count. When unset, the group has NO ceiling and grows until each
	// member's own MaxReplicas, the cluster's capacity, or external
	// quotas stop it. Aggressive scaleUpPolicy.mode REQUIRES this field
	// to be set (validated via CEL) because doubling each cooldown
	// without an upper bound is unsafe.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MaxReplicas *int32 `json:"maxReplicas,omitempty"`

	// ScaleUpPolicy controls how scale-up decisions are evaluated. The
	// API server fills every field with its declared default when the
	// caller omits it, so the persisted CR always carries an explicit,
	// inspectable value (no hidden code defaults).
	// +optional
	// +kubebuilder:default={}
	ScaleUpPolicy PoolScaleUpPolicy `json:"scaleUpPolicy"`

	// ScaleDownPolicy controls how scale-down decisions are evaluated.
	// Same defaulting contract as ScaleUpPolicy.
	// +optional
	// +kubebuilder:default={}
	ScaleDownPolicy PoolScaleDownPolicy `json:"scaleDownPolicy"`
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

	// MemberCount is the total number of member Pools, summed across every
	// cluster segment. It exists because printer columns cannot evaluate the
	// nested clusters[].members[] array. Today only the local segment is
	// observed, so it equals the local member count; once foreign segments are
	// populated it reflects the cross-cluster total.
	// +optional
	MemberCount int32 `json:"memberCount,omitempty"`

	// DesiredReplicas, RunningReplicas, IdleReplicas are env-wide rollups of
	// the per-member counts, summed across every observed member. They back
	// the printer columns (which cannot sum nested arrays) and give a single
	// at-a-glance view of capacity vs. utilisation.
	// +optional
	DesiredReplicas int32 `json:"desiredReplicas,omitempty"`
	// +optional
	RunningReplicas int32 `json:"runningReplicas,omitempty"`
	// +optional
	IdleReplicas int32 `json:"idleReplicas,omitempty"`
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

	// PendingRequests is the throttled mirror of the in-process PoolScheduler
	// claim queue length, copied from SandboxPool.Status.PendingRequests.
	// Used by Dashboard observability and (future) cross-cluster routing.
	// +optional
	PendingRequests int32 `json:"pendingRequests,omitempty"`

	// SaturatedUntil marks this member as ineligible for routing/scaling
	// until the given time. Read-only mirror of
	// SandboxPool.Status.AutoScaling.SaturatedUntil, refreshed by the Env
	// reconciler's status aggregation; the source of truth is the per-Pool
	// autoscaler. The router (EnvScheduler) holds saturated members back
	// from the primary candidate list but still tries them as fallback
	// when no fresh member can accept the request.
	// +optional
	SaturatedUntil *metav1.Time `json:"saturatedUntil,omitempty"`

	// ScalingGroup is the autoscaling group this member belongs to on its
	// owning cluster, echoed from spec for convenience. Empty when the
	// member is not in any group. Lets a cross-cluster view (where the
	// consumer does not hold the foreign cluster's spec) still attribute
	// the member to a group and link to that cluster's group detail.
	// +optional
	ScalingGroup string `json:"scalingGroup,omitempty"`

	// AutoscalingEnabled reports whether this member's ScalingGroup has the
	// autoscaler turned on in its owning cluster. Because each cluster
	// controls its own scaling independently, a same-named group may be
	// enabled in one cluster and disabled in another; this is the per-pool,
	// per-cluster truth. It disambiguates ScaleUpHeadroom == 0 (at ceiling)
	// from autoscaling being off entirely.
	// +optional
	AutoscalingEnabled bool `json:"autoscalingEnabled,omitempty"`

	// ScaleUpHeadroom estimates how many more replicas this member can still
	// add on its owning cluster before hitting the smaller of its own
	// MaxReplicas and its group's aggregate MaxReplicas (given the group's
	// current total desired). It is meaningful only when AutoscalingEnabled
	// is true:
	//   - nil  → autoscaling off, or enabled with no finite ceiling (unbounded)
	//   - 0    → enabled but already at the ceiling (cannot grow now)
	//   - >0   → enabled with this much room left
	// The value is an estimate: the group ceiling is shared across members
	// and quota/node capacity are not folded in, so treat it as advisory
	// (like idle counts, it also lags by the federation TTL for foreign
	// members).
	// +optional
	ScaleUpHeadroom *int32 `json:"scaleUpHeadroom,omitempty"`

	// UpdateRevision is the target revision hash the member Pool is rolling
	// towards, mirrored from SandboxPool.Status.UpdateRevision.
	// +optional
	UpdateRevision string `json:"updateRevision,omitempty"`

	// TemplateVersion is the SandboxTemplate spec.version the member Pool was
	// last rendered from, read off the Pool's
	// agentbox.navix.sh/template-version provenance annotation. It is an
	// observation, not a constraint: members follow the Template's current
	// body, so this reports what they actually carry. Empty for foreign
	// (cross-cluster) members — the federation payload does not carry it.
	// +optional
	TemplateVersion string `json:"templateVersion,omitempty"`

	// UpdatedReplicas is the number of the member Pool's Pods already at
	// UpdateRevision, mirrored from SandboxPool.Status.UpdatedReplicas. A
	// rollout is in progress while UpdatedReplicas < the member's replicas.
	// +optional
	UpdatedReplicas int32 `json:"updatedReplicas,omitempty"`
}

// EnvScalingGroupStatus aggregates a scalingGroup's runtime state across all
// members. Per-Pool autoscaling bookkeeping (LastScaleUpTime,
// LastScaleDownTime, IdleZeroSince, etc.) lives on
// SandboxPool.Status.AutoScaling; this struct only carries cross-member
// aggregates.
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
	// references the Env's Template by name and has finished rolling onto its
	// current revision hash.
	SandboxEnvConditionTemplateConsistent = "TemplateConsistent"
	// SandboxEnvConditionAutoscalingActive indicates the autoscaler is
	// configured, enabled, and has not stalled due to misconfiguration.
	SandboxEnvConditionAutoscalingActive = "AutoscalingActive"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=sbe
// +kubebuilder:printcolumn:name="Mode",type=string,JSONPath=`.spec.mode`
// +kubebuilder:printcolumn:name="Template",type=string,JSONPath=`.spec.templateRef.name`
// +kubebuilder:printcolumn:name="Members",type=integer,JSONPath=`.status.memberCount`
// +kubebuilder:printcolumn:name="Running",type=integer,JSONPath=`.status.runningReplicas`
// +kubebuilder:printcolumn:name="Idle",type=integer,JSONPath=`.status.idleReplicas`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
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
