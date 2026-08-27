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

// Package sandboxrender turns an EmbeddedSandboxTemplate + overrides into the
// concrete Pool spec consumed by the SandboxPool controller.
//
// Callers are:
//   - SandboxEnv Reconciler (renders member pools from the Template + overrides)
//   - any other path that needs to apply pool-level overrides on top of a
//     template snapshot
//
// The package intentionally returns plain errors (no domain.AppError) so the
// controller layer can use it without taking a service-package dependency;
// HTTP-layer callers wrap into domain.AppError at the boundary.
package sandboxrender

import (
	"fmt"

	"github.com/distribution/reference"
	corev1 "k8s.io/api/core/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// Options is the render-input for Apply. Caller composes the values from
// whatever source-of-truth they have (e.g. Env-level overrides for Image,
// Member-level InlineResources for per-Pool sizing).
type Options struct {
	// Image overrides containers[0].image. Empty = no-op.
	Image string
	// InlineResources, when non-nil, replaces containers[0].resources with
	// the given ResourceRequirements. Used when a Member declares its own
	// resource sizing independent of the Template. Future work: when an
	// InstanceType catalog provider is wired in, the Reconciler will
	// resolve InstanceType + Multiplier to ResourceRequirements before
	// calling Apply, so this field stays the single resource-sizing knob
	// the renderer consumes.
	InlineResources *corev1.ResourceRequirements
	// Volumes are the Env-level PVC mounts. Grouped into corev1.Volume entries
	// by volume source (claimName + readOnly) and appended to
	// containers[0].volumeMounts only — never to init containers, and never to
	// an injected sidecar, which may hold brokered credentials.
	Volumes []agentsv1alpha1.EnvVolumeMount
	// ImageRegistry, when non-nil, supplies per-cluster registry rewriting.
	// Whether it is applied is decided by RewriteImages.
	ImageRegistry *RegistryRewrite
	// RewriteImages turns rewriting on for every image this render produces:
	// the Template's own (IdleImage, containers, initContainers) and the
	// caller-supplied Image override. Ignored when ImageRegistry is nil.
	//
	// Opt-in, and deliberately covering the override too. The rewrite is a bare
	// registry-host swap, so only the Template author knows whether the same
	// repository path exists in every region's mirror — and an Env that
	// deliberately points its override at another region's registry (because
	// that is where the image lives) must not have it silently redirected to a
	// mirror that may not carry it. The failure would surface minutes later as
	// ImagePullBackOff on the next claim, not as an error on write.
	RewriteImages bool
}

// Empty returns true when Options carries no observable effect; callers
// can use this to skip rendering when no overrides apply.
//
// Every field must be represented here. Forgetting one means an Env whose only
// override is that field renders to nothing and the feature silently no-ops.
func (o Options) Empty() bool {
	return o.Image == "" &&
		o.InlineResources == nil &&
		len(o.Volumes) == 0 &&
		o.ImageRegistry == nil
}

// Apply mutates emb in place by applying opts.
//
// Errors are deterministic input-validation failures — the caller should
// surface them as 400 Bad Request. Apply does NOT mutate emb if it returns
// a non-nil error (best-effort: validation runs before mutation per field).
func Apply(emb *agentsv1alpha1.EmbeddedSandboxTemplate, opts Options) error {
	if emb == nil || opts.Empty() {
		return nil
	}
	if opts.Image != "" {
		if err := ValidateContainerImage(opts.Image); err != nil {
			return err
		}
		if len(emb.Template.Spec.Containers) == 0 {
			return fmt.Errorf("image override requires at least one container in the template")
		}
		img := opts.Image
		if opts.RewriteImages {
			img = opts.ImageRegistry.Rewrite(img)
		}
		emb.Template.Spec.Containers[0].Image = img
	}
	if opts.InlineResources != nil {
		if len(emb.Template.Spec.Containers) == 0 {
			return fmt.Errorf("inlineResources requires at least one container in the template")
		}
		emb.Template.Spec.Containers[0].Resources = *opts.InlineResources.DeepCopy()
	}
	if opts.ImageRegistry != nil && opts.RewriteImages {
		rewriteTemplateImages(emb, opts.ImageRegistry)
	}
	if len(opts.Volumes) > 0 {
		if err := applyVolumes(emb, opts.Volumes); err != nil {
			return err
		}
	}
	return nil
}

// rewriteTemplateImages rewrites the images the Template itself owns. The
// caller-supplied override in Options.Image is handled separately, before this
// runs, so a rewritten override is not rewritten twice (the operation is
// idempotent, but the ordering keeps the intent legible).
func rewriteTemplateImages(emb *agentsv1alpha1.EmbeddedSandboxTemplate, r *RegistryRewrite) {
	emb.IdleImage = r.Rewrite(emb.IdleImage)
	for i := range emb.Template.Spec.Containers {
		emb.Template.Spec.Containers[i].Image = r.Rewrite(emb.Template.Spec.Containers[i].Image)
	}
	for i := range emb.Template.Spec.InitContainers {
		emb.Template.Spec.InitContainers[i].Image = r.Rewrite(emb.Template.Spec.InitContainers[i].Image)
	}
}

// ValidateContainerImage checks that image is a syntactically valid Docker/OCI
// image reference (e.g. "nginx:1.25", "ghcr.io/org/repo@sha256:abc...").
// Empty strings are silently accepted (callers skip empty images before
// calling this).
func ValidateContainerImage(image string) error {
	if image == "" {
		return nil
	}
	if _, err := reference.ParseAnyReference(image); err != nil {
		return fmt.Errorf("invalid container image reference %q: %v", image, err)
	}
	return nil
}
