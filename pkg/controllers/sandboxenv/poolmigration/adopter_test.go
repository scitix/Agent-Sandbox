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

package poolmigration

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/framework/providers/instancetype"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const (
	testNamespace = "default"
	testCluster   = "local"
	testPoolName  = "pool-a"
	// testDerivedKey is the resource-key value derived from newPool()'s
	// container resources (2 CPU / 4 GiB). Used as the expected
	// ScalingGroup in adoption + migration tests.
	testDerivedKey = "2c4Gi"
)

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("clientgoscheme: %v", err)
	}
	if err := agentsv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("agentsv1alpha1 add: %v", err)
	}
	return s
}

// newPool builds a SandboxPool with a single container that requests a known
// resource shape (2 CPU / 4Gi mem) so adoption has something to put into
// InlineResources or round-trip into (InstanceType, Multiplier).
func newPool() *agentsv1alpha1.SandboxPool {
	return &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testPoolName,
			Namespace: testNamespace,
			Labels: map[string]string{
				agentsv1alpha1.LabelTeam: "t1",
				agentsv1alpha1.LabelUser: "u1",
			},
		},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			Replicas:     3,
			TemplateName: "tmpl-1",
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				Template: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "sandbox",
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("2"),
										corev1.ResourceMemory: resource.MustParse("4Gi"),
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// newReconciler wires a PoolAdoptionReconciler around the supplied seed
// objects using the Noop InstanceType provider. All current tests exercise the
// InlineResources fallback path; if a future test needs a real provider, take
// it as a parameter then.
func newReconciler(t *testing.T, seed ...client.Object) *PoolAdoptionReconciler {
	t.Helper()
	scheme := newScheme(t)
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(seed...).
		Build()
	return &PoolAdoptionReconciler{
		Client:         c,
		Scheme:         scheme,
		LocalClusterID: testCluster,
		InstanceTypes:  instancetype.NewNoop(),
	}
}

func reconcileOnce(t *testing.T, r *PoolAdoptionReconciler) {
	t.Helper()
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: testPoolName},
	})
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Scenario A: fresh adoption
// ---------------------------------------------------------------------------

func TestReconcile_A_FreshAdoption(t *testing.T) {
	pool := newPool()
	r := newReconciler(t, pool)

	reconcileOnce(t, r)

	env := &agentsv1alpha1.SandboxEnv{}
	if err := r.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testPoolName}, env); err != nil {
		t.Fatalf("Env was not created: %v", err)
	}
	if env.Spec.TemplateRef.Name != "tmpl-1" {
		t.Errorf("Env.Spec.TemplateRef.Name = %q, want tmpl-1", env.Spec.TemplateRef.Name)
	}
	if env.Spec.Mode != agentsv1alpha1.SandboxEnvModeWarmPool {
		t.Errorf("Env.Spec.Mode = %q, want WarmPool", env.Spec.Mode)
	}
	if got := env.Labels[agentsv1alpha1.LabelTeam]; got != "t1" {
		t.Errorf("team label = %q, want t1", got)
	}
	if len(env.Spec.Clusters) != 1 || env.Spec.Clusters[0].ClusterID != testCluster {
		t.Fatalf("expected exactly one local cluster segment, got %+v", env.Spec.Clusters)
	}
	if len(env.Spec.Clusters[0].Members) != 1 || env.Spec.Clusters[0].Members[0].Name != testPoolName {
		t.Fatalf("expected one member named %q, got %+v", testPoolName, env.Spec.Clusters[0].Members)
	}
	if env.Spec.Clusters[0].Members[0].InlineResources == nil {
		t.Errorf("expected InlineResources to be set (Noop provider), got nil")
	}

	freshPool := &agentsv1alpha1.SandboxPool{}
	if err := r.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testPoolName}, freshPool); err != nil {
		t.Fatalf("re-Get pool: %v", err)
	}
	if !agentsv1alpha1.HasEnvOwner(freshPool) {
		t.Fatalf("expected pool to carry SandboxEnv OwnerReference, got %+v", freshPool.OwnerReferences)
	}

	// Idempotency: a second reconcile must be a no-op (no duplicate member, no
	// duplicate owner ref).
	reconcileOnce(t, r)
	envAfter := &agentsv1alpha1.SandboxEnv{}
	_ = r.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testPoolName}, envAfter)
	if len(envAfter.Spec.Clusters[0].Members) != 1 {
		t.Errorf("members duplicated on second reconcile: %+v", envAfter.Spec.Clusters[0].Members)
	}
	poolAfter := &agentsv1alpha1.SandboxPool{}
	_ = r.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testPoolName}, poolAfter)
	if len(poolAfter.OwnerReferences) != 1 {
		t.Errorf("owner refs duplicated on second reconcile: %+v", poolAfter.OwnerReferences)
	}
}

// ---------------------------------------------------------------------------
// Scenario B: partial-failure recovery — Env already exists (e.g. earlier
// reconcile created Env then crashed before stamping the Pool).
// ---------------------------------------------------------------------------

func TestReconcile_B_PartialFailureRecovery(t *testing.T) {
	pool := newPool()
	preExistingEnv := &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testPoolName,
			Namespace: testNamespace,
			UID:       "uid-pre-existing",
		},
		Spec: agentsv1alpha1.SandboxEnvSpec{
			TemplateRef: agentsv1alpha1.SandboxEnvTemplateRef{Name: "tmpl-1"},
			Mode:        agentsv1alpha1.SandboxEnvModeWarmPool,
		},
	}
	r := newReconciler(t, pool, preExistingEnv)

	reconcileOnce(t, r)

	envAfter := &agentsv1alpha1.SandboxEnv{}
	if err := r.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testPoolName}, envAfter); err != nil {
		t.Fatalf("get env: %v", err)
	}
	if envAfter.UID != preExistingEnv.UID {
		t.Errorf("Env was recreated (UID changed) instead of reused: was %q, got %q", preExistingEnv.UID, envAfter.UID)
	}
	if len(envAfter.Spec.Clusters) != 1 || len(envAfter.Spec.Clusters[0].Members) != 1 {
		t.Fatalf("expected one member added to existing Env, got clusters=%+v", envAfter.Spec.Clusters)
	}

	poolAfter := &agentsv1alpha1.SandboxPool{}
	_ = r.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testPoolName}, poolAfter)
	if !agentsv1alpha1.HasEnvOwner(poolAfter) {
		t.Fatalf("expected owner ref on Pool after recovery, got %+v", poolAfter.OwnerReferences)
	}
	// And the ref UID must match the existing Env, not some new one.
	matched := false
	for _, ref := range poolAfter.OwnerReferences {
		if ref.Kind == agentsv1alpha1.SandboxEnvOwnerKind && ref.UID == preExistingEnv.UID {
			matched = true
		}
	}
	if !matched {
		t.Errorf("owner ref UID mismatch: refs=%+v want UID=%q", poolAfter.OwnerReferences, preExistingEnv.UID)
	}
}

// ---------------------------------------------------------------------------
// Scenario C: admin pre-created Env (with the Pool already listed as a
// member) — adoption should be a pure no-op except for the OwnerReference.
// ---------------------------------------------------------------------------

func TestReconcile_C_AdminPreCreatedEnv(t *testing.T) {
	pool := newPool()
	preExistingEnv := &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testPoolName,
			Namespace: testNamespace,
			UID:       "uid-admin",
		},
		Spec: agentsv1alpha1.SandboxEnvSpec{
			TemplateRef: agentsv1alpha1.SandboxEnvTemplateRef{Name: "tmpl-1"},
			Mode:        agentsv1alpha1.SandboxEnvModeWarmPool,
			Clusters: []agentsv1alpha1.EnvClusterSpec{
				{
					ClusterID: testCluster,
					Members: []agentsv1alpha1.EnvClusterMember{
						// Stored with the derived group already — second
						// reconcile then sees no drift and is a no-op.
						{Name: testPoolName, ScalingGroup: testDerivedKey},
					},
				},
			},
		},
	}
	r := newReconciler(t, pool, preExistingEnv)

	reconcileOnce(t, r)

	envAfter := &agentsv1alpha1.SandboxEnv{}
	_ = r.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testPoolName}, envAfter)
	if len(envAfter.Spec.Clusters[0].Members) != 1 {
		t.Errorf("members duplicated by adopter: %+v", envAfter.Spec.Clusters[0].Members)
	}

	poolAfter := &agentsv1alpha1.SandboxPool{}
	_ = r.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testPoolName}, poolAfter)
	if !agentsv1alpha1.HasEnvOwner(poolAfter) {
		t.Fatalf("expected owner ref on Pool, got %+v", poolAfter.OwnerReferences)
	}

	// Second reconcile must be a pure no-op — Env is fully adopted and the
	// stored ScalingGroup matches the value derived from the Pool's
	// resources, so backfillMemberDrift has nothing to write.
	reconcileOnce(t, r)
	envAfter2 := &agentsv1alpha1.SandboxEnv{}
	_ = r.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testPoolName}, envAfter2)
	if envAfter2.ResourceVersion != envAfter.ResourceVersion {
		t.Errorf("env was modified on second reconcile (RV %s → %s)", envAfter.ResourceVersion, envAfter2.ResourceVersion)
	}
}

// TestReconcile_BackfillsLegacyDefaultGroup confirms the adopter's
// backfillMemberDrift path migrates a pre-2026.06 member whose ScalingGroup
// is the literal "default" fallback to the derived resource-key form.
func TestReconcile_BackfillsLegacyDefaultGroup(t *testing.T) {
	pool := newPool()
	preExistingEnv := &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testPoolName,
			Namespace: testNamespace,
			UID:       "uid-admin-legacy",
		},
		Spec: agentsv1alpha1.SandboxEnvSpec{
			TemplateRef: agentsv1alpha1.SandboxEnvTemplateRef{Name: "tmpl-1"},
			Mode:        agentsv1alpha1.SandboxEnvModeWarmPool,
			Clusters: []agentsv1alpha1.EnvClusterSpec{
				{
					ClusterID: testCluster,
					Members: []agentsv1alpha1.EnvClusterMember{
						{Name: testPoolName, ScalingGroup: fallbackScalingGroup},
					},
				},
			},
		},
	}
	r := newReconciler(t, pool, preExistingEnv)
	reconcileOnce(t, r) // adopt
	reconcileOnce(t, r) // backfill drift

	envAfter := &agentsv1alpha1.SandboxEnv{}
	_ = r.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testPoolName}, envAfter)
	got := envAfter.Spec.Clusters[0].Members[0].ScalingGroup
	if got != testDerivedKey {
		t.Errorf("ScalingGroup not migrated: got %q, want %q", got, testDerivedKey)
	}
}

// ---------------------------------------------------------------------------
// Scenario D: stale state — Pool carries an OwnerReference but the named
// Env no longer exists. Adoption must re-create the Env and re-stamp.
// ---------------------------------------------------------------------------

func TestReconcile_D_StaleOwnerRefAfterEnvDeleted(t *testing.T) {
	pool := newPool()
	pool.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: agentsv1alpha1.GroupVersion.String(),
			Kind:       agentsv1alpha1.SandboxEnvOwnerKind,
			Name:       testPoolName,
			UID:        "uid-deleted-env",
		},
	}
	r := newReconciler(t, pool)

	// Pre-condition sanity: Env does not exist.
	env := &agentsv1alpha1.SandboxEnv{}
	if err := r.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testPoolName}, env); !apierrors.IsNotFound(err) {
		t.Fatalf("expected Env to be absent at start, got err=%v", err)
	}

	reconcileOnce(t, r)

	if err := r.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testPoolName}, env); err != nil {
		t.Fatalf("Env was not re-created: %v", err)
	}
	if env.UID == "uid-deleted-env" {
		t.Errorf("Env UID should be a fresh value, not the stale ref UID")
	}

	poolAfter := &agentsv1alpha1.SandboxPool{}
	_ = r.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testPoolName}, poolAfter)

	// Both refs may briefly coexist (the adopter appends without removing
	// stale refs — that's K8s GC's job). What matters is that AT LEAST one
	// ref points at the new live Env.
	matched := false
	for _, ref := range poolAfter.OwnerReferences {
		if ref.Kind == agentsv1alpha1.SandboxEnvOwnerKind && ref.UID == env.UID {
			matched = true
		}
	}
	if !matched {
		t.Errorf("expected new owner ref with UID %q, got %+v", env.UID, poolAfter.OwnerReferences)
	}
}

// ---------------------------------------------------------------------------
// Helpers / fast-path
// ---------------------------------------------------------------------------

func TestPoolFullyAdopted_LiveEnv(t *testing.T) {
	env := &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testPoolName,
			Namespace: testNamespace,
			UID:       "uid-live",
		},
	}
	pool := newPool()
	pool.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: agentsv1alpha1.GroupVersion.String(),
			Kind:       agentsv1alpha1.SandboxEnvOwnerKind,
			Name:       testPoolName,
			UID:        "uid-live",
		},
	}
	r := newReconciler(t, env, pool)

	ok, err := r.poolFullyAdopted(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Errorf("expected pool to be considered fully adopted")
	}
}

func TestPoolFullyAdopted_UIDMismatch(t *testing.T) {
	env := &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testPoolName,
			Namespace: testNamespace,
			UID:       "uid-current",
		},
	}
	pool := newPool()
	pool.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: agentsv1alpha1.GroupVersion.String(),
			Kind:       agentsv1alpha1.SandboxEnvOwnerKind,
			Name:       testPoolName,
			UID:        "uid-stale",
		},
	}
	r := newReconciler(t, env, pool)

	ok, err := r.poolFullyAdopted(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Errorf("expected pool NOT to be considered fully adopted on UID mismatch")
	}
}

func TestBuildMemberFromPool_InlineFallback(t *testing.T) {
	pool := newPool()
	m := buildMemberFromPool(pool, "", 0, testDerivedKey)
	if m.InstanceType != "" || m.Multiplier != 0 {
		t.Errorf("unexpected catalog metadata: %+v", m)
	}
	if m.ScalingGroup != testDerivedKey {
		t.Errorf("ScalingGroup = %q, want %q", m.ScalingGroup, testDerivedKey)
	}
	if m.InlineResources == nil {
		t.Fatalf("expected InlineResources, got nil")
	}
	cpu := m.InlineResources.Requests[corev1.ResourceCPU]
	if cpu.Cmp(resource.MustParse("2")) != 0 {
		t.Errorf("CPU = %s, want 2", cpu.String())
	}
}

func TestBuildMemberFromPool_CatalogMatch(t *testing.T) {
	pool := newPool()
	m := buildMemberFromPool(pool, "sci.c2", 1, "sci.c2")
	if m.InstanceType != "sci.c2" || m.Multiplier != 1 {
		t.Errorf("instance metadata not set: %+v", m)
	}
	if m.ScalingGroup != "sci.c2" {
		t.Errorf("ScalingGroup = %q, want %q", m.ScalingGroup, "sci.c2")
	}
	if m.InlineResources != nil {
		t.Errorf("expected InlineResources empty when catalog matched, got %+v", m.InlineResources)
	}
}

func TestDeriveScalingGroupName_ProviderDerives(t *testing.T) {
	// Real Noop provider returns testDerivedKey for the newPool resources.
	pool := newPool()
	name := deriveScalingGroupName(instancetype.NewNoop(), pool)
	if name != testDerivedKey {
		t.Errorf("deriveScalingGroupName = %q, want %q", name, testDerivedKey)
	}
}

func TestDeriveScalingGroupName_NoResourcesFallsBack(t *testing.T) {
	pool := &agentsv1alpha1.SandboxPool{}
	if got := deriveScalingGroupName(instancetype.NewNoop(), pool); got != fallbackScalingGroup {
		t.Errorf("got %q, want %q", got, fallbackScalingGroup)
	}
}

func TestEnvLabelsFromPool_PropagatesTeamUser(t *testing.T) {
	pool := newPool()
	labels := envLabelsFromPool(pool)
	if labels[agentsv1alpha1.LabelTeam] != "t1" {
		t.Errorf("team = %q, want t1", labels[agentsv1alpha1.LabelTeam])
	}
	if labels[agentsv1alpha1.LabelUser] != "u1" {
		t.Errorf("user = %q, want u1", labels[agentsv1alpha1.LabelUser])
	}
}

func TestEnvLabelsFromPool_EmptyReturnsNil(t *testing.T) {
	pool := newPool()
	pool.Labels = nil
	if got := envLabelsFromPool(pool); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}
