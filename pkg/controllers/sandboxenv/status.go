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
		byCluster[m.ClusterID] = append(byCluster[m.ClusterID], agentsv1alpha1.EnvObservedMember{
			Name:            m.MemberPool,
			State:           agentsv1alpha1.ObservedMemberStateActive,
			IdleCount:       m.Idle,
			RunningCount:    m.Running,
			DesiredReplicas: m.Desired,
			CurrentReplicas: m.Desired,
			PendingRequests: m.Pending,
		})
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

// templateConsistent returns true when the member Pool references the same
// SandboxTemplate (by name and, if pinned, version) the Env requires.
func templateConsistent(pool *agentsv1alpha1.SandboxPool, env *agentsv1alpha1.SandboxEnv) bool {
	if pool.Spec.TemplateName != env.Spec.TemplateRef.Name {
		return false
	}
	if env.Spec.TemplateRef.Version == "" {
		return true
	}
	return pool.Annotations[agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey] == env.Spec.TemplateRef.Version
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
// observed member's template matches the Env's templateRef AND no member is
// mid-rollout onto a new revision. A template-name/version mismatch
// (TemplateMismatch) takes precedence over an in-progress rollout
// (RolloutInProgress) in the reported reason.
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
