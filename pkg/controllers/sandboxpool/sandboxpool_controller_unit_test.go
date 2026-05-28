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

package sandboxpool

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/inplaceupdate"
	"github.com/scitix/agent-sandbox/pkg/store"
	"github.com/scitix/agent-sandbox/pkg/utils/imageresolver"
)

func TestCalculatePodStatus(t *testing.T) {
	reconciler := &SandboxPoolReconciler{}

	makePool := func(desired int32) *agentsv1alpha1.SandboxPool {
		return &agentsv1alpha1.SandboxPool{
			Spec: agentsv1alpha1.SandboxPoolSpec{Replicas: desired},
		}
	}

	// helper: create an idle pod with a specific PodReady condition
	makeIdlePod := func(name string, ready corev1.ConditionStatus) corev1.Pod {
		return corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
				Labels: map[string]string{
					agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseIdle,
				},
			},
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: ready},
				},
			},
		}
	}

	tests := []struct {
		name            string
		pods            []corev1.Pod
		desiredReplicas int32
		wantIdle        int32
		wantUnavailIdle int32
		wantRunning     int32
		wantStarting    int32
		wantStopping    int32
		wantFailed      int32
		wantPhase       agentsv1alpha1.SandboxPoolPhase
	}{
		{
			name: "all idle pods - all ready",
			pods: []corev1.Pod{
				makeIdlePod("pod1", corev1.ConditionTrue),
				makeIdlePod("pod2", corev1.ConditionTrue),
				makeIdlePod("pod3", corev1.ConditionTrue),
			},
			desiredReplicas: 3,
			wantIdle:        3,
			wantUnavailIdle: 0,
			wantPhase:       agentsv1alpha1.SandboxPoolPhaseReady,
		},
		{
			name: "idle pods - some NotReady",
			pods: []corev1.Pod{
				makeIdlePod("pod1", corev1.ConditionTrue),
				makeIdlePod("pod2", corev1.ConditionFalse),
				makeIdlePod("pod3", corev1.ConditionFalse),
			},
			desiredReplicas: 3,
			wantIdle:        3,
			wantUnavailIdle: 2,
			wantPhase:       agentsv1alpha1.SandboxPoolPhaseDegraded,
		},
		{
			name: "idle pod - PodReady condition absent (not yet scheduled)",
			pods: []corev1.Pod{
				createTestPod("pod1", agentsv1alpha1.SandboxPhaseIdle), // no conditions
			},
			desiredReplicas: 1,
			wantIdle:        1,
			wantUnavailIdle: 1,
			wantPhase:       agentsv1alpha1.SandboxPoolPhaseDegraded,
		},
		{
			name: "mixed phase pods",
			pods: []corev1.Pod{
				makeIdlePod("pod1", corev1.ConditionTrue),
				createTestPod("pod2", agentsv1alpha1.SandboxPhaseRunning),
				createTestPod("pod3", agentsv1alpha1.SandboxPhaseStarting),
				createTestPod("pod4", agentsv1alpha1.SandboxPhaseStopping),
				createTestPod("pod5", agentsv1alpha1.SandboxPhaseFailed),
			},
			desiredReplicas: 5,
			wantIdle:        1,
			wantUnavailIdle: 0,
			wantRunning:     1,
			wantStarting:    1,
			wantStopping:    1,
			wantFailed:      1,
			wantPhase:       agentsv1alpha1.SandboxPoolPhaseDegraded, // failed > 0
		},
		{
			name: "unknown phase treated as idle",
			pods: []corev1.Pod{
				createTestPod("pod1", "unknown"),
				makeIdlePod("pod2", corev1.ConditionTrue),
			},
			desiredReplicas: 2,
			wantIdle:        2,
			wantUnavailIdle: 1, // "unknown" pod has no Ready condition
			wantPhase:       agentsv1alpha1.SandboxPoolPhaseDegraded,
		},
		{
			name:            "empty pods list - desired 0",
			pods:            []corev1.Pod{},
			desiredReplicas: 0,
			wantPhase:       agentsv1alpha1.SandboxPoolPhasePending,
		},
		{
			name:            "scaling up: current < desired",
			pods:            []corev1.Pod{makeIdlePod("pod1", corev1.ConditionTrue)},
			desiredReplicas: 3,
			wantIdle:        1,
			wantPhase:       agentsv1alpha1.SandboxPoolPhaseScalingUp,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := makePool(tt.desiredReplicas)
			result := reconciler.calculatePodStatus("test-ns/test-pool", tt.pods, pool)
			if result.IdleReplicas != tt.wantIdle {
				t.Errorf("IdleReplicas = %v, want %v", result.IdleReplicas, tt.wantIdle)
			}
			if result.UnavailableIdleReplicas != tt.wantUnavailIdle {
				t.Errorf("UnavailableIdleReplicas = %v, want %v", result.UnavailableIdleReplicas, tt.wantUnavailIdle)
			}
			if result.RunningReplicas != tt.wantRunning {
				t.Errorf("RunningReplicas = %v, want %v", result.RunningReplicas, tt.wantRunning)
			}
			if result.StartingReplicas != tt.wantStarting {
				t.Errorf("StartingReplicas = %v, want %v", result.StartingReplicas, tt.wantStarting)
			}
			if result.StoppingReplicas != tt.wantStopping {
				t.Errorf("StoppingReplicas = %v, want %v", result.StoppingReplicas, tt.wantStopping)
			}
			if result.FailedReplicas != tt.wantFailed {
				t.Errorf("FailedReplicas = %v, want %v", result.FailedReplicas, tt.wantFailed)
			}
			if result.Phase != tt.wantPhase {
				t.Errorf("Phase = %v, want %v", result.Phase, tt.wantPhase)
			}
		})
	}
}

func TestSortPodsByPhasePriority(t *testing.T) {
	reconciler := &SandboxPoolReconciler{}

	now := time.Now()
	pods := []corev1.Pod{
		createTestPodWithTime("pod-running", agentsv1alpha1.SandboxPhaseRunning, now.Add(1*time.Hour)),
		createTestPodWithTime("pod-starting", agentsv1alpha1.SandboxPhaseStarting, now.Add(2*time.Hour)),
		createTestPodWithTime("pod-idle-1", agentsv1alpha1.SandboxPhaseIdle, now.Add(3*time.Hour)),
		createTestPodWithTime("pod-failed", agentsv1alpha1.SandboxPhaseFailed, now.Add(4*time.Hour)),
		createTestPodWithTime("pod-idle-2", agentsv1alpha1.SandboxPhaseIdle, now.Add(5*time.Hour)),
	}

	sorted := reconciler.sortPodsByPhasePriority(pods)

	// Expected order: idle, idle, starting, failed, running
	// Within same phase, older pods first
	expectedOrder := []string{"pod-idle-1", "pod-idle-2", "pod-starting", "pod-failed", "pod-running"}

	if len(sorted) != len(expectedOrder) {
		t.Fatalf("Expected %d pods, got %d", len(expectedOrder), len(sorted))
	}

	for i, pod := range sorted {
		if pod.Name != expectedOrder[i] {
			t.Errorf("Position %d: got %s, want %s", i, pod.Name, expectedOrder[i])
		}
	}
}

func TestSortPodsByPhasePriority_MultipleRunning(t *testing.T) {
	reconciler := &SandboxPoolReconciler{}

	now := time.Now()
	pods := []corev1.Pod{
		createTestPodWithTime("pod-running-1", agentsv1alpha1.SandboxPhaseRunning, now.Add(1*time.Hour)),
		createTestPodWithTime("pod-running-2", agentsv1alpha1.SandboxPhaseRunning, now.Add(2*time.Hour)),
		createTestPodWithTime("pod-running-3", agentsv1alpha1.SandboxPhaseRunning, now.Add(3*time.Hour)),
	}

	sorted := reconciler.sortPodsByPhasePriority(pods)

	// All running pods should be last, sorted by age (oldest first)
	expectedOrder := []string{"pod-running-1", "pod-running-2", "pod-running-3"}

	if len(sorted) != len(expectedOrder) {
		t.Fatalf("Expected %d pods, got %d", len(expectedOrder), len(sorted))
	}

	for i, pod := range sorted {
		if pod.Name != expectedOrder[i] {
			t.Errorf("Position %d: got %s, want %s", i, pod.Name, expectedOrder[i])
		}
	}
}

func TestSortPodsByPhasePriority_EmptyPhase(t *testing.T) {
	reconciler := &SandboxPoolReconciler{}

	now := time.Now()
	pods := []corev1.Pod{
		createTestPodWithTime("pod-empty-phase", "", now.Add(1*time.Hour)),
		createTestPodWithTime("pod-idle", agentsv1alpha1.SandboxPhaseIdle, now.Add(2*time.Hour)),
		createTestPodWithTime("pod-running", agentsv1alpha1.SandboxPhaseRunning, now.Add(3*time.Hour)),
	}

	sorted := reconciler.sortPodsByPhasePriority(pods)

	// Empty phase should be treated as idle (highest priority)
	expectedOrder := []string{"pod-empty-phase", "pod-idle", "pod-running"}

	if len(sorted) != len(expectedOrder) {
		t.Fatalf("Expected %d pods, got %d", len(expectedOrder), len(sorted))
	}

	for i, pod := range sorted {
		if pod.Name != expectedOrder[i] {
			t.Errorf("Position %d: got %s, want %s", i, pod.Name, expectedOrder[i])
		}
	}
}

func TestStatusEquals(t *testing.T) {
	reconciler := &SandboxPoolReconciler{}

	pool := &agentsv1alpha1.SandboxPool{
		Status: agentsv1alpha1.SandboxPoolStatus{
			Phase:                   agentsv1alpha1.SandboxPoolPhaseReady,
			IdleReplicas:            1,
			UnavailableIdleReplicas: 0,
			RunningReplicas:         2,
			StartingReplicas:        3,
			StoppingReplicas:        4,
			FailedReplicas:          5,
		},
	}

	tests := []struct {
		name      string
		newStatus agentsv1alpha1.SandboxPoolStatus
		expected  bool
	}{
		{
			name: "identical status",
			newStatus: agentsv1alpha1.SandboxPoolStatus{
				Phase:                   agentsv1alpha1.SandboxPoolPhaseReady,
				IdleReplicas:            1,
				UnavailableIdleReplicas: 0,
				RunningReplicas:         2,
				StartingReplicas:        3,
				StoppingReplicas:        4,
				FailedReplicas:          5,
			},
			expected: true,
		},
		{
			name: "different phase",
			newStatus: agentsv1alpha1.SandboxPoolStatus{
				Phase:            agentsv1alpha1.SandboxPoolPhaseDegraded,
				IdleReplicas:     1,
				RunningReplicas:  2,
				StartingReplicas: 3,
				StoppingReplicas: 4,
				FailedReplicas:   5,
			},
			expected: false,
		},
		{
			name: "different unavailable idle count",
			newStatus: agentsv1alpha1.SandboxPoolStatus{
				Phase:                   agentsv1alpha1.SandboxPoolPhaseReady,
				IdleReplicas:            1,
				UnavailableIdleReplicas: 1,
				RunningReplicas:         2,
				StartingReplicas:        3,
				StoppingReplicas:        4,
				FailedReplicas:          5,
			},
			expected: false,
		},
		{
			name: "different idle count",
			newStatus: agentsv1alpha1.SandboxPoolStatus{
				Phase:            agentsv1alpha1.SandboxPoolPhaseReady,
				IdleReplicas:     2,
				RunningReplicas:  2,
				StartingReplicas: 3,
				StoppingReplicas: 4,
				FailedReplicas:   5,
			},
			expected: false,
		},
		{
			name: "different running count",
			newStatus: agentsv1alpha1.SandboxPoolStatus{
				Phase:            agentsv1alpha1.SandboxPoolPhaseReady,
				IdleReplicas:     1,
				RunningReplicas:  3,
				StartingReplicas: 3,
				StoppingReplicas: 4,
				FailedReplicas:   5,
			},
			expected: false,
		},
		{
			name: "different condition status",
			newStatus: agentsv1alpha1.SandboxPoolStatus{
				Phase:            agentsv1alpha1.SandboxPoolPhaseReady,
				IdleReplicas:     1,
				RunningReplicas:  2,
				StartingReplicas: 3,
				StoppingReplicas: 4,
				FailedReplicas:   5,
				Conditions: []metav1.Condition{
					{Type: agentsv1alpha1.SandboxPoolConditionAvailable, Status: metav1.ConditionTrue},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reconciler.statusEquals(pool, tt.newStatus)
			if result != tt.expected {
				t.Errorf("statusEquals() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestContainsString(t *testing.T) {
	tests := []struct {
		name  string
		slice []string
		s     string
		want  bool
	}{
		{
			name:  "string exists in slice",
			slice: []string{"a", "b", "c"},
			s:     "b",
			want:  true,
		},
		{
			name:  "string does not exist in slice",
			slice: []string{"a", "b", "c"},
			s:     "d",
			want:  false,
		},
		{
			name:  "empty slice",
			slice: []string{},
			s:     "a",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsString(tt.slice, tt.s); got != tt.want {
				t.Errorf("containsString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRemoveString(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		s        string
		expected []string
	}{
		{
			name:     "remove existing string",
			slice:    []string{"a", "b", "c"},
			s:        "b",
			expected: []string{"a", "c"},
		},
		{
			name:     "remove non-existing string",
			slice:    []string{"a", "b", "c"},
			s:        "d",
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "remove from empty slice",
			slice:    []string{},
			s:        "a",
			expected: []string{},
		},
		{
			name:     "remove all occurrences",
			slice:    []string{"a", "b", "a", "c"},
			s:        "a",
			expected: []string{"b", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeString(tt.slice, tt.s)
			if len(result) != len(tt.expected) {
				t.Errorf("removeString() length = %v, want %v", len(result), len(tt.expected))
				return
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("removeString() position %d = %v, want %v", i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestSyncInplaceUpdatePhases(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	const (
		digestOld = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		digestNew = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "updating-pod",
			Namespace: "default",
			Labels: map[string]string{
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseStarting,
			},
			Annotations: map[string]string{
				inplaceupdate.PodAnnotationInPlaceUpdateStateKey: `{"phase":"starting","targetImage":"registry.example.com/sandbox:v2","targetPodPhase":"running","lastContainerStatuses":{"sandbox":{"imageID":"registry.example.com/sandbox@` + digestOld + `"}}}`,
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "sandbox", Image: "registry.example.com/sandbox:v2"}}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name: "sandbox", ImageID: "registry.example.com/sandbox@" + digestNew, Image: "registry.example.com/sandbox:v2",
			Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
		}}},
	}

	cli := newTestClientBuilder(t).WithObjects(pod).Build()
	reconciler := &SandboxPoolReconciler{Client: cli, Scheme: scheme, DigestResolver: &testDigestResolver{
		digests: map[string]string{
			"registry.example.com/sandbox:v2":           digestNew,
			"registry.example.com/sandbox@" + digestNew: digestNew,
			"registry.example.com/sandbox@" + digestOld: digestOld,
		},
	}}
	resultPods, err := reconciler.syncInplaceUpdatePhases(context.Background(), &agentsv1alpha1.SandboxPool{}, []corev1.Pod{*pod})
	if err != nil {
		t.Fatalf("sync inplace update phases: %v", err)
	}
	if len(resultPods) == 0 || resultPods[0].Labels[agentsv1alpha1.SandboxPhaseLabelKey] != agentsv1alpha1.SandboxPhaseRunning {
		t.Fatal("expected pod to be updated to Running in returned slice")
	}

	stored := &corev1.Pod{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, stored); err != nil {
		t.Fatalf("get pod: %v", err)
	}
	if stored.Labels[agentsv1alpha1.SandboxPhaseLabelKey] != agentsv1alpha1.SandboxPhaseRunning {
		t.Fatalf("expected running phase, got %s", stored.Labels[agentsv1alpha1.SandboxPhaseLabelKey])
	}
	state, err := inplaceupdate.GetInplaceUpdateState(stored)
	if err != nil {
		t.Fatalf("get update state: %v", err)
	}
	if state.Phase != inplaceupdate.InplaceUpdatePhaseCompleted {
		t.Fatalf("expected completed state, got %s", state.Phase)
	}
}

// testDigestResolver is a simple mock for controller tests.
type testDigestResolver struct {
	digests map[string]string
}

func (r *testDigestResolver) Resolve(_ context.Context, imageRef string, _ ...imageresolver.ResolveOption) (string, error) {
	if d, ok := r.digests[imageRef]; ok {
		return d, nil
	}
	return "", fmt.Errorf("unknown image: %s", imageRef)
}

func (r *testDigestResolver) DigestFromStatus(image, imageID string) (string, error) {
	if d, ok := r.digests[imageID]; ok {
		return d, nil
	}
	// Try to extract digest from imageID directly.
	resolver := imageresolver.NewResolver(nil, time.Hour)
	return resolver.DigestFromStatus(image, imageID)
}

func (r *testDigestResolver) ResolveConfigDigest(_ context.Context, imageRef string, _ ...imageresolver.ResolveOption) (string, error) {
	if d, ok := r.digests["config:"+imageRef]; ok {
		return d, nil
	}
	return "", fmt.Errorf("unknown config digest: %s", imageRef)
}

func TestSyncInplaceUpdatePhases_StoppingToIdle_WritesStoreRecord(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	runningImagesJSON := `{"sandbox":"sandbox:v1"}`
	claimedAt := "2026-01-01T10:00:00Z"
	terminatedAt := "2026-01-01T11:00:00Z"
	sandboxID := "sbx-stopping-test"

	const (
		stoppingDigestOld = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		stoppingDigestNew = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stopping-pod",
			Namespace: "default",
			Labels: map[string]string{
				agentsv1alpha1.SandboxPoolLabelKey:  "pool-a",
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseStopping,
				agentsv1alpha1.SandboxIDLabelKey:    sandboxID,
			},
			Annotations: map[string]string{
				// inplace-update state showing the update is done (pause:3.10 is now running).
				inplaceupdate.PodAnnotationInPlaceUpdateStateKey: `{"phase":"stopping","targetImage":"registry.example.com/pause:3.10","targetPodPhase":"idle","lastContainerStatuses":{"sandbox":{"imageID":"registry.example.com/sandbox@` + stoppingDigestOld + `"}}}`,
				// stop metadata written by ReleaseSandboxPod.
				agentsv1alpha1.SandboxIDAnnotationKey:            sandboxID,
				agentsv1alpha1.SandboxClaimedAtAnnotationKey:     claimedAt,
				agentsv1alpha1.SandboxStopReasonAnnotationKey:    string(agentsv1alpha1.SandboxStopReasonCompleted),
				agentsv1alpha1.SandboxTerminatedAtAnnotationKey:  terminatedAt,
				agentsv1alpha1.SandboxRunningImagesAnnotationKey: runningImagesJSON,
				agentsv1alpha1.SandboxContainerIDAnnotationKey:   "containerd://stopping-pod-cid",
			},
		},
		Spec:   corev1.PodSpec{Containers: []corev1.Container{{Name: "sandbox", Image: "registry.example.com/pause:3.10"}}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "sandbox", ImageID: "registry.example.com/pause@" + stoppingDigestNew, Image: "registry.example.com/pause:3.10"}}},
	}

	testStore := newTestStore(t)
	cli := newTestClientBuilder(t).WithObjects(pod).Build()
	reconciler := &SandboxPoolReconciler{Client: cli, Scheme: scheme, SandboxStore: testStore, DigestResolver: &testDigestResolver{
		digests: map[string]string{
			"registry.example.com/pause:3.10":                   stoppingDigestNew,
			"registry.example.com/pause@" + stoppingDigestNew:   stoppingDigestNew,
			"registry.example.com/sandbox@" + stoppingDigestOld: stoppingDigestOld,
		},
	}}

	resultPods, err := reconciler.syncInplaceUpdatePhases(context.Background(), &agentsv1alpha1.SandboxPool{
		Spec: agentsv1alpha1.SandboxPoolSpec{
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "registry.example.com/pause:3.10",
			},
		},
	}, []corev1.Pod{*pod})
	if err != nil {
		t.Fatalf("sync inplace update phases: %v", err)
	}
	if len(resultPods) == 0 || resultPods[0].Labels[agentsv1alpha1.SandboxPhaseLabelKey] != agentsv1alpha1.SandboxPhaseIdle {
		t.Fatal("expected pod to be updated to Idle in returned slice")
	}

	// Pod should now be Idle.
	stored := &corev1.Pod{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, stored); err != nil {
		t.Fatalf("get pod: %v", err)
	}
	if stored.Labels[agentsv1alpha1.SandboxPhaseLabelKey] != agentsv1alpha1.SandboxPhaseIdle {
		t.Fatalf("expected idle phase after Stopping→Idle, got %s", stored.Labels[agentsv1alpha1.SandboxPhaseLabelKey])
	}
	// sandbox-id label must be cleaned up.
	if stored.Labels[agentsv1alpha1.SandboxIDLabelKey] != "" {
		t.Fatalf("expected sandbox-id label removed after cleanup, got %q", stored.Labels[agentsv1alpha1.SandboxIDLabelKey])
	}
	// stop-reason annotation must be cleaned up.
	if stored.Annotations[agentsv1alpha1.SandboxStopReasonAnnotationKey] != "" {
		t.Fatalf("expected stop-reason annotation removed after cleanup, got %q", stored.Annotations[agentsv1alpha1.SandboxStopReasonAnnotationKey])
	}
	// container-id annotation must be cleaned up.
	if stored.Annotations[agentsv1alpha1.SandboxContainerIDAnnotationKey] != "" {
		t.Fatalf("expected container-id annotation removed after cleanup, got %q", stored.Annotations[agentsv1alpha1.SandboxContainerIDAnnotationKey])
	}

	// Store should have a Completed record.
	record, getErr := testStore.Get("default", sandboxID)
	if getErr != nil {
		t.Fatalf("get store record: %v", getErr)
	}
	if record == nil {
		t.Fatal("expected Completed record in store after Stopping→Idle")
	}
	if string(record.Status) != string(agentsv1alpha1.SandboxStopReasonCompleted) {
		t.Fatalf("expected status Completed, got %s", record.Status)
	}
	if record.TerminatedAt == nil || record.TerminatedAt.Format(time.RFC3339) != terminatedAt {
		t.Fatalf("expected terminatedAt %s, got %v", terminatedAt, record.TerminatedAt)
	}
	if record.ClaimedAt.Format(time.RFC3339) != claimedAt {
		t.Fatalf("expected claimedAt %s, got %s", claimedAt, record.ClaimedAt.Format(time.RFC3339))
	}
	if record.ContainerImages == nil || (*record.ContainerImages)["sandbox"] != "sandbox:v1" {
		t.Fatalf("expected container image sandbox:v1, got %v", record.ContainerImages)
	}
	if record.ContainerId == nil || *record.ContainerId != "containerd://stopping-pod-cid" {
		t.Fatalf("expected containerID containerd://stopping-pod-cid, got %v", record.ContainerId)
	}
}

func createTestPod(name string, phase string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				agentsv1alpha1.SandboxPhaseLabelKey: phase,
			},
		},
	}
}

func createTestPodWithTime(name string, phase string, creationTime time.Time) corev1.Pod {
	pod := createTestPod(name, phase)
	pod.CreationTimestamp = metav1.NewTime(creationTime)
	return pod
}

func newTestStore(t *testing.T) store.SandboxStore {
	t.Helper()
	s, err := store.NewSandboxStore(time.Hour)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func makeRunningPodWithStableStatuses(name, namespace, poolName, sandboxID, containerID string, restartCount int32) *corev1.Pod {
	stableAnnotation := `{"phase":"completed","targetPodPhase":"running","stableContainerStatuses":{"sandbox":{"containerID":"` +
		containerID + `","restartCount":` + formatInt32(restartCount) + `}}}`
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				agentsv1alpha1.SandboxPoolLabelKey:  poolName,
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseRunning,
				agentsv1alpha1.SandboxIDLabelKey:    sandboxID,
				agentsv1alpha1.ManagedByLabelKey:    agentsv1alpha1.ManagedBySandboxAPIServer,
			},
			Annotations: map[string]string{
				inplaceupdate.PodAnnotationInPlaceUpdateStateKey: stableAnnotation,
				agentsv1alpha1.SandboxIDAnnotationKey:            sandboxID,
				agentsv1alpha1.SandboxClaimedAtAnnotationKey:     "2026-01-01T10:00:00Z",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "sandbox", Image: "sandbox:v1"}},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "sandbox", ContainerID: containerID, RestartCount: restartCount},
			},
		},
	}
}

func formatInt32(n int32) string {
	if n == 0 {
		return "0"
	}
	result := ""
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		result = string('0'+byte(n%10)) + result
		n /= 10
	}
	if neg {
		result = "-" + result
	}
	return result
}

func TestSyncRestartedRunningPods_NoRunningPods(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := agentsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add agents scheme: %v", err)
	}

	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool-a", Namespace: "default"},
	}
	pods := []corev1.Pod{
		createTestPod("idle-pod", agentsv1alpha1.SandboxPhaseIdle),
	}

	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	reconciler := &SandboxPoolReconciler{Client: cli, Scheme: scheme}
	_, err := reconciler.syncRestartedRunningPods(context.Background(), pool, pods)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFilterPodsNotDeleting(t *testing.T) {
	deletionTime := metav1.Now()
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "running",
				Labels: map[string]string{
					agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseRunning,
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "terminating",
				DeletionTimestamp: &deletionTime,
				Labels: map[string]string{
					agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseRunning,
				},
			},
		},
	}

	filtered := filterPodsNotDeleting(pods)
	if len(filtered) != 1 {
		t.Fatalf("expected 1 non-terminating pod, got %d", len(filtered))
	}
	if filtered[0].Name != "running" {
		t.Fatalf("expected remaining pod to be running, got %s", filtered[0].Name)
	}
}

func TestSyncRestartedRunningPods_NoStableStatuses(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := agentsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add agents scheme: %v", err)
	}

	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool-a", Namespace: "default"},
	}
	// Running pod with NO stable statuses in annotation (e.g., old-format pod)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "running-pod",
			Namespace: "default",
			Labels: map[string]string{
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseRunning,
			},
			Annotations: map[string]string{
				inplaceupdate.PodAnnotationInPlaceUpdateStateKey: `{"phase":"completed","targetPodPhase":"running"}`,
			},
		},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
			{Name: "sandbox", ContainerID: "docker://abc", RestartCount: 5},
		}},
	}

	cli := newTestClientBuilder(t).WithObjects(pool, pod).Build()
	reconciler := &SandboxPoolReconciler{Client: cli, Scheme: scheme}
	_, err := reconciler.syncRestartedRunningPods(context.Background(), pool, []corev1.Pod{*pod})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSyncRestartedRunningPods_MatchingContainerID_NoRecycle(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := agentsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add agents scheme: %v", err)
	}

	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool-a", Namespace: "default"},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "pause:3.10",
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "sandbox", Image: "myapp:v1"}},
					},
				},
			},
		},
	}
	// Pod containerID matches stable — no restart detected
	pod := makeRunningPodWithStableStatuses("pod-ok", "default", "pool-a", "sbx-001", "docker://abc123", 0)

	cli := newTestClientBuilder(t).WithObjects(pool, pod).Build()
	reconciler := &SandboxPoolReconciler{Client: cli, Scheme: scheme}
	_, err := reconciler.syncRestartedRunningPods(context.Background(), pool, []corev1.Pod{*pod})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Pod should still be Running
	stored := &corev1.Pod{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, stored); err != nil {
		t.Fatalf("get pod: %v", err)
	}
	if stored.Labels[agentsv1alpha1.SandboxPhaseLabelKey] != agentsv1alpha1.SandboxPhaseRunning {
		t.Fatalf("expected running phase, got %s", stored.Labels[agentsv1alpha1.SandboxPhaseLabelKey])
	}
}

func TestSyncRestartedRunningPods_ChangedContainerID_Recycled(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := agentsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add agents scheme: %v", err)
	}

	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool-a", Namespace: "default"},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "pause:3.10",
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "sandbox", Image: "myapp:v1"}},
					},
				},
			},
		},
	}
	testStore := newTestStore(t)
	// Pod has different containerID from stable → OOM detected
	// Stable records containerID=docker://old, but current is docker://new.
	// The container's terminated FinishedAt is set to a fixed past time so we
	// can verify the controller records the DETECTION time (now), not this
	// stale kubelet-reported time.
	containerFinishedAt := metav1.NewTime(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-oom",
			Namespace: "default",
			Labels: map[string]string{
				agentsv1alpha1.SandboxPoolLabelKey:  "pool-a",
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseRunning,
				agentsv1alpha1.SandboxIDLabelKey:    "sbx-oom",
				agentsv1alpha1.ManagedByLabelKey:    agentsv1alpha1.ManagedBySandboxAPIServer,
			},
			Annotations: map[string]string{
				inplaceupdate.PodAnnotationInPlaceUpdateStateKey: `{"phase":"completed","targetPodPhase":"running","stableContainerStatuses":{"sandbox":{"containerID":"docker://old","restartCount":0}}}`,
				agentsv1alpha1.SandboxIDAnnotationKey:            "sbx-oom",
				agentsv1alpha1.SandboxClaimedAtAnnotationKey:     "2026-01-01T10:00:00Z",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "sandbox", Image: "sandbox:v1"}},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "sandbox",
					ContainerID:  "docker://new", // different from stable
					RestartCount: 1,
					LastTerminationState: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason:     "OOMKilled",
							ExitCode:   137,
							FinishedAt: containerFinishedAt,
						},
					},
				},
			},
		},
	}

	cli := newTestClientBuilder(t).WithObjects(pool, pod).Build()
	reconciler := &SandboxPoolReconciler{Client: cli, Scheme: scheme, SandboxStore: testStore}
	before := time.Now()
	resultPods, err := reconciler.syncRestartedRunningPods(context.Background(), pool, []corev1.Pod{*pod})
	after := time.Now()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resultPods) == 0 || resultPods[0].Labels[agentsv1alpha1.SandboxPhaseLabelKey] != agentsv1alpha1.SandboxPhaseStopping {
		t.Fatal("expected returned pod to be in Stopping phase when containerID changed")
	}

	// Pod should now be in Stopping phase.
	stored := &corev1.Pod{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, stored); err != nil {
		t.Fatalf("get pod: %v", err)
	}
	if stored.Labels[agentsv1alpha1.SandboxPhaseLabelKey] != agentsv1alpha1.SandboxPhaseStopping {
		t.Fatalf("expected stopping phase, got %s", stored.Labels[agentsv1alpha1.SandboxPhaseLabelKey])
	}
	// sandbox-id label must be KEPT during Stopping.
	if stored.Labels[agentsv1alpha1.SandboxIDLabelKey] == "" {
		t.Fatalf("expected sandbox-id label to be kept during Stopping, got %#v", stored.Labels)
	}
	// stop-reason annotation must be set.
	if stored.Annotations[agentsv1alpha1.SandboxStopReasonAnnotationKey] != string(agentsv1alpha1.SandboxStopReasonFailed) {
		t.Fatalf("expected stop-reason=Failed annotation, got %q", stored.Annotations[agentsv1alpha1.SandboxStopReasonAnnotationKey])
	}
	// failure-reason annotation must be set.
	if stored.Annotations[agentsv1alpha1.SandboxFailureReasonAnnotationKey] != "OOMKilled" {
		t.Fatalf("expected failure-reason=OOMKilled annotation, got %q", stored.Annotations[agentsv1alpha1.SandboxFailureReasonAnnotationKey])
	}
	// terminated-at annotation must be the DETECTION time (between `before` and
	// `after`), not the container's stale FinishedAt (containerFinishedAt in
	// 2020). This guards against regressing to the old behavior of recording
	// the kubelet-reported exit time.
	terminatedAtStr := stored.Annotations[agentsv1alpha1.SandboxTerminatedAtAnnotationKey]
	if terminatedAtStr == "" {
		t.Fatal("expected terminated-at annotation to be set")
	}
	terminatedAt, parseErr := time.Parse(time.RFC3339, terminatedAtStr)
	if parseErr != nil {
		t.Fatalf("parse terminated-at %q: %v", terminatedAtStr, parseErr)
	}
	// RFC3339 is second-granularity; allow a 1-second window on each side.
	lo := before.Add(-time.Second).UTC()
	hi := after.Add(time.Second).UTC()
	if terminatedAt.Before(lo) || terminatedAt.After(hi) {
		t.Fatalf("expected terminated-at to be the detection time between %s and %s, got %s",
			lo.Format(time.RFC3339), hi.Format(time.RFC3339), terminatedAt.Format(time.RFC3339))
	}
	if terminatedAt.Equal(containerFinishedAt.UTC()) {
		t.Fatalf("terminated-at must not equal the container's stale FinishedAt %s",
			containerFinishedAt.UTC().Format(time.RFC3339))
	}
	// Store should NOT yet have a record (deferred until Stopping→Idle).
	noRecord, getErr := testStore.Get("default", "sbx-oom")
	if getErr != nil {
		t.Fatalf("get store record: %v", getErr)
	}
	if noRecord != nil {
		t.Fatalf("expected NO store record yet (deferred write), but got one with status=%s", noRecord.Status)
	}
}

func TestSyncRestartedRunningPods_TerminatingPod_StoreWrittenNoRecycle(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := agentsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add agents scheme: %v", err)
	}

	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool-a", Namespace: "default"},
	}
	testStore := newTestStore(t)

	deletionTime := metav1.Now()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-terminating",
			Namespace: "default",
			Labels: map[string]string{
				agentsv1alpha1.SandboxPoolLabelKey:  "pool-a",
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseRunning,
				agentsv1alpha1.SandboxIDLabelKey:    "sbx-term",
			},
			Annotations: map[string]string{
				agentsv1alpha1.SandboxIDAnnotationKey:        "sbx-term",
				agentsv1alpha1.SandboxClaimedAtAnnotationKey: "2026-01-01T10:00:00Z",
			},
			DeletionTimestamp: &deletionTime,
			Finalizers:        []string{"test-finalizer", agentsv1alpha1.SandboxProtectionFinalizer},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "sandbox", Image: "sandbox:v1"}},
		},
	}

	cli := newTestClientBuilder(t).WithObjects(pool, pod).Build()
	reconciler := &SandboxPoolReconciler{Client: cli, Scheme: scheme, SandboxStore: testStore}
	resultPods, err := reconciler.syncDeletingPods(context.Background(), []corev1.Pod{*pod})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resultPods) == 0 || containsString(resultPods[0].Finalizers, agentsv1alpha1.SandboxProtectionFinalizer) {
		t.Fatal("expected sandbox-protection finalizer to be removed from terminating pod in returned slice")
	}

	// But store should have a Failed record
	record, err := testStore.Get("default", "sbx-term")
	if err != nil {
		t.Fatalf("get store record: %v", err)
	}
	if record == nil {
		t.Fatal("expected Failed record in store for terminating pod")
	}
	if string(record.Status) != string(agentsv1alpha1.SandboxStopReasonFailed) {
		t.Fatalf("expected status Failed, got %s", record.Status)
	}

	// Pod phase should remain Running (not recycled)
	stored := &corev1.Pod{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, stored); err != nil {
		t.Fatalf("get pod: %v", err)
	}
	if stored.Labels[agentsv1alpha1.SandboxPhaseLabelKey] != agentsv1alpha1.SandboxPhaseRunning {
		t.Fatalf("expected running phase (no recycle), got %s", stored.Labels[agentsv1alpha1.SandboxPhaseLabelKey])
	}
	for _, f := range stored.Finalizers {
		if f == agentsv1alpha1.SandboxProtectionFinalizer {
			t.Fatal("expected sandbox-protection finalizer to be removed from terminating pod")
		}
	}
}

func TestSyncDeletingPods_TerminatingStoppingPod_StoreWrittenAndFinalizerRemoved(t *testing.T) {
	scheme := setupScheme(t)
	testStore := newTestStore(t)

	deletionTime := metav1.NewTime(time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC))
	sandboxID := "sbx-stopping-term"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-stopping-terminating",
			Namespace: "default",
			Labels: map[string]string{
				agentsv1alpha1.SandboxPoolLabelKey:  "pool-a",
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseStopping,
				agentsv1alpha1.SandboxIDLabelKey:    sandboxID,
			},
			Annotations: map[string]string{
				agentsv1alpha1.SandboxIDAnnotationKey:            sandboxID,
				agentsv1alpha1.SandboxClaimedAtAnnotationKey:     "2026-04-14T10:30:00Z",
				agentsv1alpha1.SandboxStopReasonAnnotationKey:    "Released",
				agentsv1alpha1.SandboxTerminatedAtAnnotationKey:  "2026-04-14T11:45:00Z",
				agentsv1alpha1.SandboxRunningImagesAnnotationKey: `{"sandbox":"sandbox:v2"}`,
				agentsv1alpha1.SandboxContainerIDAnnotationKey:   "containerd://stopping-term-cid",
			},
			DeletionTimestamp: &deletionTime,
			Finalizers:        []string{"test-finalizer", agentsv1alpha1.SandboxProtectionFinalizer},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "sandbox", Image: "pause:3.10"}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	cli := newTestClientBuilder(t).WithObjects(pod).Build()
	reconciler := &SandboxPoolReconciler{Client: cli, Scheme: scheme, SandboxStore: testStore}

	resultPods, err := reconciler.syncDeletingPods(context.Background(), []corev1.Pod{*pod})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resultPods) == 0 || containsString(resultPods[0].Finalizers, agentsv1alpha1.SandboxProtectionFinalizer) {
		t.Fatal("expected sandbox-protection finalizer to be removed from terminating stopping pod in returned slice")
	}

	record, err := testStore.Get("default", sandboxID)
	if err != nil {
		t.Fatalf("get store record: %v", err)
	}
	if record == nil {
		t.Fatal("expected terminal record for terminating stopping pod")
	}
	if string(record.Status) != "Released" {
		t.Fatalf("expected Released status, got %s", record.Status)
	}
	if record.TerminatedAt == nil || record.TerminatedAt.Format(time.RFC3339) != "2026-04-14T11:45:00Z" {
		t.Fatalf("expected terminatedAt from annotation, got %v", record.TerminatedAt)
	}
	if record.ContainerImages == nil || (*record.ContainerImages)["sandbox"] != "sandbox:v2" {
		t.Fatalf("expected running image snapshot sandbox:v2, got %v", record.ContainerImages)
	}
	if record.ContainerId == nil || *record.ContainerId != "containerd://stopping-term-cid" {
		t.Fatalf("expected containerID containerd://stopping-term-cid, got %v", record.ContainerId)
	}

	stored := &corev1.Pod{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, stored); err != nil {
		t.Fatalf("get pod: %v", err)
	}
	for _, f := range stored.Finalizers {
		if f == agentsv1alpha1.SandboxProtectionFinalizer {
			t.Fatal("expected sandbox-protection finalizer to be removed from terminating stopping pod")
		}
	}
	if stored.Finalizers[0] != "test-finalizer" {
		t.Fatalf("expected unrelated finalizer to be preserved, got %#v", stored.Finalizers)
	}
}

func TestSyncFailedPods_EvictedStoppingPod_Deleted(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := agentsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add agents scheme: %v", err)
	}

	testStore := newTestStore(t)
	sandboxID := "sbx-evicted-stopping"
	terminatedAt := "2026-04-04T13:25:29Z"

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "evicted-stopping-pod",
			Namespace: "default",
			Labels: map[string]string{
				agentsv1alpha1.SandboxPoolLabelKey:  "pool-a",
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseStopping,
				agentsv1alpha1.SandboxIDLabelKey:    sandboxID,
			},
			Annotations: map[string]string{
				agentsv1alpha1.SandboxTerminatedAtAnnotationKey:  terminatedAt,
				agentsv1alpha1.SandboxStopReasonAnnotationKey:    "Canceled",
				agentsv1alpha1.SandboxClaimedAtAnnotationKey:     "2026-04-04T01:16:45Z",
				agentsv1alpha1.SandboxRunningImagesAnnotationKey: `{"sandbox":"sandbox:v1"}`,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "sandbox", Image: "pause:3.10"}},
		},
		Status: corev1.PodStatus{
			Phase:   corev1.PodFailed,
			Reason:  "Evicted",
			Message: "The node was low on resource: memory.",
		},
	}

	cli := newTestClientBuilder(t).WithObjects(pod).Build()
	reconciler := &SandboxPoolReconciler{Client: cli, Scheme: scheme, SandboxStore: testStore}

	resultPods, err := reconciler.syncFailedPods(context.Background(), []corev1.Pod{*pod})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resultPods) != 0 {
		t.Fatalf("expected failed pod to be removed from returned slice, got %d pods", len(resultPods))
	}

	// Pod should be deleted.
	stored := &corev1.Pod{}
	getErr := cli.Get(context.Background(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, stored)
	if getErr == nil {
		t.Fatal("expected pod to be deleted, but it still exists")
	}

	// Store should have a Failed record.
	record, storeErr := testStore.Get("default", sandboxID)
	if storeErr != nil {
		t.Fatalf("get store record: %v", storeErr)
	}
	if record == nil {
		t.Fatal("expected Failed record in store for evicted pod")
	}
	if string(record.Status) != string(agentsv1alpha1.SandboxStopReasonFailed) {
		t.Fatalf("expected status Failed, got %s", record.Status)
	}
	if record.FailureReason == nil || *record.FailureReason != "Evicted" {
		t.Fatalf("expected failureReason Evicted, got %v", record.FailureReason)
	}
	if record.TerminatedAt == nil || record.TerminatedAt.Format(time.RFC3339) != terminatedAt {
		t.Fatalf("expected terminatedAt from annotation %s, got %v", terminatedAt, record.TerminatedAt)
	}
}

func TestSyncPodProtectionFinalizers_BackfillsExistingRunningPod(t *testing.T) {
	scheme := setupScheme(t)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "running-pod-no-finalizer",
			Namespace: "default",
			Labels: map[string]string{
				agentsv1alpha1.SandboxPoolLabelKey:  "pool-a",
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseRunning,
				agentsv1alpha1.SandboxIDLabelKey:    "sbx-123",
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	cli := newTestClientBuilder(t).WithObjects(pod).Build()
	reconciler := &SandboxPoolReconciler{Client: cli, Scheme: scheme}

	resultPods, err := reconciler.syncPodProtectionFinalizers(context.Background(), []corev1.Pod{*pod})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resultPods) == 0 || !containsString(resultPods[0].Finalizers, agentsv1alpha1.SandboxProtectionFinalizer) {
		t.Fatal("expected sandbox-protection finalizer to be present in returned slice after backfill")
	}

	stored := &corev1.Pod{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, stored); err != nil {
		t.Fatalf("get pod: %v", err)
	}
	if !containsString(stored.Finalizers, agentsv1alpha1.SandboxProtectionFinalizer) {
		t.Fatalf("expected sandbox-protection finalizer to be backfilled, got %#v", stored.Finalizers)
	}
}

func TestSyncPodProtectionFinalizers_BackfillsExistingIdlePod(t *testing.T) {
	scheme := setupScheme(t)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "idle-pod-no-finalizer",
			Namespace: "default",
			Labels: map[string]string{
				agentsv1alpha1.SandboxPoolLabelKey:  "pool-a",
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseIdle,
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	cli := newTestClientBuilder(t).WithObjects(pod).Build()
	reconciler := &SandboxPoolReconciler{Client: cli, Scheme: scheme}

	resultPods, err := reconciler.syncPodProtectionFinalizers(context.Background(), []corev1.Pod{*pod})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resultPods) == 0 || !containsString(resultPods[0].Finalizers, agentsv1alpha1.SandboxProtectionFinalizer) {
		t.Fatal("expected sandbox-protection finalizer to be present in returned slice after backfill on idle pod")
	}

	stored := &corev1.Pod{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, stored); err != nil {
		t.Fatalf("get pod: %v", err)
	}
	if !containsString(stored.Finalizers, agentsv1alpha1.SandboxProtectionFinalizer) {
		t.Fatalf("expected sandbox-protection finalizer to be backfilled on idle pod, got %#v", stored.Finalizers)
	}
}

func TestSyncFailedPods_EvictedIdlePod_Deleted(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	// Idle pod with no sandbox-id — no store record expected.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "evicted-idle-pod",
			Namespace: "default",
			Labels: map[string]string{
				agentsv1alpha1.SandboxPoolLabelKey:  "pool-a",
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseIdle,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "sandbox", Image: "pause:3.10"}},
		},
		Status: corev1.PodStatus{
			Phase:   corev1.PodFailed,
			Reason:  "Evicted",
			Message: "The node was low on resource: memory.",
		},
	}

	cli := newTestClientBuilder(t).WithObjects(pod).Build()
	reconciler := &SandboxPoolReconciler{Client: cli, Scheme: scheme}

	resultPods, err := reconciler.syncFailedPods(context.Background(), []corev1.Pod{*pod})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resultPods) != 0 {
		t.Fatalf("expected evicted idle pod to be removed from returned slice, got %d pods", len(resultPods))
	}

	// Pod should be deleted.
	stored := &corev1.Pod{}
	getErr := cli.Get(context.Background(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, stored)
	if getErr == nil {
		t.Fatal("expected pod to be deleted, but it still exists")
	}
}

func TestSyncFailedPods_HealthyRunningPod_NotDeleted(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "healthy-running-pod",
			Namespace: "default",
			Labels: map[string]string{
				agentsv1alpha1.SandboxPoolLabelKey:  "pool-a",
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseRunning,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "sandbox", Image: "sandbox:v1"}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}

	cli := newTestClientBuilder(t).WithObjects(pod).Build()
	reconciler := &SandboxPoolReconciler{Client: cli, Scheme: scheme}

	resultPods, err := reconciler.syncFailedPods(context.Background(), []corev1.Pod{*pod})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resultPods) != 1 {
		t.Fatalf("expected healthy running pod to remain in returned slice, got %d pods", len(resultPods))
	}

	// Pod should still exist.
	stored := &corev1.Pod{}
	if getErr := cli.Get(context.Background(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, stored); getErr != nil {
		t.Fatalf("expected pod to still exist, got error: %v", getErr)
	}
}

func TestSyncFailedPods_AlreadyDeletingFailedPod_Skipped(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	deletionTime := metav1.Now()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "deleting-failed-pod",
			Namespace:         "default",
			DeletionTimestamp: &deletionTime,
			Finalizers:        []string{"test-finalizer"},
			Labels: map[string]string{
				agentsv1alpha1.SandboxPoolLabelKey:  "pool-a",
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseStopping,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "sandbox", Image: "pause:3.10"}},
		},
		Status: corev1.PodStatus{
			Phase:  corev1.PodFailed,
			Reason: "Evicted",
		},
	}

	cli := newTestClientBuilder(t).WithObjects(pod).Build()
	reconciler := &SandboxPoolReconciler{Client: cli, Scheme: scheme}

	resultPods, err := reconciler.syncFailedPods(context.Background(), []corev1.Pod{*pod})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resultPods) != 1 {
		t.Fatalf("expected already-deleting pod to remain in returned slice, got %d pods", len(resultPods))
	}
}

// ── Two-phase scale-down protection tests ─────────────────────────────────────

func TestMarkScaleDownProtected(t *testing.T) {
	scheme := setupScheme(t)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "idle-pod", Namespace: "default"},
	}
	cli := newTestClientBuilder(t).WithObjects(pod).Build()
	r := &SandboxPoolReconciler{Client: cli, Scheme: scheme}

	if err := r.markScaleDownProtected(context.Background(), pod); err != nil {
		t.Fatalf("markScaleDownProtected: %v", err)
	}

	got := &corev1.Pod{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, got); err != nil {
		t.Fatalf("get pod: %v", err)
	}
	ts := got.Annotations[agentsv1alpha1.SandboxScaleDownProtectedAnnotationKey]
	if ts == "" {
		t.Error("expected scale-down-protected annotation to be set")
	}
	// Annotation value must be a valid RFC3339 timestamp.
	if _, err := time.Parse(time.RFC3339, ts); err != nil {
		t.Errorf("annotation value %q is not a valid RFC3339 time: %v", ts, err)
	}
}

func TestScaleDownProtectionWindow_ReturnsDefault(t *testing.T) {
	pool := &agentsv1alpha1.SandboxPool{Spec: agentsv1alpha1.SandboxPoolSpec{Replicas: 5}}
	if w := scaleDownProtectionWindow(pool); w != defaultScaleDownProtectionWindow {
		t.Errorf("expected %v, got %v", defaultScaleDownProtectionWindow, w)
	}
}

// makeProtectedIdlePod builds an idle pod carrying the scale-down-protected
// annotation, simulating either a residual mark from earlier autoscaling
// activity or a fresh Phase-A mark from the current cycle.
func makeProtectedIdlePod(name, ns, poolName, ts string) *corev1.Pod { //nolint:unparam
	p := makeIdlePodForPool(name, ns, poolName)
	if p.Annotations == nil {
		p.Annotations = map[string]string{}
	}
	p.Annotations[agentsv1alpha1.SandboxScaleDownProtectedAnnotationKey] = ts
	return p
}

// TestUnmarkStaleScaleDownProtected_DirectCall exercises the bulk sweeper in
// isolation: every idle pod carrying the annotation should be cleared, while
// non-idle or unannotated pods are left alone.
func TestUnmarkStaleScaleDownProtected_DirectCall(t *testing.T) {
	const ns, poolName = "default", "pool-cleanup"
	idleWithAnnot := makeProtectedIdlePod("idle-stale-1", ns, poolName, "2026-05-08T08:20:02Z")
	idleWithoutAnnot := makeIdlePodForPool("idle-clean", ns, poolName)
	startingWithAnnot := makeIdlePodForPool("starting-pod", ns, poolName)
	startingWithAnnot.Labels[agentsv1alpha1.SandboxPhaseLabelKey] = agentsv1alpha1.SandboxPhaseStarting
	startingWithAnnot.Annotations = map[string]string{
		agentsv1alpha1.SandboxScaleDownProtectedAnnotationKey: "2026-05-08T08:20:02Z",
	}

	cli := newTestClientBuilder(t).WithObjects(idleWithAnnot, idleWithoutAnnot, startingWithAnnot).Build()
	r := &SandboxPoolReconciler{Client: cli, Scheme: setupScheme(t)}

	pods := []corev1.Pod{*idleWithAnnot, *idleWithoutAnnot, *startingWithAnnot}
	cleared := r.unmarkStaleScaleDownProtected(context.Background(), pods)
	if cleared != 1 {
		t.Fatalf("expected 1 cleared, got %d", cleared)
	}

	got := &corev1.Pod{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: idleWithAnnot.Name, Namespace: ns}, got); err != nil {
		t.Fatalf("get idle pod: %v", err)
	}
	if v, ok := got.Annotations[agentsv1alpha1.SandboxScaleDownProtectedAnnotationKey]; ok && v != "" {
		t.Errorf("expected annotation removed from idle pod, still got %q", v)
	}

	// Starting pod must keep its annotation — the sweeper only touches Idle pods.
	if err := cli.Get(context.Background(), types.NamespacedName{Name: startingWithAnnot.Name, Namespace: ns}, got); err != nil {
		t.Fatalf("get starting pod: %v", err)
	}
	if got.Annotations[agentsv1alpha1.SandboxScaleDownProtectedAnnotationKey] == "" {
		t.Error("expected starting pod's annotation to be preserved")
	}
}

// fakeIdleNotifier records NotifyIdleAvailable calls for assertions.
type fakeIdleNotifier struct {
	notified []string // ns/name pairs
}

func (f *fakeIdleNotifier) NotifyIdleAvailable(namespace, poolName string) {
	f.notified = append(f.notified, namespace+"/"+poolName)
}
func (f *fakeIdleNotifier) OnSandboxReleased(_ context.Context, _ string) {}

// TestReconcile_UnmarksStaleProtection_NoScaleDown verifies that when no
// scale-down is planned this cycle (current ≤ desired), reconcile clears every
// idle pod's residual scale-down-protected annotation and wakes the scheduler —
// the core fix for the production stuck-pool bug. No pod should be deleted.
func TestReconcile_UnmarksStaleProtection_NoScaleDown(t *testing.T) {
	const ns, poolName = "default", "terminal2"
	pool := makePoolForGuard(ns, poolName, 3)

	pods := []*corev1.Pod{
		makeProtectedIdlePod("p1", ns, poolName, "2026-05-08T08:20:02Z"),
		makeProtectedIdlePod("p2", ns, poolName, "2026-05-08T08:20:02Z"),
		makeProtectedIdlePod("p3", ns, poolName, "2026-05-08T08:20:02Z"),
	}
	objs := make([]client.Object, 0, 1+len(pods))
	objs = append(objs, pool)
	for _, p := range pods {
		objs = append(objs, p)
	}
	cli := newTestClientBuilder(t).WithObjects(objs...).Build()
	notifier := &fakeIdleNotifier{}
	r := &SandboxPoolReconciler{
		Client:       cli,
		Scheme:       setupScheme(t),
		expectations: NewPoolExpectations(),
		IdleNotifier: notifier,
	}

	if _, err := r.reconcilePods(context.Background(), pool); err != nil {
		t.Fatalf("reconcilePods: %v", err)
	}

	// All three pods must still exist (no scale-down) and lose the annotation.
	for _, p := range pods {
		got := &corev1.Pod{}
		if err := cli.Get(context.Background(), types.NamespacedName{Name: p.Name, Namespace: ns}, got); err != nil {
			t.Fatalf("pod %s should still exist: %v", p.Name, err)
		}
		if v, ok := got.Annotations[agentsv1alpha1.SandboxScaleDownProtectedAnnotationKey]; ok && v != "" {
			t.Errorf("pod %s still carries scale-down-protected annotation: %q", p.Name, v)
		}
	}
	if len(notifier.notified) == 0 {
		t.Error("expected IdleNotifier.NotifyIdleAvailable to be invoked")
	}
}

// ── PodCreationImagePolicy tests ──────────────────────────────────────────────

func TestCreatePod_PolicyPoolDefaultImage(t *testing.T) {
	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool-a", Namespace: "default"},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			Replicas:               1,
			PodCreationImagePolicy: agentsv1alpha1.PodCreationImagePolicyPoolDefaultImage,
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "pause:3.10",
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{Containers: []corev1.Container{
						{Name: "sandbox", Image: "my-sandbox:latest"},
					}},
				},
			},
		},
	}
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli, Scheme: setupScheme(t)}

	if err := r.createPod(context.Background(), pool); err != nil {
		t.Fatalf("createPod: %v", err)
	}

	podList := &corev1.PodList{}
	if err := cli.List(context.Background(), podList); err != nil {
		t.Fatalf("list pods: %v", err)
	}
	if len(podList.Items) != 1 {
		t.Fatalf("expected 1 pod, got %d", len(podList.Items))
	}
	if got := podList.Items[0].Spec.Containers[0].Image; got != "my-sandbox:latest" {
		t.Errorf("expected template image my-sandbox:latest, got %s", got)
	}
	if !containsString(podList.Items[0].Finalizers, agentsv1alpha1.SandboxProtectionFinalizer) {
		t.Fatalf("expected created pod to include sandbox-protection finalizer, got %#v", podList.Items[0].Finalizers)
	}
}

func TestCreatePod_PolicyIdleImage(t *testing.T) {
	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool-a", Namespace: "default"},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			Replicas:               1,
			PodCreationImagePolicy: agentsv1alpha1.PodCreationImagePolicyIdleImage,
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "pause:3.10",
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{Containers: []corev1.Container{
						{Name: "sandbox", Image: "my-sandbox:latest"},
					}},
				},
			},
		},
	}
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli, Scheme: setupScheme(t)}

	if err := r.createPod(context.Background(), pool); err != nil {
		t.Fatalf("createPod: %v", err)
	}

	podList := &corev1.PodList{}
	if err := cli.List(context.Background(), podList); err != nil {
		t.Fatalf("list pods: %v", err)
	}
	if len(podList.Items) != 1 {
		t.Fatalf("expected 1 pod, got %d", len(podList.Items))
	}
	if got := podList.Items[0].Spec.Containers[0].Image; got != "pause:3.10" {
		t.Errorf("expected idle image pause:3.10, got %s", got)
	}
}

func TestCreatePod_PolicyIdleImageEmpty(t *testing.T) {
	// When IdleImage is empty, should fall back to template image regardless of policy.
	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool-a", Namespace: "default"},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			Replicas:               1,
			PodCreationImagePolicy: agentsv1alpha1.PodCreationImagePolicyIdleImage,
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "", // empty — must not override
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{Containers: []corev1.Container{
						{Name: "sandbox", Image: "my-sandbox:latest"},
					}},
				},
			},
		},
	}
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli, Scheme: setupScheme(t)}

	if err := r.createPod(context.Background(), pool); err != nil {
		t.Fatalf("createPod: %v", err)
	}

	podList := &corev1.PodList{}
	if err := cli.List(context.Background(), podList); err != nil {
		t.Fatalf("list pods: %v", err)
	}
	if len(podList.Items) != 1 {
		t.Fatalf("expected 1 pod, got %d", len(podList.Items))
	}
	// Should not panic or override with empty string.
	if got := podList.Items[0].Spec.Containers[0].Image; got != "my-sandbox:latest" {
		t.Errorf("expected template image my-sandbox:latest when idleImage is empty, got %s", got)
	}
}

func TestHandleDeletion_MarksPoolTerminating(t *testing.T) {
	const ns, poolName = "default", "pool-deleting"
	deletionTime := metav1.Now()
	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:              poolName,
			Namespace:         ns,
			Finalizers:        []string{FinalizerName},
			DeletionTimestamp: &deletionTime,
		},
		Spec: agentsv1alpha1.SandboxPoolSpec{Replicas: 1},
		Status: agentsv1alpha1.SandboxPoolStatus{
			Phase:        agentsv1alpha1.SandboxPoolPhaseReady,
			IdleReplicas: 1,
		},
	}
	pod := makeIdlePodForPool("pod-1", ns, poolName)

	cli := newTestClientBuilder(t).WithObjects(pool, pod).Build()
	r := &SandboxPoolReconciler{Client: cli, Scheme: setupScheme(t)}

	result, err := r.handleDeletion(context.Background(), pool)
	if err != nil {
		t.Fatalf("handleDeletion: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Fatalf("expected requeue while deleting pool pods")
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: poolName}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Status.Phase != agentsv1alpha1.SandboxPoolPhaseTerminating {
		t.Fatalf("expected phase %q, got %q", agentsv1alpha1.SandboxPoolPhaseTerminating, updated.Status.Phase)
	}
}

// setupScheme is a small helper used by tests that need both core and agents schemes.
func setupScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := agentsv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add agents scheme: %v", err)
	}
	return s
}

// ── Expectations scale-guard tests ──────────────────────────────────────────

// makePoolForGuard returns a minimal SandboxPool with the given namespace/name
// and desired replica count, suitable for scale-guard reconcile tests.
func makePoolForGuard(ns, name string, replicas int32) *agentsv1alpha1.SandboxPool { //nolint:unparam
	return &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{FinalizerName},
		},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			Replicas: replicas,
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "c", Image: "img:latest"}},
					},
				},
			},
		},
	}
}

// makeIdlePodForPool creates an idle pod that belongs to the given pool.
func makeIdlePodForPool(name, ns, poolName string) *corev1.Pod { //nolint:unparam
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				agentsv1alpha1.SandboxPoolLabelKey:  poolName,
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseIdle,
			},
			Finalizers: []string{agentsv1alpha1.SandboxProtectionFinalizer},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "c", Image: "img:latest"}},
		},
	}
}

// TestScaleUpGuard_SkipsWhenExpectationsUnsatisfied verifies that reconcilePods
// does NOT create any pods when there are pending creations in the expectations
// map (i.e. a previous scale-up has not yet fully landed in the informer).
func TestScaleUpGuard_SkipsWhenExpectationsUnsatisfied(t *testing.T) {
	const ns, poolName = "default", "pool-guard"
	pool := makePoolForGuard(ns, poolName, 5)

	// Pre-populate with 2 idle pods so current=2, desired=5 → would normally create 3.
	pod1 := makeIdlePodForPool("pod-1", ns, poolName)
	pod2 := makeIdlePodForPool("pod-2", ns, poolName)

	cli := newTestClientBuilder(t).WithObjects(pool, pod1, pod2).Build()
	exp := NewPoolExpectations()
	r := &SandboxPoolReconciler{
		Client:       cli,
		Scheme:       setupScheme(t),
		expectations: exp,
	}

	poolKey := types.NamespacedName{Namespace: ns, Name: poolName}

	// Simulate a prior scale-up that issued 3 creates but they haven't
	// landed in the informer yet.
	exp.ExpectCreations(poolKey, 3)

	_, err := r.reconcilePods(context.Background(), pool)
	if err != nil {
		t.Fatalf("reconcilePods: %v", err)
	}

	// No new pods should have been created.
	podList := &corev1.PodList{}
	if err := cli.List(context.Background(), podList); err != nil {
		t.Fatalf("list pods: %v", err)
	}
	if len(podList.Items) != 2 {
		t.Errorf("expected 2 pods (no new creates), got %d", len(podList.Items))
	}
}

// TestScaleUpGuard_ProceedsWhenExpectationsSatisfied verifies that reconcilePods
// DOES create pods once expectations are satisfied.
func TestScaleUpGuard_ProceedsWhenExpectationsSatisfied(t *testing.T) {
	const ns, poolName = "default", "pool-guard-ok"
	pool := makePoolForGuard(ns, poolName, 3)

	pod1 := makeIdlePodForPool("pod-1", ns, poolName)

	cli := newTestClientBuilder(t).WithObjects(pool, pod1).Build()
	exp := NewPoolExpectations()
	r := &SandboxPoolReconciler{
		Client:       cli,
		Scheme:       setupScheme(t),
		expectations: exp,
	}

	// No pending expectations → Satisfied() == true → should scale up.
	_, err := r.reconcilePods(context.Background(), pool)
	if err != nil {
		t.Fatalf("reconcilePods: %v", err)
	}

	podList := &corev1.PodList{}
	if err := cli.List(context.Background(), podList); err != nil {
		t.Fatalf("list pods: %v", err)
	}
	// Should have created 2 new pods (3 desired − 1 existing).
	if len(podList.Items) != 3 {
		t.Errorf("expected 3 pods after scale-up, got %d", len(podList.Items))
	}
}

// TestScaleUpGuard_TTLUnblocks verifies that an expired expectation does not
// permanently block scale-up (safety valve behaviour).
func TestScaleUpGuard_TTLUnblocks(t *testing.T) {
	const ns, poolName = "default", "pool-guard-ttl"
	pool := makePoolForGuard(ns, poolName, 3)
	pod1 := makeIdlePodForPool("pod-1", ns, poolName)

	cli := newTestClientBuilder(t).WithObjects(pool, pod1).Build()
	exp := NewPoolExpectations()
	r := &SandboxPoolReconciler{
		Client:       cli,
		Scheme:       setupScheme(t),
		expectations: exp,
	}

	poolKey := types.NamespacedName{Namespace: ns, Name: poolName}
	exp.ExpectCreations(poolKey, 10)
	// Backdate the expectation to simulate TTL expiry.
	exp.mu.Lock()
	exp.items[poolKey].timestamp = time.Now().Add(-(expectationsTTL + time.Second))
	exp.mu.Unlock()

	_, err := r.reconcilePods(context.Background(), pool)
	if err != nil {
		t.Fatalf("reconcilePods: %v", err)
	}

	podList := &corev1.PodList{}
	if err := cli.List(context.Background(), podList); err != nil {
		t.Fatalf("list pods: %v", err)
	}
	// TTL expired → guard bypassed → 2 new pods created (3 desired − 1 existing).
	if len(podList.Items) != 3 {
		t.Errorf("expected 3 pods (TTL unblocked scale-up), got %d", len(podList.Items))
	}
}

// TestScaleDownGuard_SkipsWhenExpectationsUnsatisfied verifies that reconcilePods
// does NOT delete any pods when there are pending deletions in the expectations map.
func TestScaleDownGuard_SkipsWhenExpectationsUnsatisfied(t *testing.T) {
	const ns, poolName = "default", "pool-guard-down"
	pool := makePoolForGuard(ns, poolName, 2) // desired=2

	// 4 idle pods exist → normally would delete 2.
	pods := []*corev1.Pod{
		makeIdlePodForPool("pod-1", ns, poolName),
		makeIdlePodForPool("pod-2", ns, poolName),
		makeIdlePodForPool("pod-3", ns, poolName),
		makeIdlePodForPool("pod-4", ns, poolName),
	}
	cli := newTestClientBuilder(t).WithObjects(pool, pods[0], pods[1], pods[2], pods[3]).Build()
	exp := NewPoolExpectations()
	r := &SandboxPoolReconciler{
		Client:       cli,
		Scheme:       setupScheme(t),
		expectations: exp,
	}

	poolKey := types.NamespacedName{Namespace: ns, Name: poolName}
	// Simulate prior scale-down with 2 pending deletions not yet reflected.
	exp.ExpectDeletions(poolKey, 2)

	_, err := r.reconcilePods(context.Background(), pool)
	if err != nil {
		t.Fatalf("reconcilePods: %v", err)
	}

	podList := &corev1.PodList{}
	if err := cli.List(context.Background(), podList); err != nil {
		t.Fatalf("list pods: %v", err)
	}
	// Guard should have blocked all deletions.
	if len(podList.Items) != 4 {
		t.Errorf("expected 4 pods (scale-down blocked), got %d", len(podList.Items))
	}
}
