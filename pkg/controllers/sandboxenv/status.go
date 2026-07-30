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

package sandboxenv

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// syncStatus refreshes the Env's local cluster status segment from the
// observed state of its member SandboxPools. Foreign segments are not touched.
//
// Phase 1: there is at most one member; the function tolerates zero (no
// observation yet) and writes empty ObservedMembers when the member is
// missing.
func (r *SandboxEnvReconciler) syncStatus(ctx context.Context, env *agentsv1alpha1.SandboxEnv) error {
	localSpec, _ := findLocalClusterSpec(env, r.LocalClusterID)

	observed := make([]agentsv1alpha1.EnvObservedMember, 0, len(localSpec.Members))
	// Member name → its spec, for the post-loop autoscaling headroom pass
	// (which needs each member's group + MaxReplicas). Indexed to keep stable
	// pointers into the slice.
	memberByName := make(map[string]*agentsv1alpha1.EnvClusterMember, len(localSpec.Members))
	for i := range localSpec.Members {
		memberByName[localSpec.Members[i].Name] = &localSpec.Members[i]
	}
	// Per-scaling-group rollup. Each member contributes to the group named by
	// member.ScalingGroup. Empty group names are skipped — those entries are
	// legacy / pre-migration and the autoscaler ignores them anyway.
	type groupTotals struct{ idle, running, desired int32 }
	byGroup := map[string]*groupTotals{}
	// Env-wide rollup across every member regardless of scaling group, backing
	// the top-level status scalars (and the printer columns built on them).
	var envIdle, envRunning, envDesired int32
	// rolloutInProgress is set when any member Pool has not yet converged onto
	// its target revision; drives the TemplateConsistent condition's reason.
	rolloutInProgress := false
	for _, member := range localSpec.Members {
		pool := &agentsv1alpha1.SandboxPool{}
		err := r.Get(ctx, types.NamespacedName{Namespace: env.Namespace, Name: member.Name}, pool)
		switch {
		case apierrors.IsNotFound(err):
			observed = append(observed, agentsv1alpha1.EnvObservedMember{
				Name:         member.Name,
				InstanceType: member.Config.InstanceType,
				Multiplier:   member.Config.Multiplier,
				State:        agentsv1alpha1.ObservedMemberStateMissing,
			})
			continue
		case err != nil:
			return err
		}

		state := agentsv1alpha1.ObservedMemberStateActive
		if !templateConsistent(pool, env) {
			state = agentsv1alpha1.ObservedMemberStateInconsistent
		}

		om := agentsv1alpha1.EnvObservedMember{
			Name:               member.Name,
			InstanceType:       member.Config.InstanceType,
			Multiplier:         member.Config.Multiplier,
			EffectiveResources: effectiveResources(member, pool),
			State:              state,
			IdleCount:          pool.Status.IdleReplicas,
			RunningCount:       pool.Status.RunningReplicas,
			DesiredReplicas:    pool.Spec.Replicas,
			CurrentReplicas:    pool.Spec.Replicas,
			PendingRequests:    pool.Status.PendingRequests,
			UpdateRevision:     pool.Status.UpdateRevision,
			UpdatedReplicas:    pool.Status.UpdatedReplicas,
			TemplateVersion:    pool.Annotations[agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey],
		}
		// A member is mid-rollout while its Pods straddle revisions (or all sit
		// on an older single revision): CurrentRevision lags UpdateRevision.
		if pool.Spec.Replicas > 0 && pool.Status.UpdateRevision != "" &&
			pool.Status.CurrentRevision != pool.Status.UpdateRevision {
			rolloutInProgress = true
		}
		// SaturatedUntil is derived for the router's convenience. The
		// per-Pool autoscaler records LastScaleUpAttemptTime +
		// LastScaleUpAttemptResult on Pool.Status.AutoScaling; here
		// we project those into a saturation end time using the
		// group's SaturationCooldownSeconds. This lets EnvScheduler
		// keep its existing "compare an end timestamp to now" filter
		// without reaching into Pool status semantics.
		om.SaturatedUntil = deriveSaturatedUntil(env, pool, &member)
		observed = append(observed, om)
		envIdle += pool.Status.IdleReplicas
		envRunning += pool.Status.RunningReplicas
		envDesired += pool.Spec.Replicas
		if member.Config.ScalingGroup != "" {
			g, ok := byGroup[member.Config.ScalingGroup]
			if !ok {
				g = &groupTotals{}
				byGroup[member.Config.ScalingGroup] = g
			}
			g.idle += pool.Status.IdleReplicas
			g.running += pool.Status.RunningReplicas
			g.desired += pool.Spec.Replicas
		}
	}

	// Second pass: annotate each member with its autoscaling group + scale-up
	// headroom. Deferred until byGroup is complete because per-member headroom
	// is bounded by the group's aggregate MaxReplicas vs the group's total
	// desired across all its members.
	for i := range observed {
		om := &observed[i]
		member := memberByName[om.Name]
		if member == nil {
			continue
		}
		om.ScalingGroup = member.Config.ScalingGroup
		groupDesired := int32(0)
		if g, ok := byGroup[member.Config.ScalingGroup]; ok {
			groupDesired = g.desired
		}
		enabled, headroom := memberScaleUpHeadroom(env, member, om.DesiredReplicas, groupDesired)
		om.AutoscalingEnabled = enabled
		// nil ScaleUpHeadroom means "off, or enabled-but-unbounded"; a set
		// value (incl. 0 = at ceiling) means enabled with a finite estimate.
		if enabled && headroom >= 0 {
			v := headroom
			om.ScaleUpHeadroom = &v
		}
	}

	base := env.DeepCopy()
	now := metav1.Now()
	mutateLocalClusterStatus(env, r.LocalClusterID, func(local *agentsv1alpha1.EnvClusterStatus) {
		local.ObservedMembers = observed
		local.LastSnapshotTime = &now
	})

	// Mirror the cross-cluster capacity view (same-named Envs in other
	// clusters, learned over the federation channel) into the non-local status
	// segments so the federated routing input is visible via kubectl. Top-level
	// rollups stay local-only to preserve their existing meaning.
	r.mirrorForeignSegments(env, now)

	// Group rollup by member.ScalingGroup. Per-Pool autoscaling
	// bookkeeping lives on SandboxPool.Status.AutoScaling, so the group
	// status here only carries cross-member aggregates.
	env.Status.ScalingGroups = env.Status.ScalingGroups[:0]
	for name, totals := range byGroup {
		setScalingGroupStatus(env, name, totals.idle, totals.running, totals.desired)
	}

	env.Status.MemberCount = int32(len(observed))
	env.Status.IdleReplicas = envIdle
	env.Status.RunningReplicas = envRunning
	env.Status.DesiredReplicas = envDesired

	// Conditions
	setReadyCondition(env, observed)
	setTemplateConsistentCondition(env, observed, rolloutInProgress)

	if equality.Semantic.DeepEqual(base.Status, env.Status) {
		return nil
	}
	if err := r.Status().Patch(ctx, env, client.MergeFrom(base)); err != nil {
		if apierrors.IsConflict(err) {
			return nil
		}
		klog.ErrorS(err, "Env status patch failed",
			"env", env.Namespace+"/"+env.Name)
		return err
	}
	return nil
}

// mirrorForeignSegments rebuilds env.Status.Clusters so it holds the local
// segment (already populated by mutateLocalClusterStatus) plus one read-only
// segment per other cluster that reports a same-named Env over the federation
// channel. Foreign segments carry IsLocal=false and the observed member idle/
// running/desired counts, giving a human-visible cross-cluster capacity view.
// When federation is unconfigured or reports nothing, only the local segment
// remains.
func (r *SandboxEnvReconciler) mirrorForeignSegments(env *agentsv1alpha1.SandboxEnv, now metav1.Time) {
	if r.Federation == nil {
		return
	}
	members := r.Federation.ForeignMembers(env.Namespace, env.Name)

	byCluster := map[string][]agentsv1alpha1.EnvObservedMember{}
	var order []string
	for _, m := range members {
		if _, ok := byCluster[m.ClusterID]; !ok {
			order = append(order, m.ClusterID)
		}
		om := agentsv1alpha1.EnvObservedMember{
			Name:               m.MemberPool,
			State:              agentsv1alpha1.ObservedMemberStateActive,
			IdleCount:          m.Idle,
			RunningCount:       m.Running,
			DesiredReplicas:    m.Desired,
			CurrentReplicas:    m.Desired,
			PendingRequests:    m.Pending,
			ScalingGroup:       m.ScalingGroup,
			AutoscalingEnabled: m.AutoscalingEnabled,
		}
		// Capacity carries the foreign cluster's scale-up headroom: -1 =
		// enabled-but-unbounded (leave nil), >=0 = finite estimate (0 = at
		// ceiling). Only meaningful when autoscaling is enabled there.
		if m.AutoscalingEnabled && m.Capacity >= 0 {
			v := m.Capacity
			om.ScaleUpHeadroom = &v
		}
		byCluster[m.ClusterID] = append(byCluster[m.ClusterID], om)
	}

	// Keep the local segment; replace all foreign segments with fresh ones.
	out := make([]agentsv1alpha1.EnvClusterStatus, 0, 1+len(order))
	for i := range env.Status.Clusters {
		if env.Status.Clusters[i].ClusterID == r.LocalClusterID {
			out = append(out, env.Status.Clusters[i])
		}
	}
	for _, cid := range order {
		out = append(out, agentsv1alpha1.EnvClusterStatus{
			ClusterID:        cid,
			IsLocal:          false,
			ObservedMembers:  byCluster[cid],
			LastSnapshotTime: &now,
		})
	}
	env.Status.Clusters = out
}

// setScalingGroupStatus upserts the named group's totals in
// env.Status.ScalingGroups. Phase 1 always touches the single
// "default" group; Phase 2 will iterate over all members' ScalingGroup
// assignments.
func setScalingGroupStatus(env *agentsv1alpha1.SandboxEnv, name string, totalIdle, totalRunning, totalDesired int32) {
	for i := range env.Status.ScalingGroups {
		if env.Status.ScalingGroups[i].Name == name {
			env.Status.ScalingGroups[i].TotalIdle = totalIdle
			env.Status.ScalingGroups[i].TotalRunning = totalRunning
			env.Status.ScalingGroups[i].TotalDesired = totalDesired
			return
		}
	}
	env.Status.ScalingGroups = append(env.Status.ScalingGroups, agentsv1alpha1.EnvScalingGroupStatus{
		Name:         name,
		TotalIdle:    totalIdle,
		TotalRunning: totalRunning,
		TotalDesired: totalDesired,
	})
}

// deriveSaturatedUntil computes the router-friendly saturation end
// timestamp from the per-Pool autoscaler's last attempt state plus the
// owning scaling group's SaturationCooldownSeconds. Returns nil when:
//   - the autoscaler has not yet recorded an attempt;
//   - the last attempt was Enough (no saturation);
//   - the Pool's group is unknown or its policy disables saturation
//     cooldown (SaturationCooldownSeconds == 0);
//   - the computed end time is already in the past (cooldown elapsed).
//
// The returned pointer is freshly allocated so the caller can store it
// on EnvObservedMember without aliasing.
func deriveSaturatedUntil(env *agentsv1alpha1.SandboxEnv, pool *agentsv1alpha1.SandboxPool, member *agentsv1alpha1.EnvClusterMember) *metav1.Time {
	if pool.Status.AutoScaling == nil ||
		pool.Status.AutoScaling.LastScaleUpAttemptTime == nil {
		return nil
	}
	if !isSaturatingResult(pool.Status.AutoScaling.LastScaleUpAttemptResult) {
		return nil
	}
	cooldown := saturationCooldownForMember(env, member)
	if cooldown <= 0 {
		return nil
	}
	until := pool.Status.AutoScaling.LastScaleUpAttemptTime.Add(cooldown)
	if !until.After(time.Now()) {
		return nil
	}
	t := metav1.NewTime(until)
	return &t
}

// isSaturatingResult mirrors the autoscaler's check so the Env-derived
// saturation hint stays in lockstep with the Pool decision logic.
// Kept tiny + standalone to avoid an import cycle between sandboxenv
// and the autoscalingstate decision package.
func isSaturatingResult(r agentsv1alpha1.PoolScaleUpAttemptResult) bool {
	switch r {
	case agentsv1alpha1.PoolScaleUpAttemptInsufficient,
		agentsv1alpha1.PoolScaleUpAttemptJustRight,
		agentsv1alpha1.PoolScaleUpAttemptFailed:
		return true
	default:
		return false
	}
}

// saturationCooldownForMember returns the configured saturation
// cooldown for the member's scaling group, or 0 when the group is
// disabled, unknown, or autoscaling isn't configured on the Env.
func saturationCooldownForMember(env *agentsv1alpha1.SandboxEnv, member *agentsv1alpha1.EnvClusterMember) time.Duration {
	if env.Spec.Autoscaling == nil || member.Config.ScalingGroup == "" {
		return 0
	}
	for i := range env.Spec.Autoscaling.Groups {
		g := &env.Spec.Autoscaling.Groups[i]
		if g.Name != member.Config.ScalingGroup {
			continue
		}
		if g.ScaleUpPolicy.SaturationCooldownSeconds <= 0 {
			return 0
		}
		return time.Duration(g.ScaleUpPolicy.SaturationCooldownSeconds) * time.Second
	}
	return 0
}

// headroomUnbounded is the sentinel scale-up headroom for an autoscaling
// group with no finite ceiling — it can grow without a computable limit.
const headroomUnbounded int32 = -1

// memberScaleUpHeadroom reports, for a single member, whether its scaling
// group has autoscaling enabled on this cluster and how much scale-up room
// remains. headroom semantics:
//   - enabled == false                → headroom 0 (ignored; the member cannot autoscale)
//   - enabled, headroomUnbounded (-1)  → enabled with no finite ceiling
//   - enabled, >= 0                    → finite remaining replicas (0 = at ceiling)
//
// The estimate takes the smaller of the member's own MaxReplicas room
// (max − its desired) and the group's aggregate MaxReplicas room
// (group max − group total desired). A nil cap on either side is treated as
// unbounded on that side. It is intentionally approximate: the group ceiling
// is shared across members and quota/node capacity are not folded in.
func memberScaleUpHeadroom(env *agentsv1alpha1.SandboxEnv, member *agentsv1alpha1.EnvClusterMember, memberDesired, groupDesiredTotal int32) (enabled bool, headroom int32) {
	if env.Spec.Autoscaling == nil || member.Config.ScalingGroup == "" {
		return false, 0
	}
	var g *agentsv1alpha1.EnvAutoscalingGroup
	for i := range env.Spec.Autoscaling.Groups {
		if env.Spec.Autoscaling.Groups[i].Name == member.Config.ScalingGroup {
			g = &env.Spec.Autoscaling.Groups[i]
			break
		}
	}
	if g == nil || !g.Enabled {
		return false, 0
	}

	memberRoom := headroomUnbounded
	if member.Config.MaxReplicas != nil {
		if r := *member.Config.MaxReplicas - memberDesired; r > 0 {
			memberRoom = r
		} else {
			memberRoom = 0
		}
	}
	groupRoom := headroomUnbounded
	if g.MaxReplicas != nil {
		if r := *g.MaxReplicas - groupDesiredTotal; r > 0 {
			groupRoom = r
		} else {
			groupRoom = 0
		}
	}
	switch {
	case memberRoom == headroomUnbounded && groupRoom == headroomUnbounded:
		return true, headroomUnbounded
	case memberRoom == headroomUnbounded:
		return true, groupRoom
	case groupRoom == headroomUnbounded:
		return true, memberRoom
	case memberRoom < groupRoom:
		return true, memberRoom
	default:
		return true, groupRoom
	}
}

// effectiveResources resolves the member's effective Pod resources. Phase 1
// uses the InlineResources escape hatch when set; otherwise it falls back to
// reading the member Pool's first container (which is what the Pool
// Reconciler stamped at creation time).
func effectiveResources(member agentsv1alpha1.EnvClusterMember, pool *agentsv1alpha1.SandboxPool) *corev1.ResourceRequirements {
	if member.Config.InlineResources != nil {
		return member.Config.InlineResources.DeepCopy()
	}
	// Phase 2 will resolve InstanceType × Multiplier here via the catalog.
	// For Phase 1 the closed-source plugin is expected to keep the Pool's
	// resources in sync with the InstanceType, so reading the Pool is a
	// good proxy.
	if len(pool.Spec.Template.Spec.Containers) > 0 {
		res := pool.Spec.Template.Spec.Containers[0].Resources
		return res.DeepCopy()
	}
	return nil
}

// templateConsistent returns true when the member Pool references the
// SandboxTemplate the Env requires, compared by name only.
//
// spec.templateRef.version deliberately takes no part: a SandboxTemplate is a
// single mutable object with no version history, so an older spec.version
// cannot be resolved or served, and auto-update converges members onto the
// Template's current body regardless of what the field says. Comparing it
// would report drift that no reconcile can ever clear — and it would miss real
// drift whenever a Template body changes without a version bump. Convergence
// onto the current body is judged by revision hash instead (see the
// CurrentRevision/UpdateRevision check in syncStatus, surfaced as the
// TemplateConsistent condition's RolloutInProgress reason).
func templateConsistent(pool *agentsv1alpha1.SandboxPool, env *agentsv1alpha1.SandboxEnv) bool {
	return pool.Spec.TemplateName == env.Spec.TemplateRef.Name
}

// setReadyCondition marks the Env Ready iff every observed member is Active.
func setReadyCondition(env *agentsv1alpha1.SandboxEnv, observed []agentsv1alpha1.EnvObservedMember) {
	status := metav1.ConditionTrue
	reason := "AllMembersActive"
	message := "all members are Active"
	if len(observed) == 0 {
		status = metav1.ConditionFalse
		reason = "NoMembers"
		message = "no member Pools in the local cluster segment"
	} else {
		for _, m := range observed {
			if m.State != agentsv1alpha1.ObservedMemberStateActive {
				status = metav1.ConditionFalse
				reason = string(m.State)
				message = "at least one member is not Active"
				break
			}
		}
	}
	setCondition(env, agentsv1alpha1.SandboxEnvConditionReady, status, reason, message)
}

// setTemplateConsistentCondition marks TemplateConsistent True iff every
// observed member references the Env's Template by name AND no member is
// mid-rollout onto a new revision. A template-name mismatch (TemplateMismatch)
// takes precedence over an in-progress rollout (RolloutInProgress) in the
// reported reason. Template versions are not compared — see
// templateConsistent.
func setTemplateConsistentCondition(env *agentsv1alpha1.SandboxEnv, observed []agentsv1alpha1.EnvObservedMember, rolloutInProgress bool) {
	status := metav1.ConditionTrue
	reason := "TemplatesMatch"
	message := "all members reference " + env.Spec.TemplateRef.Name
	for _, m := range observed {
		if m.State == agentsv1alpha1.ObservedMemberStateInconsistent {
			status = metav1.ConditionFalse
			reason = "TemplateMismatch"
			message = "at least one member references a different SandboxTemplate"
			break
		}
	}
	if status == metav1.ConditionTrue && rolloutInProgress {
		status = metav1.ConditionFalse
		reason = "RolloutInProgress"
		message = "at least one member is rolling onto a new template revision"
	}
	setCondition(env, agentsv1alpha1.SandboxEnvConditionTemplateConsistent, status, reason, message)
}

// setCondition upserts a Condition entry preserving LastTransitionTime when
// the status value doesn't change.
func setCondition(env *agentsv1alpha1.SandboxEnv, conditionType string, status metav1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	for i := range env.Status.Conditions {
		if env.Status.Conditions[i].Type == conditionType {
			cond := &env.Status.Conditions[i]
			if cond.Status != status {
				cond.LastTransitionTime = now
			}
			cond.Status = status
			cond.Reason = reason
			cond.Message = message
			return
		}
	}
	env.Status.Conditions = append(env.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
	})
}
