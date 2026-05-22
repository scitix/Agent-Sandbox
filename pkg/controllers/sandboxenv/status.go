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
	totalIdle := int32(0)
	totalRunning := int32(0)
	totalDesired := int32(0)
	for _, member := range localSpec.Members {
		pool := &agentsv1alpha1.SandboxPool{}
		err := r.Get(ctx, types.NamespacedName{Namespace: env.Namespace, Name: member.Name}, pool)
		switch {
		case apierrors.IsNotFound(err):
			observed = append(observed, agentsv1alpha1.EnvObservedMember{
				Name:         member.Name,
				InstanceType: member.InstanceType,
				Multiplier:   member.Multiplier,
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

		observed = append(observed, agentsv1alpha1.EnvObservedMember{
			Name:               member.Name,
			InstanceType:       member.InstanceType,
			Multiplier:         member.Multiplier,
			EffectiveResources: effectiveResources(member, pool),
			State:              state,
			IdleCount:          pool.Status.IdleReplicas,
			RunningCount:       pool.Status.RunningReplicas,
			DesiredReplicas:    pool.Spec.Replicas,
			CurrentReplicas:    pool.Spec.Replicas,
		})
		totalIdle += pool.Status.IdleReplicas
		totalRunning += pool.Status.RunningReplicas
		totalDesired += pool.Spec.Replicas
	}

	base := env.DeepCopy()
	now := metav1.Now()
	mutateLocalClusterStatus(env, r.LocalClusterID, func(local *agentsv1alpha1.EnvClusterStatus) {
		local.ObservedMembers = observed
		local.LastSnapshotTime = &now
	})

	// Group rollup (Phase 1: single group named defaultScalingGroup).
	if grp := autoscalingGroup(env, defaultScalingGroup); grp != nil || len(observed) > 0 {
		setScalingGroupStatus(env, defaultScalingGroup, totalIdle, totalRunning, totalDesired)
	}

	env.Status.LocalMemberCount = int32(len(observed))

	// Conditions
	setReadyCondition(env, observed)
	setTemplateConsistentCondition(env, observed)

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

// effectiveResources resolves the member's effective Pod resources. Phase 1
// uses the InlineResources escape hatch when set; otherwise it falls back to
// reading the member Pool's first container (which is what the Pool
// Reconciler stamped at creation time).
func effectiveResources(member agentsv1alpha1.EnvClusterMember, pool *agentsv1alpha1.SandboxPool) *corev1.ResourceRequirements {
	if member.InlineResources != nil {
		return member.InlineResources.DeepCopy()
	}
	// Phase 2 will resolve InstanceType × Multiplier here via the catalog.
	// For Phase 1 the closed-source plugin is expected to keep the Pool's
	// resources in sync with the InstanceType, so reading the Pool is a
	// good proxy.
	if len(pool.Spec.Template.Spec.Containers) > 0 {
		res := pool.Spec.EmbeddedSandboxTemplate.Template.Spec.Containers[0].Resources
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
// observed member's template matches the Env's templateRef.
func setTemplateConsistentCondition(env *agentsv1alpha1.SandboxEnv, observed []agentsv1alpha1.EnvObservedMember) {
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
