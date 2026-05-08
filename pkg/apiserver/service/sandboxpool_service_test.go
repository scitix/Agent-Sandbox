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

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

const (
	testReasonImagePullBackOff = "ImagePullBackOff"
	testReasonOOMKilled        = "OOMKilled"
)

func newTestSandboxPoolService(t *testing.T, objs ...any) SandboxPoolService {
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
	return NewSandboxPoolService(cb.Build(), nil, nil)
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

// makePodForPool creates a pod owned by the given pool with the specified phase.
func makePodForPool(name, poolName, phase, namespace string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				agentsv1alpha1.SandboxPoolLabelKey:  poolName,
				agentsv1alpha1.SandboxPhaseLabelKey: phase,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "sandbox", Image: "myrepo/myimage:v1"}},
		},
	}
}

// makeStartingPodForPool creates a Starting pod with ImagePullBackOff container status.
func makeStartingPodForPool(name, poolName, namespace string) *corev1.Pod {
	pod := makePodForPool(name, poolName, agentsv1alpha1.SandboxPhaseStarting, namespace)
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Name:  "sandbox",
			Image: "myrepo/myimage:v1",
			State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{
					Reason:  testReasonImagePullBackOff,
					Message: "back-off pulling image",
				},
			},
		},
	}
	return pod
}

// makeFailedPodForPool creates a Failed pod with OOMKilled status.
func makeFailedPodForPool(name, poolName, namespace string) *corev1.Pod {
	pod := makePodForPool(name, poolName, agentsv1alpha1.SandboxPhaseFailed, namespace)
	pod.Status = corev1.PodStatus{
		Phase:  corev1.PodFailed,
		Reason: testReasonOOMKilled,
		ContainerStatuses: []corev1.ContainerStatus{
			{
				Name: "sandbox",
				State: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{
						Reason:   "OOMKilled",
						ExitCode: 137,
					},
				},
			},
		},
	}
	return pod
}

// ---------------------------------------------------------------------------
// existing CRUD tests (unchanged)
// ---------------------------------------------------------------------------

func TestSandboxPoolService_Create_DuplicateName(t *testing.T) {
	existing := makePoolObj("pool-a", 1)
	svc := newTestSandboxPoolService(t, existing)

	_, appErr := svc.Create(context.Background(), domain.CreateSandboxPoolInput{
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
	_, appErr := svc.Create(context.Background(), domain.CreateSandboxPoolInput{
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
	_, appErr = svc.Create(context.Background(), domain.CreateSandboxPoolInput{
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
	result, appErr := svc.Update(context.Background(), domain.UpdateSandboxPoolInput{
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
	svc := newTestSandboxPoolService(t, tmpl)

	result, appErr := svc.Create(context.Background(), domain.CreateSandboxPoolInput{
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
	if result.Spec.IdleImage != "template-idle:latest" {
		t.Fatalf("expected idleImage from template 'template-idle:latest', got %s", result.Spec.IdleImage)
	}
	if len(result.Spec.Template.Spec.Containers) == 0 {
		t.Fatal("expected containers from template")
	}
	if result.Spec.Template.Spec.Containers[0].Image != "template-base:latest" {
		t.Fatalf("expected container image 'template-base:latest', got %s",
			result.Spec.Template.Spec.Containers[0].Image)
	}
	if result.Spec.Replicas != 3 {
		t.Fatalf("expected replicas 3, got %d", result.Spec.Replicas)
	}
}

func TestSandboxPoolService_Create_FromTemplate_WithOverride(t *testing.T) {
	tmpl := makeTemplateForPool("base-template", "v1.0.0")
	svc := newTestSandboxPoolService(t, tmpl)

	result, appErr := svc.Create(context.Background(), domain.CreateSandboxPoolInput{
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
	if result.Spec.IdleImage != "template-idle:latest" {
		t.Fatalf("expected idleImage from template 'template-idle:latest', got %s", result.Spec.IdleImage)
	}
	if result.Spec.Replicas != 2 {
		t.Fatalf("expected replicas 2, got %d", result.Spec.Replicas)
	}
}

func TestSandboxPoolService_Create_TemplateNotFound(t *testing.T) {
	svc := newTestSandboxPoolService(t)

	_, appErr := svc.Create(context.Background(), domain.CreateSandboxPoolInput{
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

// ---------------------------------------------------------------------------
// diagnostic tests: List returns pod-YAML-only diagnostics
// ---------------------------------------------------------------------------

func TestSandboxPoolService_List_NoDiagnosticsForIdlePods(t *testing.T) {
	pool := makePoolObj("pool-a", 2)
	idlePod := makePodForPool("pod-idle", "pool-a", agentsv1alpha1.SandboxPhaseIdle, "default")
	svc := newTestSandboxPoolService(t, pool, idlePod)

	items, appErr := svc.List(context.Background(), "default", "", "")
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 pool, got %d", len(items))
	}
	if len(items[0].PodDiagnostics) != 0 {
		t.Errorf("expected no diagnostics for idle pod, got %d", len(items[0].PodDiagnostics))
	}
}

func TestSandboxPoolService_List_DiagnosticsForStartingPod(t *testing.T) {
	pool := makePoolObj("pool-a", 2)
	startingPod := makeStartingPodForPool("pod-starting", "pool-a", "default")
	svc := newTestSandboxPoolService(t, pool, startingPod)

	items, appErr := svc.List(context.Background(), "default", "", "")
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 pool, got %d", len(items))
	}
	diags := items[0].PodDiagnostics
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diags))
	}
	d := diags[0]
	if d.PodName != "pod-starting" {
		t.Errorf("PodName = %q, want %q", d.PodName, "pod-starting")
	}
	if d.Phase != agentsv1alpha1.SandboxPhaseStarting {
		t.Errorf("Phase = %q, want %q", d.Phase, agentsv1alpha1.SandboxPhaseStarting)
	}
	if d.Reason != testReasonImagePullBackOff {
		t.Errorf("Reason = %q, want %q", d.Reason, testReasonImagePullBackOff)
	}
	if d.Message == "" {
		t.Error("expected non-empty Message")
	}
	// List must NOT populate Events (no clientset)
	if len(d.Events) != 0 {
		t.Errorf("List should not populate Events, got %d", len(d.Events))
	}
}

func TestSandboxPoolService_List_DiagnosticsForFailedPod(t *testing.T) {
	pool := makePoolObj("pool-a", 3)
	failedPod := makeFailedPodForPool("pod-failed", "pool-a", "default")
	svc := newTestSandboxPoolService(t, pool, failedPod)

	items, appErr := svc.List(context.Background(), "default", "", "")
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if len(items[0].PodDiagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(items[0].PodDiagnostics))
	}
	d := items[0].PodDiagnostics[0]
	if d.Phase != agentsv1alpha1.SandboxPhaseFailed {
		t.Errorf("Phase = %q, want %q", d.Phase, agentsv1alpha1.SandboxPhaseFailed)
	}
	if d.Reason != testReasonOOMKilled {
		t.Errorf("Reason = %q, want %q", d.Reason, testReasonOOMKilled)
	}
}

func TestSandboxPoolService_List_MultiplePools_DiagnosticsIsolated(t *testing.T) {
	poolA := makePoolObj("pool-a", 2)
	poolB := makePoolObj("pool-b", 2)
	startingPod := makeStartingPodForPool("pod-starting", "pool-a", "default")
	idlePodB := makePodForPool("pod-idle-b", "pool-b", agentsv1alpha1.SandboxPhaseIdle, "default")
	svc := newTestSandboxPoolService(t, poolA, poolB, startingPod, idlePodB)

	items, appErr := svc.List(context.Background(), "default", "", "")
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 pools, got %d", len(items))
	}
	byName := make(map[string]domain.SandboxPool, 2)
	for _, item := range items {
		byName[item.Name] = item
	}
	if len(byName["pool-a"].PodDiagnostics) != 1 {
		t.Errorf("pool-a: expected 1 diagnostic, got %d", len(byName["pool-a"].PodDiagnostics))
	}
	if len(byName["pool-b"].PodDiagnostics) != 0 {
		t.Errorf("pool-b: expected 0 diagnostics, got %d", len(byName["pool-b"].PodDiagnostics))
	}
}

// ---------------------------------------------------------------------------
// diagnostic tests: Get returns diagnostics from Pod YAML (Events empty without clientset)
// ---------------------------------------------------------------------------

func TestSandboxPoolService_Get_DiagnosticsFromPodYAML(t *testing.T) {
	pool := makePoolObj("pool-a", 2)
	startingPod := makeStartingPodForPool("pod-starting", "pool-a", "default")
	svc := newTestSandboxPoolService(t, pool, startingPod)

	result, appErr := svc.Get(context.Background(), "default", "pool-a")
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if len(result.PodDiagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(result.PodDiagnostics))
	}
	d := result.PodDiagnostics[0]
	if d.PodName != "pod-starting" {
		t.Errorf("PodName = %q, want %q", d.PodName, "pod-starting")
	}
	if d.Reason != testReasonImagePullBackOff {
		t.Errorf("Reason = %q, want %q", d.Reason, testReasonImagePullBackOff)
	}
}

func TestSandboxPoolService_Get_NoDiagnosticsForHealthyPool(t *testing.T) {
	pool := makePoolObj("pool-a", 2)
	idlePod1 := makePodForPool("pod-idle-1", "pool-a", agentsv1alpha1.SandboxPhaseIdle, "default")
	idlePod2 := makePodForPool("pod-idle-2", "pool-a", agentsv1alpha1.SandboxPhaseIdle, "default")
	svc := newTestSandboxPoolService(t, pool, idlePod1, idlePod2)

	result, appErr := svc.Get(context.Background(), "default", "pool-a")
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if len(result.PodDiagnostics) != 0 {
		t.Errorf("expected no diagnostics for healthy pool, got %d", len(result.PodDiagnostics))
	}
}

func TestSandboxPoolService_Get_EventsEmptyWithoutClientset(t *testing.T) {
	pool := makePoolObj("pool-a", 2)
	failedPod := makeFailedPodForPool("pod-failed", "pool-a", "default")
	// clientset is nil (passed nil above)
	svc := newTestSandboxPoolService(t, pool, failedPod)

	result, appErr := svc.Get(context.Background(), "default", "pool-a")
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if len(result.PodDiagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(result.PodDiagnostics))
	}
	d := result.PodDiagnostics[0]
	if d.Reason != testReasonOOMKilled {
		t.Errorf("Reason = %q, want %q", d.Reason, testReasonOOMKilled)
	}
	// Without a clientset, Events should be empty
	if len(d.Events) != 0 {
		t.Errorf("expected no Events without clientset, got %d", len(d.Events))
	}
}

// ---------------------------------------------------------------------------
// SyncTemplate tests
// ---------------------------------------------------------------------------

func makePoolObjWithTemplateAnnotation(name, templateName, templateVersion string, replicas int32) *agentsv1alpha1.SandboxPool {
	pool := makePoolObj(name, replicas)
	pool.Annotations = map[string]string{
		agentsv1alpha1.SandboxPoolTemplateNameAnnotationKey:    templateName,
		agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey: templateVersion,
	}
	return pool
}

func TestSyncTemplate_UpdatesEmbeddedSpec(t *testing.T) {
	// Pool created from template v1.0.0 with old idleImage
	pool := makePoolObjWithTemplateAnnotation("pool-a", "my-template", "1.0.0", 3)
	pool.Spec.IdleImage = "old-image:v1"

	// Updated template with new idleImage
	tmpl := &agentsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "my-template"},
		Spec: agentsv1alpha1.SandboxTemplateSpec{
			Version: "1.1.0",
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "pause:3.10",
				Template: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "sandbox", Image: "new-image:v2"}},
					},
				},
			},
		},
	}

	svc := newTestSandboxPoolService(t, pool, tmpl)

	result, appErr := svc.SyncTemplate(context.Background(), domain.SyncSandboxPoolTemplateInput{
		Name:      "pool-a",
		Namespace: "default",
	})
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if result.Spec.IdleImage != "pause:3.10" {
		t.Errorf("IdleImage = %q, want %q", result.Spec.IdleImage, "pause:3.10")
	}
	// TemplateVersion should be updated to template's current version
	if result.TemplateVersion != "1.1.0" {
		t.Errorf("TemplateVersion = %q, want %q", result.TemplateVersion, "1.1.0")
	}
	// Replicas must not change
	if result.Spec.Replicas != 3 {
		t.Errorf("Replicas = %d, want 3", result.Spec.Replicas)
	}
}

func TestSyncTemplate_NoTemplateAnnotation_Returns400(t *testing.T) {
	// Pool without any template annotation
	pool := makePoolObj("pool-no-template", 2)
	svc := newTestSandboxPoolService(t, pool)

	_, appErr := svc.SyncTemplate(context.Background(), domain.SyncSandboxPoolTemplateInput{
		Name:      "pool-no-template",
		Namespace: "default",
	})
	if appErr == nil {
		t.Fatal("expected error, got nil")
	}
	if appErr.Code != domain.ErrCodeBadRequest {
		t.Fatalf("expected ErrCodeBadRequest, got %d", appErr.Code)
	}
}

func TestSyncTemplate_PoolNotFound_Returns404(t *testing.T) {
	svc := newTestSandboxPoolService(t)

	_, appErr := svc.SyncTemplate(context.Background(), domain.SyncSandboxPoolTemplateInput{
		Name:      "nonexistent-pool",
		Namespace: "default",
	})
	if appErr == nil {
		t.Fatal("expected error, got nil")
	}
	if appErr.Code != domain.ErrCodeNotFound {
		t.Fatalf("expected ErrCodeNotFound, got %d", appErr.Code)
	}
}

func TestSandboxPoolService_Update_InvalidImage_Returns400(t *testing.T) {
	pool := makePoolObj("pool-a", 1)
	svc := newTestSandboxPoolService(t, pool)

	_, appErr := svc.Update(context.Background(), domain.UpdateSandboxPoolInput{
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
	svc := newTestSandboxPoolService(t, pool)

	result, appErr := svc.Update(context.Background(), domain.UpdateSandboxPoolInput{
		Name:          "pool-a",
		Namespace:     "default",
		OverrideImage: "ghcr.io/org/repo:v2.0.0",
	})
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if result.Spec.Template.Spec.Containers[0].Image != "ghcr.io/org/repo:v2.0.0" {
		t.Fatalf("expected image ghcr.io/org/repo:v2.0.0, got %s", result.Spec.Template.Spec.Containers[0].Image)
	}
}

func TestSandboxPoolService_Create_FromTemplate_InvalidOverrideImage(t *testing.T) {
	tmpl := makeTemplateForPool("base-template", "v1.0.0")
	svc := newTestSandboxPoolService(t, tmpl)

	_, appErr := svc.Create(context.Background(), domain.CreateSandboxPoolInput{
		Name:         "pool-bad-image",
		Namespace:    "default",
		TemplateName: "base-template",
		Spec: agentsv1alpha1.SandboxPoolSpec{
			Replicas: 1,
		},
		Overrides: &domain.PoolTemplateOverrides{
			Image: "INVALID@@IMAGE",
		},
	})
	if appErr == nil {
		t.Fatal("expected error for invalid override image, got nil")
	}
	if appErr.Code != domain.ErrCodeBadRequest {
		t.Fatalf("expected ErrCodeBadRequest, got %d", appErr.Code)
	}
}

func TestSyncTemplate_TemplateNotFound_Returns404(t *testing.T) {
	// Pool references a template that no longer exists
	pool := makePoolObjWithTemplateAnnotation("pool-orphan", "deleted-template", "1.0.0", 2)
	svc := newTestSandboxPoolService(t, pool)

	_, appErr := svc.SyncTemplate(context.Background(), domain.SyncSandboxPoolTemplateInput{
		Name:      "pool-orphan",
		Namespace: "default",
	})
	if appErr == nil {
		t.Fatal("expected error, got nil")
	}
	if appErr.Code != domain.ErrCodeNotFound {
		t.Fatalf("expected ErrCodeNotFound, got %d", appErr.Code)
	}
}
