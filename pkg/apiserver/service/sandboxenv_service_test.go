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
	"maps"
	"testing"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service/envcommon"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

const envTestNamespace = "default"
const envTestName = "env-a"
const envTestTemplate = "envd-runtime"

// updateEnvInput builds an update body with the fixed fields echoed back from
// the live Env — what a client gets from `--json --editable`, and now what an
// update must carry. Tests that are about something else call this so the
// contract is spelled out once instead of at every call site.
func updateEnvInput(env *agentsv1alpha1.SandboxEnv, overrides *agentsv1alpha1.EnvOverridesSpec) UpdateSandboxEnvInput {
	ref := agentsv1alpha1.SandboxEnvTemplateRef{
		Name:    env.Spec.TemplateRef.Name,
		Version: env.Spec.TemplateRef.Version,
	}
	mode := gen.UpsertSandboxEnvRequestMode(env.Spec.Mode)
	in := UpdateSandboxEnvInput{
		Name:        env.Name,
		Namespace:   env.Namespace,
		Overrides:   overrides,
		TemplateRef: &ref,
		Mode:        &mode,
	}
	if len(env.Labels) > 0 {
		labels := maps.Clone(env.Labels)
		in.Labels = &labels
	}
	if len(env.Annotations) > 0 {
		annotations := maps.Clone(env.Annotations)
		in.Annotations = &annotations
	}
	return in
}

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
			TemplateRef: agentsv1alpha1.SandboxEnvTemplateRef{Name: envTestTemplate},
			Mode:        agentsv1alpha1.SandboxEnvModeWarmPool,
			Clusters: []agentsv1alpha1.EnvClusterSpec{
				{
					ClusterID: "local",
					Members: []agentsv1alpha1.EnvClusterMember{
						{Name: name, Config: agentsv1alpha1.EnvClusterMemberConfig{ScalingGroup: "1c4Gi"}},
					},
				},
			},
			Autoscaling: &agentsv1alpha1.EnvAutoscalingSpec{
				Groups: []agentsv1alpha1.EnvAutoscalingGroup{{Name: "1c4Gi"}},
			},
		},
	}
}

func newEnvService(t *testing.T, envs ...*agentsv1alpha1.SandboxEnv) SandboxEnvService {
	t.Helper()
	svc, _ := newEnvServiceWithClient(t, envs...)
	return svc
}

// Some assertions are about objects the service writes BESIDE the Env — the
// image-pull Secret, say — which no API response mentions. Handing back the
// same fake client is how those get checked at all.
func newEnvServiceWithClient(
	t *testing.T, envs ...*agentsv1alpha1.SandboxEnv,
) (SandboxEnvService, client.Client) {
	t.Helper()
	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("client builder: %v", err)
	}
	for _, e := range envs {
		cb = cb.WithObjects(e)
	}
	c := cb.Build()
	return NewSandboxEnvService(c, nil, nil, nil, nil, nil, VolumeConfig{Enabled: true}), c
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

func TestEnvToSummary_ProjectsListShape(t *testing.T) {
	env := newEnv(envTestName, "team-1", "user-1")
	// Two scaling groups, one enabled — exercises the enabled/total counts.
	env.Spec.Autoscaling.Groups = []agentsv1alpha1.EnvAutoscalingGroup{
		{Name: "1c4Gi", Enabled: true},
		{Name: "2c8Gi", Enabled: false},
	}
	env.Status = agentsv1alpha1.SandboxEnvStatus{
		MemberCount:     2,
		DesiredReplicas: 9,
		RunningReplicas: 5,
		IdleReplicas:    4,
		Conditions: []metav1.Condition{
			{Type: agentsv1alpha1.SandboxEnvConditionReady, Status: metav1.ConditionTrue},
		},
	}

	s := envToSummary(env)

	if got := ptr.Deref(s.TemplateName, ""); got != envTestTemplate {
		t.Errorf("TemplateName = %q, want %s", got, envTestTemplate)
	}
	// The generated constant is per-enum (the same wire value appears on the
	// spec, the summary and the write body), so this names the one the summary
	// is built from rather than a shared `gen.WarmPool` that no longer exists.
	if s.Mode == nil || *s.Mode != gen.SandboxEnvSummaryModeWarmPool {
		t.Errorf("Mode = %v, want WarmPool", s.Mode)
	}
	if got := ptr.Deref(s.ScalingGroupCount, 0); got != 2 {
		t.Errorf("ScalingGroupCount = %d, want 2", got)
	}
	if got := ptr.Deref(s.AutoscalingEnabledGroupCount, 0); got != 1 {
		t.Errorf("AutoscalingEnabledGroupCount = %d, want 1", got)
	}
	if got := ptr.Deref(s.MemberCount, 0); got != 2 {
		t.Errorf("MemberCount = %d, want 2", got)
	}
	if got := ptr.Deref(s.RunningReplicas, 0); got != 5 {
		t.Errorf("RunningReplicas = %d, want 5", got)
	}
	if got := ptr.Deref(s.IdleReplicas, 0); got != 4 {
		t.Errorf("IdleReplicas = %d, want 4", got)
	}
	if !ptr.Deref(s.Ready, false) {
		t.Errorf("Ready = false, want true")
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
	if result.Spec.TemplateRef.Name != envTestTemplate {
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

// poolWithOwner returns a SandboxPool (in envTestNamespace) whose
// OwnerReferences include the supplied SandboxEnv name — used to validate
// the envcommon.PoolToGen OwningEnv projection and the env-scoped Pool lookups.
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
	result := envcommon.PoolToGen(context.Background(), pool)
	if result.OwningEnv == nil || *result.OwningEnv != "env-a" {
		t.Errorf("OwningEnv = %v, want env-a", result.OwningEnv)
	}
}

func TestPoolToGen_OwningEnvNilWhenNoOwnerRef(t *testing.T) {
	pool := poolWithOwner("pool-a", "")
	result := envcommon.PoolToGen(context.Background(), pool)
	if result.OwningEnv != nil {
		t.Errorf("OwningEnv = %v, want nil", *result.OwningEnv)
	}
}

// Update is a PUT: what the caller sends is the whole editable state, and what
// they leave out is what they want gone.
//
// The second half is the one worth a test. While an omitted field meant "leave
// unchanged", no request could clear an override that had already been set —
// the console's emptied box saved successfully and changed nothing, which reads
// as a bug in the form rather than in the contract.
func TestSandboxEnvService_Update_OverridesAreDesiredState(t *testing.T) {
	env := newEnv(envTestName, "k8s", "ylli")
	env.Spec.Overrides = &agentsv1alpha1.EnvOverridesSpec{Image: "registry.example/keep:1"}
	svc := newEnvService(t, env)

	set, appErr := svc.Update(context.Background(), updateEnvInput(env,
		&agentsv1alpha1.EnvOverridesSpec{Image: "registry.example/replaced:2"}))
	if appErr != nil {
		t.Fatalf("update: %v", appErr)
	}
	if set.Spec.Overrides == nil || ptr.Deref(set.Spec.Overrides.Image, "") != "registry.example/replaced:2" {
		t.Fatalf("overrides were not replaced wholesale: %+v", set.Spec.Overrides)
	}

	cleared, appErr := svc.Update(context.Background(), updateEnvInput(env, nil))
	if appErr != nil {
		t.Fatalf("clearing update: %v", appErr)
	}
	if cleared.Spec.Overrides != nil {
		t.Fatalf("omitting overrides should remove them, got %+v", cleared.Spec.Overrides)
	}
}

// Clearing the registry credentials has to remove the Secret, not just the
// reference to it.
//
// While there was no delete path, a password the user believed they had
// revoked went on existing in the cluster and the form reported success.
// "No longer referenced" is not "revoked", and only one of those was true.
func TestSandboxEnvService_Update_ClearingRegistryCredentialsDeletesTheSecret(t *testing.T) {
	env := newEnv(envTestName, "k8s", "ylli")
	svc, k8s := newEnvServiceWithClient(t, env)
	ctx := context.Background()

	withCreds := updateEnvInput(env, nil)
	withCreds.ImagePullSecret = &gen.ImagePullSecretInput{
		Registries: []gen.RegistryCredential{
			{Registry: "registry.example", Username: "u", Password: "p"},
		},
	}
	if _, appErr := svc.Update(ctx, withCreds); appErr != nil {
		t.Fatalf("update with credentials: %v", appErr)
	}

	name := agentsv1alpha1.EnvImagePullSecretName(envTestName)
	key := types.NamespacedName{Namespace: envTestNamespace, Name: name}
	sec := &corev1.Secret{}
	if err := k8s.Get(ctx, key, sec); err != nil {
		t.Fatalf("secret should exist after an update that supplied credentials: %v", err)
	}

	// An edit to something else echoes the keep flag back, exactly as GET
	// returned it, and the credentials survive. This is the common case: the
	// passwords cannot be read back, so without it every unrelated edit would
	// revoke them.
	keep := updateEnvInput(env, nil)
	keep.KeepImagePullSecret = true
	if _, appErr := svc.Update(ctx, keep); appErr != nil {
		t.Fatalf("update that keeps credentials: %v", appErr)
	}
	if err := k8s.Get(ctx, key, sec); err != nil {
		t.Fatalf("the keep flag should preserve the secret: %v", err)
	}

	// Dropping the flag is how "remove these credentials" is said.
	if _, appErr := svc.Update(ctx, updateEnvInput(env, nil)); appErr != nil {
		t.Fatalf("update without credentials: %v", appErr)
	}
	if err := k8s.Get(ctx, key, sec); !k8serrors.IsNotFound(err) {
		t.Fatalf("omitting credentials and the keep flag should delete the secret, got err=%v", err)
	}
}
