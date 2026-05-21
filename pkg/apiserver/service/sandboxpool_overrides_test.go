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
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
)

// makeEmbeddedTmpl builds an EmbeddedSandboxTemplate with a single container
// having the given image, CPU request and memory request. Pass "" to omit a resource.
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

// cmpQty compares two Quantities by pointer (Cmp is a pointer receiver).
func cmpQty(t *testing.T, name string, got, want resource.Quantity) {
	t.Helper()
	if got.Cmp(want) != 0 {
		t.Errorf("%s = %s, want %s", name, got.String(), want.String())
	}
}

func TestApplyPoolTemplateOverrides_NilOverrides(t *testing.T) {
	tmpl := makeEmbeddedTmpl("base:v1", "500m", "4Gi")
	if err := applyPoolTemplateOverrides(&tmpl, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tmpl.Template.Spec.Containers[0].Image != "base:v1" {
		t.Errorf("image changed unexpectedly")
	}
}

func TestApplyPoolTemplateOverrides_ImageOnly(t *testing.T) {
	tmpl := makeEmbeddedTmpl("base:v1", "500m", "4Gi")
	ov := &PoolTemplateOverrides{Image: "custom:v99"}
	if err := applyPoolTemplateOverrides(&tmpl, ov); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := tmpl.Template.Spec.Containers[0].Image; got != "custom:v99" {
		t.Errorf("image = %q, want %q", got, "custom:v99")
	}
}

func TestApplyPoolTemplateOverrides_ResourceMultiplierX2(t *testing.T) {
	tmpl := makeEmbeddedTmpl("base:v1", "500m", "4Gi")
	ov := &PoolTemplateOverrides{ResourceMultiplier: 2}
	if err := applyPoolTemplateOverrides(&tmpl, ov); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := tmpl.Template.Spec.Containers[0]
	wantCPU := resource.MustParse("1000m")
	wantMem := resource.MustParse("8Gi")
	cmpQty(t, "cpu req", c.Resources.Requests[corev1.ResourceCPU], wantCPU)
	cmpQty(t, "mem req", c.Resources.Requests[corev1.ResourceMemory], wantMem)
	cmpQty(t, "cpu lim", c.Resources.Limits[corev1.ResourceCPU], wantCPU)
	cmpQty(t, "mem lim", c.Resources.Limits[corev1.ResourceMemory], wantMem)
}

func TestApplyPoolTemplateOverrides_DifferentRequestsAndLimits(t *testing.T) {
	c := corev1.Container{
		Name:  "sandbox",
		Image: "base:v1",
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			},
		},
	}
	tmpl := agentsv1alpha1.EmbeddedSandboxTemplate{
		Template: &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{Containers: []corev1.Container{c}},
		},
	}
	ov := &PoolTemplateOverrides{ResourceMultiplier: 2}
	if err := applyPoolTemplateOverrides(&tmpl, ov); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := tmpl.Template.Spec.Containers[0]
	cmpQty(t, "cpu req", got.Resources.Requests[corev1.ResourceCPU], resource.MustParse("1000m"))
	cmpQty(t, "mem req", got.Resources.Requests[corev1.ResourceMemory], resource.MustParse("8Gi"))
	cmpQty(t, "cpu lim", got.Resources.Limits[corev1.ResourceCPU], resource.MustParse("2"))
	cmpQty(t, "mem lim", got.Resources.Limits[corev1.ResourceMemory], resource.MustParse("16Gi"))
}

func TestApplyPoolTemplateOverrides_NoCPUResources_Returns400(t *testing.T) {
	c := corev1.Container{
		Name:  "sandbox",
		Image: "base:v1",
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			},
		},
	}
	tmpl := agentsv1alpha1.EmbeddedSandboxTemplate{
		Template: &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{Containers: []corev1.Container{c}},
		},
	}
	ov := &PoolTemplateOverrides{ResourceMultiplier: 2}
	appErr := applyPoolTemplateOverrides(&tmpl, ov)
	if appErr == nil {
		t.Fatal("expected error for missing CPU resources, got nil")
	}
	if appErr.Code != domain.ErrCodeBadRequest {
		t.Errorf("code = %d, want ErrCodeBadRequest (%d)", appErr.Code, domain.ErrCodeBadRequest)
	}
}

func TestApplyPoolTemplateOverrides_NoMemoryResources_Returns400(t *testing.T) {
	c := corev1.Container{
		Name:  "sandbox",
		Image: "base:v1",
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("1"),
			},
		},
	}
	tmpl := agentsv1alpha1.EmbeddedSandboxTemplate{
		Template: &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{Containers: []corev1.Container{c}},
		},
	}
	ov := &PoolTemplateOverrides{ResourceMultiplier: 2}
	appErr := applyPoolTemplateOverrides(&tmpl, ov)
	if appErr == nil {
		t.Fatal("expected error for missing memory resources, got nil")
	}
	if appErr.Code != domain.ErrCodeBadRequest {
		t.Errorf("code = %d, want ErrCodeBadRequest (%d)", appErr.Code, domain.ErrCodeBadRequest)
	}
}

func TestApplyPoolTemplateOverrides_ImageAndMultiplier(t *testing.T) {
	tmpl := makeEmbeddedTmpl("base:v1", "500m", "2Gi")
	ov := &PoolTemplateOverrides{
		Image:              "new-img:latest",
		ResourceMultiplier: 2,
	}
	if err := applyPoolTemplateOverrides(&tmpl, ov); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := tmpl.Template.Spec.Containers[0]
	if c.Image != "new-img:latest" {
		t.Errorf("image = %q, want %q", c.Image, "new-img:latest")
	}
	cmpQty(t, "cpu req", c.Resources.Requests[corev1.ResourceCPU], resource.MustParse("1000m"))
	cmpQty(t, "mem req", c.Resources.Requests[corev1.ResourceMemory], resource.MustParse("4Gi"))
}

func TestApplyPoolTemplateOverrides_InvalidImageReference_Returns400(t *testing.T) {
	tmpl := makeEmbeddedTmpl("base:v1", "500m", "4Gi")
	ov := &PoolTemplateOverrides{Image: "INVALID@@IMAGE"}
	appErr := applyPoolTemplateOverrides(&tmpl, ov)
	if appErr == nil {
		t.Fatal("expected error for invalid image reference, got nil")
	}
	if appErr.Code != domain.ErrCodeBadRequest {
		t.Errorf("code = %d, want ErrCodeBadRequest (%d)", appErr.Code, domain.ErrCodeBadRequest)
	}
}

func TestApplyPoolTemplateOverrides_ImageOverrideWithNoContainers_Returns400(t *testing.T) {
	tmpl := agentsv1alpha1.EmbeddedSandboxTemplate{
		Template: &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{Containers: []corev1.Container{}},
		},
	}
	ov := &PoolTemplateOverrides{Image: "custom:v1"}
	appErr := applyPoolTemplateOverrides(&tmpl, ov)
	if appErr == nil {
		t.Fatal("expected error for empty containers, got nil")
	}
	if appErr.Code != domain.ErrCodeBadRequest {
		t.Errorf("code = %d, want ErrCodeBadRequest (%d)", appErr.Code, domain.ErrCodeBadRequest)
	}
}
