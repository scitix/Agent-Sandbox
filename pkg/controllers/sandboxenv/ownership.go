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
	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// localStatusMutator is invoked with a writable pointer to the local cluster
// status segment with IsLocal=true preset and ClusterID prefilled.
type localStatusMutator func(local *agentsv1alpha1.EnvClusterStatus)

// scalingGroupStatusMutator is invoked with a writable pointer to a named
// scaling group's status entry. The entry is upserted if missing so the
// autoscaler can write IdleZeroSince / LastScaleUpTime onto a group that
// status aggregation hasn't filled yet.
type scalingGroupStatusMutator func(g *agentsv1alpha1.EnvScalingGroupStatus)

// mutateLocalClusterStatus is the chokepoint for status segment mutation. A
// freshly created segment
// is kept regardless of content because the Reconciler needs to record
// time-based fields even on an empty member set.
func mutateLocalClusterStatus(env *agentsv1alpha1.SandboxEnv, localClusterID string, mutator localStatusMutator) {
	if env == nil || localClusterID == "" || mutator == nil {
		return
	}

	idx := -1
	for i := range env.Status.Clusters {
		if env.Status.Clusters[i].ClusterID == localClusterID {
			idx = i
			break
		}
	}

	if idx >= 0 {
		// Reaffirm IsLocal — defensive against a buggy Sync push.
		env.Status.Clusters[idx].IsLocal = true
		mutator(&env.Status.Clusters[idx])
		return
	}

	seg := agentsv1alpha1.EnvClusterStatus{ClusterID: localClusterID, IsLocal: true}
	mutator(&seg)
	env.Status.Clusters = append(env.Status.Clusters, seg)
}

// findLocalClusterSpec returns a copy of the local cluster spec segment, or
// the zero value when none exists. Use for read paths; mutations must go
// through mutateLocalClusterSpec.
func findLocalClusterSpec(env *agentsv1alpha1.SandboxEnv, localClusterID string) (agentsv1alpha1.EnvClusterSpec, bool) {
	if env == nil || localClusterID == "" {
		return agentsv1alpha1.EnvClusterSpec{}, false
	}
	for i := range env.Spec.Clusters {
		if env.Spec.Clusters[i].ClusterID == localClusterID {
			return *env.Spec.Clusters[i].DeepCopy(), true
		}
	}
	return agentsv1alpha1.EnvClusterSpec{}, false
}

// findScalingGroupStatus returns a copy of the named group's status entry
// or the zero value (Name == "") when none exists. Use for read paths.
func findScalingGroupStatus(env *agentsv1alpha1.SandboxEnv, groupName string) agentsv1alpha1.EnvScalingGroupStatus {
	if env == nil || groupName == "" {
		return agentsv1alpha1.EnvScalingGroupStatus{}
	}
	for i := range env.Status.ScalingGroups {
		if env.Status.ScalingGroups[i].Name == groupName {
			return *env.Status.ScalingGroups[i].DeepCopy()
		}
	}
	return agentsv1alpha1.EnvScalingGroupStatus{}
}

// mutateScalingGroupStatus is the chokepoint for per-group status mutation.
// Upserts a new entry with Name preset if one is missing — the autoscaler
// needs to record IdleZeroSince / LastScaleUpTime independent of when
// syncStatus next runs.
func mutateScalingGroupStatus(env *agentsv1alpha1.SandboxEnv, groupName string, mutator scalingGroupStatusMutator) {
	if env == nil || groupName == "" || mutator == nil {
		return
	}
	for i := range env.Status.ScalingGroups {
		if env.Status.ScalingGroups[i].Name == groupName {
			mutator(&env.Status.ScalingGroups[i])
			return
		}
	}
	seg := agentsv1alpha1.EnvScalingGroupStatus{Name: groupName}
	mutator(&seg)
	env.Status.ScalingGroups = append(env.Status.ScalingGroups, seg)
}

// hasForeignClusterSegments returns true when the Env's spec or status
// contains any cluster segment whose ClusterID differs from localClusterID.
// Used as a safety guard on delete paths in Phase 2 (Hub-merged Envs); in
// Phase 1 this should always be false.
func hasForeignClusterSegments(env *agentsv1alpha1.SandboxEnv, localClusterID string) bool {
	if env == nil {
		return false
	}
	for _, c := range env.Spec.Clusters {
		if c.ClusterID != "" && c.ClusterID != localClusterID {
			return true
		}
	}
	for _, c := range env.Status.Clusters {
		if c.ClusterID != "" && c.ClusterID != localClusterID {
			return true
		}
	}
	return false
}
