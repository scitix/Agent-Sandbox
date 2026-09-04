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

package extproc

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

// makePod builds a Pod with the given phase label, sandbox-id label, and IP.
// Passing phase == "" leaves the phase label unset.
//
// A Running pod is built already armed, so these cases exercise the
// phase/sandbox-id/PodIP decision table on its own. The arming dimension has
// its own tests below.
// testPodIP is the IP every fixture pod is given.
const testPodIP = "10.1.1.1"

func makePod(name, sandboxID, phase, podIP string) *corev1.Pod { //nolint:unparam
	pod := makeUnarmedPod(name, sandboxID, phase, podIP)
	if phase == string(agentsv1alpha1.SandboxPhaseRunning) && sandboxID != "" {
		pod.Annotations = map[string]string{
			agentsv1alpha1.SandboxArmedAnnotationKey: sandboxID,
		}
	}
	return pod
}

// makeUnarmedPod builds the same Pod without the arming mark.
func makeUnarmedPod(name, sandboxID, phase, podIP string) *corev1.Pod {
	labels := map[string]string{}
	if sandboxID != "" {
		labels[agentsv1alpha1.SandboxIDLabelKey] = sandboxID
	}
	if phase != "" {
		labels[agentsv1alpha1.SandboxPhaseLabelKey] = phase
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    labels,
		},
		Status: corev1.PodStatus{PodIP: podIP},
	}
}

func newTestRouter(t *testing.T, cache *RouteCache, fallback bool, pods ...*corev1.Pod) *K8sSandboxRouter {
	t.Helper()
	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("builder: %v", err)
	}
	for _, p := range pods {
		cb = cb.WithObjects(p)
	}
	return NewK8sSandboxRouter(cb.Build(), 0, cache, fallback)
}

// --------------------------------------------------------------------------
// Cache-hit branches
//
// Decision table (per the router's current finalize logic):
//   phase == Running OR Stopping:
//     sandbox-id label matches  +  PodIP non-empty  → 200
//     sandbox-id label matches  +  PodIP empty      → 502
//     sandbox-id label mismatch                     → 502
//   phase ∈ {Starting, Idle, Failed, empty, other} → 502
//
// Every error case maps to BadGateway (502), never NotFound (404), except
// the top-level cache-miss-with-fallback-off and parameter-validation paths.
// --------------------------------------------------------------------------

func TestRouter_CacheHit_Running_ReturnsRoute(t *testing.T) {
	cache := NewRouteCache(time.Minute)
	cache.Put("sb1", RouteEntry{Namespace: "default", PodName: "pod-1"})

	pod := makePod("pod-1", "sb1", string(agentsv1alpha1.SandboxPhaseRunning), testPodIP)
	r := newTestRouter(t, cache, true, pod)

	route, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if route.PodIP != testPodIP || route.Port != 8080 {
		t.Fatalf("unexpected route: %+v", route)
	}
}

// Stopping pods still route: the sandbox is being released but the pod is
// still live and its runtime may still respond to client traffic.
func TestRouter_CacheHit_Stopping_ReturnsRoute(t *testing.T) {
	cache := NewRouteCache(time.Minute)
	cache.Put("sb1", RouteEntry{Namespace: "default", PodName: "pod-1"})

	pod := makePod("pod-1", "sb1", string(agentsv1alpha1.SandboxPhaseStopping), testPodIP)
	r := newTestRouter(t, cache, true, pod)

	route, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080)
	if err != nil {
		t.Fatalf("unexpected err (Stopping should still route): %v", err)
	}
	if route.PodIP != testPodIP {
		t.Fatalf("unexpected route: %+v", route)
	}
}

func TestRouter_CacheHit_PodMissing_ReturnsBadGateway(t *testing.T) {
	cache := NewRouteCache(time.Minute)
	cache.Put("sb1", RouteEntry{Namespace: "default", PodName: "pod-missing"})
	// Build the router with NO such Pod in the informer — simulates the lag
	// window between Controller push and ExtProc Pod watch.
	r := newTestRouter(t, cache, true)

	_, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080)
	if !errors.Is(err, ErrSandboxRouteBadGateway) {
		t.Fatalf("expected BadGateway, got %v", err)
	}
}

func TestRouter_CacheHit_LabelMismatch_ReturnsBadGateway(t *testing.T) {
	cache := NewRouteCache(time.Minute)
	cache.Put("sb1", RouteEntry{Namespace: "default", PodName: "pod-1"})
	// Pod is Running but the sandbox-id label no longer matches the querying
	// caller's ID (Pod was released + reclaimed since the cache was populated).
	pod := makePod("pod-1", "sb2", string(agentsv1alpha1.SandboxPhaseRunning), testPodIP)
	r := newTestRouter(t, cache, true, pod)

	_, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080)
	if !errors.Is(err, ErrSandboxRouteBadGateway) {
		t.Fatalf("expected BadGateway, got %v", err)
	}
}

// KEY TEST: sandbox is Starting (container swapping). Must NOT return 200 —
// routing here would send the request to the previous sandbox's runtime that
// hasn't been replaced yet, causing cross-sandbox contamination.
func TestRouter_CacheHit_Starting_ReturnsBadGateway(t *testing.T) {
	cache := NewRouteCache(time.Minute)
	cache.Put("sb1", RouteEntry{Namespace: "default", PodName: "pod-1"})
	pod := makePod("pod-1", "sb1", string(agentsv1alpha1.SandboxPhaseStarting), testPodIP)
	r := newTestRouter(t, cache, true, pod)

	_, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080)
	if !errors.Is(err, ErrSandboxRouteBadGateway) {
		t.Fatalf("expected BadGateway (Starting is not routable), got %v", err)
	}
}

func TestRouter_CacheHit_RunningNoIP_ReturnsBadGateway(t *testing.T) {
	cache := NewRouteCache(time.Minute)
	cache.Put("sb1", RouteEntry{Namespace: "default", PodName: "pod-1"})
	pod := makePod("pod-1", "sb1", string(agentsv1alpha1.SandboxPhaseRunning), "")
	r := newTestRouter(t, cache, true, pod)

	_, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080)
	if !errors.Is(err, ErrSandboxRouteBadGateway) {
		t.Fatalf("expected BadGateway (Running with no IP), got %v", err)
	}
}

func TestRouter_CacheHit_StoppingNoIP_ReturnsBadGateway(t *testing.T) {
	cache := NewRouteCache(time.Minute)
	cache.Put("sb1", RouteEntry{Namespace: "default", PodName: "pod-1"})
	pod := makePod("pod-1", "sb1", string(agentsv1alpha1.SandboxPhaseStopping), "")
	r := newTestRouter(t, cache, true, pod)

	_, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080)
	if !errors.Is(err, ErrSandboxRouteBadGateway) {
		t.Fatalf("expected BadGateway (Stopping with no IP), got %v", err)
	}
}

func TestRouter_CacheHit_Idle_ReturnsBadGateway(t *testing.T) {
	cache := NewRouteCache(time.Minute)
	cache.Put("sb1", RouteEntry{Namespace: "default", PodName: "pod-1"})
	pod := makePod("pod-1", "sb1", string(agentsv1alpha1.SandboxPhaseIdle), testPodIP)
	r := newTestRouter(t, cache, true, pod)

	_, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080)
	if !errors.Is(err, ErrSandboxRouteBadGateway) {
		t.Fatalf("expected BadGateway (Idle), got %v", err)
	}
}

func TestRouter_CacheHit_Failed_ReturnsBadGateway(t *testing.T) {
	cache := NewRouteCache(time.Minute)
	cache.Put("sb1", RouteEntry{Namespace: "default", PodName: "pod-1"})
	pod := makePod("pod-1", "sb1", string(agentsv1alpha1.SandboxPhaseFailed), testPodIP)
	r := newTestRouter(t, cache, true, pod)

	_, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080)
	if !errors.Is(err, ErrSandboxRouteBadGateway) {
		t.Fatalf("expected BadGateway (Failed), got %v", err)
	}
}

func TestRouter_CacheHit_EmptyPhase_ReturnsBadGateway(t *testing.T) {
	cache := NewRouteCache(time.Minute)
	cache.Put("sb1", RouteEntry{Namespace: "default", PodName: "pod-1"})
	pod := makePod("pod-1", "sb1", "", testPodIP) // no phase label
	r := newTestRouter(t, cache, true, pod)

	_, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080)
	if !errors.Is(err, ErrSandboxRouteBadGateway) {
		t.Fatalf("expected BadGateway (no phase label), got %v", err)
	}
}

// --------------------------------------------------------------------------
// Cache-miss branches
// --------------------------------------------------------------------------

func TestRouter_CacheMiss_FallbackOff_ReturnsNotFound(t *testing.T) {
	pod := makePod("pod-1", "sb1", string(agentsv1alpha1.SandboxPhaseRunning), testPodIP)
	r := newTestRouter(t, NewRouteCache(time.Minute), false, pod)

	// Pod exists in informer under sb1, but fallback is off → we treat cache
	// as authoritative.
	_, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080)
	if !errors.Is(err, ErrSandboxRouteNotFound) {
		t.Fatalf("expected NotFound (pure push mode), got %v", err)
	}
}

func TestRouter_CacheMiss_FallbackOn_IndexerHitRunning_ReturnsRouteAndBackfills(t *testing.T) {
	cache := NewRouteCache(time.Minute)
	pod := makePod("pod-1", "sb1", string(agentsv1alpha1.SandboxPhaseRunning), "10.2.2.2")
	r := newTestRouter(t, cache, true, pod)

	route, err := r.ResolveSandboxRoute(context.Background(), "sb1", 9090)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if route.PodIP != "10.2.2.2" || route.Port != 9090 {
		t.Fatalf("unexpected route: %+v", route)
	}
	// Backfill check: cache should now contain the mapping.
	if e, ok := cache.Get("sb1"); !ok || e.PodName != "pod-1" {
		t.Fatalf("expected cache backfill, got %+v ok=%v", e, ok)
	}
}

func TestRouter_CacheMiss_FallbackOn_IndexerHitStopping_ReturnsRouteAndBackfills(t *testing.T) {
	cache := NewRouteCache(time.Minute)
	pod := makePod("pod-1", "sb1", string(agentsv1alpha1.SandboxPhaseStopping), "10.2.2.2")
	r := newTestRouter(t, cache, true, pod)

	// Stopping pods still serve traffic per the current finalize rule.
	route, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080)
	if err != nil {
		t.Fatalf("unexpected err (Stopping via indexer should route): %v", err)
	}
	if route.PodIP != "10.2.2.2" {
		t.Fatalf("unexpected route: %+v", route)
	}
	if _, ok := cache.Get("sb1"); !ok {
		t.Fatal("expected cache backfill on success")
	}
}

func TestRouter_CacheMiss_FallbackOn_IndexerHitStarting_ReturnsBadGateway(t *testing.T) {
	cache := NewRouteCache(time.Minute)
	pod := makePod("pod-1", "sb1", string(agentsv1alpha1.SandboxPhaseStarting), "10.2.2.2")
	r := newTestRouter(t, cache, true, pod)

	_, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080)
	if !errors.Is(err, ErrSandboxRouteBadGateway) {
		t.Fatalf("expected BadGateway (Starting via indexer), got %v", err)
	}
	// No backfill on non-success.
	if _, ok := cache.Get("sb1"); ok {
		t.Fatal("did not expect cache backfill on non-success")
	}
}

func TestRouter_CacheMiss_FallbackOn_IndexerEmpty_ReturnsNotFound(t *testing.T) {
	r := newTestRouter(t, NewRouteCache(time.Minute), true)

	_, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080)
	if !errors.Is(err, ErrSandboxRouteNotFound) {
		t.Fatalf("expected NotFound (nothing anywhere), got %v", err)
	}
}

// --------------------------------------------------------------------------
// Edge cases
// --------------------------------------------------------------------------

func TestRouter_NilCache_UsesIndexer(t *testing.T) {
	pod := makePod("pod-1", "sb1", string(agentsv1alpha1.SandboxPhaseRunning), "10.3.3.3")
	r := newTestRouter(t, nil, true, pod)

	route, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if route.PodIP != "10.3.3.3" {
		t.Fatalf("unexpected route: %+v", route)
	}
}

func TestRouter_EmptySandboxID_ReturnsNotFound(t *testing.T) {
	r := newTestRouter(t, NewRouteCache(time.Minute), true)
	_, err := r.ResolveSandboxRoute(context.Background(), "", 8080)
	if !errors.Is(err, ErrSandboxRouteNotFound) {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestRouter_InvalidPort_ReturnsNotFound(t *testing.T) {
	r := newTestRouter(t, NewRouteCache(time.Minute), true)
	_, err := r.ResolveSandboxRoute(context.Background(), "sb1", 0)
	if !errors.Is(err, ErrSandboxRouteNotFound) {
		t.Fatalf("expected NotFound for port=0, got %v", err)
	}
	_, err = r.ResolveSandboxRoute(context.Background(), "sb1", 70000)
	if !errors.Is(err, ErrSandboxRouteNotFound) {
		t.Fatalf("expected NotFound for port=70000, got %v", err)
	}
}

// --------------------------------------------------------------------------
// Arming gate
//
// A Running pod whose image is in place but whose sandbox has not finished
// being set up must not be served: its env vars, injected CA, egress policy
// and credentials are still being delivered. Stopping pods are exempt — they
// were armed while Running and the release path strips the mark, so requiring
// it would cut off in-flight teardown traffic.
// --------------------------------------------------------------------------

func TestRouter_Running_NotArmed_ReturnsBadGateway(t *testing.T) {
	cache := NewRouteCache(time.Minute)
	cache.Put("sb1", RouteEntry{Namespace: "default", PodName: "pod-1"})

	pod := makeUnarmedPod("pod-1", "sb1", string(agentsv1alpha1.SandboxPhaseRunning), testPodIP)
	r := newTestRouter(t, cache, false, pod)

	if _, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080); !errors.Is(err, ErrSandboxRouteBadGateway) {
		t.Fatalf("expected BadGateway for an unarmed Running pod, got %v", err)
	}
}

func TestRouter_Running_ArmedForAnotherSandbox_ReturnsBadGateway(t *testing.T) {
	cache := NewRouteCache(time.Minute)
	cache.Put("sb1", RouteEntry{Namespace: "default", PodName: "pod-1"})

	// The pod was recycled: it now serves sb1 but still carries the previous
	// claim's arming mark. Reading that as "armed" is exactly the bug the
	// sandbox-ID-valued annotation exists to prevent.
	pod := makeUnarmedPod("pod-1", "sb1", string(agentsv1alpha1.SandboxPhaseRunning), testPodIP)
	pod.Annotations = map[string]string{agentsv1alpha1.SandboxArmedAnnotationKey: "sb0"}
	r := newTestRouter(t, cache, false, pod)

	if _, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080); !errors.Is(err, ErrSandboxRouteBadGateway) {
		t.Fatalf("expected BadGateway for a stale arming mark, got %v", err)
	}
}

func TestRouter_Stopping_NotArmed_StillRoutes(t *testing.T) {
	cache := NewRouteCache(time.Minute)
	cache.Put("sb1", RouteEntry{Namespace: "default", PodName: "pod-1"})

	pod := makeUnarmedPod("pod-1", "sb1", string(agentsv1alpha1.SandboxPhaseStopping), testPodIP)
	r := newTestRouter(t, cache, false, pod)

	route, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080)
	if err != nil {
		t.Fatalf("expected a route for a Stopping pod without the mark, got %v", err)
	}
	if route.PodIP != testPodIP {
		t.Fatalf("unexpected route: %+v", route)
	}
}

func TestRouter_CacheMiss_FallbackOn_IndexerHitUnarmed_ReturnsBadGateway(t *testing.T) {
	pod := makeUnarmedPod("pod-1", "sb1", string(agentsv1alpha1.SandboxPhaseRunning), testPodIP)
	r := newTestRouter(t, NewRouteCache(time.Minute), true, pod)

	if _, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080); !errors.Is(err, ErrSandboxRouteBadGateway) {
		t.Fatalf("expected BadGateway via the indexer path too, got %v", err)
	}
}
