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
	"encoding/json"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/inplaceupdate"
)

// --- test pod builders ---------------------------------------------------------

func rtIdlePod(name, hash string, ready bool, ageSecs int) corev1.Pod {
	rs := corev1.ConditionFalse
	if ready {
		rs = corev1.ConditionTrue
	}
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Duration(ageSecs) * time.Second)),
			Labels: map[string]string{
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseIdle,
				agentsv1alpha1.TemplateHashLabelKey: hash,
			},
		},
		Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: rs}}},
	}
}

func rtPhasePod(name, hash, phase string) corev1.Pod {
	return corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: name,
		Labels: map[string]string{
			agentsv1alpha1.SandboxPhaseLabelKey: phase,
			agentsv1alpha1.TemplateHashLabelKey: hash,
		},
	}}
}

func rtStoppingPod(name string, since time.Time) corev1.Pod {
	st := inplaceupdate.InplaceUpdateState{
		Phase:           agentsv1alpha1.SandboxPhaseStopping,
		TargetPodPhase:  agentsv1alpha1.SandboxPhaseIdle,
		UpdateTimestamp: metav1.NewTime(since),
	}
	b, _ := json.Marshal(st)
	return corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name:        name,
		Labels:      map[string]string{agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseStopping},
		Annotations: map[string]string{inplaceupdate.PodAnnotationInPlaceUpdateStateKey: string(b)},
	}}
}

func victimNames(v []*corev1.Pod) map[string]bool {
	m := make(map[string]bool, len(v))
	for _, p := range v {
		m[p.Name] = true
	}
	return m
}

func TestResolveMaxUnavailableCount(t *testing.T) {
	pct := func(s string) *intstr.IntOrString { v := intstr.FromString(s); return &v }
	abs := func(n int) *intstr.IntOrString { v := intstr.FromInt32(int32(n)); return &v }

	cases := []struct {
		name    string
		mu      *intstr.IntOrString
		desired int
		want    int
	}{
		{"nil defaults to 20% of 10", nil, 10, 2},
		{"20% of 10", pct("20%"), 10, 2},
		{"percent rounds down", pct("25%"), 10, 2},
		{"percent floored at 1 for small pool", pct("20%"), 3, 1},
		{"percent floored at 1 when zero", pct("10%"), 1, 1},
		{"absolute value", abs(3), 10, 3},
		{"absolute floored at 1", abs(0), 10, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveMaxUnavailableCount(tc.mu, tc.desired); got != tc.want {
				t.Errorf("resolveMaxUnavailableCount(%v, %d) = %d, want %d", tc.mu, tc.desired, got, tc.want)
			}
		})
	}
}

func TestPlanStaleIdleRoll_BreaksBrokenPodDeadlock(t *testing.T) {
	// kata-like: 30 desired, maxUnav 50%(=15), 14 idle-Ready(old) serving +
	// 14 idle-NotReady(old, ImagePullBackOff). readyIdle=14. The old budget
	// (unavailable 14+... >= 15) deadlocked; now the 14 broken get free-recycled
	// and serving is protected (servingBudget = 14 - max(0,30-15)=15 -> 0).
	var pods []corev1.Pod
	for i := range 14 {
		pods = append(pods, rtIdlePod("ready-old-"+string(rune('a'+i)), "old", true, i))
	}
	for i := range 14 {
		pods = append(pods, rtIdlePod("broken-old-"+string(rune('a'+i)), "old", false, i))
	}
	v := planStaleIdleRoll(pods, "new", 15, 30, 14)
	if len(v) != 14 {
		t.Fatalf("expected 14 free-recycled broken pods, got %d", len(v))
	}
	got := victimNames(v)
	for i := range 14 {
		if !got["broken-old-"+string(rune('a'+i))] {
			t.Errorf("broken pod %d not recycled", i)
		}
	}
	for i := range 14 {
		if got["ready-old-"+string(rune('a'+i))] {
			t.Errorf("serving pod %d must NOT be taken down when at the serving floor", i)
		}
	}
}

func TestPlanStaleIdleRoll_HealthyPoolBudgeted(t *testing.T) {
	// 10 desired, maxUnav 2, all 10 stale idle-Ready. servingBudget = 10 - 8 = 2.
	var pods []corev1.Pod
	for i := range 10 {
		pods = append(pods, rtIdlePod("p"+string(rune('a'+i)), "old", true, 10-i)) // varying age
	}
	v := planStaleIdleRoll(pods, "new", 2, 10, 10)
	if len(v) != 2 {
		t.Fatalf("expected 2 serving pods rolled within budget, got %d", len(v))
	}
}

func TestPlanStaleIdleRoll_SizeOneRolls(t *testing.T) {
	// size-1 healthy: maxUnav floored to 1, servingBudget = 1 - max(0,1-1)=1.
	pods := []corev1.Pod{rtIdlePod("only", "old", true, 0)}
	v := planStaleIdleRoll(pods, "new", 1, 1, 1)
	if len(v) != 1 || v[0].Name != "only" {
		t.Fatalf("size-1 pool must roll its single stale pod, got %v", victimNames(v))
	}
}

func TestPlanStaleIdleRoll_NeverTouchesRunningStoppingStartingOrCurrent(t *testing.T) {
	pods := []corev1.Pod{
		rtPhasePod("running-old", "old", agentsv1alpha1.SandboxPhaseRunning),
		rtPhasePod("stopping-old", "old", agentsv1alpha1.SandboxPhaseStopping),
		rtPhasePod("starting-old", "old", agentsv1alpha1.SandboxPhaseStarting),
		rtIdlePod("idle-current", "new", true, 0), // already on target revision
		rtIdlePod("idle-old", "old", true, 0),     // the only legit victim
	}
	v := planStaleIdleRoll(pods, "new", 5, 5, 1)
	got := victimNames(v)
	if !got["idle-old"] || len(got) != 1 {
		t.Fatalf("expected only idle-old rolled, got %v", got)
	}
	for _, bad := range []string{"running-old", "stopping-old", "starting-old", "idle-current"} {
		if got[bad] {
			t.Errorf("%s must never be rolled", bad)
		}
	}
}

func TestPlanStaleIdleRoll_FreeRecycleCappedPerCycle(t *testing.T) {
	// 20 broken stale idle, maxUnav 5 -> free-recycle capped at 5/cycle.
	var pods []corev1.Pod
	for i := range 20 {
		pods = append(pods, rtIdlePod("b"+string(rune('a'+i)), "old", false, 20-i))
	}
	v := planStaleIdleRoll(pods, "new", 5, 100, 0)
	if len(v) != 5 {
		t.Fatalf("expected free-recycle capped at 5, got %d", len(v))
	}
}

func TestIsStoppingStuck(t *testing.T) {
	now := time.Now().UTC()
	if !isStoppingStuck(&[]corev1.Pod{rtStoppingPod("s", now.Add(-6*time.Minute))}[0], 5*time.Minute, now) {
		t.Error("6m in Stopping with 5m timeout should be stuck")
	}
	if isStoppingStuck(&[]corev1.Pod{rtStoppingPod("s", now.Add(-1*time.Minute))}[0], 5*time.Minute, now) {
		t.Error("1m in Stopping with 5m timeout should NOT be stuck")
	}
	idle := rtIdlePod("i", "h", true, 0)
	if isStoppingStuck(&idle, 5*time.Minute, now) {
		t.Error("idle pod is never stuck-stopping")
	}
}

func TestStoppingTimeout(t *testing.T) {
	if got := stoppingTimeout(&agentsv1alpha1.SandboxPool{}); got != defaultStoppingTimeout {
		t.Errorf("nil DefaultStartupTimeout should fall back to default, got %v", got)
	}
	p := &agentsv1alpha1.SandboxPool{Spec: agentsv1alpha1.SandboxPoolSpec{
		DefaultStartupTimeout: &metav1.Duration{Duration: 3 * time.Minute},
	}}
	if got := stoppingTimeout(p); got != 3*time.Minute {
		t.Errorf("should reuse DefaultStartupTimeout, got %v", got)
	}
}
