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

package service

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

const envTestNamespace = "default"
const envTestName = "env-a"

func newEnv(name, team, user string) *agentsv1alpha1.SandboxEnv {
	return &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: envTestNamespace,
			Labels: map[string]string{
				agentsv1alpha1.LabelTeam: team,
				agentsv1alpha1.LabelUser: user,
			},
		},
		Spec: agentsv1alpha1.SandboxEnvSpec{
			TemplateRef: agentsv1alpha1.SandboxEnvTemplateRef{Name: "envd-runtime"},
			Mode:        agentsv1alpha1.SandboxEnvModeWarmPool,
			Clusters: []agentsv1alpha1.EnvClusterSpec{
				{
					ClusterID: "local",
					Members: []agentsv1alpha1.EnvClusterMember{
						{Name: name, ScalingGroup: "1c4Gi"},
					},
				},
			},
			Autoscaling: &agentsv1alpha1.EnvAutoscalingSpec{
				Enabled: false,
				Groups:  []agentsv1alpha1.EnvAutoscalingGroup{{Name: "1c4Gi"}},
			},
		},
	}
}

func newEnvService(t *testing.T, envs ...*agentsv1alpha1.SandboxEnv) SandboxEnvService {
	t.Helper()
	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("client builder: %v", err)
	}
	for _, e := range envs {
		cb = cb.WithObjects(e)
	}
	return NewSandboxEnvService(cb.Build())
}

func TestSandboxEnvService_List_FiltersByTeamAndUser(t *testing.T) {
	svc := newEnvService(t,
		newEnv(envTestName, "team-1", "user-1"),
		newEnv("env-b", "team-1", "user-2"),
		newEnv("env-c", "team-2", "user-1"),
	)
	items, err := svc.List(context.Background(), "default", "team-1", "user-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 env, got %d", len(items))
	}
	if items[0].Name != envTestName {
		t.Errorf("Name = %s, want env-a", items[0].Name)
	}
}

func TestSandboxEnvService_List_NoFilterReturnsAll(t *testing.T) {
	svc := newEnvService(t,
		newEnv(envTestName, "team-1", "user-1"),
		newEnv("env-b", "team-1", "user-2"),
	)
	items, err := svc.List(context.Background(), "default", "", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 envs, got %d", len(items))
	}
	if items[0].Name != envTestName || items[1].Name != "env-b" {
		t.Errorf("unexpected sort order: %v", []string{items[0].Name, items[1].Name})
	}
}

func TestSandboxEnvService_Get_NotFound(t *testing.T) {
	svc := newEnvService(t)
	_, err := svc.Get(context.Background(), "default", "ghost")
	if err == nil || err.Code != domain.ErrCodeNotFound {
		t.Fatalf("expected NotFound, got %+v", err)
	}
}

func TestSandboxEnvService_Get_ProjectsSpec(t *testing.T) {
	svc := newEnvService(t, newEnv(envTestName, "team-1", "user-1"))
	result, err := svc.Get(context.Background(), envTestNamespace, envTestName)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if result.Spec.TemplateRef.Name != "envd-runtime" {
		t.Errorf("TemplateRef.Name = %s", result.Spec.TemplateRef.Name)
	}
	if string(result.Spec.Mode) != "WarmPool" {
		t.Errorf("Mode = %s", result.Spec.Mode)
	}
	if result.Spec.Clusters == nil || len(*result.Spec.Clusters) != 1 {
		t.Fatalf("expected 1 cluster spec, got %+v", result.Spec.Clusters)
	}
	cluster := (*result.Spec.Clusters)[0]
	if cluster.ClusterID != "local" {
		t.Errorf("ClusterID = %s", cluster.ClusterID)
	}
	if cluster.Members == nil || len(*cluster.Members) != 1 {
		t.Fatalf("expected 1 member, got %+v", cluster.Members)
	}
	if (*cluster.Members)[0].Name != "env-a" {
		t.Errorf("Member name = %s", (*cluster.Members)[0].Name)
	}
}

func TestSandboxEnvService_UpdateAutoscaling_Persists(t *testing.T) {
	svc := newEnvService(t, newEnv(envTestName, "team-1", "user-1"))

	enabled := true
	maxR := int32(20)
	mode := gen.Default
	cooldown := int32(60)
	groups := []gen.EnvAutoscalingGroup{{
		Name:        "1c4Gi",
		MaxReplicas: ptr.To(maxR),
		ScaleUpPolicy: &gen.PoolScaleUpPolicy{
			Mode:            &mode,
			CooldownSeconds: ptr.To(cooldown),
		},
	}}
	input := UpdateEnvAutoscalingInput{
		Name:      "env-a",
		Namespace: "default",
		Autoscaling: &gen.EnvAutoscalingSpec{
			Enabled: &enabled,
			Groups:  &groups,
		},
	}
	result, err := svc.UpdateAutoscaling(context.Background(), input)
	if err != nil {
		t.Fatalf("UpdateAutoscaling: %v", err)
	}
	if result.Spec.Autoscaling == nil || result.Spec.Autoscaling.Enabled == nil || !*result.Spec.Autoscaling.Enabled {
		t.Errorf("Enabled not persisted: %+v", result.Spec.Autoscaling)
	}
	if result.Spec.Autoscaling.Groups == nil || len(*result.Spec.Autoscaling.Groups) != 1 {
		t.Fatalf("Groups not persisted: %+v", result.Spec.Autoscaling.Groups)
	}
	g := (*result.Spec.Autoscaling.Groups)[0]
	if g.MaxReplicas == nil || *g.MaxReplicas != 20 {
		t.Errorf("MaxReplicas not persisted: %+v", g.MaxReplicas)
	}
}

func TestSandboxEnvService_UpdateAutoscaling_NotFound(t *testing.T) {
	svc := newEnvService(t)
	_, err := svc.UpdateAutoscaling(context.Background(), UpdateEnvAutoscalingInput{
		Name:        "ghost",
		Namespace:   "default",
		Autoscaling: &gen.EnvAutoscalingSpec{},
	})
	if err == nil || err.Code != domain.ErrCodeNotFound {
		t.Fatalf("expected NotFound, got %+v", err)
	}
}

func TestSandboxEnvService_UpdateAutoscaling_RejectsInvalidMode(t *testing.T) {
	svc := newEnvService(t, newEnv(envTestName, "team-1", "user-1"))
	bogus := gen.PoolScaleUpPolicyMode("Bogus")
	groups := []gen.EnvAutoscalingGroup{{
		Name: "1c4Gi",
		ScaleUpPolicy: &gen.PoolScaleUpPolicy{
			Mode: &bogus,
		},
	}}
	_, err := svc.UpdateAutoscaling(context.Background(), UpdateEnvAutoscalingInput{
		Name:      "env-a",
		Namespace: "default",
		Autoscaling: &gen.EnvAutoscalingSpec{
			Groups: &groups,
		},
	})
	if err == nil || err.Code != domain.ErrCodeBadRequest {
		t.Fatalf("expected BadRequest, got %+v", err)
	}
}

func TestSandboxEnvService_UpdateAutoscaling_RejectsEmptyGroupName(t *testing.T) {
	svc := newEnvService(t, newEnv(envTestName, "team-1", "user-1"))
	groups := []gen.EnvAutoscalingGroup{{Name: ""}}
	_, err := svc.UpdateAutoscaling(context.Background(), UpdateEnvAutoscalingInput{
		Name:      "env-a",
		Namespace: "default",
		Autoscaling: &gen.EnvAutoscalingSpec{
			Groups: &groups,
		},
	})
	if err == nil || err.Code != domain.ErrCodeBadRequest {
		t.Fatalf("expected BadRequest, got %+v", err)
	}
}

// poolWithOwner returns a SandboxPool whose OwnerReferences include the
// supplied SandboxEnv name — used to validate the poolToGen OwningEnv
// projection.
func poolWithOwner(name, ns, envName string) *agentsv1alpha1.SandboxPool {
	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: agentsv1alpha1.SandboxPoolSpec{Replicas: 1},
	}
	if envName != "" {
		pool.OwnerReferences = []metav1.OwnerReference{{
			APIVersion: agentsv1alpha1.GroupVersion.String(),
			Kind:       agentsv1alpha1.SandboxEnvOwnerKind,
			Name:       envName,
			UID:        types.UID("uid-" + envName),
		}}
	}
	return pool
}

func TestPoolToGen_SetsOwningEnvFromOwnerRef(t *testing.T) {
	pool := poolWithOwner("pool-a", envTestNamespace, envTestName)
	result := poolToGen(context.Background(), pool, nil)
	if result.OwningEnv == nil || *result.OwningEnv != "env-a" {
		t.Errorf("OwningEnv = %v, want env-a", result.OwningEnv)
	}
}

func TestPoolToGen_OwningEnvNilWhenNoOwnerRef(t *testing.T) {
	pool := poolWithOwner("pool-a", "default", "")
	result := poolToGen(context.Background(), pool, nil)
	if result.OwningEnv != nil {
		t.Errorf("OwningEnv = %v, want nil", *result.OwningEnv)
	}
}
