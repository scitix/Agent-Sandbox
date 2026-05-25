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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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
	return NewSandboxEnvService(cb.Build(), nil, nil, nil)
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
	input := UpdateSandboxEnvInput{
		Name:      "env-a",
		Namespace: "default",
		Autoscaling: &gen.EnvAutoscalingSpec{
			Enabled: &enabled,
			Groups:  &groups,
		},
	}
	result, err := svc.Update(context.Background(), input)
	if err != nil {
		t.Fatalf("Update: %v", err)
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
	_, err := svc.Update(context.Background(), UpdateSandboxEnvInput{
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
	_, err := svc.Update(context.Background(), UpdateSandboxEnvInput{
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
	_, err := svc.Update(context.Background(), UpdateSandboxEnvInput{
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

// poolWithOwner returns a SandboxPool (in envTestNamespace) whose
// OwnerReferences include the supplied SandboxEnv name — used to validate
// the poolToGen OwningEnv projection and the env-scoped Pool lookups.
// Pass envName="" to produce an unowned pool.
func poolWithOwner(name, envName string) *agentsv1alpha1.SandboxPool {
	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: envTestNamespace,
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
	pool := poolWithOwner("pool-a", envTestName)
	result := poolToGen(context.Background(), pool, nil)
	if result.OwningEnv == nil || *result.OwningEnv != "env-a" {
		t.Errorf("OwningEnv = %v, want env-a", result.OwningEnv)
	}
}

func TestPoolToGen_OwningEnvNilWhenNoOwnerRef(t *testing.T) {
	pool := poolWithOwner("pool-a", "")
	result := poolToGen(context.Background(), pool, nil)
	if result.OwningEnv != nil {
		t.Errorf("OwningEnv = %v, want nil", *result.OwningEnv)
	}
}

// envSyncTestSetup builds an Env + a matching SandboxTemplate + one member
// pool referencing the template, suitable for exercising SyncTemplate.
func envSyncTestSetup(t *testing.T, podImage string) (SandboxEnvService, *agentsv1alpha1.SandboxEnv) {
	t.Helper()
	env := newEnv(envTestName, "team-1", "user-1")
	env.UID = types.UID("env-uid-sync")
	env.Spec.Overrides = &agentsv1alpha1.EnvOverridesSpec{
		Image: "ghcr.io/foo:override",
	}
	tmpl := &agentsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "envd-runtime"},
		Spec: agentsv1alpha1.SandboxTemplateSpec{
			Version: "2.0.0",
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "pause:3.10",
				Template:  podTemplateWithImage(podImage),
			},
		},
	}
	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "env-a-foo",
			Namespace: envTestNamespace,
			Annotations: map[string]string{
				agentsv1alpha1.SandboxPoolTemplateNameAnnotationKey:    "envd-runtime",
				agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey: "1.0.0",
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: agentsv1alpha1.GroupVersion.String(),
				Kind:       agentsv1alpha1.SandboxEnvOwnerKind,
				Name:       env.Name,
				UID:        env.UID,
			}},
		},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			TemplateName: "envd-runtime",
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "pause:3.10",
				Template:  podTemplateWithImage("stale:v0"),
			},
		},
	}

	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("client builder: %v", err)
	}
	cli := cb.WithObjects(env, tmpl, pool).Build()
	return NewSandboxEnvService(cli, nil, nil, nil), env
}

func podTemplateWithImage(image string) *corev1.PodTemplateSpec {
	return &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "sandbox", Image: image}},
		},
	}
}

func TestSandboxEnvService_SyncTemplate_PatchesMemberPools(t *testing.T) {
	svc, env := envSyncTestSetup(t, "base:v2")
	if _, err := svc.SyncTemplate(context.Background(), envTestNamespace, env.Name); err != nil {
		t.Fatalf("SyncTemplate: %v", err)
	}
	cli := svc.(*k8sSandboxEnvService).client
	pool := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: envTestNamespace, Name: "env-a-foo"}, pool); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if pool.Spec.Template == nil || pool.Spec.Template.Spec.Containers[0].Image != "ghcr.io/foo:override" {
		t.Errorf("expected overrides image applied via SyncTemplate, got %+v", pool.Spec.Template)
	}
	if pool.Annotations[agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey] != "2.0.0" {
		t.Errorf("template-version annotation must advance, got %q", pool.Annotations[agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey])
	}
}

func TestSandboxEnvService_SyncTemplate_NotFound(t *testing.T) {
	svc := newEnvService(t)
	_, err := svc.SyncTemplate(context.Background(), "default", "ghost")
	if err == nil || err.Code != domain.ErrCodeNotFound {
		t.Fatalf("expected NotFound, got %+v", err)
	}
}

// ---------------------------------------------------------------------------
// Env-scoped Pool CRUD
// ---------------------------------------------------------------------------

const envLocalCluster = "local"

// newEnvForPoolOps returns an Env named "env-x" with an empty members slice
// on the local cluster — the canonical starting point for the AddMember
// tests. The fixed name keeps the tests readable; spin up a hand-written
// env when a different name is required.
func newEnvForPoolOps() *agentsv1alpha1.SandboxEnv {
	return &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "env-x",
			Namespace: envTestNamespace,
		},
		Spec: agentsv1alpha1.SandboxEnvSpec{
			TemplateRef: agentsv1alpha1.SandboxEnvTemplateRef{Name: "envd-runtime"},
			Mode:        agentsv1alpha1.SandboxEnvModeWarmPool,
			Clusters: []agentsv1alpha1.EnvClusterSpec{
				{ClusterID: envLocalCluster},
			},
		},
	}
}

// memberWithResources returns an EnvClusterMember whose InlineResources let
// derivePoolMember compute a "2c8Gi" key (no instanceType catalog needed).
func memberWithResources(replicas int32) agentsv1alpha1.EnvClusterMember {
	return agentsv1alpha1.EnvClusterMember{
		Replicas: replicas,
		InlineResources: &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resourceMustParse("2"),
				corev1.ResourceMemory: resourceMustParse("8Gi"),
			},
		},
	}
}

func resourceMustParse(s string) resource.Quantity {
	q, err := resource.ParseQuantity(s)
	if err != nil {
		panic(err)
	}
	return q
}

func TestSandboxEnvService_AddMemberPool_DerivesNameAndScalingGroup(t *testing.T) {
	svc := newEnvService(t, newEnvForPoolOps())

	res, err := svc.AddMemberPool(context.Background(), envTestNamespace, "env-x", envLocalCluster, memberWithResources(2))
	if err != nil {
		t.Fatalf("AddMemberPool: %+v", err)
	}
	if res.Name != "env-x-2c8Gi" {
		t.Fatalf("derived name = %q, want env-x-2c8Gi", res.Name)
	}
	if res.Spec.Replicas != 2 {
		t.Fatalf("expected replicas 2, got %d", res.Spec.Replicas)
	}
	if res.OwningEnv == nil || *res.OwningEnv != "env-x" {
		t.Fatalf("OwningEnv missing")
	}
	cli := svc.(*k8sSandboxEnvService).client
	got := &agentsv1alpha1.SandboxEnv{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: envTestNamespace, Name: "env-x"}, got); err != nil {
		t.Fatalf("Get env: %v", err)
	}
	if len(got.Spec.Clusters[0].Members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(got.Spec.Clusters[0].Members))
	}
	m := got.Spec.Clusters[0].Members[0]
	if m.Name != "env-x-2c8Gi" || m.ScalingGroup != "2c8Gi" {
		t.Fatalf("derived fields wrong: %+v", m)
	}
}

func TestSandboxEnvService_AddMemberPool_RejectsMissingResources(t *testing.T) {
	svc := newEnvService(t, newEnvForPoolOps())
	_, err := svc.AddMemberPool(context.Background(), envTestNamespace, "env-x", envLocalCluster, agentsv1alpha1.EnvClusterMember{})
	if err == nil || err.Code != domain.ErrCodeBadRequest {
		t.Fatalf("expected BadRequest, got %+v", err)
	}
}

func TestSandboxEnvService_AddMemberPool_NoLocalClusterID_503(t *testing.T) {
	svc := newEnvService(t, newEnvForPoolOps())
	_, err := svc.AddMemberPool(context.Background(), envTestNamespace, "env-x", "", memberWithResources(1))
	if err == nil || err.Code != domain.ErrCodeServiceUnavailable {
		t.Fatalf("expected ServiceUnavailable, got %+v", err)
	}
}

func TestSandboxEnvService_AddMemberPool_Duplicate_409(t *testing.T) {
	env := newEnvForPoolOps()
	env.Spec.Clusters[0].Members = []agentsv1alpha1.EnvClusterMember{{Name: "env-x-2c8Gi"}}
	svc := newEnvService(t, env)

	_, err := svc.AddMemberPool(context.Background(), envTestNamespace, "env-x", envLocalCluster, memberWithResources(1))
	if err == nil || err.Code != domain.ErrCodeConflict {
		t.Fatalf("expected Conflict, got %+v", err)
	}
}

func TestSandboxEnvService_AddMemberPool_EnvNotFound_404(t *testing.T) {
	svc := newEnvService(t)
	_, err := svc.AddMemberPool(context.Background(), envTestNamespace, "ghost", envLocalCluster, memberWithResources(1))
	if err == nil || err.Code != domain.ErrCodeNotFound {
		t.Fatalf("expected NotFound, got %+v", err)
	}
}

func TestSandboxEnvService_UpdateMemberPool_AdjustsReplicas(t *testing.T) {
	env := newEnvForPoolOps()
	env.Spec.Clusters[0].Members = []agentsv1alpha1.EnvClusterMember{
		{Name: "m1", Replicas: 1, ScalingGroup: "1c4Gi"},
	}
	svc := newEnvService(t, env)

	r := int32(5)
	res, err := svc.UpdateMemberPool(context.Background(), envTestNamespace, "env-x", "m1", envLocalCluster, MemberPoolPatch{Replicas: &r})
	if err != nil {
		t.Fatalf("UpdateMemberPool: %+v", err)
	}
	if res.Spec.Replicas != 5 {
		t.Fatalf("expected replicas 5, got %d", res.Spec.Replicas)
	}
	cli := svc.(*k8sSandboxEnvService).client
	got := &agentsv1alpha1.SandboxEnv{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: envTestNamespace, Name: "env-x"}, got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	m := got.Spec.Clusters[0].Members[0]
	if m.Replicas != 5 || m.ScalingGroup != "1c4Gi" {
		t.Fatalf("expected only replicas changed, got %+v", m)
	}
}

func TestSandboxEnvService_UpdateMemberPool_RejectsReplicasWhenAutoscalingOn(t *testing.T) {
	env := newEnvForPoolOps()
	env.Spec.Clusters[0].Members = []agentsv1alpha1.EnvClusterMember{
		{Name: "m1", Replicas: 1, ScalingGroup: "2c8Gi"},
	}
	env.Spec.Autoscaling = &agentsv1alpha1.EnvAutoscalingSpec{
		Enabled: true,
		Groups:  []agentsv1alpha1.EnvAutoscalingGroup{{Name: "2c8Gi"}},
	}
	svc := newEnvService(t, env)

	r := int32(7)
	_, err := svc.UpdateMemberPool(context.Background(), envTestNamespace, "env-x", "m1", envLocalCluster, MemberPoolPatch{Replicas: &r})
	if err == nil || err.Code != domain.ErrCodeBadRequest {
		t.Fatalf("expected BadRequest, got %+v", err)
	}

	// MaxReplicas is always editable.
	mr := int32(20)
	if _, err := svc.UpdateMemberPool(context.Background(), envTestNamespace, "env-x", "m1", envLocalCluster, MemberPoolPatch{MaxReplicas: &mr}); err != nil {
		t.Fatalf("MaxReplicas update should be accepted, got %+v", err)
	}
}

func TestSandboxEnvService_UpdateMemberPool_NotFound_404(t *testing.T) {
	svc := newEnvService(t, newEnvForPoolOps())

	r := int32(1)
	_, err := svc.UpdateMemberPool(context.Background(), envTestNamespace, "env-x", "missing", envLocalCluster, MemberPoolPatch{Replicas: &r})
	if err == nil || err.Code != domain.ErrCodeNotFound {
		t.Fatalf("expected NotFound, got %+v", err)
	}
}

func TestSandboxEnvService_DeleteMemberPool_Removes(t *testing.T) {
	env := newEnvForPoolOps()
	env.Spec.Clusters[0].Members = []agentsv1alpha1.EnvClusterMember{{Name: "m1"}, {Name: "m2"}}
	svc := newEnvService(t, env)

	res, err := svc.DeleteMemberPool(context.Background(), envTestNamespace, "env-x", "m1", envLocalCluster)
	if err != nil {
		t.Fatalf("DeleteMemberPool: %+v", err)
	}
	if res.Name != "m1" || res.Status != "Terminating" {
		t.Fatalf("unexpected result: %+v", res)
	}
	cli := svc.(*k8sSandboxEnvService).client
	got := &agentsv1alpha1.SandboxEnv{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: envTestNamespace, Name: "env-x"}, got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Spec.Clusters[0].Members) != 1 || got.Spec.Clusters[0].Members[0].Name != "m2" {
		t.Fatalf("member not removed: %+v", got.Spec.Clusters[0].Members)
	}
}

func TestSandboxEnvService_DeleteMemberPool_NotFound_404(t *testing.T) {
	svc := newEnvService(t, newEnvForPoolOps())

	_, err := svc.DeleteMemberPool(context.Background(), envTestNamespace, "env-x", "missing", envLocalCluster)
	if err == nil || err.Code != domain.ErrCodeNotFound {
		t.Fatalf("expected NotFound, got %+v", err)
	}
}

func TestSandboxEnvService_ListMemberPools_FiltersByOwnerRef(t *testing.T) {
	env := newEnvForPoolOps()
	env.UID = types.UID("uid-env-x")
	mine := poolWithOwner("p-mine", "env-x")
	other := poolWithOwner("p-other", "env-y")
	orphan := poolWithOwner("p-orphan", "")

	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("client builder: %v", err)
	}
	cli := cb.WithObjects(env, mine, other, orphan).Build()
	svc := NewSandboxEnvService(cli, nil, nil, nil)

	items, appErr := svc.ListMemberPools(context.Background(), envTestNamespace, "env-x")
	if appErr != nil {
		t.Fatalf("ListMemberPools: %+v", appErr)
	}
	if len(items) != 1 || items[0].Name != "p-mine" {
		t.Fatalf("expected only p-mine, got %+v", items)
	}
}

func TestSandboxEnvService_GetMemberPool_RejectsForeignOwner(t *testing.T) {
	env := newEnvForPoolOps()
	foreign := poolWithOwner("p-foreign", "other-env")
	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("client builder: %v", err)
	}
	cli := cb.WithObjects(env, foreign).Build()
	svc := NewSandboxEnvService(cli, nil, nil, nil)

	_, appErr := svc.GetMemberPool(context.Background(), envTestNamespace, "env-x", "p-foreign")
	if appErr == nil || appErr.Code != domain.ErrCodeNotFound {
		t.Fatalf("expected NotFound for foreign-owned pool, got %+v", appErr)
	}
}

// keep ptr import referenced even when only some tests use it.
var _ = ptr.To[int32]

// fakeAdmitter records every admission call and lets the test override the
// behaviour via callbacks. Used to verify that env-scoped pool ops fire the
// expected hook and propagate plugin mutations back to the member.
type fakeAdmitter struct {
	createCalls int
	updateCalls int
	deleteCalls int

	createFn func(p *agentsv1alpha1.SandboxPool) *domain.AppError
	updateFn func(p *agentsv1alpha1.SandboxPool) (bool, *domain.AppError)
	deleteFn func(p *agentsv1alpha1.SandboxPool) *domain.AppError
}

func (a *fakeAdmitter) AdmitCreate(_ context.Context, p *agentsv1alpha1.SandboxPool) *domain.AppError {
	a.createCalls++
	if a.createFn != nil {
		return a.createFn(p)
	}
	return nil
}
func (a *fakeAdmitter) AdmitUpdate(_ context.Context, p *agentsv1alpha1.SandboxPool, _ []corev1.Pod) (bool, *domain.AppError) {
	a.updateCalls++
	if a.updateFn != nil {
		return a.updateFn(p)
	}
	return false, nil
}
func (a *fakeAdmitter) AdmitDelete(_ context.Context, p *agentsv1alpha1.SandboxPool) *domain.AppError {
	a.deleteCalls++
	if a.deleteFn != nil {
		return a.deleteFn(p)
	}
	return nil
}

func newEnvServiceWithAdmitter(t *testing.T, admitter PoolAdmitter, envs ...*agentsv1alpha1.SandboxEnv) SandboxEnvService {
	t.Helper()
	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("client builder: %v", err)
	}
	for _, e := range envs {
		cb = cb.WithObjects(e)
	}
	return NewSandboxEnvService(cb.Build(), admitter, nil, nil)
}

func TestSandboxEnvService_AddMemberPool_RunsAdmitter(t *testing.T) {
	admitter := &fakeAdmitter{}
	svc := newEnvServiceWithAdmitter(t, admitter, newEnvForPoolOps())

	_, err := svc.AddMemberPool(context.Background(), envTestNamespace, "env-x", envLocalCluster,
		memberWithResources(1))
	if err != nil {
		t.Fatalf("AddMemberPool: %+v", err)
	}
	if admitter.createCalls != 1 {
		t.Fatalf("expected 1 AdmitCreate call, got %d", admitter.createCalls)
	}
}

func TestSandboxEnvService_AddMemberPool_AdmitterRejection_Bubbles(t *testing.T) {
	admitter := &fakeAdmitter{
		createFn: func(_ *agentsv1alpha1.SandboxPool) *domain.AppError {
			return domain.NewTooManyRequests("quota exceeded", nil, nil)
		},
	}
	svc := newEnvServiceWithAdmitter(t, admitter, newEnvForPoolOps())

	_, err := svc.AddMemberPool(context.Background(), envTestNamespace, "env-x", envLocalCluster,
		memberWithResources(1))
	if err == nil || err.Code != domain.ErrCodeTooManyRequests {
		t.Fatalf("expected TooManyRequests bubble-up, got %+v", err)
	}
}

func TestSandboxEnvService_AddMemberPool_PropagatesPluginLabelMutation(t *testing.T) {
	admitter := &fakeAdmitter{
		createFn: func(p *agentsv1alpha1.SandboxPool) *domain.AppError {
			if p.Labels == nil {
				p.Labels = map[string]string{}
			}
			p.Labels["quota.scitix.ai/reservation-id"] = "res-xyz"
			return nil
		},
	}
	svc := newEnvServiceWithAdmitter(t, admitter, newEnvForPoolOps())

	_, err := svc.AddMemberPool(context.Background(), envTestNamespace, "env-x", envLocalCluster,
		memberWithResources(1))
	if err != nil {
		t.Fatalf("AddMemberPool: %+v", err)
	}
	cli := svc.(*k8sSandboxEnvService).client
	got := &agentsv1alpha1.SandboxEnv{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: envTestNamespace, Name: "env-x"}, got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	persisted := got.Spec.Clusters[0].Members[0]
	if persisted.Labels["quota.scitix.ai/reservation-id"] != "res-xyz" {
		t.Fatalf("plugin label not propagated to member: %+v", persisted.Labels)
	}
}

func TestSandboxEnvService_DeleteMemberPool_SkipsAdmitterWhenPoolMissing(t *testing.T) {
	admitter := &fakeAdmitter{}
	env := newEnvForPoolOps()
	env.Spec.Clusters[0].Members = []agentsv1alpha1.EnvClusterMember{{Name: "m1"}}
	svc := newEnvServiceWithAdmitter(t, admitter, env)

	_, err := svc.DeleteMemberPool(context.Background(), envTestNamespace, "env-x", "m1", envLocalCluster)
	if err != nil {
		t.Fatalf("DeleteMemberPool: %+v", err)
	}
	if admitter.deleteCalls != 0 {
		t.Fatalf("expected 0 AdmitDelete calls when Pool not materialised, got %d", admitter.deleteCalls)
	}
}

func TestSandboxEnvService_DeleteMemberPool_CallsAdmitterWhenPoolExists(t *testing.T) {
	admitter := &fakeAdmitter{}
	env := newEnvForPoolOps()
	env.UID = types.UID("uid-env-x")
	env.Spec.Clusters[0].Members = []agentsv1alpha1.EnvClusterMember{{Name: "p1"}}
	pool := poolWithOwner("p1", "env-x")

	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("client builder: %v", err)
	}
	cli := cb.WithObjects(env, pool).Build()
	svc := NewSandboxEnvService(cli, admitter, nil, nil)

	_, appErr := svc.DeleteMemberPool(context.Background(), envTestNamespace, "env-x", "p1", envLocalCluster)
	if appErr != nil {
		t.Fatalf("DeleteMemberPool: %+v", appErr)
	}
	if admitter.deleteCalls != 1 {
		t.Fatalf("expected 1 AdmitDelete call, got %d", admitter.deleteCalls)
	}
}
