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
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

func armWaitPod(sandboxID string, annotations map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-1",
			Namespace: "ns",
			Labels: map[string]string{
				agentsv1alpha1.SandboxIDLabelKey: sandboxID,
			},
			Annotations: annotations,
		},
	}
}

// armWaitService returns a service whose client sees the given pod.
func armWaitService(t *testing.T, pod *corev1.Pod) *k8sSandboxService {
	t.Helper()
	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("get fake client builder: %v", err)
	}
	return &k8sSandboxService{client: cb.WithObjects(pod).Build()}
}

func TestWaitUntilArmed_ReturnsOnceMarked(t *testing.T) {
	pod := armWaitPod("sb1", map[string]string{
		agentsv1alpha1.SandboxArmedAnnotationKey: "sb1",
	})
	s := armWaitService(t, pod)

	got, appErr := s.waitUntilArmed(context.Background(), pod, "sb1", 5*time.Second)
	if appErr != nil {
		t.Fatalf("expected success, got %v", appErr)
	}
	if got == nil || got.Name != "pod-1" {
		t.Fatalf("expected the refreshed pod, got %+v", got)
	}
}

// The mark carries the sandbox ID precisely so a recycled pod's leftover mark
// is not read as this claim being armed.
func TestWaitUntilArmed_StaleMarkFromPreviousClaim_TimesOut(t *testing.T) {
	pod := armWaitPod("sb1", map[string]string{
		agentsv1alpha1.SandboxArmedAnnotationKey: "sb0",
	})
	s := armWaitService(t, pod)

	// Negative startup timeout so budget = armWaitGrace is the only term; keep
	// the wait short by overriding through the context instead.
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	_, appErr := s.waitUntilArmed(ctx, pod, "sb1", 0)
	if appErr == nil {
		t.Fatal("expected the stale mark to be ignored")
	}
}

func TestWaitUntilArmed_CallerGivesUp_IsNotReportedAsTimeout(t *testing.T) {
	pod := armWaitPod("sb1", nil)
	s := armWaitService(t, pod)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	_, appErr := s.waitUntilArmed(ctx, pod, "sb1", 5*time.Minute)
	if appErr == nil {
		t.Fatal("expected an error")
	}
	// The client disconnecting is not the sandbox being slow; conflating them
	// would put a 504 in the logs for every abandoned request.
	if !strings.Contains(appErr.Message, "canceled by client") {
		t.Fatalf("expected a client-cancellation message, got %q", appErr.Message)
	}
}

func TestWaitUntilArmed_ArmErrorIsSurfacedWithItsReason(t *testing.T) {
	pod := armWaitPod("sb1", map[string]string{
		agentsv1alpha1.SandboxArmErrorAnnotationKey: "runtime never answered",
	})
	s := armWaitService(t, pod)

	_, appErr := s.waitUntilArmed(context.Background(), pod, "sb1", 5*time.Second)
	if appErr == nil {
		t.Fatal("expected an error")
	}
	if appErr.Code != domain.ErrCodeInternal {
		t.Fatalf("expected 500, got %d", appErr.Code)
	}
	// The reason is the only thing that tells the caller what to fix.
	if !strings.Contains(appErr.Message, "runtime never answered") {
		t.Fatalf("expected the arming reason in the message, got %q", appErr.Message)
	}
}

func TestWaitUntilArmed_Timeout_Returns504(t *testing.T) {
	// Shrink the grace so the internal budget — not the caller's context —
	// is what runs out; the two exits are deliberately distinguished.
	restore := armWaitGrace
	armWaitGrace = 400 * time.Millisecond
	t.Cleanup(func() { armWaitGrace = restore })

	pod := armWaitPod("sb1", nil)
	s := armWaitService(t, pod)

	start := time.Now()
	_, appErr := s.waitUntilArmed(context.Background(), pod, "sb1", 0)
	if appErr == nil {
		t.Fatal("expected a timeout")
	}
	if appErr.Code != domain.ErrCodeGatewayTimeout {
		t.Fatalf("a wait that ran out of time must be 504, got %d (%s)", appErr.Code, appErr.Message)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("wait overran its budget: %s", time.Since(start))
	}
}

// A pod reclaimed for a different sandbox mid-wait must fail fast rather than
// burn the whole budget.
func TestWaitUntilArmed_PodReclaimed_FailsImmediately(t *testing.T) {
	pod := armWaitPod("sb-other", nil)
	s := armWaitService(t, pod)

	start := time.Now()
	_, appErr := s.waitUntilArmed(context.Background(), pod, "sb1", 5*time.Second)
	if appErr == nil {
		t.Fatal("expected an error for a reclaimed pod")
	}
	if !strings.Contains(appErr.Message, "reclaimed") {
		t.Fatalf("expected a reclaim message, got %q", appErr.Message)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("should have failed fast, took %s", time.Since(start))
	}
}

func TestWaitUntilArmed_PodDeleted_FailsImmediately(t *testing.T) {
	pod := armWaitPod("sb1", nil)
	s := armWaitService(t, pod)
	if err := s.client.Delete(context.Background(), pod.DeepCopy()); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, appErr := s.waitUntilArmed(context.Background(), pod, "sb1", 5*time.Second)
	if appErr == nil {
		t.Fatal("expected an error for a deleted pod")
	}
	if !strings.Contains(appErr.Message, "deleted") {
		t.Fatalf("expected a deletion message, got %q", appErr.Message)
	}
}
