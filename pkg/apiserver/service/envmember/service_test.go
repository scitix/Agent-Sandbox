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

package envmember_test

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service/envmember"
	"github.com/scitix/agent-sandbox/pkg/framework"
	"github.com/scitix/agent-sandbox/pkg/framework/plugins"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

const (
	envTestNamespace = "default"
	envLocalCluster  = "local"
	testEnvName      = "env-x"
	testTemplateName = "envd-runtime"
)

func newEnvForPoolOps() *agentsv1alpha1.SandboxEnv {
	return &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testEnvName,
			Namespace: envTestNamespace,
		},
		Spec: agentsv1alpha1.SandboxEnvSpec{
			TemplateRef: agentsv1alpha1.SandboxEnvTemplateRef{Name: testTemplateName},
			Mode:        agentsv1alpha1.SandboxEnvModeWarmPool,
			Clusters: []agentsv1alpha1.EnvClusterSpec{
				{ClusterID: envLocalCluster},
			},
		},
	}
}

// newTestTemplate provides the source SandboxTemplate the member-rendering
// path needs to resolve env.Spec.TemplateRef.Name.
func newTestTemplate() *agentsv1alpha1.SandboxTemplate {
	return &agentsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: testTemplateName},
		Spec: agentsv1alpha1.SandboxTemplateSpec{
			Version: "1.0.0",
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "pause:3.10",
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "sandbox", Image: "base:v1"}},
					},
				},
			},
		},
	}
}

func memberWithResources(replicas int32) agentsv1alpha1.EnvClusterMember {
	return agentsv1alpha1.EnvClusterMember{
		Spec: agentsv1alpha1.SandboxPoolSpec{Replicas: replicas},
		Config: agentsv1alpha1.EnvClusterMemberConfig{
			InlineResources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
			},
		},
	}
}

// poolWithOwner returns a SandboxPool (in envTestNamespace) whose
// OwnerReferences include the supplied SandboxEnv name. Pass envName="" to
// produce an unowned pool.
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

func newClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("client builder: %v", err)
	}
	return cb.WithObjects(objs...).Build()
}

// newService wires a fresh service.Service backed by a fake client seeded
// with the supplied objects plus a default SandboxTemplate so the
// renderer-driven Add path can always resolve env.Spec.TemplateRef.Name.
func newService(t *testing.T, objs ...client.Object) envmember.MemberPoolService {
	t.Helper()
	objs = append(objs, newTestTemplate())
	return envmember.New(newClient(t, objs...), nil, nil, nil)
}

func TestAdd_DerivesNameAndScalingGroup(t *testing.T) {
	env := newEnvForPoolOps()
	svc := newService(t, env)

	res, err := svc.AddMember(context.Background(), envTestNamespace, testEnvName, envLocalCluster, memberWithResources(2))
	if err != nil {
		t.Fatalf("Add: %+v", err)
	}
	if res.Name != "env-x-2c8gi" {
		t.Fatalf("derived name = %q, want env-x-2c8gi", res.Name)
	}
}

func TestAdd_RejectsMissingResources(t *testing.T) {
	svc := newService(t, newEnvForPoolOps())
	_, err := svc.AddMember(context.Background(), envTestNamespace, testEnvName, envLocalCluster, agentsv1alpha1.EnvClusterMember{})
	if err == nil || err.Code != domain.ErrCodeBadRequest {
		t.Fatalf("expected BadRequest, got %+v", err)
	}
}

func TestAdd_NoLocalClusterID_503(t *testing.T) {
	svc := newService(t, newEnvForPoolOps())
	_, err := svc.AddMember(context.Background(), envTestNamespace, testEnvName, "", memberWithResources(1))
	if err == nil || err.Code != domain.ErrCodeServiceUnavailable {
		t.Fatalf("expected ServiceUnavailable, got %+v", err)
	}
}

func TestAdd_Duplicate_409(t *testing.T) {
	env := newEnvForPoolOps()
	env.Spec.Clusters[0].Members = []agentsv1alpha1.EnvClusterMember{{Name: "env-x-2c8gi"}}
	svc := newService(t, env)

	_, err := svc.AddMember(context.Background(), envTestNamespace, testEnvName, envLocalCluster, memberWithResources(1))
	if err == nil || err.Code != domain.ErrCodeConflict {
		t.Fatalf("expected Conflict, got %+v", err)
	}
}

func TestAdd_EnvNotFound_404(t *testing.T) {
	svc := newService(t)
	_, err := svc.AddMember(context.Background(), envTestNamespace, "ghost", envLocalCluster, memberWithResources(1))
	if err == nil || err.Code != domain.ErrCodeNotFound {
		t.Fatalf("expected NotFound, got %+v", err)
	}
}

// TestAdd_CandidateCarriesEmbeddedTemplate verifies the bug the user
// flagged: the pool candidate handed to plugin admission MUST carry the
// rendered EmbeddedSandboxTemplate, not an empty pod spec, so quota /
// scheduler plugins can compute resources × replicas correctly.
func TestAdd_CandidateCarriesEmbeddedTemplate(t *testing.T) {
	pl := &capturingPlugin{}
	cli := newClient(t, newEnvForPoolOps(), newTestTemplate())
	svc := envmember.New(cli, plugins.NewPluginManager(pl), nil, nil)

	_, err := svc.AddMember(context.Background(), envTestNamespace, testEnvName, envLocalCluster, memberWithResources(3))
	if err != nil {
		t.Fatalf("Add: %+v", err)
	}
	if pl.lastCreate == nil {
		t.Fatalf("PreCreatePool was never called")
	}
	cand := pl.lastCreate
	if cand.Spec.IdleImage != "pause:3.10" {
		t.Errorf("candidate.IdleImage missing (renderer didn't copy template); got %q", cand.Spec.IdleImage)
	}
	if len(cand.Spec.Template.Spec.Containers) == 0 {
		t.Fatalf("candidate.Template empty — renderer didn't materialise pod spec for admission")
	}
	got := cand.Spec.Template.Spec.Containers[0].Resources
	if got.Requests.Cpu().Cmp(resource.MustParse("2")) != 0 {
		t.Errorf("candidate.Resources.CPU = %v, want 2 (InlineResources)", got.Requests.Cpu())
	}
	if cand.Spec.Replicas != 3 {
		t.Errorf("candidate.Replicas = %d, want 3", cand.Spec.Replicas)
	}
}

func TestAdd_PropagatesPluginLabelMutation(t *testing.T) {
	pl := &capturingPlugin{
		createFn: func(p *agentsv1alpha1.SandboxPool) (bool, *domain.AppError) {
			if p.Labels == nil {
				p.Labels = map[string]string{}
			}
			p.Labels["quota.scitix.ai/reservation-id"] = "res-xyz"
			return true, nil
		},
	}
	cli := newClient(t, newEnvForPoolOps(), newTestTemplate())
	svc := envmember.New(cli, plugins.NewPluginManager(pl), nil, nil)

	_, err := svc.AddMember(context.Background(), envTestNamespace, testEnvName, envLocalCluster, memberWithResources(1))
	if err != nil {
		t.Fatalf("Add: %+v", err)
	}
	got := &agentsv1alpha1.SandboxEnv{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: envTestNamespace, Name: testEnvName}, got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	persisted := got.Spec.Clusters[0].Members[0]
	if persisted.Metadata.Labels["quota.scitix.ai/reservation-id"] != "res-xyz" {
		t.Fatalf("plugin label not propagated to member.metadata: %+v", persisted.Metadata.Labels)
	}
}

// TestAdd_PersistsEnvIdentityLabels guards the regression where the
// SandboxPool ended up with empty metadata.labels (no team/user) because
// EnvClusterMember.Metadata was typed as metav1.ObjectMeta and got pruned
// by the K8s API server in admission. The fix moves Metadata to a
// dedicated MemberMetadata struct with explicit labels/annotations
// properties so the snapshot survives round-trips.
func TestAdd_PersistsEnvIdentityLabels(t *testing.T) {
	env := newEnvForPoolOps()
	env.Labels = map[string]string{
		agentsv1alpha1.LabelTeam: "ai-infra",
		agentsv1alpha1.LabelUser: "admin",
	}
	cli := newClient(t, env, newTestTemplate())
	svc := envmember.New(cli, nil, nil, nil)

	if _, err := svc.AddMember(context.Background(), envTestNamespace, testEnvName, envLocalCluster, memberWithResources(1)); err != nil {
		t.Fatalf("Add: %+v", err)
	}

	got := &agentsv1alpha1.SandboxEnv{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: envTestNamespace, Name: testEnvName}, got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Spec.Clusters[0].Members) != 1 {
		t.Fatalf("expected one member, got %d", len(got.Spec.Clusters[0].Members))
	}
	persisted := got.Spec.Clusters[0].Members[0]
	if persisted.Metadata.Labels[agentsv1alpha1.LabelTeam] != "ai-infra" {
		t.Errorf("team identity not persisted on member.Metadata: %+v", persisted.Metadata.Labels)
	}
	if persisted.Metadata.Labels[agentsv1alpha1.LabelUser] != "admin" {
		t.Errorf("user identity not persisted on member.Metadata: %+v", persisted.Metadata.Labels)
	}
}

func TestAdd_AdmitterRejection_Bubbles(t *testing.T) {
	pl := &capturingPlugin{
		createFn: func(_ *agentsv1alpha1.SandboxPool) (bool, *domain.AppError) {
			return false, domain.NewTooManyRequests("quota exceeded", nil, nil)
		},
	}
	cli := newClient(t, newEnvForPoolOps(), newTestTemplate())
	svc := envmember.New(cli, plugins.NewPluginManager(pl), nil, nil)

	_, err := svc.AddMember(context.Background(), envTestNamespace, testEnvName, envLocalCluster, memberWithResources(1))
	if err == nil || err.Code != domain.ErrCodeTooManyRequests {
		t.Fatalf("expected TooManyRequests bubble-up, got %+v", err)
	}
}

func TestUpdate_AdjustsReplicas(t *testing.T) {
	env := newEnvForPoolOps()
	env.Spec.Clusters[0].Members = []agentsv1alpha1.EnvClusterMember{
		{
			Name:   "m1",
			Spec:   agentsv1alpha1.SandboxPoolSpec{Replicas: 1},
			Config: agentsv1alpha1.EnvClusterMemberConfig{ScalingGroup: "1c4Gi"},
		},
	}
	svc := newService(t, env)

	r := int32(5)
	res, err := svc.UpdateMember(context.Background(), envTestNamespace, testEnvName, "m1", envLocalCluster, envmember.MemberPoolPatch{Replicas: &r})
	if err != nil {
		t.Fatalf("Update: %+v", err)
	}
	if res.Spec.Replicas != 5 {
		t.Fatalf("expected replicas 5, got %d", res.Spec.Replicas)
	}
}

func TestUpdate_RejectsReplicasWhenAutoscalingOn(t *testing.T) {
	env := newEnvForPoolOps()
	env.Spec.Clusters[0].Members = []agentsv1alpha1.EnvClusterMember{
		{
			Name:   "m1",
			Spec:   agentsv1alpha1.SandboxPoolSpec{Replicas: 1},
			Config: agentsv1alpha1.EnvClusterMemberConfig{ScalingGroup: "2c8Gi"},
		},
	}
	env.Spec.Autoscaling = &agentsv1alpha1.EnvAutoscalingSpec{
		Groups: []agentsv1alpha1.EnvAutoscalingGroup{{Name: "2c8Gi", Enabled: true}},
	}
	svc := newService(t, env)

	r := int32(7)
	_, err := svc.UpdateMember(context.Background(), envTestNamespace, testEnvName, "m1", envLocalCluster, envmember.MemberPoolPatch{Replicas: &r})
	if err == nil || err.Code != domain.ErrCodeBadRequest {
		t.Fatalf("expected BadRequest, got %+v", err)
	}
	// MaxReplicas is always editable.
	mr := int32(20)
	if _, err := svc.UpdateMember(context.Background(), envTestNamespace, testEnvName, "m1", envLocalCluster, envmember.MemberPoolPatch{MaxReplicas: &mr}); err != nil {
		t.Fatalf("MaxReplicas update should be accepted, got %+v", err)
	}
}

// TestUpdate_AllowsNoopReplicasResendUnderAutoscaling guards the
// regression where Dashboard "edit member" forms (which re-submit
// every field) were rejected with "replicas is owned by the
// autoscaler" even when the value the form sent equalled the stored
// value. The intent of the rejection is to forbid manual *changes*
// while the autoscaler owns the field — not to refuse acknowledgement
// of the current value.
func TestUpdate_AllowsNoopReplicasResendUnderAutoscaling(t *testing.T) {
	env := newEnvForPoolOps()
	env.Spec.Clusters[0].Members = []agentsv1alpha1.EnvClusterMember{
		{
			Name:   "m1",
			Spec:   agentsv1alpha1.SandboxPoolSpec{Replicas: 4},
			Config: agentsv1alpha1.EnvClusterMemberConfig{ScalingGroup: "2c8Gi"},
		},
	}
	env.Spec.Autoscaling = &agentsv1alpha1.EnvAutoscalingSpec{
		Groups: []agentsv1alpha1.EnvAutoscalingGroup{{Name: "2c8Gi", Enabled: true}},
	}
	svc := newService(t, env)

	// Resend replicas=4 alongside a maxReplicas edit. Should succeed
	// even though the group has autoscaling enabled.
	same := int32(4)
	mr := int32(8)
	if _, err := svc.UpdateMember(context.Background(), envTestNamespace, testEnvName, "m1", envLocalCluster,
		envmember.MemberPoolPatch{Replicas: &same, MaxReplicas: &mr}); err != nil {
		t.Fatalf("no-op replicas resend rejected: %+v", err)
	}
	// Different replicas value is still rejected.
	diff := int32(7)
	if _, err := svc.UpdateMember(context.Background(), envTestNamespace, testEnvName, "m1", envLocalCluster,
		envmember.MemberPoolPatch{Replicas: &diff}); err == nil || err.Code != domain.ErrCodeBadRequest {
		t.Fatalf("expected BadRequest for actual replicas change, got %+v", err)
	}
}

// TestUpdate_MaxReplicasOnlySkipsAdmission guards the regression where a
// maxReplicas-only edit (autoscaling owns replicas) still ran PreUpdatePool —
// re-submitting the scheduler reservation for the unchanged replica count and
// failing on quota. maxReplicas lives on Member.Config, never on the Pool
// Spec, so the candidate Spec is unchanged and admission must be skipped.
func TestUpdate_MaxReplicasOnlySkipsAdmission(t *testing.T) {
	env := newEnvForPoolOps()
	env.Spec.Clusters[0].Members = []agentsv1alpha1.EnvClusterMember{
		{
			Name:   "m1",
			Spec:   agentsv1alpha1.SandboxPoolSpec{Replicas: 12},
			Config: agentsv1alpha1.EnvClusterMemberConfig{ScalingGroup: "1c15Gi"},
		},
	}
	pl := &capturingPlugin{}
	cli := newClient(t, env)
	svc := envmember.New(cli, plugins.NewPluginManager(pl), nil, nil)

	mr := int32(20)
	if _, err := svc.UpdateMember(context.Background(), envTestNamespace, testEnvName, "m1", envLocalCluster,
		envmember.MemberPoolPatch{MaxReplicas: &mr}); err != nil {
		t.Fatalf("maxReplicas-only update: %+v", err)
	}
	if pl.updateCalls != 0 {
		t.Fatalf("PreUpdatePool must not run for a maxReplicas-only edit, got %d calls", pl.updateCalls)
	}
	got := &agentsv1alpha1.SandboxEnv{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: envTestNamespace, Name: testEnvName}, got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	persisted := got.Spec.Clusters[0].Members[0]
	if persisted.Config.MaxReplicas == nil || *persisted.Config.MaxReplicas != 20 {
		t.Fatalf("expected persisted Config.MaxReplicas=20, got %+v", persisted.Config.MaxReplicas)
	}

	// A genuine replica change must still run admission exactly once.
	r := int32(5)
	if _, err := svc.UpdateMember(context.Background(), envTestNamespace, testEnvName, "m1", envLocalCluster,
		envmember.MemberPoolPatch{Replicas: &r}); err != nil {
		t.Fatalf("replicas update: %+v", err)
	}
	if pl.updateCalls != 1 {
		t.Fatalf("PreUpdatePool must run once for a real replica change, got %d calls", pl.updateCalls)
	}
}

// TestUpdate_MinReplicasPersistsAndValidates covers the per-member MinReplicas
// edit path: a minReplicas-only update persists onto Member.Config without
// running admission (it is Config, not Pool Spec), and min > max is rejected.
func TestUpdate_MinReplicasPersistsAndValidates(t *testing.T) {
	env := newEnvForPoolOps()
	maxR := int32(10)
	env.Spec.Clusters[0].Members = []agentsv1alpha1.EnvClusterMember{
		{
			Name:   "m1",
			Spec:   agentsv1alpha1.SandboxPoolSpec{Replicas: 4},
			Config: agentsv1alpha1.EnvClusterMemberConfig{ScalingGroup: "1c4Gi", MaxReplicas: &maxR},
		},
	}
	pl := &capturingPlugin{}
	cli := newClient(t, env)
	svc := envmember.New(cli, plugins.NewPluginManager(pl), nil, nil)

	minR := int32(2)
	if _, err := svc.UpdateMember(context.Background(), envTestNamespace, testEnvName, "m1", envLocalCluster,
		envmember.MemberPoolPatch{MinReplicas: &minR}); err != nil {
		t.Fatalf("minReplicas-only update: %+v", err)
	}
	if pl.updateCalls != 0 {
		t.Fatalf("PreUpdatePool must not run for a minReplicas-only edit, got %d calls", pl.updateCalls)
	}
	got := &agentsv1alpha1.SandboxEnv{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: envTestNamespace, Name: testEnvName}, got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if mr := got.Spec.Clusters[0].Members[0].Config.MinReplicas; mr == nil || *mr != 2 {
		t.Fatalf("expected persisted Config.MinReplicas=2, got %+v", mr)
	}

	// min > max is rejected (existing max=10).
	tooBig := int32(11)
	if _, err := svc.UpdateMember(context.Background(), envTestNamespace, testEnvName, "m1", envLocalCluster,
		envmember.MemberPoolPatch{MinReplicas: &tooBig}); err == nil || err.Code != domain.ErrCodeBadRequest {
		t.Fatalf("expected BadRequest for minReplicas>maxReplicas, got %+v", err)
	}
}

// TestAdd_DefaultReplicasFromGroupMin confirms that joining an
// autoscaling group with min > existing-siblings-total lifts the new
// member's replicas to satisfy the shortfall. With minReplicas=3 and
// no existing members, a request supplying replicas=0 (the form
// default) yields a Pool with replicas=3.
func TestAdd_DefaultReplicasFromGroupMin(t *testing.T) {
	env := newEnvForPoolOps()
	min := int32(3)
	env.Spec.Autoscaling = &agentsv1alpha1.EnvAutoscalingSpec{
		Groups: []agentsv1alpha1.EnvAutoscalingGroup{{
			// memberWithResources(...) derives ScalingGroup "2c8gi"
			// (lower-case) from its 2 CPU / 8 GiB request.
			Name:        "2c8gi",
			Enabled:     true,
			MinReplicas: &min,
		}},
	}
	svc := newService(t, env)

	pool, err := svc.AddMember(context.Background(), envTestNamespace, testEnvName, envLocalCluster, memberWithResources(0))
	if err != nil {
		t.Fatalf("Add: %+v", err)
	}
	if pool.Spec.Replicas != 3 {
		t.Fatalf("expected replicas defaulted to group min=3, got %d", pool.Spec.Replicas)
	}
}

// TestAdd_RejectsExceedingGroupMax asserts that a request whose
// explicit replicas would push the group above maxReplicas is denied
// with BadRequest rather than silently clamped.
func TestAdd_RejectsExceedingGroupMax(t *testing.T) {
	env := newEnvForPoolOps()
	env.Spec.Clusters[0].Members = []agentsv1alpha1.EnvClusterMember{{
		Name:   "existing",
		Spec:   agentsv1alpha1.SandboxPoolSpec{Replicas: 8},
		Config: agentsv1alpha1.EnvClusterMemberConfig{ScalingGroup: "2c8gi"},
	}}
	max := int32(10)
	env.Spec.Autoscaling = &agentsv1alpha1.EnvAutoscalingSpec{
		Groups: []agentsv1alpha1.EnvAutoscalingGroup{{
			Name:        "2c8gi",
			Enabled:     true,
			MaxReplicas: &max,
		}},
	}
	svc := newService(t, env)

	_, err := svc.AddMember(context.Background(), envTestNamespace, testEnvName, envLocalCluster, memberWithResources(5))
	if err == nil || err.Code != domain.ErrCodeBadRequest {
		t.Fatalf("expected BadRequest exceeding maxReplicas, got %+v", err)
	}
}

// TestAdd_CreatesScalingGroup asserts AddMember materialises the matching
// autoscaling group inline so the member's ScalingGroup always has a
// corresponding group entry. The fresh group starts disabled (manual mode).
func TestAdd_CreatesScalingGroup(t *testing.T) {
	cli := newClient(t, newEnvForPoolOps(), newTestTemplate())
	svc := envmember.New(cli, nil, nil, nil)

	if _, err := svc.AddMember(context.Background(), envTestNamespace, testEnvName, envLocalCluster, memberWithResources(1)); err != nil {
		t.Fatalf("Add: %+v", err)
	}

	env := &agentsv1alpha1.SandboxEnv{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: envTestNamespace, Name: testEnvName}, env); err != nil {
		t.Fatalf("get env: %v", err)
	}
	if env.Spec.Autoscaling == nil || len(env.Spec.Autoscaling.Groups) != 1 {
		t.Fatalf("expected exactly one autoscaling group, got %+v", env.Spec.Autoscaling)
	}
	g := env.Spec.Autoscaling.Groups[0]
	if g.Name != "2c8gi" {
		t.Fatalf("expected group %q, got %q", "2c8gi", g.Name)
	}
	if g.Enabled {
		t.Fatalf("expected auto-created group to start disabled, got enabled")
	}
}

// TestAdd_PreservesExistingScalingGroup asserts AddMember does not duplicate
// or clobber a group that already exists for the member's ScalingGroup —
// the user's min/enabled config survives.
func TestAdd_PreservesExistingScalingGroup(t *testing.T) {
	env := newEnvForPoolOps()
	min := int32(3)
	env.Spec.Autoscaling = &agentsv1alpha1.EnvAutoscalingSpec{
		Groups: []agentsv1alpha1.EnvAutoscalingGroup{{
			Name:        "2c8gi",
			Enabled:     true,
			MinReplicas: &min,
		}},
	}
	cli := newClient(t, env, newTestTemplate())
	svc := envmember.New(cli, nil, nil, nil)

	if _, err := svc.AddMember(context.Background(), envTestNamespace, testEnvName, envLocalCluster, memberWithResources(0)); err != nil {
		t.Fatalf("Add: %+v", err)
	}

	got := &agentsv1alpha1.SandboxEnv{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: envTestNamespace, Name: testEnvName}, got); err != nil {
		t.Fatalf("get env: %v", err)
	}
	if got.Spec.Autoscaling == nil || len(got.Spec.Autoscaling.Groups) != 1 {
		t.Fatalf("expected exactly one group (no duplicate), got %+v", got.Spec.Autoscaling)
	}
	g := got.Spec.Autoscaling.Groups[0]
	if !g.Enabled || g.MinReplicas == nil || *g.MinReplicas != 3 {
		t.Fatalf("expected existing group config preserved (enabled, min=3), got %+v", g)
	}
}

func TestUpdate_NotFound_404(t *testing.T) {
	svc := newService(t, newEnvForPoolOps())

	r := int32(1)
	_, err := svc.UpdateMember(context.Background(), envTestNamespace, testEnvName, "missing", envLocalCluster, envmember.MemberPoolPatch{Replicas: &r})
	if err == nil || err.Code != domain.ErrCodeNotFound {
		t.Fatalf("expected NotFound, got %+v", err)
	}
}

func TestDelete_Removes(t *testing.T) {
	env := newEnvForPoolOps()
	env.Spec.Clusters[0].Members = []agentsv1alpha1.EnvClusterMember{{Name: "m1"}, {Name: "m2"}}
	svc := newService(t, env)

	res, err := svc.DeleteMember(context.Background(), envTestNamespace, testEnvName, "m1", envLocalCluster)
	if err != nil {
		t.Fatalf("Delete: %+v", err)
	}
	if res.Name != "m1" || res.Status != "Terminating" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestDelete_NotFound_404(t *testing.T) {
	svc := newService(t, newEnvForPoolOps())
	_, err := svc.DeleteMember(context.Background(), envTestNamespace, testEnvName, "missing", envLocalCluster)
	if err == nil || err.Code != domain.ErrCodeNotFound {
		t.Fatalf("expected NotFound, got %+v", err)
	}
}

func TestDelete_SkipsAdmitterWhenPoolMissing(t *testing.T) {
	pl := &capturingPlugin{}
	env := newEnvForPoolOps()
	env.Spec.Clusters[0].Members = []agentsv1alpha1.EnvClusterMember{{Name: "m1"}}
	cli := newClient(t, env, newTestTemplate())
	svc := envmember.New(cli, plugins.NewPluginManager(pl), nil, nil)

	if _, err := svc.DeleteMember(context.Background(), envTestNamespace, testEnvName, "m1", envLocalCluster); err != nil {
		t.Fatalf("Delete: %+v", err)
	}
	if pl.deleteCalls != 0 {
		t.Fatalf("expected 0 PreDeletePool calls when Pool not materialised, got %d", pl.deleteCalls)
	}
}

func TestDelete_CallsAdmitterWhenPoolExists(t *testing.T) {
	pl := &capturingPlugin{}
	env := newEnvForPoolOps()
	env.UID = types.UID("uid-" + testEnvName)
	env.Spec.Clusters[0].Members = []agentsv1alpha1.EnvClusterMember{{Name: "p1"}}
	pool := poolWithOwner("p1", testEnvName)

	cli := newClient(t, env, pool, newTestTemplate())
	svc := envmember.New(cli, plugins.NewPluginManager(pl), nil, nil)

	if _, err := svc.DeleteMember(context.Background(), envTestNamespace, testEnvName, "p1", envLocalCluster); err != nil {
		t.Fatalf("Delete: %+v", err)
	}
	if pl.deleteCalls != 1 {
		t.Fatalf("expected 1 PreDeletePool call, got %d", pl.deleteCalls)
	}
}

func TestList_FiltersByOwnerRef(t *testing.T) {
	env := newEnvForPoolOps()
	env.UID = types.UID("uid-" + testEnvName)
	mine := poolWithOwner("p-mine", testEnvName)
	other := poolWithOwner("p-other", "env-y")
	orphan := poolWithOwner("p-orphan", "")

	cli := newClient(t, env, mine, other, orphan)
	svc := envmember.New(cli, nil, nil, nil)

	items, err := svc.ListMembers(context.Background(), envTestNamespace, testEnvName)
	if err != nil {
		t.Fatalf("List: %+v", err)
	}
	if len(items) != 1 || items[0].Name != "p-mine" {
		t.Fatalf("expected only p-mine, got %+v", items)
	}
}

func TestGet_RejectsForeignOwner(t *testing.T) {
	env := newEnvForPoolOps()
	foreign := poolWithOwner("p-foreign", "other-env")
	cli := newClient(t, env, foreign)
	svc := envmember.New(cli, nil, nil, nil)

	_, err := svc.GetMember(context.Background(), envTestNamespace, testEnvName, "p-foreign")
	if err == nil || err.Code != domain.ErrCodeNotFound {
		t.Fatalf("expected NotFound for foreign-owned pool, got %+v", err)
	}
}

// capturingPlugin records every admission call and stores the candidate
// passed to PreCreatePool so tests can assert the candidate shape.
type capturingPlugin struct {
	createCalls int
	updateCalls int
	deleteCalls int

	lastCreate *agentsv1alpha1.SandboxPool

	createFn func(p *agentsv1alpha1.SandboxPool) (bool, *domain.AppError)
	updateFn func(p *agentsv1alpha1.SandboxPool) (plugins.PoolAdmission, *domain.AppError)
	deleteFn func(p *agentsv1alpha1.SandboxPool) (bool, *domain.AppError)
}

func (*capturingPlugin) Name() string                                      { return "capturing" }
func (*capturingPlugin) Start(_ context.Context, _ framework.Handle) error { return nil }
func (*capturingPlugin) PreCreatePod(_ context.Context, _ *corev1.Pod, _ *agentsv1alpha1.SandboxPool) (bool, *domain.AppError) {
	return false, nil
}

func (a *capturingPlugin) PreCreatePool(_ context.Context, p *agentsv1alpha1.SandboxPool) (bool, *domain.AppError) {
	a.createCalls++
	a.lastCreate = p
	if a.createFn != nil {
		return a.createFn(p)
	}
	return false, nil
}
func (a *capturingPlugin) PreUpdatePool(_ context.Context, p *agentsv1alpha1.SandboxPool, _ []corev1.Pod) (plugins.PoolAdmission, *domain.AppError) {
	a.updateCalls++
	if a.updateFn != nil {
		return a.updateFn(p)
	}
	return plugins.PoolAdmission{}, nil
}
func (a *capturingPlugin) PreDeletePool(_ context.Context, p *agentsv1alpha1.SandboxPool) (bool, *domain.AppError) {
	a.deleteCalls++
	if a.deleteFn != nil {
		return a.deleteFn(p)
	}
	return false, nil
}

var _ plugins.Plugin = (*capturingPlugin)(nil)
