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

package envautoscaler_test

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service/envautoscaler"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ns      = "default"
	envName = "demo"
	group   = "1c4Gi"
)

func seed(t *testing.T, g agentsv1alpha1.EnvAutoscalingGroup) client.Client {
	t.Helper()
	env := &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: envName},
		Spec: agentsv1alpha1.SandboxEnvSpec{
			Autoscaling: &agentsv1alpha1.EnvAutoscalingSpec{
				Groups: []agentsv1alpha1.EnvAutoscalingGroup{g},
			},
			Clusters: []agentsv1alpha1.EnvClusterSpec{{
				ClusterID: "local",
				Members: []agentsv1alpha1.EnvClusterMember{{
					Name:   "m1",
					Config: agentsv1alpha1.EnvClusterMemberConfig{ScalingGroup: group},
				}},
			}},
		},
	}
	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("client builder: %v", err)
	}
	return cb.WithObjects(env).Build()
}

func groupOf(t *testing.T, c client.Client) agentsv1alpha1.EnvAutoscalingGroup {
	t.Helper()
	got := &agentsv1alpha1.SandboxEnv{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: envName}, got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	return got.Spec.Autoscaling.Groups[0]
}

// A group's maxReplicas is a live ceiling. Under the previous "absent means
// unchanged" reading there was no request that could take it back off: an
// operator who had capped a group at 64 could raise it or lower it, but never
// return the group to running uncapped. The form offered an empty box for
// exactly that and reported success without doing anything.
func TestUpdateGroup_OmittedBoundsAreCleared(t *testing.T) {
	c := seed(t, agentsv1alpha1.EnvAutoscalingGroup{
		Name:        group,
		Enabled:     true,
		MinReplicas: ptr.To(int32(2)),
		MaxReplicas: ptr.To(int32(64)),
	})
	svc := envautoscaler.New(c)

	if _, err := svc.UpdateAutoscalingGroup(context.Background(), ns, envName, group,
		envautoscaler.GroupPatch{Enabled: ptr.To(true)}); err != nil {
		t.Fatalf("clearing both bounds must be accepted, got %+v", err)
	}

	g := groupOf(t, c)
	if g.MaxReplicas != nil {
		t.Errorf("maxReplicas should be cleared (no ceiling), got %d", *g.MaxReplicas)
	}
	if g.MinReplicas != nil {
		t.Errorf("minReplicas should be cleared, got %d", *g.MinReplicas)
	}
	if !g.Enabled {
		t.Error("enabled was supplied as true and must survive")
	}
}

// The other direction still has to work: a supplied bound replaces.
func TestUpdateGroup_SuppliedBoundsReplace(t *testing.T) {
	c := seed(t, agentsv1alpha1.EnvAutoscalingGroup{
		Name:        group,
		Enabled:     true,
		MaxReplicas: ptr.To(int32(64)),
	})
	svc := envautoscaler.New(c)

	if _, err := svc.UpdateAutoscalingGroup(context.Background(), ns, envName, group,
		envautoscaler.GroupPatch{Enabled: ptr.To(true), MaxReplicas: ptr.To(int32(128))}); err != nil {
		t.Fatalf("update: %+v", err)
	}
	if g := groupOf(t, c); g.MaxReplicas == nil || *g.MaxReplicas != 128 {
		t.Fatalf("maxReplicas should be 128, got %v", g.MaxReplicas)
	}
}
