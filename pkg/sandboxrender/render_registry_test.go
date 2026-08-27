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

package sandboxrender

import (
	"testing"
)

// Images as authored in the "foo" region, and the same images after being
// rewritten for "bar". Named because several tests assert on both forms.
const (
	remoteIdleImage = "registry-a.example.com/agentbox/idle-base:v1"
	remoteBaseImage = "registry-a.example.com/agentbox/e2b-base:v2"
	remoteEnvdImage = "registry-a.example.com/agentbox/envd:0.3.1"

	localIdleImage = "registry-b.example.com/agentbox/idle-base:v1"
	localBaseImage = "registry-b.example.com/agentbox/e2b-base:v2"
	localEnvdImage = "registry-b.example.com/agentbox/envd:0.3.1"
)

// fooToBar mirrors the production shape: two clusters, each with one
// private registry, no Type set (which is how every real cluster is configured).
func fooToBar() *RegistryRewrite {
	return &RegistryRewrite{
		LocalClusterID: "bar",
		Store: buildFakeStore(map[string][]registryEntrySpec{
			"foo": {{host: "registry-a.example.com", typ: ""}},
			"bar": {{host: "registry-b.example.com", typ: ""}},
		}),
	}
}

// TestApply_RewriteTemplateImages_OptIn covers the whole point of Part A: with
// the Template opted in, every image the Template owns lands on the local
// registry, and the repository path plus tag survive verbatim.
func TestApply_RewriteTemplateImages_OptIn(t *testing.T) {
	emb := embWithContainers()
	emb.IdleImage = remoteIdleImage
	emb.Template.Spec.Containers[0].Image = remoteBaseImage
	emb.Template.Spec.InitContainers[0].Image = remoteEnvdImage

	if err := Apply(emb, Options{
		ImageRegistry:         fooToBar(),
		RewriteTemplateImages: true,
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	want := map[string]string{
		"idleImage":     localIdleImage,
		"container":     localBaseImage,
		"initContainer": localEnvdImage,
	}
	got := map[string]string{
		"idleImage":     emb.IdleImage,
		"container":     emb.Template.Spec.Containers[0].Image,
		"initContainer": emb.Template.Spec.InitContainers[0].Image,
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %q, want %q", k, got[k], w)
		}
	}
}

// TestApply_RewriteTemplateImages_NotOptedIn is the zero-regression guarantee:
// without the annotation the Template's images are untouched even though the
// rewriter is wired up.
func TestApply_RewriteTemplateImages_NotOptedIn(t *testing.T) {
	emb := embWithContainers()
	emb.IdleImage = remoteIdleImage
	emb.Template.Spec.Containers[0].Image = remoteBaseImage
	emb.Template.Spec.InitContainers[0].Image = remoteEnvdImage

	before := emb.DeepCopy()

	if err := Apply(emb, Options{
		ImageRegistry:         fooToBar(),
		RewriteTemplateImages: false,
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if emb.IdleImage != before.IdleImage {
		t.Errorf("idleImage changed without opt-in: %q", emb.IdleImage)
	}
	if emb.Template.Spec.Containers[0].Image != before.Template.Spec.Containers[0].Image {
		t.Errorf("container image changed without opt-in: %q", emb.Template.Spec.Containers[0].Image)
	}
	if emb.Template.Spec.InitContainers[0].Image != before.Template.Spec.InitContainers[0].Image {
		t.Errorf("initContainer image changed without opt-in: %q", emb.Template.Spec.InitContainers[0].Image)
	}
}

// TestApply_OverrideImageRewrittenWithoutOptIn: a caller-supplied image is the
// same class of input as the claim-time image, which is already rewritten
// unconditionally. It must not require the Template annotation.
func TestApply_OverrideImageRewrittenWithoutOptIn(t *testing.T) {
	emb := embWithContainers()
	if err := Apply(emb, Options{
		Image:                 "registry-a.example.com/agentbox/custom:v9",
		ImageRegistry:         fooToBar(),
		RewriteTemplateImages: false,
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := "registry-b.example.com/agentbox/custom:v9"
	if got := emb.Template.Spec.Containers[0].Image; got != want {
		t.Errorf("override image = %q, want %q", got, want)
	}
}

// TestApply_RewriteIsIdempotent matters because the API freezes a member spec
// and the Reconciler re-renders it on every pass. If rewriting were not
// idempotent the two would disagree, the revision hash would flip, and idle
// Pods would rebuild forever.
func TestApply_RewriteIsIdempotent(t *testing.T) {
	emb := embWithContainers()
	emb.IdleImage = remoteIdleImage
	emb.Template.Spec.Containers[0].Image = remoteBaseImage

	opts := Options{ImageRegistry: fooToBar(), RewriteTemplateImages: true}
	if err := Apply(emb, opts); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	afterFirst := emb.DeepCopy()

	for range 3 {
		if err := Apply(emb, opts); err != nil {
			t.Fatalf("repeat Apply: %v", err)
		}
		if emb.IdleImage != afterFirst.IdleImage {
			t.Fatalf("idleImage not idempotent: %q then %q", afterFirst.IdleImage, emb.IdleImage)
		}
		if emb.Template.Spec.Containers[0].Image != afterFirst.Template.Spec.Containers[0].Image {
			t.Fatalf("container image not idempotent: %q then %q",
				afterFirst.Template.Spec.Containers[0].Image, emb.Template.Spec.Containers[0].Image)
		}
	}
}

// TestApply_RewriteLeavesPublicImagesAlone: the mirror layout only guarantees
// the same repository path across the private registries, so public images must
// never be touched.
func TestApply_RewriteLeavesPublicImagesAlone(t *testing.T) {
	emb := embWithContainers()
	emb.IdleImage = "ubuntu:22.04"
	emb.Template.Spec.Containers[0].Image = "ghcr.io/org/repo:v1"
	emb.Template.Spec.InitContainers[0].Image = "docker.io/library/busybox:1.36"

	if err := Apply(emb, Options{
		ImageRegistry:         fooToBar(),
		RewriteTemplateImages: true,
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if emb.IdleImage != "ubuntu:22.04" {
		t.Errorf("implicit docker.io image rewritten: %q", emb.IdleImage)
	}
	if emb.Template.Spec.Containers[0].Image != "ghcr.io/org/repo:v1" {
		t.Errorf("unknown registry rewritten: %q", emb.Template.Spec.Containers[0].Image)
	}
	if emb.Template.Spec.InitContainers[0].Image != "docker.io/library/busybox:1.36" {
		t.Errorf("explicit docker.io image rewritten: %q", emb.Template.Spec.InitContainers[0].Image)
	}
}

// TestApply_NilRewriterIsSafe: RegistryRewrite.Rewrite has a nil receiver
// guard so callers can hold an unconditional field.
func TestApply_NilRewriterIsSafe(t *testing.T) {
	var r *RegistryRewrite
	if got := r.Rewrite("registry-a.example.com/agentbox/x:v1"); got != "registry-a.example.com/agentbox/x:v1" {
		t.Errorf("nil rewriter changed the image: %q", got)
	}

	emb := embWithContainers()
	before := emb.DeepCopy()
	if err := Apply(emb, Options{RewriteTemplateImages: true}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if emb.IdleImage != before.IdleImage {
		t.Error("RewriteTemplateImages without a rewriter must be a no-op")
	}
}

func TestClassifyRewrite(t *testing.T) {
	store := buildFakeStore(map[string][]registryEntrySpec{
		"foo": {{host: "registry-a.example.com", typ: ""}},
		"bar": {{host: "registry-b.example.com", typ: ""}},
		// baz declares a registry of a Type bar does not have.
		"baz": {{host: "registry-c.example.com", typ: "harbor"}},
	})

	cases := []struct {
		name  string
		image string
		want  RewriteSkipReason
	}{
		{"peer cluster with local counterpart", "registry-a.example.com/a/b:v1", RewriteApplied},
		{"already local", "registry-b.example.com/a/b:v1", RewriteSkipNotApplicable},
		{"public registry", "ghcr.io/a/b:v1", RewriteSkipNotApplicable},
		{"implicit docker.io", "ubuntu:22.04", RewriteSkipNotApplicable},
		{"empty", "", RewriteSkipNotApplicable},
		// This is the case worth alerting on: a real peer registry that this
		// cluster cannot mirror, which surfaces later as ImagePullBackOff.
		{"peer cluster, no local counterpart", "registry-c.example.com/a/b:v1", RewriteSkipNoLocalRegistry},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyRewrite(tc.image, "bar", store); got != tc.want {
				t.Errorf("ClassifyRewrite(%q) = %q, want %q", tc.image, got, tc.want)
			}
		})
	}
}
