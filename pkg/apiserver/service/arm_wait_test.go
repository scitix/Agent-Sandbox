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
	"errors"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

// armWaitFixture builds a service whose cache holds one claimed pod.
func armWaitFixture(t *testing.T, sandboxID string) (*k8sSandboxService, *corev1.Pod) {
	t.Helper()
	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("fake client: %v", err)
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-1",
			Namespace: "ns",
			Labels:    map[string]string{agentsv1alpha1.SandboxIDLabelKey: sandboxID},
		},
	}
	return &k8sSandboxService{client: cb.WithObjects(pod).Build()}, pod
}

func TestAwaitArming_ReturnsOnSuccess(t *testing.T) {
	s, pod := armWaitFixture(t, "sb1")
	ch := make(chan error, 1)
	ch <- nil

	if appErr := s.awaitArming(context.Background(), ch, pod, "sb1"); appErr != nil {
		t.Fatalf("expected success, got %v", appErr)
	}
}

// The reason is the only thing that tells the caller what to fix, so it has to
// survive the trip from the arming goroutine to the HTTP response.
func TestAwaitArming_SurfacesTheArmingReason(t *testing.T) {
	ch := make(chan error, 1)
	ch <- errors.New("runtime never answered")

	s, pod := armWaitFixture(t, "sb1")
	appErr := s.awaitArming(context.Background(), ch, pod, "sb1")
	if appErr == nil {
		t.Fatal("expected an error")
	}
	if appErr.Code != domain.ErrCodeInternal {
		t.Fatalf("expected 500, got %d", appErr.Code)
	}
	if !strings.Contains(appErr.Message, "runtime never answered") {
		t.Fatalf("expected the arming reason in the message, got %q", appErr.Message)
	}
}

// The backstop is for a verdict that never arrives — the runner reports its own
// timeout, so reaching this means the callback was lost.
func TestAwaitArming_LostVerdict_HitsTheBackstop(t *testing.T) {
	restore := armWaitBackstop
	armWaitBackstop = 200 * time.Millisecond
	t.Cleanup(func() { armWaitBackstop = restore })

	s, pod := armWaitFixture(t, "sb1")
	start := time.Now()
	appErr := s.awaitArming(context.Background(), make(chan error), pod, "sb1")
	if appErr == nil {
		t.Fatal("expected the backstop to fire")
	}
	if appErr.Code != domain.ErrCodeGatewayTimeout {
		t.Fatalf("a lost verdict must be 504, got %d (%s)", appErr.Code, appErr.Message)
	}
	if !strings.Contains(appErr.Message, "retry") {
		t.Fatalf("the message should tell the caller what to do: %q", appErr.Message)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("wait overran its budget: %s", time.Since(start))
	}
}

// A client that gave up is not a slow sandbox; conflating them would put a 504
// in the logs for every abandoned request.
func TestAwaitArming_ClientCancellationIsNotATimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s, pod := armWaitFixture(t, "sb1")
	appErr := s.awaitArming(ctx, make(chan error), pod, "sb1")
	if appErr == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(appErr.Message, "canceled by client") {
		t.Fatalf("expected a client-cancellation message, got %q", appErr.Message)
	}
}

// The verdict must be waited for, not polled: a create that returns before
// arming finishes is the whole bug this path exists to prevent.
func TestAwaitArming_BlocksUntilTheVerdictArrives(t *testing.T) {
	ch := make(chan error, 1)
	go func() {
		time.Sleep(150 * time.Millisecond)
		ch <- nil
	}()

	s, pod := armWaitFixture(t, "sb1")
	start := time.Now()
	if appErr := s.awaitArming(context.Background(), ch, pod, "sb1"); appErr != nil {
		t.Fatalf("expected success, got %v", appErr)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("returned before the verdict arrived (%s)", elapsed)
	}
}

// --------------------------------------------------------------------------
// Abandonment: the wait must end when the claim stops being ours
//
// None of these produce a verdict. Without the poll the request would hold open
// for the whole backstop while the thing it is waiting for no longer exists —
// and, worse, a delete issued during that window would look stuck.
// --------------------------------------------------------------------------

func TestAwaitArming_PodDeleted_EndsTheWaitEarly(t *testing.T) {
	restore := armAbandonPollInterval
	armAbandonPollInterval = 20 * time.Millisecond
	t.Cleanup(func() { armAbandonPollInterval = restore })

	s, pod := armWaitFixture(t, "sb1")
	if err := s.client.Delete(context.Background(), pod.DeepCopy()); err != nil {
		t.Fatalf("delete: %v", err)
	}

	start := time.Now()
	appErr := s.awaitArming(context.Background(), make(chan error), pod, "sb1")
	if appErr == nil {
		t.Fatal("expected the wait to end")
	}
	if !strings.Contains(appErr.Message, "deleted") {
		t.Fatalf("expected a deletion message, got %q", appErr.Message)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("should have ended promptly, took %s", time.Since(start))
	}
}

func TestAwaitArming_PodReclaimed_EndsTheWaitEarly(t *testing.T) {
	restore := armAbandonPollInterval
	armAbandonPollInterval = 20 * time.Millisecond
	t.Cleanup(func() { armAbandonPollInterval = restore })

	// The pod now serves a different sandbox: our claim is gone.
	s, pod := armWaitFixture(t, "sb-other")

	start := time.Now()
	appErr := s.awaitArming(context.Background(), make(chan error), pod, "sb1")
	if appErr == nil {
		t.Fatal("expected the wait to end")
	}
	if !strings.Contains(appErr.Message, "reclaimed") {
		t.Fatalf("expected a reclaim message, got %q", appErr.Message)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("should have ended promptly, took %s", time.Since(start))
	}
}

// A delete issued while a create is still waiting must not appear to hang: the
// deletion timestamp ends the wait on the next poll.
func TestAwaitArming_PodBeingDeleted_EndsTheWaitEarly(t *testing.T) {
	restore := armAbandonPollInterval
	armAbandonPollInterval = 20 * time.Millisecond
	t.Cleanup(func() { armAbandonPollInterval = restore })

	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("fake client: %v", err)
	}
	now := metav1.Now()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "pod-1",
			Namespace:         "ns",
			Labels:            map[string]string{agentsv1alpha1.SandboxIDLabelKey: "sb1"},
			DeletionTimestamp: &now,
			Finalizers:        []string{"agentbox.navix.sh/test"},
		},
	}
	s := &k8sSandboxService{client: cb.WithObjects(pod).Build()}

	appErr := s.awaitArming(context.Background(), make(chan error), pod, "sb1")
	if appErr == nil {
		t.Fatal("expected the wait to end")
	}
	if !strings.Contains(appErr.Message, "being deleted") {
		t.Fatalf("expected a deletion message, got %q", appErr.Message)
	}
}

// A verdict that arrives in the same tick as an abandonment check must still be
// honoured — the sandbox really was armed.
func TestAwaitArming_VerdictWinsOverAPollThatWouldAlsoFire(t *testing.T) {
	restore := armAbandonPollInterval
	armAbandonPollInterval = time.Hour
	t.Cleanup(func() { armAbandonPollInterval = restore })

	s, pod := armWaitFixture(t, "sb1")
	ch := make(chan error, 1)
	ch <- nil

	if appErr := s.awaitArming(context.Background(), ch, pod, "sb1"); appErr != nil {
		t.Fatalf("expected success, got %v", appErr)
	}
}
