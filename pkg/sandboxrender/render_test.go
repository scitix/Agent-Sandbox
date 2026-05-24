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

package sandboxrender_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/sandboxrender"
)

// makeEmbeddedTmpl builds an EmbeddedSandboxTemplate with a single container
// having the given image, CPU and memory request+limit. Pass "" to omit a
// resource pair.
func makeEmbeddedTmpl(image, cpuReq, memReq string) agentsv1alpha1.EmbeddedSandboxTemplate { //nolint:unparam
	c := corev1.Container{Name: "sandbox", Image: image}
	if cpuReq != "" || memReq != "" {
		c.Resources.Requests = corev1.ResourceList{}
		c.Resources.Limits = corev1.ResourceList{}
		if cpuReq != "" {
			c.Resources.Requests[corev1.ResourceCPU] = resource.MustParse(cpuReq)
			c.Resources.Limits[corev1.ResourceCPU] = resource.MustParse(cpuReq)
		}
		if memReq != "" {
			c.Resources.Requests[corev1.ResourceMemory] = resource.MustParse(memReq)
			c.Resources.Limits[corev1.ResourceMemory] = resource.MustParse(memReq)
		}
	}
	return agentsv1alpha1.EmbeddedSandboxTemplate{
		Template: &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{Containers: []corev1.Container{c}},
		},
	}
}

func cmpQty(t *testing.T, name string, got, want resource.Quantity) {
	t.Helper()
	if got.Cmp(want) != 0 {
		t.Errorf("%s = %s, want %s", name, got.String(), want.String())
	}
}

func TestApply_EmptyOptionsIsNoOp(t *testing.T) {
	tmpl := makeEmbeddedTmpl("base:v1", "500m", "4Gi")
	if err := sandboxrender.Apply(&tmpl, sandboxrender.Options{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tmpl.Template.Spec.Containers[0].Image != "base:v1" {
		t.Errorf("image changed unexpectedly")
	}
}

func TestApply_ImageOnly(t *testing.T) {
	tmpl := makeEmbeddedTmpl("base:v1", "500m", "4Gi")
	ov := sandboxrender.Options{Image: "custom:v99"}
	if err := sandboxrender.Apply(&tmpl, ov); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := tmpl.Template.Spec.Containers[0].Image; got != "custom:v99" {
		t.Errorf("image = %q, want %q", got, "custom:v99")
	}
}

func TestApply_InlineResources_ReplacesContainerResources(t *testing.T) {
	tmpl := makeEmbeddedTmpl("base:v1", "500m", "4Gi")
	ov := sandboxrender.Options{
		InlineResources: &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("16Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("32Gi"),
			},
		},
	}
	if err := sandboxrender.Apply(&tmpl, ov); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := tmpl.Template.Spec.Containers[0]
	cmpQty(t, "cpu req", c.Resources.Requests[corev1.ResourceCPU], resource.MustParse("2"))
	cmpQty(t, "mem req", c.Resources.Requests[corev1.ResourceMemory], resource.MustParse("16Gi"))
	cmpQty(t, "cpu lim", c.Resources.Limits[corev1.ResourceCPU], resource.MustParse("4"))
	cmpQty(t, "mem lim", c.Resources.Limits[corev1.ResourceMemory], resource.MustParse("32Gi"))
}

func TestApply_InlineResourcesReplacesNotMerges(t *testing.T) {
	// Template defines CPU + Memory; InlineResources only specifies CPU.
	// The CPU value replaces template's CPU; memory is dropped (full
	// replacement semantics — InlineResources is the authoritative shape).
	tmpl := makeEmbeddedTmpl("base:v1", "500m", "4Gi")
	ov := sandboxrender.Options{
		InlineResources: &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("1"),
			},
		},
	}
	if err := sandboxrender.Apply(&tmpl, ov); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := tmpl.Template.Spec.Containers[0]
	cmpQty(t, "cpu req", c.Resources.Requests[corev1.ResourceCPU], resource.MustParse("1"))
	if _, ok := c.Resources.Requests[corev1.ResourceMemory]; ok {
		t.Errorf("memory request should have been dropped by full replacement, got %+v", c.Resources.Requests)
	}
	if len(c.Resources.Limits) != 0 {
		t.Errorf("limits should have been dropped, got %+v", c.Resources.Limits)
	}
}

func TestApply_InlineResourcesDeepCopied(t *testing.T) {
	// Mutating the caller's InlineResources after Apply must not affect
	// the rendered pod spec.
	tmpl := makeEmbeddedTmpl("base:v1", "500m", "2Gi")
	ir := &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("1"),
		},
	}
	if err := sandboxrender.Apply(&tmpl, sandboxrender.Options{InlineResources: ir}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ir.Requests[corev1.ResourceCPU] = resource.MustParse("999")
	cmpQty(t, "cpu req", tmpl.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU], resource.MustParse("1"))
}

func TestApply_ImageAndInlineResources(t *testing.T) {
	tmpl := makeEmbeddedTmpl("base:v1", "500m", "2Gi")
	ov := sandboxrender.Options{
		Image: "new-img:latest",
		InlineResources: &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			},
		},
	}
	if err := sandboxrender.Apply(&tmpl, ov); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := tmpl.Template.Spec.Containers[0]
	if c.Image != "new-img:latest" {
		t.Errorf("image = %q, want %q", c.Image, "new-img:latest")
	}
	cmpQty(t, "cpu req", c.Resources.Requests[corev1.ResourceCPU], resource.MustParse("2"))
	cmpQty(t, "mem req", c.Resources.Requests[corev1.ResourceMemory], resource.MustParse("8Gi"))
}

func TestApply_InlineResourcesWithNoContainers(t *testing.T) {
	tmpl := agentsv1alpha1.EmbeddedSandboxTemplate{
		Template: &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{Containers: []corev1.Container{}},
		},
	}
	ov := sandboxrender.Options{
		InlineResources: &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
		},
	}
	if err := sandboxrender.Apply(&tmpl, ov); err == nil {
		t.Fatal("expected error for empty containers, got nil")
	}
}

func TestApply_InvalidImageReference(t *testing.T) {
	tmpl := makeEmbeddedTmpl("base:v1", "500m", "4Gi")
	ov := sandboxrender.Options{Image: "INVALID@@IMAGE"}
	if err := sandboxrender.Apply(&tmpl, ov); err == nil {
		t.Fatal("expected error for invalid image reference, got nil")
	}
}

func TestApply_ImageOverrideWithNoContainers(t *testing.T) {
	tmpl := agentsv1alpha1.EmbeddedSandboxTemplate{
		Template: &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{Containers: []corev1.Container{}},
		},
	}
	ov := sandboxrender.Options{Image: "custom:v1"}
	if err := sandboxrender.Apply(&tmpl, ov); err == nil {
		t.Fatal("expected error for empty containers, got nil")
	}
}
