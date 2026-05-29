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
	"fmt"
	"sort"

	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// reconcileScalingGroups keeps env.Spec.Autoscaling.Groups in one-to-one
// correspondence with the set of ScalingGroup names referenced by members,
// in both directions:
//
//   - build missing: every member.Config.ScalingGroup that lacks a matching
//     group gets a minimal disabled group appended (CRD defaulting fills the
//     policy fields on patch). This is a safety net — AddMember already
//     creates the group inline; this covers members added by direct kubectl
//     edits or pre-dating that behaviour.
//   - delete orphans: every group no member references is removed. This is
//     the sole GC path for groups left behind when their last member is
//     deleted (DeleteMember only drops the member; the group lingers until
//     here). Removing a group discards its min/max/enabled/policy config.
//
// Referenced names are collected across ALL cluster segments, not just the
// local one, so a group still referenced by a (future Hub-synced) foreign
// member is never deleted by the local reconciler.
//
// The function is idempotent: once Groups equals the referenced set it makes
// no change, so the spec patch (which bumps generation and re-enqueues the
// Env) converges in one extra reconcile rather than looping.
func (r *SandboxEnvReconciler) reconcileScalingGroups(ctx context.Context, env *agentsv1alpha1.SandboxEnv) error {
	log := klog.FromContext(ctx)
	if !scalingGroupsNeedSync(env) {
		return nil
	}

	key := client.ObjectKeyFromObject(env)
	var synced *agentsv1alpha1.SandboxEnv
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &agentsv1alpha1.SandboxEnv{}
		if err := r.Get(ctx, key, current); err != nil {
			return err
		}
		if !scalingGroupsNeedSync(current) {
			synced = current
			return nil
		}
		base := current.DeepCopy()
		applyScalingGroupSync(current)
		if err := r.Patch(ctx, current, client.MergeFrom(base)); err != nil {
			return err
		}
		synced = current
		return nil
	}); err != nil {
		return fmt.Errorf("sync scaling groups: %w", err)
	}

	// Reflect the synced spec onto the caller's env so downstream steps
	// (status aggregation) observe the same group set.
	if synced != nil {
		env.Spec.Autoscaling = synced.Spec.Autoscaling
		log.V(1).Info("Reconciled autoscaling groups against member scaling groups")
	}
	return nil
}

// referencedScalingGroups returns the deduplicated set of non-empty
// ScalingGroup names declared by any member in any cluster segment.
func referencedScalingGroups(env *agentsv1alpha1.SandboxEnv) map[string]struct{} {
	out := map[string]struct{}{}
	if env == nil {
		return out
	}
	for ci := range env.Spec.Clusters {
		for mi := range env.Spec.Clusters[ci].Members {
			if sg := env.Spec.Clusters[ci].Members[mi].Config.ScalingGroup; sg != "" {
				out[sg] = struct{}{}
			}
		}
	}
	return out
}

// scalingGroupsNeedSync reports whether the group set diverges from the
// referenced set in either direction (missing group or orphan group).
func scalingGroupsNeedSync(env *agentsv1alpha1.SandboxEnv) bool {
	referenced := referencedScalingGroups(env)
	present := map[string]struct{}{}
	if env != nil && env.Spec.Autoscaling != nil {
		for i := range env.Spec.Autoscaling.Groups {
			present[env.Spec.Autoscaling.Groups[i].Name] = struct{}{}
		}
	}
	for name := range referenced {
		if _, ok := present[name]; !ok {
			return true // missing group
		}
	}
	for name := range present {
		if _, ok := referenced[name]; !ok {
			return true // orphan group
		}
	}
	return false
}

// applyScalingGroupSync rewrites env.Spec.Autoscaling.Groups so it contains
// exactly the referenced group names: existing groups that are still
// referenced are kept verbatim (preserving their config), orphans are
// dropped, and missing names are appended as minimal disabled groups in
// deterministic (sorted) order so repeated runs are stable.
func applyScalingGroupSync(env *agentsv1alpha1.SandboxEnv) {
	referenced := referencedScalingGroups(env)

	if len(referenced) == 0 {
		// No member references any group: drop the whole Groups list. Leave
		// the Autoscaling pointer in place (it may carry future env-wide
		// settings) but empty its Groups.
		if env.Spec.Autoscaling != nil {
			env.Spec.Autoscaling.Groups = nil
		}
		return
	}

	if env.Spec.Autoscaling == nil {
		env.Spec.Autoscaling = &agentsv1alpha1.EnvAutoscalingSpec{}
	}

	kept := make([]agentsv1alpha1.EnvAutoscalingGroup, 0, len(referenced))
	keptNames := map[string]struct{}{}
	for i := range env.Spec.Autoscaling.Groups {
		g := env.Spec.Autoscaling.Groups[i]
		if _, ok := referenced[g.Name]; ok {
			kept = append(kept, g)
			keptNames[g.Name] = struct{}{}
		}
	}

	missing := make([]string, 0)
	for name := range referenced {
		if _, ok := keptNames[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	for _, name := range missing {
		kept = append(kept, agentsv1alpha1.EnvAutoscalingGroup{Name: name})
	}

	env.Spec.Autoscaling.Groups = kept
}
