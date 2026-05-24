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
//   - SandboxEnv Reconciler / SyncTemplate endpoint (renders member pools)
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
}

// Empty returns true when Options carries no observable effect; callers
// can use this to skip rendering when no overrides apply.
func (o Options) Empty() bool { return o.Image == "" && o.InlineResources == nil }

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
		if emb.Template == nil || len(emb.Template.Spec.Containers) == 0 {
			return fmt.Errorf("image override requires at least one container in the template")
		}
		emb.Template.Spec.Containers[0].Image = opts.Image
	}
	if opts.InlineResources != nil {
		if emb.Template == nil || len(emb.Template.Spec.Containers) == 0 {
			return fmt.Errorf("inlineResources requires at least one container in the template")
		}
		emb.Template.Spec.Containers[0].Resources = *opts.InlineResources.DeepCopy()
	}
	return nil
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
