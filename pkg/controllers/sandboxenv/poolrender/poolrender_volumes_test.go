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
	"strings"
	"testing"

	"k8s.io/utils/ptr"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/controllers/sandboxenv/poolrender"
	"github.com/scitix/agent-sandbox/pkg/sandboxrender"
)

// fakeStore is a minimal RegistryStore: one private registry per cluster, no
// Type, which is how every production cluster is configured.
type fakeStore struct {
	hostToCluster map[string]string
	clusterToHost map[string]string
}

func (f fakeStore) LookupRegistry(host string) (string, string, bool) {
	c, ok := f.hostToCluster[host]
	return c, "", ok
}

func (f fakeStore) RegistryForType(clusterID, _ string) (string, bool) {
	h, ok := f.clusterToHost[clusterID]
	return h, ok
}

func cetusRewriter() *sandboxrender.RegistryRewrite {
	return &sandboxrender.RegistryRewrite{
		LocalClusterID: "bar",
		Store: fakeStore{
			hostToCluster: map[string]string{
				"reg-foo.example.com": "foo",
				"reg-bar.example.com": "bar",
			},
			clusterToHost: map[string]string{
				"foo": "reg-foo.example.com",
				"bar": "reg-bar.example.com",
			},
		},
	}
}

// templateWithRemoteImages returns a Template whose images all live in another
// cluster's registry, as happens for any Template authored in one region and
// broadcast to the rest.
func templateWithRemoteImages(optIn bool) *agentsv1alpha1.SandboxTemplate {
	tmpl := newTestTemplate()
	tmpl.Spec.IdleImage = "reg-foo.example.com/agentbox/idle-base:v1"
	tmpl.Spec.Template.Spec.Containers[0].Image = "reg-foo.example.com/agentbox/base:v2"
	if optIn {
		tmpl.Annotations[agentsv1alpha1.RegistryRewriteAnnotationKey] = "true"
	}
	return tmpl
}

func TestRenderSandboxPool_RegistryRewrite_AnnotationDriven(t *testing.T) {
	cases := []struct {
		name          string
		optIn         string // annotation value; "" means absent
		wantIdleImage string
	}{
		{"absent", "", "reg-foo.example.com/agentbox/idle-base:v1"},
		{"true", "true", "reg-bar.example.com/agentbox/idle-base:v1"},
		{"1", "1", "reg-bar.example.com/agentbox/idle-base:v1"},
		{"false", "false", "reg-foo.example.com/agentbox/idle-base:v1"},
		// A typo must degrade to the pre-existing behaviour, never error.
		{"unparseable", "yes-please", "reg-foo.example.com/agentbox/idle-base:v1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmpl := templateWithRemoteImages(false)
			if tc.optIn != "" {
				tmpl.Annotations[agentsv1alpha1.RegistryRewriteAnnotationKey] = tc.optIn
			}
			pool, err := poolrender.RenderSandboxPool(poolrender.Inputs{
				Env:           newTestEnv(),
				Template:      tmpl,
				Member:        agentsv1alpha1.EnvClusterMember{Name: "m1"},
				ImageRegistry: cetusRewriter(),
			})
			if err != nil {
				t.Fatalf("RenderSandboxPool: %v", err)
			}
			if pool.Spec.IdleImage != tc.wantIdleImage {
				t.Errorf("idleImage = %q, want %q", pool.Spec.IdleImage, tc.wantIdleImage)
			}
		})
	}
}

// TestRenderSandboxPool_RegistryRewrite_NilRewriterNoOp: an operator without
// --local-cluster-id must behave exactly as before, even for an opted-in
// Template.
func TestRenderSandboxPool_RegistryRewrite_NilRewriterNoOp(t *testing.T) {
	pool, err := poolrender.RenderSandboxPool(poolrender.Inputs{
		Env:      newTestEnv(),
		Template: templateWithRemoteImages(true),
		Member:   agentsv1alpha1.EnvClusterMember{Name: "m1"},
	})
	if err != nil {
		t.Fatalf("RenderSandboxPool: %v", err)
	}
	if pool.Spec.IdleImage != "reg-foo.example.com/agentbox/idle-base:v1" {
		t.Errorf("nil rewriter must not rewrite, got %q", pool.Spec.IdleImage)
	}
}

// TestRenderSandboxPool_RegistryRewrite_HashStableAcrossRenders is the
// invariant that keeps the API-time freeze and the reconcile-time refresh in
// agreement. If they disagreed the revision hash would differ on every pass and
// idle Pods would rebuild forever.
func TestRenderSandboxPool_RegistryRewrite_HashStableAcrossRenders(t *testing.T) {
	in := poolrender.Inputs{
		Env:           newTestEnv(),
		Template:      templateWithRemoteImages(true),
		Member:        agentsv1alpha1.EnvClusterMember{Name: "m1"},
		ImageRegistry: cetusRewriter(),
	}
	first, err := poolrender.RenderSandboxPool(in)
	if err != nil {
		t.Fatalf("RenderSandboxPool: %v", err)
	}
	want := first.Spec.Template.Labels[agentsv1alpha1.TemplateHashLabelKey]
	if want == "" {
		t.Fatal("expected a revision hash to be stamped")
	}
	for range 5 {
		again, err := poolrender.RenderSandboxPool(in)
		if err != nil {
			t.Fatalf("RenderSandboxPool: %v", err)
		}
		if got := again.Spec.Template.Labels[agentsv1alpha1.TemplateHashLabelKey]; got != want {
			t.Fatalf("revision hash flapped: %q then %q", want, got)
		}
	}
}

// TestRenderSandboxPool_VolumesReachPoolSpec is the end-to-end assertion for
// Part B's render step: what the user declared on the Env has to land in the
// Pool's pod template, because everything downstream copies that verbatim.
func TestRenderSandboxPool_VolumesReachPoolSpec(t *testing.T) {
	env := newTestEnv()
	env.Spec.Overrides = &agentsv1alpha1.EnvOverridesSpec{
		Volumes: []agentsv1alpha1.EnvVolumeMount{
			{ClaimName: "team-data-41", MountPath: "/volume/zystore", ReadOnly: ptr.To(false)},
			{ClaimName: "shared-models", MountPath: "/volume/models", SubPath: "Qwen/Qwen2.5-7B"},
		},
	}

	pool, err := poolrender.RenderSandboxPool(poolrender.Inputs{
		Env:      env,
		Template: newTestTemplate(),
		Member:   agentsv1alpha1.EnvClusterMember{Name: "m1"},
	})
	if err != nil {
		t.Fatalf("RenderSandboxPool: %v", err)
	}

	var claims []string
	roByClaim := map[string]bool{}
	for _, v := range pool.Spec.Template.Spec.Volumes {
		if v.PersistentVolumeClaim == nil {
			continue
		}
		if !strings.HasPrefix(v.Name, sandboxrender.ReservedVolumeNamePrefix) {
			t.Errorf("injected volume %q lacks the reserved prefix", v.Name)
		}
		claims = append(claims, v.PersistentVolumeClaim.ClaimName)
		roByClaim[v.PersistentVolumeClaim.ClaimName] = v.PersistentVolumeClaim.ReadOnly
	}
	if len(claims) != 2 {
		t.Fatalf("want 2 PVC volumes in the pool spec, got %d (%v)", len(claims), claims)
	}
	if roByClaim["team-data-41"] {
		t.Error("explicit readOnly=false must render read-write")
	}
	if !roByClaim["shared-models"] {
		t.Error("omitted readOnly must render read-only")
	}

	mounts := 0
	for _, m := range pool.Spec.Template.Spec.Containers[0].VolumeMounts {
		if strings.HasPrefix(m.Name, sandboxrender.ReservedVolumeNamePrefix) {
			mounts++
		}
	}
	if mounts != 2 {
		t.Errorf("want 2 injected volumeMounts, got %d", mounts)
	}
}

// TestRenderSandboxPool_VolumeChangeFlipsHash / _ReorderDoesNot together give
// the rollout semantics: editing the mount list rebuilds idle Pods, while
// reordering it is a no-op.
func TestRenderSandboxPool_VolumeChangeFlipsHash(t *testing.T) {
	hashFor := func(vols []agentsv1alpha1.EnvVolumeMount) string {
		env := newTestEnv()
		env.Spec.Overrides = &agentsv1alpha1.EnvOverridesSpec{Volumes: vols}
		pool, err := poolrender.RenderSandboxPool(poolrender.Inputs{
			Env:      env,
			Template: newTestTemplate(),
			Member:   agentsv1alpha1.EnvClusterMember{Name: "m1"},
		})
		if err != nil {
			t.Fatalf("RenderSandboxPool: %v", err)
		}
		return pool.Spec.Template.Labels[agentsv1alpha1.TemplateHashLabelKey]
	}

	none := hashFor(nil)
	one := hashFor([]agentsv1alpha1.EnvVolumeMount{{ClaimName: "a", MountPath: "/volume/a"}})
	two := hashFor([]agentsv1alpha1.EnvVolumeMount{
		{ClaimName: "a", MountPath: "/volume/a"},
		{ClaimName: "b", MountPath: "/volume/b"},
	})
	flipped := hashFor([]agentsv1alpha1.EnvVolumeMount{
		{ClaimName: "a", MountPath: "/volume/a", ReadOnly: ptr.To(false)},
	})

	if none == one {
		t.Error("adding a volume must flip the revision hash")
	}
	if one == two {
		t.Error("adding a second volume must flip the revision hash")
	}
	if one == flipped {
		t.Error("changing readOnly must flip the revision hash")
	}

	reordered := hashFor([]agentsv1alpha1.EnvVolumeMount{
		{ClaimName: "b", MountPath: "/volume/b"},
		{ClaimName: "a", MountPath: "/volume/a"},
	})
	if reordered != two {
		t.Error("reordering the declared list must NOT flip the revision hash")
	}
}
