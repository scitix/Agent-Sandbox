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
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/utils/ptr"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

func newTestSandboxPoolService(t *testing.T, objs ...any) SandboxPoolService {
	t.Helper()
	svc, _ := newTestSandboxPoolServiceWithClient(t, objs...)
	return svc
}

func newTestSandboxPoolServiceWithClient(t *testing.T, objs ...any) (SandboxPoolService, client.Client) {
	t.Helper()
	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("get fake client builder: %v", err)
	}
	for _, o := range objs {
		switch v := o.(type) {
		case *agentsv1alpha1.SandboxPool:
			cb = cb.WithObjects(v)
		case *corev1.Pod:
			cb = cb.WithObjects(v)
		case *agentsv1alpha1.SandboxTemplate:
			cb = cb.WithObjects(v)
		}
	}
	cli := cb.Build()
	return NewSandboxPoolService(cli, nil, nil), cli
}

// fetchCRDPool reads the underlying CRD from the test client so tests can
// verify CRD-spec fields that are no longer exposed via gen.SandboxPool.
// Tests in this file all use namespace "default".
func fetchCRDPool(t *testing.T, cli client.Client, name string) *agentsv1alpha1.SandboxPool {
	t.Helper()
	p := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: name}, p); err != nil {
		t.Fatalf("get pool default/%s: %v", name, err)
	}
	return p
}

func makePoolObj(name string, replicas int32) *agentsv1alpha1.SandboxPool {
	return &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			Replicas: replicas,
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "pause:3.10",
				Template: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "sandbox", Image: "busybox:1.36"}},
					},
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// existing CRUD tests (unchanged)
// ---------------------------------------------------------------------------

func TestSandboxPoolService_Create_DuplicateName(t *testing.T) {
	existing := makePoolObj("pool-a", 1)
	svc := newTestSandboxPoolService(t, existing)

	_, appErr := svc.Create(context.Background(), CreateSandboxPoolInput{
		Name:      "pool-a",
		Namespace: "default",
		Spec: agentsv1alpha1.SandboxPoolSpec{
			Replicas: 2,
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				Template: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{},
				},
			},
		},
	})
	if appErr == nil {
		t.Fatal("expected error, got nil")
	}
	if appErr.Code != domain.ErrCodeConflict {
		t.Fatalf("expected ErrCodeConflict, got %d", appErr.Code)
	}
}

func TestSandboxPoolService_Create_IdleImageValidation(t *testing.T) {
	svc := newTestSandboxPoolService(t)

	// Missing idleImage
	_, appErr := svc.Create(context.Background(), CreateSandboxPoolInput{
		Name:      "pool-no-idle",
		Namespace: "default",
		Spec: agentsv1alpha1.SandboxPoolSpec{
			Replicas: 1,
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				Template: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "sandbox", Image: "myapp:v1"}},
					},
				},
			},
		},
	})
	if appErr == nil {
		t.Fatal("expected error for missing idleImage, got nil")
	}
	if appErr.Code != domain.ErrCodeBadRequest {
		t.Fatalf("expected ErrCodeBadRequest, got %d", appErr.Code)
	}

	// IdleImage same as container image
	_, appErr = svc.Create(context.Background(), CreateSandboxPoolInput{
		Name:      "pool-same-image",
		Namespace: "default",
		Spec: agentsv1alpha1.SandboxPoolSpec{
			Replicas: 1,
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "myapp:v1",
				Template: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "sandbox", Image: "myapp:v1"}},
					},
				},
			},
		},
	})
	if appErr == nil {
		t.Fatal("expected error for idleImage == containerImage, got nil")
	}
	if appErr.Code != domain.ErrCodeBadRequest {
		t.Fatalf("expected ErrCodeBadRequest, got %d", appErr.Code)
	}
}

func TestSandboxPoolService_List(t *testing.T) {
	pool1 := makePoolObj("pool-a", 1)
	pool2 := makePoolObj("pool-b", 2)
	svc := newTestSandboxPoolService(t, pool1, pool2)

	items, appErr := svc.List(context.Background(), "default", "", "")
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
}

func TestSandboxPoolService_Update_Replicas(t *testing.T) {
	pool := makePoolObj("pool-a", 1)
	svc := newTestSandboxPoolService(t, pool)

	replicas := int32(5)
	result, appErr := svc.Update(context.Background(), UpdateSandboxPoolInput{
		Name:      "pool-a",
		Namespace: "default",
		Replicas:  &replicas,
	})
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if result.Spec.Replicas != 5 {
		t.Fatalf("expected replicas 5, got %d", result.Spec.Replicas)
	}
}

func TestSandboxPoolService_Delete_NotFound(t *testing.T) {
	svc := newTestSandboxPoolService(t)

	_, appErr := svc.Delete(context.Background(), "default", "nonexistent")
	if appErr == nil {
		t.Fatal("expected error, got nil")
	}
	if appErr.Code != domain.ErrCodeNotFound {
		t.Fatalf("expected ErrCodeNotFound, got %d", appErr.Code)
	}
}

func TestSandboxPoolService_Get_NotFound(t *testing.T) {
	svc := newTestSandboxPoolService(t)

	_, appErr := svc.Get(context.Background(), "default", "nonexistent")
	if appErr == nil {
		t.Fatal("expected error, got nil")
	}
	if appErr.Code != domain.ErrCodeNotFound {
		t.Fatalf("expected ErrCodeNotFound, got %d", appErr.Code)
	}
}

func makeTemplateForPool(name, version string) *agentsv1alpha1.SandboxTemplate {
	return &agentsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: agentsv1alpha1.SandboxTemplateSpec{
			Version:     version,
			Description: "Test template",
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "template-idle:latest",
				Template: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "sandbox", Image: "template-base:latest"},
						},
					},
				},
			},
		},
	}
}

func TestSandboxPoolService_Create_FromTemplate(t *testing.T) {
	tmpl := makeTemplateForPool("bench-template", "v1.0.0")
	svc, cli := newTestSandboxPoolServiceWithClient(t, tmpl)

	result, appErr := svc.Create(context.Background(), CreateSandboxPoolInput{
		Name:         "pool-from-template",
		Namespace:    "default",
		TemplateName: "bench-template",
		Spec: agentsv1alpha1.SandboxPoolSpec{
			Replicas: 3,
		},
	})
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if result.Name != "pool-from-template" {
		t.Fatalf("expected name pool-from-template, got %s", result.Name)
	}
	if result.Spec.Replicas != 3 {
		t.Fatalf("expected replicas 3, got %d", result.Spec.Replicas)
	}

	crd := fetchCRDPool(t, cli, "pool-from-template")
	if crd.Spec.IdleImage != "template-idle:latest" {
		t.Fatalf("expected idleImage from template 'template-idle:latest', got %s", crd.Spec.IdleImage)
	}
	if len(crd.Spec.Template.Spec.Containers) == 0 {
		t.Fatal("expected containers from template")
	}
	if crd.Spec.Template.Spec.Containers[0].Image != "template-base:latest" {
		t.Fatalf("expected container image 'template-base:latest', got %s",
			crd.Spec.Template.Spec.Containers[0].Image)
	}
}

func TestSandboxPoolService_Create_FromTemplate_WithOverride(t *testing.T) {
	tmpl := makeTemplateForPool("base-template", "v1.0.0")
	svc, cli := newTestSandboxPoolServiceWithClient(t, tmpl)

	result, appErr := svc.Create(context.Background(), CreateSandboxPoolInput{
		Name:         "pool-override",
		Namespace:    "default",
		TemplateName: "base-template",
		Spec: agentsv1alpha1.SandboxPoolSpec{
			Replicas: 2,
		},
	})
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if result.Spec.Replicas != 2 {
		t.Fatalf("expected replicas 2, got %d", result.Spec.Replicas)
	}
	crd := fetchCRDPool(t, cli, "pool-override")
	if crd.Spec.IdleImage != "template-idle:latest" {
		t.Fatalf("expected idleImage from template 'template-idle:latest', got %s", crd.Spec.IdleImage)
	}
}

func TestSandboxPoolService_Create_TemplateNotFound(t *testing.T) {
	svc := newTestSandboxPoolService(t)

	_, appErr := svc.Create(context.Background(), CreateSandboxPoolInput{
		Name:         "pool-missing-tmpl",
		Namespace:    "default",
		TemplateName: "does-not-exist",
		Spec: agentsv1alpha1.SandboxPoolSpec{
			Replicas: 1,
		},
	})
	if appErr == nil {
		t.Fatal("expected error for missing template, got nil")
	}
	if appErr.Code != domain.ErrCodeNotFound {
		t.Fatalf("expected ErrCodeNotFound, got %d", appErr.Code)
	}
}

func TestSandboxPoolService_Update_InvalidImage_Returns400(t *testing.T) {
	pool := makePoolObj("pool-a", 1)
	svc := newTestSandboxPoolService(t, pool)

	_, appErr := svc.Update(context.Background(), UpdateSandboxPoolInput{
		Name:          "pool-a",
		Namespace:     "default",
		OverrideImage: "INVALID@@IMAGE",
	})
	if appErr == nil {
		t.Fatal("expected error for invalid image, got nil")
	}
	if appErr.Code != domain.ErrCodeBadRequest {
		t.Fatalf("expected ErrCodeBadRequest, got %d", appErr.Code)
	}
}

func TestSandboxPoolService_Update_ValidImage(t *testing.T) {
	pool := makePoolObj("pool-a", 1)
	svc, cli := newTestSandboxPoolServiceWithClient(t, pool)

	_, appErr := svc.Update(context.Background(), UpdateSandboxPoolInput{
		Name:          "pool-a",
		Namespace:     "default",
		OverrideImage: "ghcr.io/org/repo:v2.0.0",
	})
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	crd := fetchCRDPool(t, cli, "pool-a")
	if crd.Spec.Template.Spec.Containers[0].Image != "ghcr.io/org/repo:v2.0.0" {
		t.Fatalf("expected image ghcr.io/org/repo:v2.0.0, got %s", crd.Spec.Template.Spec.Containers[0].Image)
	}
}

func TestSandboxPoolService_Create_FromTemplate_InvalidOverrideImage(t *testing.T) {
	tmpl := makeTemplateForPool("base-template", "v1.0.0")
	svc := newTestSandboxPoolService(t, tmpl)

	_, appErr := svc.Create(context.Background(), CreateSandboxPoolInput{
		Name:         "pool-bad-image",
		Namespace:    "default",
		TemplateName: "base-template",
		Spec: agentsv1alpha1.SandboxPoolSpec{
			Replicas: 1,
		},
		Overrides: &gen.PoolTemplateOverrides{
			Image: ptr.To("INVALID@@IMAGE"),
		},
	})
	if appErr == nil {
		t.Fatal("expected error for invalid override image, got nil")
	}
	if appErr.Code != domain.ErrCodeBadRequest {
		t.Fatalf("expected ErrCodeBadRequest, got %d", appErr.Code)
	}
}
