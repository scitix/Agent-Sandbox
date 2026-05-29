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
	"sort"
	"testing"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

func memberInGroup(name, group string) agentsv1alpha1.EnvClusterMember {
	return agentsv1alpha1.EnvClusterMember{
		Name:   name,
		Config: agentsv1alpha1.EnvClusterMemberConfig{ScalingGroup: group},
	}
}

func groupNames(env *agentsv1alpha1.SandboxEnv) []string {
	if env.Spec.Autoscaling == nil {
		return nil
	}
	out := make([]string, 0, len(env.Spec.Autoscaling.Groups))
	for _, g := range env.Spec.Autoscaling.Groups {
		out = append(out, g.Name)
	}
	sort.Strings(out)
	return out
}

// TestReconcileScalingGroups_BuildsMissingAndDeletesOrphans asserts the
// reconciler converges Autoscaling.Groups to exactly the set of ScalingGroups
// referenced by members: the orphan group is removed, the missing one is
// created, and a still-referenced group keeps its user config. A second pass
// is a no-op (idempotent).
func TestReconcileScalingGroups_BuildsMissingAndDeletesOrphans(t *testing.T) {
	env := envWithMembers(memberInGroup("m1", "g1"), memberInGroup("m2", "g2"))
	min := int32(2)
	env.Spec.Autoscaling = &agentsv1alpha1.EnvAutoscalingSpec{
		Groups: []agentsv1alpha1.EnvAutoscalingGroup{
			{Name: "g1", Enabled: true, MinReplicas: &min},
			{Name: "orphan", Enabled: true},
		},
	}
	r := newReconcileTestReconciler(t, env)

	if err := r.reconcileScalingGroups(context.Background(), env); err != nil {
		t.Fatalf("reconcileScalingGroups: %v", err)
	}

	// In-memory env reflects the synced set.
	if got, want := groupNames(env), []string{"g1", "g2"}; !equalStrings(got, want) {
		t.Fatalf("in-memory groups = %v, want %v", got, want)
	}
	// Persisted env matches.
	persisted := &agentsv1alpha1.SandboxEnv{}
	if err := r.Get(context.Background(), types.NamespacedName{Namespace: env.Namespace, Name: env.Name}, persisted); err != nil {
		t.Fatalf("get env: %v", err)
	}
	if got, want := groupNames(persisted), []string{"g1", "g2"}; !equalStrings(got, want) {
		t.Fatalf("persisted groups = %v, want %v", got, want)
	}
	// g1's user config survived; g2 was created disabled.
	for _, g := range persisted.Spec.Autoscaling.Groups {
		switch g.Name {
		case "g1":
			if !g.Enabled || g.MinReplicas == nil || *g.MinReplicas != 2 {
				t.Errorf("g1 config not preserved: %+v", g)
			}
		case "g2":
			if g.Enabled {
				t.Errorf("g2 should be created disabled, got %+v", g)
			}
		}
	}

	// Idempotent: nothing left to sync, second call is a no-op.
	if scalingGroupsNeedSync(persisted) {
		t.Fatalf("expected converged group set, still needs sync: %+v", persisted.Spec.Autoscaling)
	}
	if err := r.reconcileScalingGroups(context.Background(), persisted); err != nil {
		t.Fatalf("second reconcileScalingGroups: %v", err)
	}
	if got, want := groupNames(persisted), []string{"g1", "g2"}; !equalStrings(got, want) {
		t.Fatalf("groups drifted on second pass = %v, want %v", got, want)
	}
}

// TestReconcileScalingGroups_NoMembersClearsGroups asserts that once no member
// references any group, the orphan groups are all garbage-collected.
func TestReconcileScalingGroups_NoMembersClearsGroups(t *testing.T) {
	env := envWithMembers() // bare shell, no members
	env.Spec.Autoscaling = &agentsv1alpha1.EnvAutoscalingSpec{
		Groups: []agentsv1alpha1.EnvAutoscalingGroup{
			{Name: "g1", Enabled: true, MaxReplicas: ptr.To(int32(5))},
		},
	}
	r := newReconcileTestReconciler(t, env)

	if err := r.reconcileScalingGroups(context.Background(), env); err != nil {
		t.Fatalf("reconcileScalingGroups: %v", err)
	}
	if len(groupNames(env)) != 0 {
		t.Fatalf("expected all groups GC'd, got %v", groupNames(env))
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
