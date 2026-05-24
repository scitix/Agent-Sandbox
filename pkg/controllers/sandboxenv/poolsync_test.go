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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

const (
	testLocalCluster  = "local"
	testEnvUID        = "env-uid-1"
	testOverrideImage = "ghcr.io/foo:1.2"
)

// envWithMembers returns a SandboxEnv whose local cluster segment carries
// the supplied member entries. Used by tests that exercise buildDesiredPools.
func envWithMembers(members ...agentsv1alpha1.EnvClusterMember) *agentsv1alpha1.SandboxEnv {
	env := &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "env-a",
			Namespace: "default",
			Labels: map[string]string{
				agentsv1alpha1.LabelTeam: "team-1",
				agentsv1alpha1.LabelUser: "user-1",
			},
		},
		Spec: agentsv1alpha1.SandboxEnvSpec{
			TemplateRef: agentsv1alpha1.SandboxEnvTemplateRef{Name: "tmpl"},
			Mode:        agentsv1alpha1.SandboxEnvModeWarmPool,
		},
	}
	if len(members) > 0 {
		env.Spec.Clusters = []agentsv1alpha1.EnvClusterSpec{{
			ClusterID: testLocalCluster,
			Members:   members,
		}}
	}
	return env
}

func sortedKeys(m map[string]desiredPool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestBuildDesiredPools_NoMembersFallsBackToNamesakePool(t *testing.T) {
	env := envWithMembers()
	got := buildDesiredPools(env, testLocalCluster)
	if len(got) != 1 {
		t.Fatalf("expected 1 pool, got %d", len(got))
	}
	d, ok := got["env-a"]
	if !ok {
		t.Fatalf("expected pool name=env-a, got %v", sortedKeys(got))
	}
	if d.TemplateName != "tmpl" {
		t.Errorf("TemplateName = %q, want tmpl", d.TemplateName)
	}
	// Identity labels still flow through so List filters work.
	if d.Labels[agentsv1alpha1.LabelTeam] != "team-1" || d.Labels[agentsv1alpha1.LabelUser] != "user-1" {
		t.Errorf("Identity labels not propagated: %+v", d.Labels)
	}
	if len(d.Annotations) != 0 {
		t.Errorf("expected no annotations for namesake pool, got %+v", d.Annotations)
	}
}

func TestBuildDesiredPools_OnePoolPerMember(t *testing.T) {
	env := envWithMembers(
		agentsv1alpha1.EnvClusterMember{
			Name:   "env-a-exclusive",
			Labels: map[string]string{"quota.scitix.ai/url": "zxli.ai-lab.math.exclusive"},
		},
		agentsv1alpha1.EnvClusterMember{
			Name:        "env-a-ondemand",
			Labels:      map[string]string{"quota.scitix.ai/url": "upgrader.autoupg.test.ondemand"},
			Annotations: map[string]string{"agentbox.io/reservation": "preferred"},
		},
	)
	got := buildDesiredPools(env, testLocalCluster)
	keys := sortedKeys(got)
	want := []string{"env-a-exclusive", "env-a-ondemand"}
	if len(keys) != 2 || keys[0] != want[0] || keys[1] != want[1] {
		t.Fatalf("pool names = %v, want %v", keys, want)
	}
	d := got["env-a-exclusive"]
	if d.Labels["quota.scitix.ai/url"] != "zxli.ai-lab.math.exclusive" {
		t.Errorf("quota label not propagated: %+v", d.Labels)
	}
	if d.Labels[agentsv1alpha1.LabelTeam] != "team-1" {
		t.Errorf("env identity labels not merged: %+v", d.Labels)
	}
	d2 := got["env-a-ondemand"]
	if d2.Annotations["agentbox.io/reservation"] != "preferred" {
		t.Errorf("member annotation not propagated: %+v", d2.Annotations)
	}
}

func TestBuildDesiredPools_MemberLabelOverridesEnvIdentityWhenConflicts(t *testing.T) {
	// Member declares its own team label — should win over env's team.
	env := envWithMembers(agentsv1alpha1.EnvClusterMember{
		Name:   "env-a-foo",
		Labels: map[string]string{agentsv1alpha1.LabelTeam: "team-override"},
	})
	got := buildDesiredPools(env, testLocalCluster)
	d := got["env-a-foo"]
	if d.Labels[agentsv1alpha1.LabelTeam] != "team-override" {
		t.Errorf("expected member label to win, got %q", d.Labels[agentsv1alpha1.LabelTeam])
	}
}

func TestBuildDesiredPools_IgnoresForeignClusterMembers(t *testing.T) {
	env := envWithMembers()
	env.Spec.Clusters = []agentsv1alpha1.EnvClusterSpec{
		{
			ClusterID: "remote",
			Members: []agentsv1alpha1.EnvClusterMember{
				{Name: "remote-pool"},
			},
		},
	}
	got := buildDesiredPools(env, testLocalCluster)
	if _, ok := got["remote-pool"]; ok {
		t.Errorf("foreign-cluster member must not produce a local Pool: %v", sortedKeys(got))
	}
	// Falls back to namesake pool when local segment is missing.
	if _, ok := got["env-a"]; !ok {
		t.Errorf("expected namesake fallback when local segment is empty, got %v", sortedKeys(got))
	}
}

func TestBuildDesiredPools_PropagatesEnvLevelOverrides(t *testing.T) {
	// Env-wide image / policy / timeouts come from Env.Spec.Overrides.
	env := envWithMembers(agentsv1alpha1.EnvClusterMember{Name: "env-a-ondemand"})
	env.Spec.Overrides = &agentsv1alpha1.EnvOverridesSpec{
		Image:                  testOverrideImage,
		PodCreationImagePolicy: agentsv1alpha1.PodCreationImagePolicyIdleImage,
		DefaultStartupTimeout:  &metav1.Duration{Duration: 90_000_000_000},
		DefaultIdleTimeout:     &metav1.Duration{Duration: 600_000_000_000},
	}

	got := buildDesiredPools(env, testLocalCluster)
	d, ok := got["env-a-ondemand"]
	if !ok {
		t.Fatalf("missing pool; got %v", sortedKeys(got))
	}
	if d.RenderOptions.Image != testOverrideImage {
		t.Errorf("RenderOptions.Image not propagated from Env.Spec.Overrides: %+v", d.RenderOptions)
	}
	if d.PodCreationImagePolicy != agentsv1alpha1.PodCreationImagePolicyIdleImage {
		t.Errorf("PodCreationImagePolicy = %q", d.PodCreationImagePolicy)
	}
	if d.DefaultStartupTimeout == nil || d.DefaultStartupTimeout.Seconds() != 90 {
		t.Errorf("DefaultStartupTimeout = %+v", d.DefaultStartupTimeout)
	}
	if d.DefaultIdleTimeout == nil || d.DefaultIdleTimeout.Minutes() != 10 {
		t.Errorf("DefaultIdleTimeout = %+v", d.DefaultIdleTimeout)
	}
}

func TestBuildDesiredPools_PerMemberReplicasAndInlineResources(t *testing.T) {
	// Two members with different replicas and InlineResources; Env image
	// stays uniform.
	smallResources := &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("1Gi"),
		},
	}
	largeResources := &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("4"),
			corev1.ResourceMemory: resource.MustParse("16Gi"),
		},
	}
	env := envWithMembers(
		agentsv1alpha1.EnvClusterMember{
			Name:            "small",
			Replicas:        2,
			InlineResources: smallResources,
		},
		agentsv1alpha1.EnvClusterMember{
			Name:            "large",
			Replicas:        8,
			InlineResources: largeResources,
		},
	)
	env.Spec.Overrides = &agentsv1alpha1.EnvOverridesSpec{Image: testOverrideImage}
	got := buildDesiredPools(env, testLocalCluster)
	if got["small"].Replicas != 2 || got["large"].Replicas != 8 {
		t.Errorf("per-member replicas not propagated: small=%d large=%d", got["small"].Replicas, got["large"].Replicas)
	}
	if got["small"].RenderOptions.InlineResources == nil || got["large"].RenderOptions.InlineResources == nil {
		t.Fatalf("per-member InlineResources not propagated: %+v %+v", got["small"].RenderOptions, got["large"].RenderOptions)
	}
	if got["small"].RenderOptions.InlineResources.Requests[corev1.ResourceCPU] != resource.MustParse("500m") {
		t.Errorf("small InlineResources.cpu = %v", got["small"].RenderOptions.InlineResources.Requests[corev1.ResourceCPU])
	}
	if got["large"].RenderOptions.InlineResources.Requests[corev1.ResourceCPU] != resource.MustParse("4") {
		t.Errorf("large InlineResources.cpu = %v", got["large"].RenderOptions.InlineResources.Requests[corev1.ResourceCPU])
	}
	if got["small"].RenderOptions.Image != testOverrideImage || got["large"].RenderOptions.Image != testOverrideImage {
		t.Errorf("Env-level image must apply to every member, got %+v %+v", got["small"].RenderOptions, got["large"].RenderOptions)
	}
}

func TestMergeStringMaps_OverlayWins(t *testing.T) {
	base := map[string]string{"a": "1", "b": "2"}
	overlay := map[string]string{"b": "override", "c": "3"}
	got := mergeStringMaps(base, overlay)
	if got["a"] != "1" || got["b"] != "override" || got["c"] != "3" {
		t.Errorf("merge result wrong: %+v", got)
	}
}

func TestMergeStringMaps_BothNilReturnsNil(t *testing.T) {
	if got := mergeStringMaps(nil, nil); got != nil {
		t.Errorf("nil+nil should yield nil, got %v", got)
	}
}

func TestMapsDifferOnKeys_IgnoresForeignKeys(t *testing.T) {
	live := map[string]string{"foreign": "kept"}
	if mapsDifferOnKeys(live, nil) {
		t.Errorf("foreign keys must not register as drift")
	}
	if mapsDifferOnKeys(live, map[string]string{"foreign": "kept"}) {
		t.Errorf("identical kept entry must not drift")
	}
	if !mapsDifferOnKeys(live, map[string]string{"foreign": "changed"}) {
		t.Errorf("desired-side change must drift")
	}
}

// newReconcileTestScheme registers the API groups used by the fake client.
func newReconcileTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("clientgo scheme: %v", err)
	}
	if err := agentsv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("agentsv1alpha1 scheme: %v", err)
	}
	return s
}

func newReconcileTestReconciler(t *testing.T, seed ...client.Object) *SandboxEnvReconciler {
	t.Helper()
	scheme := newReconcileTestScheme(t)
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(seed...).
		Build()
	return &SandboxEnvReconciler{
		Client:         c,
		Scheme:         scheme,
		LocalClusterID: testLocalCluster,
	}
}

// testTemplate returns a minimal but valid SandboxTemplate suitable for
// the Reconciler to render against.
func testTemplate() *agentsv1alpha1.SandboxTemplate {
	return &agentsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "tmpl"},
		Spec: agentsv1alpha1.SandboxTemplateSpec{
			Version: "1.0.0",
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "pause:3.10",
			},
		},
	}
}

func TestReconcilePools_StampsImagePullSecretWhenPresent(t *testing.T) {
	env := envWithMembers(agentsv1alpha1.EnvClusterMember{Name: "env-a-foo"})
	env.UID = testEnvUID
	// Materialised image-pull Secret living in the same namespace.
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agentsv1alpha1.EnvImagePullSecretName(env.Name),
			Namespace: env.Namespace,
		},
		Type: corev1.SecretTypeDockerConfigJson,
	}
	// Need a Template with a non-nil Template field so stamping has a
	// place to inject the LocalObjectReference.
	tmpl := testTemplate()
	tmpl.Spec.Template = &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "sandbox", Image: "base:v1"}}},
	}
	r := newReconcileTestReconciler(t, env, secret, tmpl)

	if err := r.reconcilePools(context.Background(), env); err != nil {
		t.Fatalf("reconcilePools: %v", err)
	}
	got := &agentsv1alpha1.SandboxPool{}
	if err := r.Get(context.Background(), types.NamespacedName{Namespace: env.Namespace, Name: "env-a-foo"}, got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Spec.Template == nil || len(got.Spec.Template.Spec.ImagePullSecrets) != 1 ||
		got.Spec.Template.Spec.ImagePullSecrets[0].Name != secret.Name {
		t.Errorf("expected pool.imagePullSecrets[0].name = %q, got %+v", secret.Name, got.Spec.Template)
	}
}

func TestReconcilePools_NoSecretMeansNoStamp(t *testing.T) {
	// Without the ips-{env} Secret, the Reconciler must not inject a
	// dangling LocalObjectReference.
	env := envWithMembers(agentsv1alpha1.EnvClusterMember{Name: "env-a-foo"})
	env.UID = testEnvUID
	tmpl := testTemplate()
	tmpl.Spec.Template = &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "sandbox", Image: "base:v1"}}},
	}
	r := newReconcileTestReconciler(t, env, tmpl)
	if err := r.reconcilePools(context.Background(), env); err != nil {
		t.Fatalf("reconcilePools: %v", err)
	}
	got := &agentsv1alpha1.SandboxPool{}
	if err := r.Get(context.Background(), types.NamespacedName{Namespace: env.Namespace, Name: "env-a-foo"}, got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Spec.Template != nil && len(got.Spec.Template.Spec.ImagePullSecrets) != 0 {
		t.Errorf("expected no imagePullSecrets, got %+v", got.Spec.Template.Spec.ImagePullSecrets)
	}
}

func TestReconcilePools_CreatesMembers(t *testing.T) {
	env := envWithMembers(
		agentsv1alpha1.EnvClusterMember{
			Name:   "env-a-exclusive",
			Labels: map[string]string{"quota.scitix.ai/url": "lab.math.exclusive"},
		},
		agentsv1alpha1.EnvClusterMember{
			Name:        "env-a-ondemand",
			Annotations: map[string]string{"agentbox.io/reservation": "preferred"},
		},
	)
	env.UID = testEnvUID
	r := newReconcileTestReconciler(t, env, testTemplate())

	if err := r.reconcilePools(context.Background(), env); err != nil {
		t.Fatalf("reconcilePools: %v", err)
	}

	got := &agentsv1alpha1.SandboxPool{}
	if err := r.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "env-a-exclusive"}, got); err != nil {
		t.Fatalf("exclusive pool not created: %v", err)
	}
	if got.Labels["quota.scitix.ai/url"] != "lab.math.exclusive" {
		t.Errorf("quota label missing: %+v", got.Labels)
	}
	if got.Spec.TemplateName != "tmpl" {
		t.Errorf("TemplateName = %q", got.Spec.TemplateName)
	}
	if got.Spec.IdleImage != "pause:3.10" {
		t.Errorf("expected embedded IdleImage rendered from Template, got %q", got.Spec.IdleImage)
	}
	if got.Annotations[agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey] != "1.0.0" {
		t.Errorf("template-version provenance missing: %+v", got.Annotations)
	}
	if len(got.OwnerReferences) != 1 || got.OwnerReferences[0].Name != "env-a" {
		t.Errorf("OwnerRef not stamped: %+v", got.OwnerReferences)
	}
	if got.OwnerReferences[0].Controller == nil || !*got.OwnerReferences[0].Controller {
		t.Errorf("OwnerRef must be controlling for cascade delete")
	}

	other := &agentsv1alpha1.SandboxPool{}
	if err := r.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "env-a-ondemand"}, other); err != nil {
		t.Fatalf("ondemand pool not created: %v", err)
	}
	if other.Annotations["agentbox.io/reservation"] != "preferred" {
		t.Errorf("member annotation not stamped: %+v", other.Annotations)
	}
}

func TestReconcilePools_DeletesPoolsRemovedFromSpec(t *testing.T) {
	env := envWithMembers(agentsv1alpha1.EnvClusterMember{Name: "kept"})
	env.UID = testEnvUID
	// Pre-seed an obsolete pool owned by this Env that's no longer in spec.
	obsolete := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "removed",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: agentsv1alpha1.GroupVersion.String(),
				Kind:       agentsv1alpha1.SandboxEnvOwnerKind,
				Name:       "env-a",
				UID:        env.UID,
			}},
		},
	}
	r := newReconcileTestReconciler(t, env, obsolete, testTemplate())

	if err := r.reconcilePools(context.Background(), env); err != nil {
		t.Fatalf("reconcilePools: %v", err)
	}

	check := &agentsv1alpha1.SandboxPool{}
	err := r.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "removed"}, check)
	if err == nil {
		t.Errorf("expected obsolete pool to be deleted, but Get succeeded")
	}
}

func TestReconcilePools_LeavesForeignPoolsUntouched(t *testing.T) {
	env := envWithMembers(agentsv1alpha1.EnvClusterMember{Name: "env-a-foo"})
	env.UID = testEnvUID
	// Pool with no OwnerRef back to this env — should be untouched.
	standalone := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: "standalone", Namespace: "default"},
	}
	r := newReconcileTestReconciler(t, env, standalone, testTemplate())

	if err := r.reconcilePools(context.Background(), env); err != nil {
		t.Fatalf("reconcilePools: %v", err)
	}

	check := &agentsv1alpha1.SandboxPool{}
	if err := r.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "standalone"}, check); err != nil {
		t.Errorf("foreign pool unexpectedly removed: %v", err)
	}
}

func TestReconcilePools_UpdatesLabelDrift(t *testing.T) {
	env := envWithMembers(agentsv1alpha1.EnvClusterMember{
		Name:   "env-a-foo",
		Labels: map[string]string{"quota.scitix.ai/url": "new-quota"},
	})
	env.UID = testEnvUID
	// Pool exists with an old quota label; reconcile must update it.
	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "env-a-foo",
			Namespace: "default",
			Labels:    map[string]string{"quota.scitix.ai/url": "old-quota", "foreign": "untouched"},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: agentsv1alpha1.GroupVersion.String(),
				Kind:       agentsv1alpha1.SandboxEnvOwnerKind,
				Name:       "env-a",
				UID:        env.UID,
			}},
		},
		Spec: agentsv1alpha1.SandboxPoolSpec{TemplateName: "tmpl"},
	}
	r := newReconcileTestReconciler(t, env, pool, testTemplate())

	if err := r.reconcilePools(context.Background(), env); err != nil {
		t.Fatalf("reconcilePools: %v", err)
	}

	got := &agentsv1alpha1.SandboxPool{}
	if err := r.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "env-a-foo"}, got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Labels["quota.scitix.ai/url"] != "new-quota" {
		t.Errorf("expected quota label updated, got %q", got.Labels["quota.scitix.ai/url"])
	}
	if got.Labels["foreign"] != "untouched" {
		t.Errorf("foreign labels must be preserved, got %+v", got.Labels)
	}
}

func TestReconcilePools_RerendersOnOverridesChange(t *testing.T) {
	// Pool was last rendered against the live Template; env.Spec.Overrides
	// now declares an image override → Reconciler must re-apply on top of
	// the same Template body and patch pool.spec.embedded.
	env := envWithMembers(agentsv1alpha1.EnvClusterMember{Name: "env-a-foo"})
	env.UID = testEnvUID
	env.Spec.Overrides = &agentsv1alpha1.EnvOverridesSpec{
		Image: "ghcr.io/foo:override",
	}
	// Existing pool has the un-overridden template body.
	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "env-a-foo",
			Namespace: "default",
			Annotations: map[string]string{
				agentsv1alpha1.SandboxPoolTemplateNameAnnotationKey:    "tmpl",
				agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey: "1.0.0",
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: agentsv1alpha1.GroupVersion.String(),
				Kind:       agentsv1alpha1.SandboxEnvOwnerKind,
				Name:       "env-a",
				UID:        env.UID,
			}},
		},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			TemplateName: "tmpl",
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "pause:3.10",
				Template: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "sandbox", Image: "base:v1"}},
					},
				},
			},
		},
	}
	tmpl := testTemplate()
	tmpl.Spec.Template = &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "sandbox", Image: "base:v1"}},
		},
	}

	r := newReconcileTestReconciler(t, env, pool, tmpl)
	if err := r.reconcilePools(context.Background(), env); err != nil {
		t.Fatalf("reconcilePools: %v", err)
	}

	got := &agentsv1alpha1.SandboxPool{}
	if err := r.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "env-a-foo"}, got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Spec.Template == nil || len(got.Spec.Template.Spec.Containers) == 0 {
		t.Fatalf("expected rendered Template.Spec.Containers")
	}
	if image := got.Spec.Template.Spec.Containers[0].Image; image != "ghcr.io/foo:override" {
		t.Errorf("expected overrides image applied to Pool spec, got %q", image)
	}
}

func TestReconcilePools_SkipsRenderWhenTemplateVersionDrifts(t *testing.T) {
	// Pool was last rendered at template version 1.0.0; the live Template
	// has advanced to 2.0.0. Reconciler must NOT re-render — that requires
	// an explicit Env sync-template.
	env := envWithMembers(agentsv1alpha1.EnvClusterMember{Name: "env-a-foo"})
	env.UID = testEnvUID
	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "env-a-foo",
			Namespace: "default",
			Annotations: map[string]string{
				agentsv1alpha1.SandboxPoolTemplateNameAnnotationKey:    "tmpl",
				agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey: "1.0.0",
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: agentsv1alpha1.GroupVersion.String(),
				Kind:       agentsv1alpha1.SandboxEnvOwnerKind,
				Name:       "env-a",
				UID:        env.UID,
			}},
		},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			TemplateName: "tmpl",
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "frozen:v1",
			},
		},
	}
	newerTmpl := testTemplate()
	newerTmpl.Spec.Version = "2.0.0"
	newerTmpl.Spec.IdleImage = "fresh:v2"

	r := newReconcileTestReconciler(t, env, pool, newerTmpl)
	if err := r.reconcilePools(context.Background(), env); err != nil {
		t.Fatalf("reconcilePools: %v", err)
	}

	got := &agentsv1alpha1.SandboxPool{}
	if err := r.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "env-a-foo"}, got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Spec.IdleImage != "frozen:v1" {
		t.Errorf("template body must NOT be picked up without sync-template; got IdleImage = %q", got.Spec.IdleImage)
	}
	if got.Annotations[agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey] != "1.0.0" {
		t.Errorf("pinned version must not advance during normal reconcile, got %q", got.Annotations[agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey])
	}
}

func TestMergeOwnedMapKeys_PreservesForeignKeys(t *testing.T) {
	dst := map[string]string{
		"foreign": "kept",
	}
	mergeOwnedMapKeys(&dst, map[string]string{"managed": "v"})
	if dst["foreign"] != "kept" {
		t.Errorf("foreign keys must be preserved, got %+v", dst)
	}
	if dst["managed"] != "v" {
		t.Errorf("managed key not stamped, got %+v", dst)
	}
}
