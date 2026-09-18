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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/activity"
)

// ---------------------------------------------------------------------------
// resolveLastActive
// ---------------------------------------------------------------------------

// The answer is a maximum over every source, never a priority chain. A chain
// that prefers one source outright lets a stale value from it pull the answer
// backwards, which is indistinguishable from "the sandbox is idle" — and the
// release that follows is of a sandbox that is in use.
func TestResolveLastActive_IsTheMaximumOfEverySource(t *testing.T) {
	claimed := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	started := claimed.Add(30 * time.Second)
	seen := claimed.Add(10 * time.Minute)

	cases := []struct {
		name        string
		annotations map[string]string
		want        time.Time
	}{
		{
			name:        "nothing recorded: the creation timestamp is the floor",
			annotations: nil,
			want:        claimed,
		},
		{
			name: "last-active is the only activity signal",
			annotations: map[string]string{
				agentsv1alpha1.SandboxLastActiveAnnotationKey: seen.Format(time.RFC3339),
			},
			want: seen,
		},
		{
			name: "started-at still wins while the sandbox has not been used",
			annotations: map[string]string{
				agentsv1alpha1.SandboxStartedAtAnnotationKey: started.Format(time.RFC3339),
			},
			want: started,
		},
		{
			// The regression this guards: a priority chain that reads started-at
			// first (or that trusts a stale last-active over a newer one) would
			// answer `started`, releasing a sandbox used ten minutes later.
			name: "last-active wins over an older started-at",
			annotations: map[string]string{
				agentsv1alpha1.SandboxStartedAtAnnotationKey:  started.Format(time.RFC3339),
				agentsv1alpha1.SandboxLastActiveAnnotationKey: seen.Format(time.RFC3339),
			},
			want: seen,
		},
		{
			name: "a stale last-active cannot drag the answer before started-at",
			annotations: map[string]string{
				agentsv1alpha1.SandboxStartedAtAnnotationKey:  started.Format(time.RFC3339),
				agentsv1alpha1.SandboxLastActiveAnnotationKey: claimed.Add(-time.Hour).Format(time.RFC3339),
			},
			want: started,
		},
		{
			name: "malformed timestamps are ignored rather than treated as zero",
			annotations: map[string]string{
				agentsv1alpha1.SandboxStartedAtAnnotationKey:  "not-a-time",
				agentsv1alpha1.SandboxLastActiveAnnotationKey: seen.Format(time.RFC3339),
			},
			want: seen,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "pod-1",
					Namespace:         "default",
					CreationTimestamp: metav1.NewTime(claimed),
					Annotations:       tc.annotations,
				},
			}
			if got := resolveLastActive(pod); !got.Equal(tc.want) {
				t.Errorf("resolveLastActive() = %v, want %v", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The reconciler: the annotation is the whole signal
// ---------------------------------------------------------------------------

func idleTimeoutPod(sandboxID, idleTimeout, lastActive string) *corev1.Pod { //nolint:unparam // sandboxID is a fixture knob; today's cases all use one
	annotations := map[string]string{
		agentsv1alpha1.SandboxIdleTimeoutAnnotationKey: idleTimeout,
		agentsv1alpha1.SandboxMetadataAnnotationKey:    `{"suite":"unit"}`,
	}
	if lastActive != "" {
		annotations[agentsv1alpha1.SandboxLastActiveAnnotationKey] = lastActive
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-" + sandboxID,
			Namespace: "default",
			Labels: map[string]string{
				agentsv1alpha1.SandboxPoolLabelKey:  "pool",
				agentsv1alpha1.SandboxIDLabelKey:    sandboxID,
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseRunning,
			},
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "sandbox", Image: "myapp:v1"}}},
	}
}

func idleTimeoutPool() *agentsv1alpha1.SandboxPool {
	return &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "default"},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			Replicas: 1,
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "pause:3.10",
				Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{
					{Name: "sandbox", Image: "myapp:v1"},
				}}},
			},
		},
	}
}

// runIdleCheck drives one reconcile tick and returns the pod as it now stands.
func runIdleCheck(t *testing.T, pod *corev1.Pod) *corev1.Pod {
	t.Helper()
	cli := newTestClientBuilder(t).WithObjects(idleTimeoutPool(), pod).Build()
	r := NewIdleTimeoutReconciler(cli, nil, 5*time.Minute)
	r.checkAndReleaseIdleSandboxes(context.Background())

	out := &corev1.Pod{}
	if err := cli.Get(context.Background(),
		types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, out); err != nil {
		t.Fatalf("get pod: %v", err)
	}
	return out
}

func released(pod *corev1.Pod) bool {
	return pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey] == agentsv1alpha1.SandboxPhaseStopping
}

func TestIdleTimeoutReconciler_ReleasesOnlyPastTimeoutPlusBuffer(t *testing.T) {
	const timeout = 10 * time.Minute
	buffer := activity.ReclaimBuffer(timeout)
	now := time.Now().UTC()

	cases := []struct {
		name       string
		lastActive time.Duration // how long ago
		want       bool
	}{
		{"used a moment ago", time.Second, false},
		{"idle for the timeout alone", timeout - time.Minute, false},
		{"idle right up to timeout+buffer", timeout + buffer - time.Minute, false},
		{"idle past timeout+buffer", timeout + buffer + time.Minute, true},
		{"idle for hours", 6 * time.Hour, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pod := idleTimeoutPod("sb1", "600", now.Add(-tc.lastActive).Format(time.RFC3339))
			got := released(runIdleCheck(t, pod))
			if got != tc.want {
				t.Errorf("released = %v, want %v (idle %v, timeout %v, buffer %v)",
					got, tc.want, tc.lastActive, timeout, buffer)
			}
		})
	}
}

// The multi-replica case, stated directly: the only evidence that a sandbox is
// in use may be an annotation written by a replica this controller never spoke
// to. Reading that annotation is the entire fix.
func TestIdleTimeoutReconciler_TrustsActivityFromAnyReplica(t *testing.T) {
	now := time.Now().UTC()
	pod := idleTimeoutPod("sb1", "600", now.Format(time.RFC3339))

	if released(runIdleCheck(t, pod)) {
		t.Error("a sandbox touched a moment ago was released — activity from a replica " +
			"the controller never polled was not honoured")
	}
}

func TestIdleTimeoutReconciler_SkipsSandboxesWithoutATimeout(t *testing.T) {
	// No idle-timeout annotation: nothing to enforce, however old it looks.
	pod := idleTimeoutPod("sb1", "", time.Now().UTC().Add(-100*time.Hour).Format(time.RFC3339))
	delete(pod.Annotations, agentsv1alpha1.SandboxIdleTimeoutAnnotationKey)

	if released(runIdleCheck(t, pod)) {
		t.Error("a sandbox with no idle timeout was released")
	}
}

// A sandbox that has not been touched since it was claimed falls back to
// started-at, so a long-lived idle one is still collected.
func TestIdleTimeoutReconciler_FallsBackToStartedAt(t *testing.T) {
	pod := idleTimeoutPod("sb1", "60", "")
	pod.Annotations[agentsv1alpha1.SandboxStartedAtAnnotationKey] =
		time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)

	if !released(runIdleCheck(t, pod)) {
		t.Error("a sandbox idle since it started was not released")
	}
}
