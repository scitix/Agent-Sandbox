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

const (
	testNamespace = "default"
	testCluster   = "local"
	testPoolName  = "pool-a"
	// testDerivedKey is the resource-key value derived from newPool()'s
	// container resources (2 CPU / 4 GiB). Used as the expected
	// ScalingGroup in adoption + migration tests.
	testDerivedKey   = "2c4Gi"
	testTemplateName = "tmpl-1"
	testTmplVer      = "v1.0.0"
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
// InlineResources or round-trip into (InstanceType, Multiplier). The Pool
// carries an inline template by default, so existing-Pool migration paths
// exercise the "copy from Pool" branch.
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
			TemplateName: testTemplateName,
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "ghcr.io/idle:0",
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "sandbox",
								Image: "ghcr.io/runtime:base",
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

// newSandboxTemplate produces the cluster-scoped SandboxTemplate referenced
// by newPool().TemplateName, with an EmbeddedSandboxTemplate the adopter
// can copy onto a Pool whose inline template is missing.
func newSandboxTemplate() *agentsv1alpha1.SandboxTemplate {
	return &agentsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: testTemplateName},
		Spec: agentsv1alpha1.SandboxTemplateSpec{
			Version: testTmplVer,
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "ghcr.io/idle:from-sbt",
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "sandbox",
								Image: "ghcr.io/runtime:from-sbt",
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

func reconcileOnce(t *testing.T, r *PoolAdoptionReconciler) (ctrl.Result, error) {
	t.Helper()
	return r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: testPoolName},
	})
}

// reconcileUntilStable runs Reconcile until two consecutive calls produce
// the same Env ResourceVersion, mirroring the controller-runtime requeue
// loop. Caps at maxSteps so a runaway diff terminates the test instead of
// hanging.
func reconcileUntilStable(t *testing.T, r *PoolAdoptionReconciler) {
	t.Helper()
	const maxSteps = 8
	var lastRV string
	for range maxSteps {
		if _, err := reconcileOnce(t, r); err != nil {
			t.Fatalf("Reconcile error: %v", err)
		}
		env := &agentsv1alpha1.SandboxEnv{}
		err := r.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testPoolName}, env)
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			t.Fatalf("get env: %v", err)
		}
		if env.ResourceVersion == lastRV {
			return
		}
		lastRV = env.ResourceVersion
	}
	t.Fatalf("reconcile did not stabilise after %d passes", maxSteps)
}

// ---------------------------------------------------------------------------
// Scenario A: fresh adoption
// ---------------------------------------------------------------------------

func TestReconcile_A_FreshAdoption(t *testing.T) {
	pool := newPool()
	r := newReconciler(t, pool)

	reconcileUntilStable(t, r)

	env := &agentsv1alpha1.SandboxEnv{}
	if err := r.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testPoolName}, env); err != nil {
		t.Fatalf("Env was not created: %v", err)
	}
	if env.Spec.TemplateRef.Name != testTemplateName {
		t.Errorf("Env.Spec.TemplateRef.Name = %q, want %q", env.Spec.TemplateRef.Name, testTemplateName)
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
	member := env.Spec.Clusters[0].Members[0]
	if member.Config.InlineResources == nil {
		t.Errorf("expected Config.InlineResources to be set (Noop provider), got nil")
	}
	if member.Config.ScalingGroup != testDerivedKey {
		t.Errorf("Config.ScalingGroup = %q, want %q", member.Config.ScalingGroup, testDerivedKey)
	}
	if len(member.Spec.Template.Spec.Containers) == 0 ||
		member.Spec.Template.Spec.Containers[0].Image != "ghcr.io/runtime:base" {
		t.Errorf("Member.Spec.Template not copied from Pool: %+v", member.Spec.Template)
	}
	if member.Spec.TemplateName != testTemplateName {
		t.Errorf("Member.Spec.TemplateName = %q, want %q", member.Spec.TemplateName, testTemplateName)
	}
	if member.Spec.Replicas != 3 {
		t.Errorf("Member.Spec.Replicas = %d, want 3 (seeded from Pool)", member.Spec.Replicas)
	}

	freshPool := &agentsv1alpha1.SandboxPool{}
	if err := r.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testPoolName}, freshPool); err != nil {
		t.Fatalf("re-Get pool: %v", err)
	}
	if !agentsv1alpha1.HasEnvOwner(freshPool) {
		t.Fatalf("expected pool to carry SandboxEnv OwnerReference, got %+v", freshPool.OwnerReferences)
	}

	// Idempotency: a further reconcile must be a no-op.
	if _, err := reconcileOnce(t, r); err != nil {
		t.Fatalf("idempotency reconcile: %v", err)
	}
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
			TemplateRef: agentsv1alpha1.SandboxEnvTemplateRef{Name: testTemplateName},
			Mode:        agentsv1alpha1.SandboxEnvModeWarmPool,
		},
	}
	r := newReconciler(t, pool, preExistingEnv)

	reconcileUntilStable(t, r)

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
// Scenario C: admin pre-created Env with the Pool already listed as a
// member, drift-free — adoption is a pure no-op except for the OwnerReference.
// ---------------------------------------------------------------------------

func TestReconcile_C_AdminPreCreatedEnv(t *testing.T) {
	pool := newPool()
	member := agentsv1alpha1.EnvClusterMember{
		Name: testPoolName,
		Metadata: agentsv1alpha1.MemberMetadata{
			Labels: map[string]string{
				agentsv1alpha1.LabelTeam: "t1",
				agentsv1alpha1.LabelUser: "u1",
			},
		},
		Spec: *pool.Spec.DeepCopy(),
		Config: agentsv1alpha1.EnvClusterMemberConfig{
			ScalingGroup:    testDerivedKey,
			InlineResources: firstContainerResources(pool).DeepCopy(),
		},
	}
	preExistingEnv := &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testPoolName,
			Namespace: testNamespace,
			UID:       "uid-admin",
		},
		Spec: agentsv1alpha1.SandboxEnvSpec{
			TemplateRef: agentsv1alpha1.SandboxEnvTemplateRef{Name: testTemplateName},
			Mode:        agentsv1alpha1.SandboxEnvModeWarmPool,
			Clusters: []agentsv1alpha1.EnvClusterSpec{
				{ClusterID: testCluster, Members: []agentsv1alpha1.EnvClusterMember{member}},
			},
		},
	}
	r := newReconciler(t, pool, preExistingEnv)

	reconcileUntilStable(t, r)

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
}

// TestReconcile_BackfillsLegacyDefaultGroup confirms the adopter migrates a
// pre-2026.06 member whose ScalingGroup is the literal "default" fallback
// to the derived resource-key form on a subsequent reconcile.
func TestReconcile_BackfillsLegacyDefaultGroup(t *testing.T) {
	pool := newPool()
	preExistingEnv := &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testPoolName,
			Namespace: testNamespace,
			UID:       "uid-admin-legacy",
		},
		Spec: agentsv1alpha1.SandboxEnvSpec{
			TemplateRef: agentsv1alpha1.SandboxEnvTemplateRef{Name: testTemplateName},
			Mode:        agentsv1alpha1.SandboxEnvModeWarmPool,
			Clusters: []agentsv1alpha1.EnvClusterSpec{
				{
					ClusterID: testCluster,
					Members: []agentsv1alpha1.EnvClusterMember{
						{Name: testPoolName, Config: agentsv1alpha1.EnvClusterMemberConfig{ScalingGroup: fallbackScalingGroup}},
					},
				},
			},
		},
	}
	r := newReconciler(t, pool, preExistingEnv)
	reconcileUntilStable(t, r)

	envAfter := &agentsv1alpha1.SandboxEnv{}
	_ = r.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testPoolName}, envAfter)
	got := envAfter.Spec.Clusters[0].Members[0].Config.ScalingGroup
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

	env := &agentsv1alpha1.SandboxEnv{}
	if err := r.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testPoolName}, env); !apierrors.IsNotFound(err) {
		t.Fatalf("expected Env to be absent at start, got err=%v", err)
	}

	reconcileUntilStable(t, r)

	if err := r.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testPoolName}, env); err != nil {
		t.Fatalf("Env was not re-created: %v", err)
	}
	if env.UID == "uid-deleted-env" {
		t.Errorf("Env UID should be a fresh value, not the stale ref UID")
	}

	poolAfter := &agentsv1alpha1.SandboxPool{}
	_ = r.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testPoolName}, poolAfter)
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
// Scenario E: Pool stripped of its inline template — Member.Spec was
// truncated by an earlier-broken Env→Pool sync. Adopter must resolve the
// SandboxTemplate, refill Member.Spec.Template, and stamp the
// template-version annotation on the Pool.
// ---------------------------------------------------------------------------

func TestReconcile_E_BrokenMemberRefillsFromSandboxTemplate(t *testing.T) {
	pool := newPool()
	pool.Spec.EmbeddedSandboxTemplate = agentsv1alpha1.EmbeddedSandboxTemplate{}
	pool.Spec.TemplateName = testTemplateName
	pool.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: agentsv1alpha1.GroupVersion.String(),
			Kind:       agentsv1alpha1.SandboxEnvOwnerKind,
			Name:       testPoolName,
			UID:        "uid-existing",
		},
	}
	preExistingEnv := &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testPoolName,
			Namespace: testNamespace,
			UID:       "uid-existing",
		},
		Spec: agentsv1alpha1.SandboxEnvSpec{
			TemplateRef: agentsv1alpha1.SandboxEnvTemplateRef{Name: testTemplateName},
			Mode:        agentsv1alpha1.SandboxEnvModeWarmPool,
			Clusters: []agentsv1alpha1.EnvClusterSpec{
				{
					ClusterID: testCluster,
					Members: []agentsv1alpha1.EnvClusterMember{
						{
							Name: testPoolName,
							Spec: agentsv1alpha1.SandboxPoolSpec{Replicas: 1},
						},
					},
				},
			},
		},
	}
	sbt := newSandboxTemplate()
	r := newReconciler(t, pool, preExistingEnv, sbt)
	reconcileUntilStable(t, r)

	envAfter := &agentsv1alpha1.SandboxEnv{}
	_ = r.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testPoolName}, envAfter)
	m := envAfter.Spec.Clusters[0].Members[0]
	if len(m.Spec.Template.Spec.Containers) == 0 {
		t.Fatalf("Member.Spec.Template not refilled: %+v", m.Spec.Template)
	}
	if m.Spec.Template.Spec.Containers[0].Image != "ghcr.io/runtime:from-sbt" {
		t.Errorf("Template image = %q, want value from SandboxTemplate", m.Spec.Template.Spec.Containers[0].Image)
	}
	if m.Spec.TemplateName != testTemplateName {
		t.Errorf("TemplateName = %q, want %q", m.Spec.TemplateName, testTemplateName)
	}
	if m.Spec.Replicas != 1 {
		t.Errorf("Replicas mutated: got %d, want 1 (preserved)", m.Spec.Replicas)
	}

	poolAfter := &agentsv1alpha1.SandboxPool{}
	_ = r.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testPoolName}, poolAfter)
	if got := poolAfter.Annotations[agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey]; got != testTmplVer {
		t.Errorf("template-version annotation = %q, want %q", got, testTmplVer)
	}
}

// ---------------------------------------------------------------------------
// Scenario F: missing SandboxTemplate — Pool has TemplateName but the sbt
// is absent. Adopter must surface an error.
// ---------------------------------------------------------------------------

func TestReconcile_F_MissingSandboxTemplateErrors(t *testing.T) {
	pool := newPool()
	pool.Spec.EmbeddedSandboxTemplate = agentsv1alpha1.EmbeddedSandboxTemplate{}
	pool.Spec.TemplateName = "does-not-exist"
	preExistingEnv := &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testPoolName,
			Namespace: testNamespace,
			UID:       "uid-exists",
		},
		Spec: agentsv1alpha1.SandboxEnvSpec{
			TemplateRef: agentsv1alpha1.SandboxEnvTemplateRef{Name: "does-not-exist"},
			Mode:        agentsv1alpha1.SandboxEnvModeWarmPool,
			Clusters: []agentsv1alpha1.EnvClusterSpec{
				{
					ClusterID: testCluster,
					Members: []agentsv1alpha1.EnvClusterMember{
						{Name: testPoolName},
					},
				},
			},
		},
	}
	r := newReconciler(t, pool, preExistingEnv)
	if _, err := reconcileOnce(t, r); err == nil {
		t.Fatalf("expected error when SandboxTemplate is missing, got nil")
	}
}

// ---------------------------------------------------------------------------
// Scenario G: sbt upgrade does NOT auto-propagate to an already-populated
// Member.Spec. Frozen-snapshot invariant.
// ---------------------------------------------------------------------------

func TestReconcile_G_TemplateUpgradeDoesNotPropagate(t *testing.T) {
	pool := newPool()
	pool.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: agentsv1alpha1.GroupVersion.String(),
			Kind:       agentsv1alpha1.SandboxEnvOwnerKind,
			Name:       testPoolName,
			UID:        "uid-anchored",
		},
	}
	preExistingEnv := &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testPoolName,
			Namespace: testNamespace,
			UID:       "uid-anchored",
		},
		Spec: agentsv1alpha1.SandboxEnvSpec{
			TemplateRef: agentsv1alpha1.SandboxEnvTemplateRef{Name: testTemplateName, Version: testTmplVer},
			Mode:        agentsv1alpha1.SandboxEnvModeWarmPool,
			Clusters: []agentsv1alpha1.EnvClusterSpec{
				{
					ClusterID: testCluster,
					Members: []agentsv1alpha1.EnvClusterMember{
						{
							Name: testPoolName,
							Spec: *pool.Spec.DeepCopy(),
							Config: agentsv1alpha1.EnvClusterMemberConfig{
								ScalingGroup:    testDerivedKey,
								InlineResources: firstContainerResources(pool).DeepCopy(),
							},
						},
					},
				},
			},
		},
	}
	upgradedSbt := newSandboxTemplate()
	upgradedSbt.Spec.Version = "v2.0.0"
	upgradedSbt.Spec.Template.Spec.Containers[0].Image = "ghcr.io/runtime:from-sbt-upgraded"
	r := newReconciler(t, pool, preExistingEnv, upgradedSbt)

	reconcileUntilStable(t, r)

	envAfter := &agentsv1alpha1.SandboxEnv{}
	_ = r.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testPoolName}, envAfter)
	gotImage := envAfter.Spec.Clusters[0].Members[0].Spec.Template.Spec.Containers[0].Image
	if gotImage != "ghcr.io/runtime:base" {
		t.Errorf("Template auto-propagated from upgraded sbt: image = %q, want %q (frozen)",
			gotImage, "ghcr.io/runtime:base")
	}
}

// ---------------------------------------------------------------------------
// Scenario H: Phase-2 Pool (Pool.Name != owning Env.Name) — adopter must
// not touch it.
// ---------------------------------------------------------------------------

func TestReconcile_H_Phase2PoolEarlyExit(t *testing.T) {
	pool := newPool()
	pool.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: agentsv1alpha1.GroupVersion.String(),
			Kind:       agentsv1alpha1.SandboxEnvOwnerKind,
			Name:       "env-different",
			UID:        "uid-phase2-env",
		},
	}
	phase2Env := &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "env-different",
			Namespace: testNamespace,
			UID:       "uid-phase2-env",
		},
		Spec: agentsv1alpha1.SandboxEnvSpec{
			TemplateRef: agentsv1alpha1.SandboxEnvTemplateRef{Name: testTemplateName},
			Mode:        agentsv1alpha1.SandboxEnvModeWarmPool,
		},
	}
	r := newReconciler(t, pool, phase2Env)
	if _, err := reconcileOnce(t, r); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	envAfter := &agentsv1alpha1.SandboxEnv{}
	if err := r.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: "env-different"}, envAfter); err != nil {
		t.Fatalf("get phase-2 env: %v", err)
	}
	if len(envAfter.Spec.Clusters) != 0 {
		t.Errorf("Phase-2 Env was modified by adopter: clusters=%+v", envAfter.Spec.Clusters)
	}
	sameNameEnv := &agentsv1alpha1.SandboxEnv{}
	err := r.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testPoolName}, sameNameEnv)
	if !apierrors.IsNotFound(err) {
		t.Errorf("adopter created a same-name Env for a Phase-2 Pool: err=%v", err)
	}
}

// ---------------------------------------------------------------------------
// Scenario I: Member.Spec.Replicas owned by autoscaler/user — adopter must
// not overwrite it on subsequent reconciles.
// ---------------------------------------------------------------------------

func TestReconcile_I_PreservesUserReplicas(t *testing.T) {
	pool := newPool()
	pool.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: agentsv1alpha1.GroupVersion.String(),
			Kind:       agentsv1alpha1.SandboxEnvOwnerKind,
			Name:       testPoolName,
			UID:        "uid-user-replicas",
		},
	}
	preExistingEnv := &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testPoolName,
			Namespace: testNamespace,
			UID:       "uid-user-replicas",
		},
		Spec: agentsv1alpha1.SandboxEnvSpec{
			TemplateRef: agentsv1alpha1.SandboxEnvTemplateRef{Name: testTemplateName},
			Mode:        agentsv1alpha1.SandboxEnvModeWarmPool,
			Clusters: []agentsv1alpha1.EnvClusterSpec{
				{
					ClusterID: testCluster,
					Members: []agentsv1alpha1.EnvClusterMember{
						{
							Name: testPoolName,
							Spec: agentsv1alpha1.SandboxPoolSpec{
								Replicas:                7,
								TemplateName:            testTemplateName,
								EmbeddedSandboxTemplate: *pool.Spec.EmbeddedSandboxTemplate.DeepCopy(),
							},
							Config: agentsv1alpha1.EnvClusterMemberConfig{
								ScalingGroup:    testDerivedKey,
								InlineResources: firstContainerResources(pool).DeepCopy(),
							},
						},
					},
				},
			},
		},
	}
	r := newReconciler(t, pool, preExistingEnv)
	reconcileUntilStable(t, r)

	envAfter := &agentsv1alpha1.SandboxEnv{}
	_ = r.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testPoolName}, envAfter)
	got := envAfter.Spec.Clusters[0].Members[0].Spec.Replicas
	if got != 7 {
		t.Errorf("Replicas overwritten: got %d, want 7 (user-edited, preserved)", got)
	}
}

// ---------------------------------------------------------------------------
// Helper tests
// ---------------------------------------------------------------------------

func TestComposeDesiredMember_InlineFallback(t *testing.T) {
	r := newReconciler(t)
	pool := newPool()
	m, _, err := r.composeDesiredMember(context.Background(), pool, nil)
	if err != nil {
		t.Fatalf("composeDesiredMember: %v", err)
	}
	if m.Config.InstanceType != "" || m.Config.Multiplier != 0 {
		t.Errorf("unexpected catalog metadata: %+v", m.Config)
	}
	if m.Config.ScalingGroup != testDerivedKey {
		t.Errorf("ScalingGroup = %q, want %q", m.Config.ScalingGroup, testDerivedKey)
	}
	if m.Config.InlineResources == nil {
		t.Fatalf("expected Config.InlineResources, got nil")
	}
	cpu := m.Config.InlineResources.Requests[corev1.ResourceCPU]
	if cpu.Cmp(resource.MustParse("2")) != 0 {
		t.Errorf("CPU = %s, want 2", cpu.String())
	}
}

func TestDeriveScalingGroupName_ProviderDerives(t *testing.T) {
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

func TestIsEnvManagedPool(t *testing.T) {
	pool := newPool()
	if isEnvManagedPool(pool) {
		t.Errorf("orphan pool flagged as Env-managed")
	}
	pool.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: agentsv1alpha1.GroupVersion.String(),
		Kind:       agentsv1alpha1.SandboxEnvOwnerKind,
		Name:       testPoolName,
		UID:        "uid-same",
	}}
	if isEnvManagedPool(pool) {
		t.Errorf("same-name Env owner flagged as Env-managed (should be legacy)")
	}
	pool.OwnerReferences[0].Name = "env-different"
	if !isEnvManagedPool(pool) {
		t.Errorf("different-name Env owner NOT flagged as Env-managed")
	}
}
