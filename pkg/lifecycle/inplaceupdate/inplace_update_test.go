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

package inplaceupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/utils/imageresolver"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

// mockResolver implements imageresolver.DigestResolver for testing.
// It wraps a real Resolver for digest parsing and adds a seedDigests map
// for pre-populating tag→digest mappings that would normally come from
// registry HEAD requests. Prefix keys with "config:" to seed config digests.
type mockResolver struct {
	inner       *imageresolver.Resolver
	seedDigests map[string]string // tag → digest (pre-populated for test scenarios)
}

func newMockResolver() *mockResolver {
	return &mockResolver{
		inner:       imageresolver.NewResolver(nil, 24*time.Hour),
		seedDigests: make(map[string]string),
	}
}

func (m *mockResolver) Resolve(ctx context.Context, imageRef string, opts ...imageresolver.ResolveOption) (string, error) {
	// Check seed digests first (test overrides take priority).
	if d, ok := m.seedDigests[imageRef]; ok {
		return d, nil
	}
	// Fall back to the real resolver (handles digest refs).
	return m.inner.Resolve(ctx, imageRef, opts...)
}

func (m *mockResolver) ResolveConfigDigest(_ context.Context, imageRef string, _ ...imageresolver.ResolveOption) (string, error) {
	if d, ok := m.seedDigests["config:"+imageRef]; ok {
		return d, nil
	}
	return "", fmt.Errorf("no config digest seeded for %s", imageRef)
}

func (m *mockResolver) DigestFromStatus(image, imageID string) (string, error) {
	return m.inner.DigestFromStatus(image, imageID)
}

func TestGetInplaceUpdateState(t *testing.T) {
	tests := []struct {
		name        string
		pod         *corev1.Pod
		expectNil   bool
		expectError bool
	}{
		{
			name:      "no annotation",
			pod:       &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "default"}},
			expectNil: true,
		},
		{
			name: "invalid annotation",
			pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
				Name:      "pod",
				Namespace: "default",
				Annotations: map[string]string{
					PodAnnotationInPlaceUpdateStateKey: `{"phase":`,
				},
			}},
			expectError: true,
		},
		{
			name: "valid annotation",
			pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
				Name:      "pod",
				Namespace: "default",
				Annotations: map[string]string{
					PodAnnotationInPlaceUpdateStateKey: `{"phase":"starting","targetImage":"nginx:1.28","targetPodPhase":"running","updateTimestamp":"2026-01-01T00:00:00Z","lastContainerStatuses":{"sandbox":{"imageID":"sha256:old"}}}`,
				},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, err := GetInplaceUpdateState(tt.pod)
			if tt.expectError {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.expectNil {
				if state != nil {
					t.Fatalf("expected nil state, got %#v", state)
				}
				return
			}
			if state == nil {
				t.Fatal("expected state")
			}
			if state.Phase != InplaceUpdatePhaseStarting {
				t.Fatalf("unexpected phase: %s", state.Phase)
			}
			if state.TargetImage != "nginx:1.28" {
				t.Fatalf("unexpected target image: %s", state.TargetImage)
			}
			if state.TargetPodPhase != agentsv1alpha1.SandboxPhaseRunning {
				t.Fatalf("unexpected target phase: %s", state.TargetPodPhase)
			}
		})
	}
}

func TestTriggerUpdateWithOptions(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-1",
			Namespace: "default",
			Labels: map[string]string{
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseIdle,
			},
		},
		Spec:   corev1.PodSpec{Containers: []corev1.Container{{Name: "sandbox", Image: "pause:3.10"}, {Name: "sidecar", Image: "busybox:1.36"}}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "sandbox", ImageID: "sha256:old-sandbox"}, {Name: "sidecar", ImageID: "sha256:old-sidecar"}}},
	}

	cli := newTestClientBuilder(t).WithObjects(pod).Build()
	_, err := TriggerUpdateWithOptions(context.Background(), cli, pod, UpdateOptions{
		ContainerImages: map[string]string{
			"sandbox": "sandbox:v2",
			"sidecar": "sidecar:v2",
		},
		Labels: map[string]string{
			"app.kubernetes.io/managed-by": "api-server",
		},
		Annotations: map[string]string{
			"agentbox.navix.sh/sandbox-id": "sbx-123",
		},
		TargetPodPhase: agentsv1alpha1.SandboxPhaseRunning,
	})
	if err != nil {
		t.Fatalf("trigger update: %v", err)
	}

	updated := &corev1.Pod{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, updated); err != nil {
		t.Fatalf("get pod: %v", err)
	}

	if updated.Spec.Containers[0].Image != "sandbox:v2" || updated.Spec.Containers[1].Image != "sidecar:v2" {
		t.Fatalf("images not updated: %#v", updated.Spec.Containers)
	}
	if updated.Labels[agentsv1alpha1.SandboxPhaseLabelKey] != agentsv1alpha1.SandboxPhaseStarting {
		t.Fatalf("expected starting phase, got %s", updated.Labels[agentsv1alpha1.SandboxPhaseLabelKey])
	}
	if updated.Labels["app.kubernetes.io/managed-by"] != "api-server" {
		t.Fatalf("expected label to be patched")
	}
	if updated.Annotations["agentbox.navix.sh/sandbox-id"] != "sbx-123" {
		t.Fatalf("expected annotation to be patched")
	}

	state, err := GetInplaceUpdateState(updated)
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	if state == nil {
		t.Fatal("expected state")
	}
	if state.Phase != InplaceUpdatePhaseStarting {
		t.Fatalf("unexpected state phase: %s", state.Phase)
	}
	if state.TargetPodPhase != agentsv1alpha1.SandboxPhaseRunning {
		t.Fatalf("unexpected target pod phase: %s", state.TargetPodPhase)
	}
	if len(state.TargetImages) != 2 {
		t.Fatalf("expected 2 target images, got %d", len(state.TargetImages))
	}
	if state.LastContainerStatuses["sandbox"].ImageID != "sha256:old-sandbox" {
		t.Fatalf("unexpected sandbox imageID snapshot: %s", state.LastContainerStatuses["sandbox"].ImageID)
	}
	if state.LastContainerStatuses["sidecar"].ImageID != "sha256:old-sidecar" {
		t.Fatalf("unexpected sidecar imageID snapshot: %s", state.LastContainerStatuses["sidecar"].ImageID)
	}
}

func TestTriggerUpdateMetadataOnly(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-2",
			Namespace: "default",
			Labels: map[string]string{
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseIdle,
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "sandbox", Image: "pause:3.10"}}},
	}

	cli := newTestClientBuilder(t).WithObjects(pod).Build()
	if _, err := TriggerUpdateWithOptions(context.Background(), cli, pod, UpdateOptions{
		Labels:         map[string]string{"agentbox.navix.sh/claimed": "true"},
		TargetPodPhase: agentsv1alpha1.SandboxPhaseRunning,
	}); err != nil {
		t.Fatalf("trigger metadata update: %v", err)
	}

	updated := &corev1.Pod{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, updated); err != nil {
		t.Fatalf("get pod: %v", err)
	}
	if updated.Labels[agentsv1alpha1.SandboxPhaseLabelKey] != agentsv1alpha1.SandboxPhaseRunning {
		t.Fatalf("expected running phase, got %s", updated.Labels[agentsv1alpha1.SandboxPhaseLabelKey])
	}
	if updated.Annotations[PodAnnotationInPlaceUpdateStateKey] != "" {
		t.Fatalf("expected no inplace update annotation for metadata-only patch")
	}
}

func TestTriggerUpdateRemovesManagedMetadata(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-remove",
			Namespace: "default",
			Labels: map[string]string{
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseRunning,
				"agentbox.navix.sh/sandbox-id":      "sandbox-1",
				"custom-label":                      "to-remove",
			},
			Annotations: map[string]string{
				"custom-annotation": "to-remove",
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "sandbox", Image: "pause:3.10"}}},
	}

	cli := newTestClientBuilder(t).WithObjects(pod).Build()
	if _, err := TriggerUpdateWithOptions(context.Background(), cli, pod, UpdateOptions{
		TargetPodPhase:    agentsv1alpha1.SandboxPhaseIdle,
		RemoveLabels:      []string{"agentbox.navix.sh/sandbox-id", "custom-label"},
		RemoveAnnotations: []string{"custom-annotation"},
	}); err != nil {
		t.Fatalf("trigger metadata removal: %v", err)
	}

	updated := &corev1.Pod{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, updated); err != nil {
		t.Fatalf("get pod: %v", err)
	}
	if updated.Labels["agentbox.navix.sh/sandbox-id"] != "" || updated.Labels["custom-label"] != "" {
		t.Fatalf("expected labels to be removed, got %#v", updated.Labels)
	}
	if updated.Annotations["custom-annotation"] != "" {
		t.Fatalf("expected annotation to be removed, got %#v", updated.Annotations)
	}
	if updated.Labels[agentsv1alpha1.SandboxPhaseLabelKey] != agentsv1alpha1.SandboxPhaseIdle {
		t.Fatalf("expected idle phase, got %s", updated.Labels[agentsv1alpha1.SandboxPhaseLabelKey])
	}
}

func TestNormalizeImage(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"ubuntu:22.04", "ubuntu:22.04"},
		{"docker.io/library/ubuntu:22.04", "ubuntu:22.04"},
		{"docker.io/nginx:latest", "nginx:latest"},
		{"registry.example.com/org/image:tag", "registry.example.com/org/image:tag"},
	}
	for _, tt := range tests {
		if got := normalizeImage(tt.input); got != tt.want {
			t.Errorf("normalizeImage(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestHasUnexpectedRestart(t *testing.T) {
	stableAnnotation := func(containerID string, restartCount int32) string {
		return `{"phase":"completed","targetPodPhase":"running","stableContainerStatuses":{"sandbox":{"containerID":"` + containerID + `","restartCount":` + fmt.Sprintf("%d", restartCount) + `}}}`
	}

	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected bool
	}{
		{
			name:     "no state",
			pod:      &corev1.Pod{},
			expected: false,
		},
		{
			name: "no stable statuses",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					PodAnnotationInPlaceUpdateStateKey: `{"phase":"completed","targetPodPhase":"running"}`,
				}},
				Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
					{Name: "sandbox", ContainerID: "docker://abc", RestartCount: 0},
				}},
			},
			expected: false,
		},
		{
			name: "matching containerID and restartCount",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					PodAnnotationInPlaceUpdateStateKey: stableAnnotation("docker://abc", 0),
				}},
				Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
					{Name: "sandbox", ContainerID: "docker://abc", RestartCount: 0},
				}},
			},
			expected: false,
		},
		{
			name: "higher restartCount same containerID - not flagged (restartCount alone not a signal)",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					PodAnnotationInPlaceUpdateStateKey: stableAnnotation("docker://abc", 0),
				}},
				Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
					{Name: "sandbox", ContainerID: "docker://abc", RestartCount: 1},
				}},
			},
			expected: false,
		},
		{
			name: "different containerID (OOM/crash - new container created)",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					PodAnnotationInPlaceUpdateStateKey: stableAnnotation("docker://abc", 0),
				}},
				Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
					{Name: "sandbox", ContainerID: "docker://xyz", RestartCount: 0},
				}},
			},
			expected: true,
		},
		{
			// stable.ContainerID == "" means the snapshot was taken while the cache
			// hadn't yet reflected the containerID; skip to avoid false positives.
			name: "stable containerID empty - skipped",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					PodAnnotationInPlaceUpdateStateKey: `{"phase":"completed","targetPodPhase":"running","stableContainerStatuses":{"sandbox":{"restartCount":0}}}`,
				}},
				Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
					{Name: "sandbox", ContainerID: "docker://xyz", RestartCount: 1},
				}},
			},
			expected: false,
		},
		{
			// containerID changed AND restartCount higher - still a restart (OOM).
			name: "different containerID with higher restartCount",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					PodAnnotationInPlaceUpdateStateKey: stableAnnotation("docker://abc", 5),
				}},
				Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
					{Name: "sandbox", ContainerID: "docker://xyz", RestartCount: 6},
				}},
			},
			expected: true,
		},
		{
			name: "container not in stable statuses - ignored",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					PodAnnotationInPlaceUpdateStateKey: stableAnnotation("docker://abc", 0),
				}},
				Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
					{Name: "other-container", ContainerID: "docker://xyz", RestartCount: 5},
				}},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasUnexpectedRestart(tt.pod); got != tt.expected {
				t.Fatalf("HasUnexpectedRestart() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestGetPodPhaseSinceAndDuration(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	creationTime := metav1.NewTime(now.Add(-10 * time.Minute))
	startingSince := now.Add(-2 * time.Minute).Truncate(time.Second)
	completedSince := now.Add(-30 * time.Second).Truncate(time.Second)

	tests := []struct {
		name         string
		pod          *corev1.Pod
		phase        string
		wantSince    time.Time
		wantDuration time.Duration
		wantOK       bool
	}{
		{
			name: "starting uses update timestamp",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: creationTime,
					Labels: map[string]string{
						agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseStarting,
					},
					Annotations: map[string]string{
						PodAnnotationInPlaceUpdateStateKey: `{"phase":"starting","targetPodPhase":"running","updateTimestamp":"` + startingSince.Format(time.RFC3339) + `"}`,
					},
				},
			},
			phase:        agentsv1alpha1.SandboxPhaseStarting,
			wantSince:    startingSince,
			wantDuration: 2 * time.Minute,
			wantOK:       true,
		},
		{
			name: "running completed uses refreshed completion timestamp",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: creationTime,
					Labels: map[string]string{
						agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseRunning,
					},
					Annotations: map[string]string{
						PodAnnotationInPlaceUpdateStateKey: `{"phase":"completed","targetPodPhase":"running","updateTimestamp":"` + completedSince.Format(time.RFC3339) + `"}`,
					},
				},
			},
			phase:        agentsv1alpha1.SandboxPhaseRunning,
			wantSince:    completedSince,
			wantDuration: 30 * time.Second,
			wantOK:       true,
		},
		{
			name: "fresh idle pod falls back to creation timestamp",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: creationTime,
					Labels: map[string]string{
						agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseIdle,
					},
				},
			},
			phase:        agentsv1alpha1.SandboxPhaseIdle,
			wantSince:    creationTime.UTC(),
			wantDuration: 10 * time.Minute,
			wantOK:       true,
		},
		{
			name: "phase mismatch returns not ok",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: creationTime,
					Labels: map[string]string{
						agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseIdle,
					},
				},
			},
			phase:  agentsv1alpha1.SandboxPhaseRunning,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			since, ok, err := GetPodPhaseSince(tt.pod, tt.phase)
			if err != nil {
				t.Fatalf("GetPodPhaseSince() error = %v", err)
			}
			if ok != tt.wantOK {
				t.Fatalf("GetPodPhaseSince() ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if !since.Equal(tt.wantSince) {
				t.Fatalf("GetPodPhaseSince() = %s, want %s", since, tt.wantSince)
			}

			duration, ok, err := GetPodPhaseDuration(tt.pod, tt.phase, now)
			if err != nil {
				t.Fatalf("GetPodPhaseDuration() error = %v", err)
			}
			if ok != tt.wantOK {
				t.Fatalf("GetPodPhaseDuration() ok = %v, want %v", ok, tt.wantOK)
			}
			if duration != tt.wantDuration {
				t.Fatalf("GetPodPhaseDuration() = %s, want %s", duration, tt.wantDuration)
			}
		})
	}
}

func newTestClientBuilder(t *testing.T) *fake.ClientBuilder {
	t.Helper()
	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("get fake client builder: %v", err)
	}
	return cb
}

// ─────────────────────────────────────────────────────────────────────────────
// Same-image fast path
// ─────────────────────────────────────────────────────────────────────────────

func TestApplyUpdate_SameImageFastPath(t *testing.T) {
	tests := []struct {
		name             string
		pod              *corev1.Pod
		opts             UpdateOptions
		expectFastPath   bool // true = phase should jump to Running directly
		expectedPhase    string
		expectStatePhase string
		expectStartedAt  bool
		expectChanged    bool
	}{
		{
			name: "same image + restartCount 0 → fast path to Running",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-fast",
					Namespace: "default",
					Labels: map[string]string{
						agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseIdle,
					},
				},
				Spec: corev1.PodSpec{Containers: []corev1.Container{
					{Name: "sandbox", Image: "myapp:v1"},
				}},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "sandbox", ContainerID: "containerd://abc123", RestartCount: 0, ImageID: "sha256:aaa"},
					},
				},
			},
			opts: UpdateOptions{
				ContainerImages: map[string]string{"sandbox": "myapp:v1"},
				Labels: map[string]string{
					agentsv1alpha1.SandboxIDLabelKey: "sbx-fast-1",
				},
				Annotations: map[string]string{
					"agentbox.navix.sh/sandbox-id": "sbx-fast-1",
				},
				TargetPodPhase: agentsv1alpha1.SandboxPhaseRunning,
				UpdatePodPhase: agentsv1alpha1.SandboxPhaseStarting,
			},
			expectFastPath:   true,
			expectedPhase:    agentsv1alpha1.SandboxPhaseRunning,
			expectStatePhase: InplaceUpdatePhaseCompleted,
			expectStartedAt:  true,
			expectChanged:    true,
		},
		{
			name: "same image + restartCount > 0 → normal path (no fast path)",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-restarted",
					Namespace: "default",
					Labels: map[string]string{
						agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseIdle,
					},
				},
				Spec: corev1.PodSpec{Containers: []corev1.Container{
					{Name: "sandbox", Image: "myapp:v1"},
				}},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "sandbox", ContainerID: "containerd://abc123", RestartCount: 1, ImageID: "sha256:aaa"},
					},
				},
			},
			opts: UpdateOptions{
				ContainerImages: map[string]string{"sandbox": "myapp:v1"},
				TargetPodPhase:  agentsv1alpha1.SandboxPhaseRunning,
				UpdatePodPhase:  agentsv1alpha1.SandboxPhaseStarting,
			},
			expectFastPath:   false,
			expectedPhase:    agentsv1alpha1.SandboxPhaseRunning, // TargetPodPhase applied via fallthrough
			expectStatePhase: "",                                 // no inplace state written (no image change)
			expectStartedAt:  false,
			expectChanged:    true, // TargetPodPhase differs from current label
		},
		{
			name: "different image → normal path (Starting with image change)",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-diff",
					Namespace: "default",
					Labels: map[string]string{
						agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseIdle,
					},
				},
				Spec: corev1.PodSpec{Containers: []corev1.Container{
					{Name: "sandbox", Image: "pause:3.10"},
				}},
				Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
					{Name: "sandbox", ContainerID: "containerd://abc123", RestartCount: 0, ImageID: "sha256:pause"},
				}},
			},
			opts: UpdateOptions{
				ContainerImages: map[string]string{"sandbox": "myapp:v1"},
				TargetPodPhase:  agentsv1alpha1.SandboxPhaseRunning,
				UpdatePodPhase:  agentsv1alpha1.SandboxPhaseStarting,
			},
			expectFastPath:   false,
			expectedPhase:    agentsv1alpha1.SandboxPhaseStarting,
			expectStatePhase: InplaceUpdatePhaseStarting,
			expectStartedAt:  false,
			expectChanged:    true,
		},
		{
			name: "same image with docker.io prefix normalization → fast path",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-dockerio",
					Namespace: "default",
					Labels: map[string]string{
						agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseIdle,
					},
				},
				Spec: corev1.PodSpec{Containers: []corev1.Container{
					{Name: "sandbox", Image: "docker.io/library/ubuntu:22.04"},
				}},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "sandbox", ContainerID: "containerd://abc123", RestartCount: 0, ImageID: "sha256:ubuntu"},
					},
				},
			},
			opts: UpdateOptions{
				ContainerImages: map[string]string{"sandbox": "ubuntu:22.04"},
				TargetPodPhase:  agentsv1alpha1.SandboxPhaseRunning,
				UpdatePodPhase:  agentsv1alpha1.SandboxPhaseStarting,
			},
			expectFastPath:   true,
			expectedPhase:    agentsv1alpha1.SandboxPhaseRunning,
			expectStatePhase: InplaceUpdatePhaseCompleted,
			expectStartedAt:  true,
			expectChanged:    true,
		},
		{
			name: "Stopping phase (not Starting) → no fast path even if images match",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-stopping",
					Namespace: "default",
					Labels: map[string]string{
						agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseRunning,
					},
				},
				Spec: corev1.PodSpec{Containers: []corev1.Container{
					{Name: "sandbox", Image: "pause:3.10"},
				}},
				Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
					{Name: "sandbox", ContainerID: "containerd://abc123", RestartCount: 0, ImageID: "sha256:pause"},
				}},
			},
			opts: UpdateOptions{
				ContainerImages: map[string]string{"sandbox": "pause:3.10"},
				TargetPodPhase:  agentsv1alpha1.SandboxPhaseIdle,
				UpdatePodPhase:  agentsv1alpha1.SandboxPhaseStopping,
			},
			expectFastPath:   false,
			expectedPhase:    agentsv1alpha1.SandboxPhaseIdle, // TargetPodPhase applied via fallthrough
			expectStatePhase: "",
			expectStartedAt:  false,
			expectChanged:    true, // TargetPodPhase differs from current label
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := tt.pod.DeepCopy()
			changed, err := applyUpdate(pod, tt.opts)
			if err != nil {
				t.Fatalf("applyUpdate error: %v", err)
			}
			if changed != tt.expectChanged {
				t.Fatalf("expected changed=%v, got %v", tt.expectChanged, changed)
			}

			if tt.expectedPhase != "" {
				gotPhase := pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey]
				if gotPhase != tt.expectedPhase {
					t.Fatalf("expected phase %q, got %q", tt.expectedPhase, gotPhase)
				}
			}

			state, stateErr := GetInplaceUpdateState(pod)
			if stateErr != nil {
				t.Fatalf("GetInplaceUpdateState error: %v", stateErr)
			}

			if tt.expectStatePhase != "" {
				if state == nil {
					t.Fatal("expected inplace update state, got nil")
				}
				if state.Phase != tt.expectStatePhase {
					t.Fatalf("expected state phase %q, got %q", tt.expectStatePhase, state.Phase)
				}
			}

			if tt.expectFastPath {
				// Fast path: should have StableContainerStatuses recorded
				if state == nil {
					t.Fatal("fast path: expected state")
				}
				if len(state.StableContainerStatuses) == 0 {
					t.Fatal("fast path: expected StableContainerStatuses to be populated")
				}
				// Verify the stable container status matches the original
				for _, cs := range tt.pod.Status.ContainerStatuses {
					stable, ok := state.StableContainerStatuses[cs.Name]
					if !ok {
						t.Fatalf("fast path: missing stable status for container %q", cs.Name)
					}
					if stable.ContainerID != cs.ContainerID {
						t.Fatalf("fast path: containerID mismatch for %q: want %q, got %q", cs.Name, cs.ContainerID, stable.ContainerID)
					}
					if stable.RestartCount != cs.RestartCount {
						t.Fatalf("fast path: restartCount mismatch for %q: want %d, got %d", cs.Name, cs.RestartCount, stable.RestartCount)
					}
				}
			}

			if tt.expectStartedAt {
				if pod.Annotations[agentsv1alpha1.SandboxStartedAtAnnotationKey] == "" {
					t.Fatal("expected started-at annotation to be set")
				}
				if pod.Annotations[agentsv1alpha1.SandboxLastActiveAnnotationKey] == "" {
					t.Fatal("expected last-active annotation to be set")
				}
			}
		})
	}
}

func TestApplyUpdate_SameImageFastPath_WithLabelsAndAnnotations(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-labels",
			Namespace: "default",
			Labels: map[string]string{
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseIdle,
				"old-label":                         "old-value",
			},
			Annotations: map[string]string{
				"old-annotation": "old-value",
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{
			{Name: "sandbox", Image: "myapp:v1"},
		}},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "sandbox", ContainerID: "containerd://abc", RestartCount: 0},
			},
		},
	}

	changed, err := applyUpdate(pod, UpdateOptions{
		ContainerImages:   map[string]string{"sandbox": "myapp:v1"},
		Labels:            map[string]string{agentsv1alpha1.SandboxIDLabelKey: "sbx-100"},
		Annotations:       map[string]string{"agentbox.navix.sh/sandbox-id": "sbx-100"},
		RemoveLabels:      []string{"old-label"},
		RemoveAnnotations: []string{"old-annotation"},
		TargetPodPhase:    agentsv1alpha1.SandboxPhaseRunning,
		UpdatePodPhase:    agentsv1alpha1.SandboxPhaseStarting,
	})
	if err != nil {
		t.Fatalf("applyUpdate error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}

	// Phase should be Running (fast path)
	if pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey] != agentsv1alpha1.SandboxPhaseRunning {
		t.Fatalf("expected Running phase, got %q", pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey])
	}

	// Labels and annotations should be applied
	if pod.Labels[agentsv1alpha1.SandboxIDLabelKey] != "sbx-100" {
		t.Fatal("expected sandbox-id label to be set")
	}
	if pod.Annotations["agentbox.navix.sh/sandbox-id"] != "sbx-100" {
		t.Fatal("expected sandbox-id annotation to be set")
	}

	// Removed labels and annotations should be gone
	if _, exists := pod.Labels["old-label"]; exists {
		t.Fatal("expected old-label to be removed")
	}
	if _, exists := pod.Annotations["old-annotation"]; exists {
		t.Fatal("expected old-annotation to be removed")
	}
}

func TestTriggerUpdateWithOptions_SameImageFastPath_E2E(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-fast-e2e",
			Namespace: "default",
			Labels: map[string]string{
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseIdle,
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{
			{Name: "sandbox", Image: "myapp:v1"},
		}},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "sandbox", ContainerID: "containerd://abc123", RestartCount: 0, ImageID: "sha256:aaa"},
			},
		},
	}

	cli := newTestClientBuilder(t).WithObjects(pod).Build()

	_, err := TriggerUpdateWithOptions(context.Background(), cli, pod, UpdateOptions{
		ContainerImages: map[string]string{"sandbox": "myapp:v1"},
		Labels: map[string]string{
			agentsv1alpha1.SandboxIDLabelKey: "sbx-e2e-1",
			agentsv1alpha1.ManagedByLabelKey: agentsv1alpha1.ManagedBySandboxAPIServer,
		},
		Annotations: map[string]string{
			"agentbox.navix.sh/sandbox-id": "sbx-e2e-1",
		},
		TargetPodPhase:              agentsv1alpha1.SandboxPhaseRunning,
		UpdatePodPhase:              agentsv1alpha1.SandboxPhaseStarting,
		ExpectedCurrentSandboxPhase: agentsv1alpha1.SandboxPhaseIdle,
	})
	if err != nil {
		t.Fatalf("TriggerUpdateWithOptions: %v", err)
	}

	updated := &corev1.Pod{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, updated); err != nil {
		t.Fatalf("get pod: %v", err)
	}

	// Should be Running directly (no Starting phase)
	if updated.Labels[agentsv1alpha1.SandboxPhaseLabelKey] != agentsv1alpha1.SandboxPhaseRunning {
		t.Fatalf("expected Running phase, got %s", updated.Labels[agentsv1alpha1.SandboxPhaseLabelKey])
	}

	// State should be completed
	state, err := GetInplaceUpdateState(updated)
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	if state == nil {
		t.Fatal("expected state")
	}
	if state.Phase != InplaceUpdatePhaseCompleted {
		t.Fatalf("expected completed state phase, got %s", state.Phase)
	}

	// started-at should be set
	if updated.Annotations[agentsv1alpha1.SandboxStartedAtAnnotationKey] == "" {
		t.Fatal("expected started-at annotation")
	}

	// Image should remain unchanged
	if updated.Spec.Containers[0].Image != "myapp:v1" {
		t.Fatalf("expected image unchanged, got %s", updated.Spec.Containers[0].Image)
	}
}

// TestMarkUpdateCompleted_ConcurrentPhaseChange_SkipsUpdate verifies that the
// phase guard in MarkUpdateCompleted fires correctly: if the pod's phase in
// Kubernetes has already changed to a different value from what was observed
// when the caller decided to invoke MarkUpdateCompleted, the call must be a
// no-op and must NOT overwrite any state (including StableContainerStatuses).
//
// This is a regression test for the bug where line 272 read from the stale
// `pod` argument instead of the freshly-fetched `current`, making the guard
// always a no-op.  In the same-image fast-path scenario the stale call would
// re-snapshot StableContainerStatuses with a potentially old ContainerID,
// which then caused HasUnexpectedRestart to fire as a false positive.
func TestMarkUpdateCompleted_ConcurrentPhaseChange_SkipsUpdate(t *testing.T) {
	now := metav1.NewTime(time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second))

	// Step 1: Create a pod that is currently in Starting phase (what the
	// "stale" caller observed).
	stalePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-concurrent",
			Namespace: "default",
			Labels: map[string]string{
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseStarting,
			},
			Annotations: map[string]string{
				PodAnnotationInPlaceUpdateStateKey: `{"phase":"starting","targetImage":"myapp:v2","targetPodPhase":"running","updateTimestamp":"` +
					now.UTC().Format(time.RFC3339) + `","lastContainerStatuses":{"sandbox":{"imageID":"sha256:old","containerID":"docker://old-id"}}}`,
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "sandbox", Image: "myapp:v2"}}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name:        "sandbox",
			ImageID:     "sha256:old",
			ContainerID: "docker://old-id",
			Ready:       false,
		}}},
	}

	cli := newTestClientBuilder(t).WithObjects(stalePod).Build()

	// Step 2: Simulate a concurrent update that already advanced the pod to
	// Running with a new ContainerID (e.g. same-image fast path completed first).
	concurrentNow := metav1.NewTime(time.Now().UTC().Add(-1 * time.Minute).Truncate(time.Second))
	alreadyRunningPod := stalePod.DeepCopy()
	alreadyRunningPod.Labels[agentsv1alpha1.SandboxPhaseLabelKey] = agentsv1alpha1.SandboxPhaseRunning
	alreadyRunningPod.Annotations[PodAnnotationInPlaceUpdateStateKey] = `{"phase":"completed","targetPodPhase":"running","updateTimestamp":"` +
		concurrentNow.UTC().Format(time.RFC3339) + `","stableContainerStatuses":{"sandbox":{"containerID":"docker://new-id","restartCount":0}}}`
	alreadyRunningPod.Status.ContainerStatuses = []corev1.ContainerStatus{{
		Name:        "sandbox",
		ImageID:     "sha256:new",
		ContainerID: "docker://new-id",
		Ready:       true,
		State:       corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
	}}
	if err := cli.Update(context.Background(), alreadyRunningPod); err != nil {
		t.Fatalf("simulate concurrent update: %v", err)
	}

	// Step 3: Call MarkUpdateCompleted with the STALE starting-phase pod.
	// The phase guard must detect that the live pod's phase ("running") differs
	// from currentPhase ("starting") and exit without writing anything.
	if _, err := MarkUpdateCompleted(context.Background(), cli, &agentsv1alpha1.SandboxPool{}, stalePod, newMockResolver()); err != nil {
		t.Fatalf("stale MarkUpdateCompleted: %v", err)
	}

	// Step 4: Verify the pod is still Running and that StableContainerStatuses
	// still contains the new ContainerID — not overwritten by the stale call.
	final := &corev1.Pod{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: stalePod.Name, Namespace: stalePod.Namespace}, final); err != nil {
		t.Fatalf("get final pod: %v", err)
	}

	if got := final.Labels[agentsv1alpha1.SandboxPhaseLabelKey]; got != agentsv1alpha1.SandboxPhaseRunning {
		t.Fatalf("phase guard failed: expected pod to remain %q, got %q", agentsv1alpha1.SandboxPhaseRunning, got)
	}

	state, err := GetInplaceUpdateState(final)
	if err != nil {
		t.Fatalf("GetInplaceUpdateState: %v", err)
	}
	if state == nil {
		t.Fatal("expected non-nil InplaceUpdateState after concurrent update")
	}
	if state.Phase != InplaceUpdatePhaseCompleted {
		t.Fatalf("expected completed phase in state, got %s", state.Phase)
	}
	stable, ok := state.StableContainerStatuses["sandbox"]
	if !ok {
		t.Fatal("expected StableContainerStatuses to contain sandbox entry")
	}
	if stable.ContainerID != "docker://new-id" {
		t.Fatalf("StableContainerStatuses overwritten by stale call: expected docker://new-id, got %s", stable.ContainerID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// IsInplaceUpdateCompleted
// ─────────────────────────────────────────────────────────────────────────────

// Realistic digest constants for test data.
const (
	digestOld       = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	digestNew       = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	digestPause     = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	digestRunning   = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	digestOldMain   = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	digestNewMain   = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	digestOldSide   = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
	digestNewSide   = "sha256:4444444444444444444444444444444444444444444444444444444444444444"
	digestManifest1 = "sha256:5555555555555555555555555555555555555555555555555555555555555555"
	digestManifest2 = "sha256:6666666666666666666666666666666666666666666666666666666666666666"
	digestConfig    = "sha256:7777777777777777777777777777777777777777777777777777777777777777"
	// Shared digest: two different image names with the same content.
	digestShared = "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
)

func imageID(repo, digest string) string {
	return repo + "@" + digest
}

func TestIsInplaceUpdateCompleted(t *testing.T) {
	makeState := func(phase, targetPodPhase string, lastStatuses map[string]InplaceUpdateContainerStatus, targetImages map[string]string) string {
		s := InplaceUpdateState{
			Phase:                 phase,
			TargetPodPhase:        targetPodPhase,
			LastContainerStatuses: lastStatuses,
			TargetImages:          targetImages,
		}
		b, _ := json.Marshal(s)
		return string(b)
	}

	poolWithIdleImage := func(img string) *agentsv1alpha1.SandboxPool {
		p := &agentsv1alpha1.SandboxPool{}
		p.Spec.IdleImage = img
		return p
	}
	emptyPool := &agentsv1alpha1.SandboxPool{}

	tests := []struct {
		name string
		pool *agentsv1alpha1.SandboxPool
		pod  *corev1.Pod
		// prePopulate populates the resolver cache before calling IsInplaceUpdateCompleted.
		// Each entry maps an image tag → digest (simulating a prior DigestFromStatus call).
		prePopulate map[string]string
		want        bool
	}{
		// ── trivial fast-exits ────────────────────────────────────────────────
		{
			name: "no annotation → completed",
			pool: emptyPool,
			pod:  &corev1.Pod{},
			want: true,
		},
		{
			name: "phase=completed annotation → completed",
			pool: emptyPool,
			pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				PodAnnotationInPlaceUpdateStateKey: makeState(InplaceUpdatePhaseCompleted, agentsv1alpha1.SandboxPhaseRunning, nil, nil),
			}}},
			want: true,
		},
		// ── spec image not yet equal to target → not completed ────────────────
		{
			name: "spec image still old (kubelet hasn't applied the patch yet) → not completed",
			pool: emptyPool,
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					PodAnnotationInPlaceUpdateStateKey: makeState(
						InplaceUpdatePhaseStarting, agentsv1alpha1.SandboxPhaseRunning,
						map[string]InplaceUpdateContainerStatus{"sandbox": {ImageID: imageID("registry.example.com/myapp", digestOld)}},
						map[string]string{"sandbox": "registry.example.com/myapp:v2"},
					),
				}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "sandbox", Image: "registry.example.com/myapp:v1"}}},
				Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
					{Name: "sandbox", Image: "registry.example.com/myapp:v1", ImageID: imageID("registry.example.com/myapp", digestOld)},
				}},
			},
			want: false,
		},
		// ── container not yet in status → not completed ───────────────────────
		{
			name: "container missing from status → not completed",
			pool: emptyPool,
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					PodAnnotationInPlaceUpdateStateKey: makeState(
						InplaceUpdatePhaseStarting, agentsv1alpha1.SandboxPhaseRunning,
						map[string]InplaceUpdateContainerStatus{"sandbox": {ImageID: imageID("registry.example.com/myapp", digestOld)}},
						map[string]string{"sandbox": "registry.example.com/myapp:v2"},
					),
				}},
				Spec:   corev1.PodSpec{Containers: []corev1.Container{{Name: "sandbox", Image: "registry.example.com/myapp:v2"}}},
				Status: corev1.PodStatus{},
			},
			want: false,
		},
		// ── Running target: digest matches (spec image == status image) ────────
		{
			name: "Running: same image name, new digest → completed",
			pool: emptyPool,
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					PodAnnotationInPlaceUpdateStateKey: makeState(
						InplaceUpdatePhaseStarting, agentsv1alpha1.SandboxPhaseRunning,
						map[string]InplaceUpdateContainerStatus{"sandbox": {ImageID: imageID("registry.example.com/myapp", digestOld)}},
						map[string]string{"sandbox": "registry.example.com/myapp:v2"},
					),
				}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "sandbox", Image: "registry.example.com/myapp:v2"}}},
				Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
					{Name: "sandbox", Image: "registry.example.com/myapp:v2", ImageID: imageID("registry.example.com/myapp", digestNew)},
				}},
			},
			prePopulate: map[string]string{"registry.example.com/myapp:v2": digestNew},
			want:        true,
		},
		{
			name: "Running: kubelet-normalised status image → completed",
			pool: emptyPool,
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					PodAnnotationInPlaceUpdateStateKey: makeState(
						InplaceUpdatePhaseStarting, agentsv1alpha1.SandboxPhaseRunning,
						map[string]InplaceUpdateContainerStatus{"sandbox": {ImageID: imageID("docker.io/library/ubuntu", digestOld)}},
						map[string]string{"sandbox": "ubuntu:22.04"},
					),
				}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "sandbox", Image: "ubuntu:22.04"}}},
				Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
					{Name: "sandbox", Image: "docker.io/library/ubuntu:22.04", ImageID: imageID("docker.io/library/ubuntu", digestNew)},
				}},
			},
			prePopulate: map[string]string{"ubuntu:22.04": digestNew},
			want:        true,
		},
		// ── Running target: digest mismatch (different content) ─────────────
		{
			name: "Running: different digest still running → not completed",
			pool: emptyPool,
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					PodAnnotationInPlaceUpdateStateKey: makeState(
						InplaceUpdatePhaseStarting, agentsv1alpha1.SandboxPhaseRunning,
						map[string]InplaceUpdateContainerStatus{"sandbox": {ImageID: imageID("registry.example.com/myapp", digestOld)}},
						map[string]string{"sandbox": "registry.example.com/myapp:v2"},
					),
				}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "sandbox", Image: "registry.example.com/myapp:v2"}}},
				Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
					{Name: "sandbox", Image: "registry.example.com/myapp:v2", ImageID: imageID("registry.example.com/myapp", digestOld)},
				}},
			},
			// Tag now points to new digest in registry, but pod still runs old.
			prePopulate: map[string]string{"registry.example.com/myapp:v2": digestNew},
			want:        false,
		},
		// ── THE BUG FIX: same digest, different image names ─────────────────
		{
			// This is the production bug: target "terminal-rl-seta-78:tag" has same
			// digest as the actually running "terminal-bench-195:tag". The digest
			// comparison detects this as completed because the content is identical.
			name: "Running: same digest different image names → completed (the bug fix)",
			pool: emptyPool,
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					PodAnnotationInPlaceUpdateStateKey: makeState(
						InplaceUpdatePhaseStarting, agentsv1alpha1.SandboxPhaseRunning,
						map[string]InplaceUpdateContainerStatus{"sandbox": {ImageID: imageID("registry.example.com/idle-base", digestOld)}},
						map[string]string{"sandbox": "registry.example.com/project/terminal-rl-seta-78:tag1"},
					),
				}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "sandbox", Image: "registry.example.com/project/terminal-rl-seta-78:tag1"}}},
				Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
					{Name: "sandbox", Image: "registry.example.com/project/terminal-bench-195:tag2",
						ImageID: imageID("registry.example.com/project/terminal-bench-195", digestShared)},
				}},
			},
			// Pre-populate: spec image → same shared digest (simulates registry HEAD result).
			prePopulate: map[string]string{
				"registry.example.com/project/terminal-rl-seta-78:tag1": digestShared,
			},
			want: true,
		},
		// ── Idle target: digest matches ───────────────────────────────────────
		{
			name: "Idle: digest matches → completed",
			pool: poolWithIdleImage("registry.example.com/pause:3.10"),
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					PodAnnotationInPlaceUpdateStateKey: makeState(
						InplaceUpdatePhaseStopping, agentsv1alpha1.SandboxPhaseIdle,
						map[string]InplaceUpdateContainerStatus{"sandbox": {ImageID: imageID("registry.example.com/running", digestRunning)}},
						map[string]string{"sandbox": "registry.example.com/pause:3.10"},
					),
				}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "sandbox", Image: "registry.example.com/pause:3.10"}}},
				Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
					{Name: "sandbox", Image: "registry.example.com/pause:3.10", ImageID: imageID("registry.example.com/pause", digestPause)},
				}},
			},
			prePopulate: map[string]string{"registry.example.com/pause:3.10": digestPause},
			want:        true,
		},
		// ── Idle target: same digest (same-digest idle image) ────────────────
		{
			name: "Idle: same digest idle image → completed",
			pool: poolWithIdleImage("registry.example.com/pause:3.10"),
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					PodAnnotationInPlaceUpdateStateKey: makeState(
						InplaceUpdatePhaseStopping, agentsv1alpha1.SandboxPhaseIdle,
						map[string]InplaceUpdateContainerStatus{"sandbox": {ImageID: imageID("registry.example.com/pause", digestPause)}},
						map[string]string{"sandbox": "registry.example.com/pause:3.10"},
					),
				}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "sandbox", Image: "registry.example.com/pause:3.10"}}},
				Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
					{Name: "sandbox", Image: "registry.example.com/pause:3.10", ImageID: imageID("registry.example.com/pause", digestPause)},
				}},
			},
			prePopulate: map[string]string{"registry.example.com/pause:3.10": digestPause},
			want:        true,
		},
		{
			name: "Idle: digest mismatch → not completed",
			pool: poolWithIdleImage("registry.example.com/pause:3.10"),
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					PodAnnotationInPlaceUpdateStateKey: makeState(
						InplaceUpdatePhaseStopping, agentsv1alpha1.SandboxPhaseIdle,
						map[string]InplaceUpdateContainerStatus{"sandbox": {ImageID: imageID("registry.example.com/running", digestRunning)}},
						map[string]string{"sandbox": "registry.example.com/pause:3.10"},
					),
				}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "sandbox", Image: "registry.example.com/pause:3.10"}}},
				Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
					// status image still shows the running image, different digest
					{Name: "sandbox", Image: "registry.example.com/myapp:v1", ImageID: imageID("registry.example.com/myapp", digestOld)},
				}},
			},
			// Spec says pause:3.10 which resolves to digestPause, but status shows digestOld.
			prePopulate: map[string]string{"registry.example.com/pause:3.10": digestPause},
			want:        false,
		},
		// ── config-digest fallback: same content re-pushed ────────────────────
		{
			// Manifest digests differ (image re-pushed to a new registry) but config
			// digests are identical → treat as completed.
			name: "manifest digests differ but config digests match → completed (re-push fallback)",
			pool: emptyPool,
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					PodAnnotationInPlaceUpdateStateKey: makeState(
						InplaceUpdatePhaseStarting, agentsv1alpha1.SandboxPhaseRunning,
						map[string]InplaceUpdateContainerStatus{"sandbox": {ImageID: imageID("registry.example.com/old-registry/app", digestOld)}},
						map[string]string{"sandbox": "registry.example.com/new-registry/app:latest"},
					),
				}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "sandbox", Image: "registry.example.com/new-registry/app:latest"}}},
				Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
					{Name: "sandbox", Image: "registry.example.com/new-registry/app:latest",
						ImageID: imageID("registry.example.com/new-registry/app", digestManifest1)},
				}},
			},
			// Manifest digests differ (manifest1 vs manifest2), but both resolve to the same config digest.
			prePopulate: map[string]string{
				"registry.example.com/new-registry/app:latest":                                digestManifest2,
				"config:" + imageID("registry.example.com/new-registry/app", digestManifest1): digestConfig,
				"config:registry.example.com/new-registry/app:latest":                         digestConfig,
			},
			want: true,
		},
		{
			// Manifest digests differ AND config digests differ → truly different images, not completed.
			name: "manifest digests differ and config digests also differ → not completed",
			pool: emptyPool,
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					PodAnnotationInPlaceUpdateStateKey: makeState(
						InplaceUpdatePhaseStarting, agentsv1alpha1.SandboxPhaseRunning,
						map[string]InplaceUpdateContainerStatus{"sandbox": {ImageID: imageID("registry.example.com/app", digestOld)}},
						map[string]string{"sandbox": "registry.example.com/app:v2"},
					),
				}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "sandbox", Image: "registry.example.com/app:v2"}}},
				Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
					{Name: "sandbox", Image: "registry.example.com/app:v2",
						ImageID: imageID("registry.example.com/app", digestManifest1)},
				}},
			},
			prePopulate: map[string]string{
				"registry.example.com/app:v2":                                    digestManifest2,
				"config:" + imageID("registry.example.com/app", digestManifest1): digestConfig,
				"config:registry.example.com/app:v2":                             digestNew, // different config digest
			},
			want: false,
		},
		// ── multi-container: all must pass ────────────────────────────────────
		{
			name: "Running: both containers' digests match → completed",
			pool: emptyPool,
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					PodAnnotationInPlaceUpdateStateKey: makeState(
						InplaceUpdatePhaseStarting, agentsv1alpha1.SandboxPhaseRunning,
						map[string]InplaceUpdateContainerStatus{
							"main":    {ImageID: imageID("registry.example.com/main", digestOldMain)},
							"sidecar": {ImageID: imageID("registry.example.com/sidecar", digestOldSide)},
						},
						map[string]string{"main": "registry.example.com/main:v2", "sidecar": "registry.example.com/sidecar:v2"},
					),
				}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{
					{Name: "main", Image: "registry.example.com/main:v2"},
					{Name: "sidecar", Image: "registry.example.com/sidecar:v2"},
				}},
				Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
					{Name: "main", Image: "registry.example.com/main:v2", ImageID: imageID("registry.example.com/main", digestNewMain)},
					{Name: "sidecar", Image: "registry.example.com/sidecar:v2", ImageID: imageID("registry.example.com/sidecar", digestNewSide)},
				}},
			},
			prePopulate: map[string]string{
				"registry.example.com/main:v2":    digestNewMain,
				"registry.example.com/sidecar:v2": digestNewSide,
			},
			want: true,
		},
		{
			name: "Running: one container digest still old → not completed",
			pool: emptyPool,
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					PodAnnotationInPlaceUpdateStateKey: makeState(
						InplaceUpdatePhaseStarting, agentsv1alpha1.SandboxPhaseRunning,
						map[string]InplaceUpdateContainerStatus{
							"main":    {ImageID: imageID("registry.example.com/main", digestOldMain)},
							"sidecar": {ImageID: imageID("registry.example.com/sidecar", digestOldSide)},
						},
						map[string]string{"main": "registry.example.com/main:v2", "sidecar": "registry.example.com/sidecar:v2"},
					),
				}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{
					{Name: "main", Image: "registry.example.com/main:v2"},
					{Name: "sidecar", Image: "registry.example.com/sidecar:v2"},
				}},
				Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
					{Name: "main", Image: "registry.example.com/main:v2", ImageID: imageID("registry.example.com/main", digestNewMain)},
					// sidecar digest still old
					{Name: "sidecar", Image: "registry.example.com/sidecar:v2", ImageID: imageID("registry.example.com/sidecar", digestOldSide)},
				}},
			},
			prePopulate: map[string]string{
				"registry.example.com/main:v2":    digestNewMain,
				"registry.example.com/sidecar:v2": digestNewSide,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := newMockResolver()
			// Pre-populate seed digests for test cases that need tag→digest mappings.
			maps.Copy(resolver.seedDigests, tt.prePopulate)
			got := IsInplaceUpdateCompleted(context.Background(), tt.pool, tt.pod, resolver)
			if got != tt.want {
				t.Fatalf("IsInplaceUpdateCompleted() = %v, want %v", got, tt.want)
			}
		})
	}
}
