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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

// testPodIP is the IP every fixture pod is given.
const testPodIP = "10.1.1.1"

// makePod builds a Pod with the given phase label, sandbox-id label, and IP.
// Passing phase == "" leaves the phase label unset.
func makePod(name, sandboxID, phase, podIP string) *corev1.Pod { //nolint:unparam // name is a fixture knob; the recycled-pod case reads better with it explicit
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

func newTestRouter(t *testing.T, pods ...*corev1.Pod) *K8sSandboxRouter {
	t.Helper()
	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("builder: %v", err)
	}
	for _, p := range pods {
		cb = cb.WithObjects(p)
	}
	return NewK8sSandboxRouter(cb.Build(), 0)
}

// --------------------------------------------------------------------------
// The decision table
//
//	phase == Running OR Stopping:
//	  sandbox-id label matches  +  PodIP non-empty  → 200
//	  sandbox-id label matches  +  PodIP empty      → 502
//	  sandbox-id label mismatch                     → 502
//	phase ∈ {Starting, Idle, Failed, empty, other}  → 502
//
// Every routing failure is served as 502, including a sandbox that does not
// exist. See ErrSandboxRouteNotFound for why this router is in no position to
// claim a 404.
// --------------------------------------------------------------------------

func TestRouter_Running_ReturnsRoute(t *testing.T) {
	r := newTestRouter(t, makePod("pod-1", "sb1", string(agentsv1alpha1.SandboxPhaseRunning), testPodIP))

	route, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if route.PodIP != testPodIP || route.Port != 8080 {
		t.Fatalf("unexpected route: %+v", route)
	}
}

// Stopping pods still route: the sandbox is being released but the pod is
// still live and its runtime may still respond to in-flight client traffic.
func TestRouter_Stopping_ReturnsRoute(t *testing.T) {
	r := newTestRouter(t, makePod("pod-1", "sb1", string(agentsv1alpha1.SandboxPhaseStopping), testPodIP))

	route, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if route.PodIP != testPodIP {
		t.Fatalf("unexpected route: %+v", route)
	}
}

// Starting must not route: the container is swapping images, and answering
// there would reach the previous sandbox's runtime.
func TestRouter_Starting_ReturnsBadGateway(t *testing.T) {
	r := newTestRouter(t, makePod("pod-1", "sb1", string(agentsv1alpha1.SandboxPhaseStarting), testPodIP))

	if _, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080); !errors.Is(err, ErrSandboxRouteBadGateway) {
		t.Fatalf("expected BadGateway, got %v", err)
	}
}

func TestRouter_RunningWithoutIP_ReturnsBadGateway(t *testing.T) {
	r := newTestRouter(t, makePod("pod-1", "sb1", string(agentsv1alpha1.SandboxPhaseRunning), ""))

	if _, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080); !errors.Is(err, ErrSandboxRouteBadGateway) {
		t.Fatalf("expected BadGateway, got %v", err)
	}
}

func TestRouter_StoppingWithoutIP_ReturnsBadGateway(t *testing.T) {
	r := newTestRouter(t, makePod("pod-1", "sb1", string(agentsv1alpha1.SandboxPhaseStopping), ""))

	if _, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080); !errors.Is(err, ErrSandboxRouteBadGateway) {
		t.Fatalf("expected BadGateway, got %v", err)
	}
}

func TestRouter_IdleAndFailedAndUnsetPhase_ReturnBadGateway(t *testing.T) {
	for _, phase := range []string{
		string(agentsv1alpha1.SandboxPhaseIdle),
		string(agentsv1alpha1.SandboxPhaseFailed),
		"",
		"something-else",
	} {
		r := newTestRouter(t, makePod("pod-1", "sb1", phase, testPodIP))
		if _, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080); !errors.Is(err, ErrSandboxRouteBadGateway) {
			t.Fatalf("phase %q: expected BadGateway, got %v", phase, err)
		}
	}
}

// A released Pod has had its sandbox-id label stripped, so the index no longer
// resolves the old ID. This is the whole of what "the sandbox is gone" means
// here, and the reason releasing one needs to notify nobody.
func TestRouter_ReleasedSandbox_IsNotFound(t *testing.T) {
	// A pod with no sandbox-id label at all — what release leaves behind.
	r := newTestRouter(t, makePod("pod-1", "", string(agentsv1alpha1.SandboxPhaseIdle), testPodIP))

	if _, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080); !errors.Is(err, ErrSandboxRouteNotFound) {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestRouter_UnknownSandbox_IsNotFound(t *testing.T) {
	r := newTestRouter(t)

	if _, err := r.ResolveSandboxRoute(context.Background(), "sb-nope", 8080); !errors.Is(err, ErrSandboxRouteNotFound) {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestRouter_EmptySandboxID_ReturnsNotFound(t *testing.T) {
	r := newTestRouter(t)
	if _, err := r.ResolveSandboxRoute(context.Background(), "", 8080); !errors.Is(err, ErrSandboxRouteNotFound) {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestRouter_InvalidPort_ReturnsNotFound(t *testing.T) {
	r := newTestRouter(t)
	if _, err := r.ResolveSandboxRoute(context.Background(), "sb1", 0); !errors.Is(err, ErrSandboxRouteNotFound) {
		t.Fatalf("expected NotFound for port=0, got %v", err)
	}
	if _, err := r.ResolveSandboxRoute(context.Background(), "sb1", 70000); !errors.Is(err, ErrSandboxRouteNotFound) {
		t.Fatalf("expected NotFound for port=70000, got %v", err)
	}
}

// --------------------------------------------------------------------------
// What replaced the pushed route cache
// --------------------------------------------------------------------------

// The property that lets the control plane stay silent: a sandbox is
// resolvable from the moment it is claimed, because the claim writes the
// sandbox-id label in the same CAS that moves the Pod to Starting.
//
// Resolvable is not the same as routable — Starting still answers 502, by the
// rule above. But the mapping is already there, so when the Pod reaches
// Running there is nothing left to propagate and no window to race.
func TestRouter_ResolvesASandboxWhileItIsStillStarting(t *testing.T) {
	starting := makePod("pod-1", "sb1", string(agentsv1alpha1.SandboxPhaseStarting), testPodIP)
	r := newTestRouter(t, starting)

	// Known: refused as not-yet-routable, NOT as unknown.
	_, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080)
	if errors.Is(err, ErrSandboxRouteNotFound) {
		t.Fatal("a claimed sandbox must be resolvable while Starting; the gateway would otherwise need to be told about it")
	}
	if !errors.Is(err, ErrSandboxRouteBadGateway) {
		t.Fatalf("expected BadGateway while Starting, got %v", err)
	}
}

// Two routers over the same cluster state agree, which is what replication
// rests on. A pushed cache could not offer this: one gRPC connection reaches
// one replica, leaving the others to answer differently for the same sandbox.
func TestRouter_TwoReplicasAgree(t *testing.T) {
	pod := makePod("pod-1", "sb1", string(agentsv1alpha1.SandboxPhaseRunning), testPodIP)

	a := newTestRouter(t, pod)
	b := newTestRouter(t, pod.DeepCopy())

	routeA, errA := a.ResolveSandboxRoute(context.Background(), "sb1", 8080)
	routeB, errB := b.ResolveSandboxRoute(context.Background(), "sb1", 8080)
	if errA != nil || errB != nil {
		t.Fatalf("both replicas must resolve: %v / %v", errA, errB)
	}
	if routeA.DestHost() != routeB.DestHost() {
		t.Fatalf("replicas disagree: %s vs %s", routeA.DestHost(), routeB.DestHost())
	}
}

// A recycled Pod serving a new sandbox must not answer for the old ID. The
// index is keyed on the label, so the stale ID resolves to nothing at all.
func TestRouter_RecycledPodDoesNotAnswerForThePreviousSandbox(t *testing.T) {
	// pod-1 now carries sb2; sb1 is the ID it used to serve.
	r := newTestRouter(t, makePod("pod-1", "sb2", string(agentsv1alpha1.SandboxPhaseRunning), testPodIP))

	if _, err := r.ResolveSandboxRoute(context.Background(), "sb1", 8080); !errors.Is(err, ErrSandboxRouteNotFound) {
		t.Fatalf("the previous sandbox ID must not resolve, got %v", err)
	}
	if _, err := r.ResolveSandboxRoute(context.Background(), "sb2", 8080); err != nil {
		t.Fatalf("the current sandbox must resolve: %v", err)
	}
}
