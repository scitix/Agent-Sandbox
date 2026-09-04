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

package poststarthooks

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add corev1: %v", err)
	}
	if err := agentsv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add agents: %v", err)
	}
	return s
}

// armPod builds a claimed sandbox pod for the fixed test sandbox ID.
func armPod(podIP string) *corev1.Pod {
	const (
		sandboxID = "sb1"
		poolName  = "pool"
	)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "p",
			Namespace: "ns",
			Labels: map[string]string{
				agentsv1alpha1.SandboxIDLabelKey:   sandboxID,
				agentsv1alpha1.SandboxPoolLabelKey: poolName,
			},
			Annotations: map[string]string{
				agentsv1alpha1.SandboxIDAnnotationKey: sandboxID,
			},
		},
		Status: corev1.PodStatus{PodIP: podIP},
	}
}

// poolWithProbe builds a pool whose single runtime probes the given host:port.
// poolWithProbe builds a pool whose single runtime probes /health on port.
func poolWithProbe(port int) *agentsv1alpha1.SandboxPool {
	const path = "/health"
	return &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "ns"},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				Runtimes: []agentsv1alpha1.SandboxRuntimeSpec{{
					Name: "envd",
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(port),
								Path: path,
							},
						},
					},
				}},
			},
		},
	}
}

// --------------------------------------------------------------------------
// armDeadline
// --------------------------------------------------------------------------

func TestArmDeadline_NoAnnotations_UsesMaxBudget(t *testing.T) {
	pod := armPod("10.0.0.1")
	got := time.Until(armDeadline(pod))
	// Allow a little slack for the clock read between the two calls.
	if got < maxArmBudget-time.Second || got > maxArmBudget {
		t.Fatalf("expected ~%s budget, got %s", maxArmBudget, got)
	}
}

func TestArmDeadline_SubtractsTimeAlreadySpentClaimed(t *testing.T) {
	pod := armPod("10.0.0.1")
	pod.Annotations[agentsv1alpha1.SandboxStartupTimeoutAnnotationKey] = "300"
	pod.Annotations[agentsv1alpha1.SandboxClaimedAtAnnotationKey] =
		time.Now().Add(-100 * time.Second).Format(time.RFC3339)

	got := time.Until(armDeadline(pod))
	if got < 195*time.Second || got > 200*time.Second {
		t.Fatalf("expected ~200s remaining of a 300s budget, got %s", got)
	}
}

// A claim that raced close to its startup deadline still needs enough time for
// the arming round trips; the floor is what keeps it from failing on
// arithmetic alone.
func TestArmDeadline_ExhaustedBudget_ClampsToFloor(t *testing.T) {
	pod := armPod("10.0.0.1")
	pod.Annotations[agentsv1alpha1.SandboxStartupTimeoutAnnotationKey] = "60"
	pod.Annotations[agentsv1alpha1.SandboxClaimedAtAnnotationKey] =
		time.Now().Add(-10 * time.Minute).Format(time.RFC3339)

	got := time.Until(armDeadline(pod))
	if got < minArmBudget-time.Second || got > minArmBudget {
		t.Fatalf("expected the %s floor, got %s", minArmBudget, got)
	}
}

func TestArmDeadline_HugeTimeout_ClampsToCeiling(t *testing.T) {
	pod := armPod("10.0.0.1")
	pod.Annotations[agentsv1alpha1.SandboxStartupTimeoutAnnotationKey] = "86400"

	got := time.Until(armDeadline(pod))
	if got > maxArmBudget {
		t.Fatalf("expected the %s ceiling, got %s", maxArmBudget, got)
	}
}

// --------------------------------------------------------------------------
// readinessTargets
// --------------------------------------------------------------------------

func TestReadinessTargets_BuildsPodDirectURL(t *testing.T) {
	pod := armPod("10.0.0.1")
	r := &Runner{crClient: fake.NewClientBuilder().WithScheme(testScheme(t)).
		WithObjects(poolWithProbe(49983)).Build()}

	targets, err := r.readinessTargets(context.Background(), pod)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if targets[0].url != "http://10.0.0.1:49983/health" {
		t.Fatalf("probes must go straight to the pod, got %q", targets[0].url)
	}
}

func TestReadinessTargets_IPv6PodIP_IsBracketed(t *testing.T) {
	pod := armPod("fd00::1")
	r := &Runner{crClient: fake.NewClientBuilder().WithScheme(testScheme(t)).
		WithObjects(poolWithProbe(49983)).Build()}

	targets, err := r.readinessTargets(context.Background(), pod)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if targets[0].url != "http://[fd00::1]:49983/health" {
		t.Fatalf("IPv6 host must be bracketed, got %q", targets[0].url)
	}
}

// A runtime with no probe, or a non-HTTP probe, counts as ready — the same rule
// the readiness API applies — so it must not produce a target.
func TestReadinessTargets_SkipsRuntimesWithoutHTTPProbe(t *testing.T) {
	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "ns"},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				Runtimes: []agentsv1alpha1.SandboxRuntimeSpec{
					{Name: "no-probe"},
					{Name: "tcp-probe", ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt(1234)},
						},
					}},
				},
			},
		},
	}
	pod := armPod("10.0.0.1")
	r := &Runner{crClient: fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(pool).Build()}

	targets, err := r.readinessTargets(context.Background(), pod)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(targets) != 0 {
		t.Fatalf("expected no targets, got %+v", targets)
	}
}

func TestReadinessTargets_NoPodIP_IsAnError(t *testing.T) {
	pod := armPod("")
	r := &Runner{crClient: fake.NewClientBuilder().WithScheme(testScheme(t)).
		WithObjects(poolWithProbe(49983)).Build()}

	if _, err := r.readinessTargets(context.Background(), pod); err == nil {
		t.Fatal("expected an error when the pod has no IP")
	}
}

// --------------------------------------------------------------------------
// waitRuntimesReady
// --------------------------------------------------------------------------

// hostPort splits a test server URL into host and port.
func hostPort(t *testing.T, rawURL string) (string, int) {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %q: %v", rawURL, err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("port of %q: %v", rawURL, err)
	}
	return u.Hostname(), port
}

func TestWaitRuntimesReady_ReturnsOnceProbePasses(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Unready twice, then ready — the shape of a runtime still booting.
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	host, port := hostPort(t, srv.URL)
	pod := armPod(host)
	r := &Runner{
		httpClient: srv.Client(),
		crClient: fake.NewClientBuilder().WithScheme(testScheme(t)).
			WithObjects(poolWithProbe(port)).Build(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.waitRuntimesReady(ctx, pod); err != nil {
		t.Fatalf("expected the wait to succeed once the probe passed: %v", err)
	}
	if got := calls.Load(); got < 3 {
		t.Fatalf("expected at least 3 probe attempts, got %d", got)
	}
}

func TestWaitRuntimesReady_TimesOutAndNamesTheRuntime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	host, port := hostPort(t, srv.URL)
	pod := armPod(host)
	r := &Runner{
		httpClient: srv.Client(),
		crClient: fake.NewClientBuilder().WithScheme(testScheme(t)).
			WithObjects(poolWithProbe(port)).Build(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	err := r.waitRuntimesReady(ctx, pod)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	// The message has to say which runtime and where, or an operator reading
	// the arm-error annotation learns nothing actionable.
	if !strings.Contains(err.Error(), "envd") || !strings.Contains(err.Error(), "/health") {
		t.Fatalf("error should name the runtime and probe URL, got %q", err)
	}
}

// No controller-runtime client (unit wiring) must not block arming.
func TestWaitRuntimesReady_NoClient_IsNoop(t *testing.T) {
	pod := armPod("10.0.0.1")
	if err := (&Runner{}).waitRuntimesReady(context.Background(), pod); err != nil {
		t.Fatalf("expected a no-op, got %v", err)
	}
}

// --------------------------------------------------------------------------
// Arming verdicts
// --------------------------------------------------------------------------

// recordingNotifier captures what the runner reports.
type recordingNotifier struct {
	id  string
	err error
	got bool
}

func (n *recordingNotifier) Notify(sandboxID string, err error) {
	n.id, n.err, n.got = sandboxID, err, true
}

// The verdict goes to the waiting request directly. Writing it onto the Pod
// instead would cost an API-server write per sandbox, fanned out to every Pod
// informer in the cluster, to carry a fact between two goroutines of the same
// process.
func TestOnSandboxReady_ReportsFailureToTheWaiter(t *testing.T) {
	// Point the /init hook at a port nothing listens on, so the dial is
	// refused immediately. A routable-but-silent address would make the retry
	// backoff wait out its full budget and turn this into a minutes-long test.
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	host, _ := hostPort(t, closed.URL)
	closed.Close()

	pod := armPod(host)
	// No crClient, so the pool lookup is skipped; the /init hook then fails
	// against the closed port — which is the failure we want reported.
	r := &Runner{httpClient: &http.Client{Timeout: time.Second}}
	n := &recordingNotifier{}
	r.armNotifier = n

	r.OnSandboxReady(context.Background(), pod)

	if !n.got {
		t.Fatal("the waiting request was never told the outcome")
	}
	if n.id != "sb1" {
		t.Fatalf("verdict addressed to %q", n.id)
	}
	if n.err == nil {
		t.Fatal("a failed arming must be reported as a failure")
	}
}

// A runner built without a notifier must not panic: that is every unit test and
// any build without the API server half.
func TestNewRunner_NilNotifierIsANoop(t *testing.T) {
	r := NewRunner("", nil, nil, nil, nil)
	if r.armNotifier == nil {
		t.Fatal("expected a no-op notifier rather than nil")
	}
	r.armNotifier.Notify("sb1", nil)
}

func TestArmOutcome_DistinguishesTimeoutFromError(t *testing.T) {
	if got := armOutcome(context.DeadlineExceeded); got != "timeout" {
		t.Fatalf("DeadlineExceeded should be a timeout, got %q", got)
	}
	if got := armOutcome(errors.New("timed out waiting for sandbox runtimes")); got != "timeout" {
		t.Fatalf("wrapped wait timeout should be a timeout, got %q", got)
	}
	if got := armOutcome(errors.New("exec failed")); got != "error" {
		t.Fatalf("other failures should be errors, got %q", got)
	}
}
