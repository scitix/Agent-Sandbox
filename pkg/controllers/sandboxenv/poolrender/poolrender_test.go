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

package poolrender_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/controllers/sandboxenv/poolrender"
)

const (
	testTeam        = "team-1"
	testUser        = "user-1"
	testEnvName     = "env-a"
	testTemplateVer = "1.0.0"
)

func newTestEnv() *agentsv1alpha1.SandboxEnv {
	return &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testEnvName,
			Namespace: "default",
			UID:       "env-uid-1",
			Labels: map[string]string{
				agentsv1alpha1.LabelTeam: testTeam,
				agentsv1alpha1.LabelUser: testUser,
			},
		},
		Spec: agentsv1alpha1.SandboxEnvSpec{
			TemplateRef: agentsv1alpha1.SandboxEnvTemplateRef{Name: "tmpl"},
			Mode:        agentsv1alpha1.SandboxEnvModeWarmPool,
		},
	}
}

func newTestTemplate() *agentsv1alpha1.SandboxTemplate {
	return &agentsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "tmpl",
			Labels:      map[string]string{"scheduling.navix.sh/queue": "default"},
			Annotations: map[string]string{"agentbox.io/owner": "platform"},
		},
		Spec: agentsv1alpha1.SandboxTemplateSpec{
			Version: testTemplateVer,
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
}

func TestRenderSandboxPool_BasicShape(t *testing.T) {
	env := newTestEnv()
	tmpl := newTestTemplate()
	pool, err := poolrender.RenderSandboxPool(poolrender.Inputs{
		Env:      env,
		Template: tmpl,
		Member:   agentsv1alpha1.EnvClusterMember{Name: "env-a-foo", Replicas: 3},
	})
	if err != nil {
		t.Fatalf("RenderSandboxPool: %v", err)
	}
	if pool.Name != "env-a-foo" || pool.Namespace != "default" {
		t.Errorf("identity wrong: %s/%s", pool.Namespace, pool.Name)
	}
	if pool.Spec.Replicas != 3 || pool.Spec.TemplateName != "tmpl" {
		t.Errorf("spec wrong: %+v", pool.Spec)
	}
	if pool.Spec.IdleImage != "pause:3.10" {
		t.Errorf("EmbeddedSandboxTemplate not copied: IdleImage=%q", pool.Spec.IdleImage)
	}
	if pool.Spec.Template == nil || pool.Spec.Template.Spec.Containers[0].Image != "base:v1" {
		t.Errorf("Template body not copied: %+v", pool.Spec.Template)
	}
}

func TestRenderSandboxPool_IdentityAndTemplateLabelsMerged(t *testing.T) {
	env := newTestEnv()
	tmpl := newTestTemplate()
	pool, err := poolrender.RenderSandboxPool(poolrender.Inputs{
		Env:      env,
		Template: tmpl,
		Member: agentsv1alpha1.EnvClusterMember{
			Name:   "env-a-foo",
			Labels: map[string]string{"quota.scitix.ai/url": "lab.math.x"},
		},
	})
	if err != nil {
		t.Fatalf("RenderSandboxPool: %v", err)
	}
	if pool.Labels[agentsv1alpha1.LabelTeam] != testTeam {
		t.Errorf("team identity label missing: %+v", pool.Labels)
	}
	if pool.Labels["scheduling.navix.sh/queue"] != "default" {
		t.Errorf("template label not synced: %+v", pool.Labels)
	}
	if pool.Labels["quota.scitix.ai/url"] != "lab.math.x" {
		t.Errorf("member label not stamped: %+v", pool.Labels)
	}
}

func TestRenderSandboxPool_MemberLabelOverridesIdentity(t *testing.T) {
	env := newTestEnv()
	pool, err := poolrender.RenderSandboxPool(poolrender.Inputs{
		Env:      env,
		Template: newTestTemplate(),
		Member: agentsv1alpha1.EnvClusterMember{
			Name:   "env-a-foo",
			Labels: map[string]string{agentsv1alpha1.LabelTeam: "team-override"},
		},
	})
	if err != nil {
		t.Fatalf("RenderSandboxPool: %v", err)
	}
	if pool.Labels[agentsv1alpha1.LabelTeam] != "team-override" {
		t.Errorf("member label should win, got %q", pool.Labels[agentsv1alpha1.LabelTeam])
	}
}

func TestRenderSandboxPool_TemplateProvenanceAnnotations(t *testing.T) {
	env := newTestEnv()
	pool, err := poolrender.RenderSandboxPool(poolrender.Inputs{
		Env:      env,
		Template: newTestTemplate(),
		Member: agentsv1alpha1.EnvClusterMember{
			Name:        "env-a-foo",
			Annotations: map[string]string{"agentbox.io/reservation": "preferred"},
		},
	})
	if err != nil {
		t.Fatalf("RenderSandboxPool: %v", err)
	}
	if got := pool.Annotations[agentsv1alpha1.SandboxPoolTemplateNameAnnotationKey]; got != "tmpl" {
		t.Errorf("template-name annotation = %q", got)
	}
	if got := pool.Annotations[agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey]; got != testTemplateVer {
		t.Errorf("template-version annotation = %q", got)
	}
	if pool.Annotations["agentbox.io/reservation"] != "preferred" {
		t.Errorf("member annotation not stamped: %+v", pool.Annotations)
	}
	if pool.Annotations["agentbox.io/owner"] != "platform" {
		t.Errorf("template annotation not synced: %+v", pool.Annotations)
	}
}

func TestRenderSandboxPool_ImageOverrideApplied(t *testing.T) {
	env := newTestEnv()
	env.Spec.Overrides = &agentsv1alpha1.EnvOverridesSpec{Image: "ghcr.io/foo:v9"}
	pool, err := poolrender.RenderSandboxPool(poolrender.Inputs{
		Env:      env,
		Template: newTestTemplate(),
		Member:   agentsv1alpha1.EnvClusterMember{Name: "env-a-foo"},
	})
	if err != nil {
		t.Fatalf("RenderSandboxPool: %v", err)
	}
	if got := pool.Spec.Template.Spec.Containers[0].Image; got != "ghcr.io/foo:v9" {
		t.Errorf("image override not applied: %q", got)
	}
}

func TestRenderSandboxPool_InlineResourcesApplied(t *testing.T) {
	env := newTestEnv()
	inline := &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("4"),
			corev1.ResourceMemory: resource.MustParse("16Gi"),
		},
	}
	pool, err := poolrender.RenderSandboxPool(poolrender.Inputs{
		Env:      env,
		Template: newTestTemplate(),
		Member: agentsv1alpha1.EnvClusterMember{
			Name:            "env-a-foo",
			InlineResources: inline,
		},
	})
	if err != nil {
		t.Fatalf("RenderSandboxPool: %v", err)
	}
	got := pool.Spec.Template.Spec.Containers[0].Resources
	if got.Requests.Cpu().Cmp(resource.MustParse("4")) != 0 {
		t.Errorf("CPU requests = %v", got.Requests.Cpu())
	}
}

func TestRenderSandboxPool_EnvOverridesTimeoutsAndPolicy(t *testing.T) {
	env := newTestEnv()
	env.Spec.Overrides = &agentsv1alpha1.EnvOverridesSpec{
		PodCreationImagePolicy: agentsv1alpha1.PodCreationImagePolicyIdleImage,
		DefaultStartupTimeout:  &metav1.Duration{Duration: 90_000_000_000},
		DefaultIdleTimeout:     &metav1.Duration{Duration: 600_000_000_000},
	}
	pool, err := poolrender.RenderSandboxPool(poolrender.Inputs{
		Env:      env,
		Template: newTestTemplate(),
		Member:   agentsv1alpha1.EnvClusterMember{Name: "env-a-foo"},
	})
	if err != nil {
		t.Fatalf("RenderSandboxPool: %v", err)
	}
	if pool.Spec.PodCreationImagePolicy != agentsv1alpha1.PodCreationImagePolicyIdleImage {
		t.Errorf("PodCreationImagePolicy = %q", pool.Spec.PodCreationImagePolicy)
	}
	if pool.Spec.DefaultStartupTimeout == nil || pool.Spec.DefaultStartupTimeout.Seconds() != 90 {
		t.Errorf("DefaultStartupTimeout = %+v", pool.Spec.DefaultStartupTimeout)
	}
	if pool.Spec.DefaultIdleTimeout == nil || pool.Spec.DefaultIdleTimeout.Minutes() != 10 {
		t.Errorf("DefaultIdleTimeout = %+v", pool.Spec.DefaultIdleTimeout)
	}
}

func TestRenderSandboxPool_OwnerRefControlling(t *testing.T) {
	env := newTestEnv()
	pool, err := poolrender.RenderSandboxPool(poolrender.Inputs{
		Env:      env,
		Template: newTestTemplate(),
		Member:   agentsv1alpha1.EnvClusterMember{Name: "env-a-foo"},
	})
	if err != nil {
		t.Fatalf("RenderSandboxPool: %v", err)
	}
	if len(pool.OwnerReferences) != 1 {
		t.Fatalf("expected 1 owner ref, got %d", len(pool.OwnerReferences))
	}
	ref := pool.OwnerReferences[0]
	if ref.Kind != agentsv1alpha1.SandboxEnvOwnerKind || ref.Name != env.Name || ref.UID != env.UID {
		t.Errorf("owner ref shape wrong: %+v", ref)
	}
	if ref.Controller == nil || !*ref.Controller {
		t.Errorf("OwnerRef must be controlling, got %+v", ref.Controller)
	}
	if ref.BlockOwnerDeletion == nil || !*ref.BlockOwnerDeletion {
		t.Errorf("OwnerRef must block-owner-deletion, got %+v", ref.BlockOwnerDeletion)
	}
}

func TestRenderSandboxPool_ImagePullSecretStampToggle(t *testing.T) {
	env := newTestEnv()
	tmpl := newTestTemplate()
	t.Run("missing-secret-no-stamp", func(t *testing.T) {
		pool, err := poolrender.RenderSandboxPool(poolrender.Inputs{
			Env: env, Template: tmpl, Member: agentsv1alpha1.EnvClusterMember{Name: "env-a-foo"},
			ImagePullSecretExists: false,
		})
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if len(pool.Spec.Template.Spec.ImagePullSecrets) != 0 {
			t.Errorf("must not stamp ref when Secret missing: %+v", pool.Spec.Template.Spec.ImagePullSecrets)
		}
	})
	t.Run("present-secret-stamps", func(t *testing.T) {
		pool, err := poolrender.RenderSandboxPool(poolrender.Inputs{
			Env: env, Template: tmpl, Member: agentsv1alpha1.EnvClusterMember{Name: "env-a-foo"},
			ImagePullSecretExists: true,
		})
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		refs := pool.Spec.Template.Spec.ImagePullSecrets
		if len(refs) != 1 || refs[0].Name != agentsv1alpha1.EnvImagePullSecretName(env.Name) {
			t.Errorf("expected stamped ref, got %+v", refs)
		}
	})
}

func TestRenderSandboxPool_RejectsInvalidInputs(t *testing.T) {
	cases := []struct {
		name string
		in   poolrender.Inputs
	}{
		{"nil-env", poolrender.Inputs{Template: newTestTemplate(), Member: agentsv1alpha1.EnvClusterMember{Name: "x"}}},
		{"nil-template", poolrender.Inputs{Env: newTestEnv(), Member: agentsv1alpha1.EnvClusterMember{Name: "x"}}},
		{"empty-member-name", poolrender.Inputs{Env: newTestEnv(), Template: newTestTemplate()}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := poolrender.RenderSandboxPool(c.in); err == nil {
				t.Errorf("expected error, got nil")
			}
		})
	}
}

func TestValidate_RejectsEqualIdleAndContainerImage(t *testing.T) {
	spec := &agentsv1alpha1.SandboxPoolSpec{
		EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
			IdleImage: "same:v1",
			Template: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Image: "same:v1"}}},
			},
		},
	}
	if err := poolrender.Validate(spec); err == nil {
		t.Errorf("expected error for equal idle/container image")
	}
	spec.IdleImage = "idle:v1"
	if err := poolrender.Validate(spec); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_RequiresIdleImage(t *testing.T) {
	if err := poolrender.Validate(&agentsv1alpha1.SandboxPoolSpec{}); err == nil {
		t.Errorf("expected error for missing idleImage")
	}
}
