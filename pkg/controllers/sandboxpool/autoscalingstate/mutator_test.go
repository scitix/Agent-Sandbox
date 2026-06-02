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

package autoscalingstate

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// fakeEventRecorder is the test-side stand-in for the
// k8s.io/client-go/tools/events.EventRecorder interface. The new
// events API has no stdlib FakeRecorder, so we implement just enough
// to capture (eventType, reason, action, message) tuples for assertion.
type fakeEventRecorder struct{ events []string }

func (f *fakeEventRecorder) Eventf(_, _ runtime.Object, eventType, reason, action, note string, args ...any) {
	f.events = append(f.events, fmt.Sprintf("%s %s %s %s", eventType, reason, action, fmt.Sprintf(note, args...)))
}

// ---------- Mutator accumulators ----------

func TestMutator_PatchStatus_NilSafe(t *testing.T) {
	m := NewMutator(&Snapshot{Pool: poolFixture{name: "p"}.build()})
	m.PatchStatus(nil) // must not panic
	if m.HasWrites() {
		t.Error("expected no writes after PatchStatus(nil)")
	}
}

func TestMutator_SetTargetReplicas(t *testing.T) {
	m := NewMutator(&Snapshot{Pool: poolFixture{name: "p"}.build()})
	if _, ok := m.TargetReplicas(); ok {
		t.Error("expected no target before SetTargetReplicas")
	}
	m.SetTargetReplicas(3)
	if v, ok := m.TargetReplicas(); !ok || v != 3 {
		t.Errorf("got (%d, %v), want (3, true)", v, ok)
	}
	m.SetTargetReplicas(7) // last wins
	if v, _ := m.TargetReplicas(); v != 7 {
		t.Errorf("expected last-wins 7, got %d", v)
	}
	m.SetTargetReplicas(-5) // clamped to 0
	if v, _ := m.TargetReplicas(); v != 0 {
		t.Errorf("expected clamped 0, got %d", v)
	}
}

func TestMutator_PodAnnotationOps_NilSafe(t *testing.T) {
	m := NewMutator(&Snapshot{Pool: poolFixture{name: "p"}.build()})
	m.MarkPodScaleDownProtected(nil, time.Now())
	m.UnmarkPodScaleDownProtected(nil)
	if m.HasWrites() {
		t.Error("expected no writes after nil pod ops")
	}
}

func TestMutator_HasWrites(t *testing.T) {
	m := NewMutator(&Snapshot{Pool: poolFixture{name: "p"}.build()})
	if m.HasWrites() {
		t.Error("expected no writes initially")
	}
	m.PatchStatus(func(*agentsv1alpha1.PoolAutoScalingStatus) {})
	if !m.HasWrites() {
		t.Error("expected HasWrites after PatchStatus")
	}
}

// ---------- Commit success paths ----------

func TestCommit_StatusPatch_SetsFields(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "p", replicas: 2}.build()
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pool).
		WithStatusSubresource(&agentsv1alpha1.SandboxPool{}).
		Build()
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC).Local()

	m := NewMutator(&Snapshot{Pool: pool})
	m.PatchStatus(func(s *agentsv1alpha1.PoolAutoScalingStatus) {
		t := metav1.NewTime(now)
		s.LastScaleUpTime = &t
		s.LastScaleUpAttemptResult = "Success"
	})

	if err := m.Commit(context.Background(), c, nil); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	got := &agentsv1alpha1.SandboxPool{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: pool.Namespace, Name: pool.Name}, got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status.AutoScaling == nil {
		t.Fatal("AutoScaling status not written")
	}
	if got.Status.AutoScaling.LastScaleUpAttemptResult != "Success" {
		t.Errorf("LastScaleUpAttemptResult = %q", got.Status.AutoScaling.LastScaleUpAttemptResult)
	}
	if got.Status.AutoScaling.LastScaleUpTime == nil ||
		!got.Status.AutoScaling.LastScaleUpTime.Time.Equal(now) {
		t.Errorf("LastScaleUpTime = %v, want %v", got.Status.AutoScaling.LastScaleUpTime, now)
	}
	if got.Status.AutoScaling.ObservedGeneration != got.Generation {
		t.Errorf("ObservedGeneration = %d, want %d", got.Status.AutoScaling.ObservedGeneration, got.Generation)
	}
}

func TestCommit_SpecPatch_UpdatesEnvMemberReplicas(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "p", replicas: 2}.build()
	env := envFixture{
		members: []agentsv1alpha1.EnvClusterMember{{
			Name: pool.Name,
			Spec: agentsv1alpha1.SandboxPoolSpec{Replicas: 2},
		}},
	}.build()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, env).Build()

	m := NewMutator(&Snapshot{Pool: pool, Env: env})
	m.SetTargetReplicas(5)

	if err := m.Commit(context.Background(), c, nil); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	gotEnv := &agentsv1alpha1.SandboxEnv{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: env.Namespace, Name: env.Name}, gotEnv); err != nil {
		t.Fatalf("get env: %v", err)
	}
	if got := gotEnv.Spec.Clusters[0].Members[0].Spec.Replicas; got != 5 {
		t.Errorf("Env Member.Spec.Replicas = %d, want 5", got)
	}
	// The live Pool spec must NOT be touched directly — that's the Env
	// reconciler's job once it observes the Env change.
	gotPool := &agentsv1alpha1.SandboxPool{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: pool.Namespace, Name: pool.Name}, gotPool); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if gotPool.Spec.Replicas != 2 {
		t.Errorf("Pool.Spec.Replicas was modified to %d; only Env reconciler should write it", gotPool.Spec.Replicas)
	}
}

func TestCommit_SpecPatch_NoOpWhenSameValue(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "p", replicas: 4}.build()
	env := envFixture{
		members: []agentsv1alpha1.EnvClusterMember{{
			Name: pool.Name,
			Spec: agentsv1alpha1.SandboxPoolSpec{Replicas: 4},
		}},
	}.build()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, env).Build()

	m := NewMutator(&Snapshot{Pool: pool, Env: env})
	m.SetTargetReplicas(4) // same as current

	if err := m.Commit(context.Background(), c, nil); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	gotEnv := &agentsv1alpha1.SandboxEnv{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: env.Namespace, Name: env.Name}, gotEnv); err != nil {
		t.Fatalf("get env: %v", err)
	}
	if got := gotEnv.Spec.Clusters[0].Members[0].Spec.Replicas; got != 4 {
		t.Errorf("Env Member.Spec.Replicas changed unexpectedly to %d", got)
	}
}

func TestCommit_SpecPatch_RequiresEnvInSnapshot(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "p", replicas: 1}.build()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()

	m := NewMutator(&Snapshot{Pool: pool}) // no Env on snapshot
	m.SetTargetReplicas(2)

	if err := m.Commit(context.Background(), c, nil); err == nil {
		t.Error("expected error when SetTargetReplicas called without Env in Snapshot")
	}
}

func TestCommit_SpecPatch_MemberNotPresent(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "missing"}.build()
	env := envFixture{
		members: []agentsv1alpha1.EnvClusterMember{{Name: "other"}},
	}.build()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, env).Build()

	m := NewMutator(&Snapshot{Pool: pool, Env: env})
	m.SetTargetReplicas(3)

	if err := m.Commit(context.Background(), c, nil); err == nil {
		t.Error("expected error when target pool name not in env members")
	}
}

func TestCommit_StatusPatch_NoOpWhenNoDiff(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "p"}.build()
	// Seed the pool with a pre-existing AutoScaling block.
	existing := metav1.NewTime(time.Date(2026, 5, 26, 8, 0, 0, 0, time.UTC).Local())
	pool.Status.AutoScaling = &agentsv1alpha1.PoolAutoScalingStatus{
		LastScaleUpTime:    &existing,
		ObservedGeneration: pool.Generation,
	}
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pool).
		WithStatusSubresource(&agentsv1alpha1.SandboxPool{}).
		Build()

	m := NewMutator(&Snapshot{Pool: pool})
	// Mutator that touches the field but writes the same value as the seed.
	m.PatchStatus(func(s *agentsv1alpha1.PoolAutoScalingStatus) {
		v := metav1.NewTime(existing.Time)
		s.LastScaleUpTime = &v
	})

	if err := m.Commit(context.Background(), c, nil); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	// Status should remain identical.
	got := &agentsv1alpha1.SandboxPool{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: pool.Namespace, Name: pool.Name}, got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status.AutoScaling == nil ||
		got.Status.AutoScaling.LastScaleUpTime == nil ||
		!got.Status.AutoScaling.LastScaleUpTime.Time.Equal(existing.Time) {
		t.Errorf("expected unchanged status, got %+v", got.Status.AutoScaling)
	}
}

func TestCommit_PodAnnotation_MarkAndUnmark(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "p"}.build()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: testNS, Name: "p-pod-0"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, pod).Build()

	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)

	// Mark.
	m := NewMutator(&Snapshot{Pool: pool})
	m.MarkPodScaleDownProtected(pod, now)
	if err := m.Commit(context.Background(), c, nil); err != nil {
		t.Fatalf("mark commit: %v", err)
	}
	got := &corev1.Pod{}
	_ = c.Get(context.Background(), client.ObjectKey{Namespace: testNS, Name: pod.Name}, got)
	if got.Annotations[agentsv1alpha1.SandboxScaleDownProtectedAnnotationKey] != now.Format(time.RFC3339) {
		t.Errorf("mark annotation = %q", got.Annotations[agentsv1alpha1.SandboxScaleDownProtectedAnnotationKey])
	}

	// Unmark.
	m2 := NewMutator(&Snapshot{Pool: pool})
	m2.UnmarkPodScaleDownProtected(pod)
	if err := m2.Commit(context.Background(), c, nil); err != nil {
		t.Fatalf("unmark commit: %v", err)
	}
	got = &corev1.Pod{}
	_ = c.Get(context.Background(), client.ObjectKey{Namespace: testNS, Name: pod.Name}, got)
	if _, ok := got.Annotations[agentsv1alpha1.SandboxScaleDownProtectedAnnotationKey]; ok {
		t.Errorf("expected annotation cleared, got %v", got.Annotations)
	}
}

func TestCommit_PodAnnotation_PodGone_NotFatal(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "p"}.build()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()

	// Pod is referenced but not seeded — Get returns NotFound.
	ghostPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: testNS, Name: "ghost"}}
	m := NewMutator(&Snapshot{Pool: pool})
	m.MarkPodScaleDownProtected(ghostPod, time.Now())

	if err := m.Commit(context.Background(), c, nil); err != nil {
		t.Errorf("Commit returned error for missing pod (should be nop): %v", err)
	}
}

// ---------- ScaleUpAttempt + probe resolution ----------

// fakeProber returns canned (accepted, result, msg) tuples so probe
// outcomes can be unit-tested without standing up the plugin chain.
type fakeProber struct {
	accepted int32
	result   agentsv1alpha1.PoolScaleUpAttemptResult
	errMsg   string
}

func (f *fakeProber) Probe(_ context.Context, _ *agentsv1alpha1.SandboxPool, _, _ int32) (int32, agentsv1alpha1.PoolScaleUpAttemptResult, string) {
	return f.accepted, f.result, f.errMsg
}

func TestMutator_ResolveScaleUpAttempt_Enough(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "p", replicas: 2}.build()
	env := envFixture{members: []agentsv1alpha1.EnvClusterMember{{
		Name: pool.Name,
		Spec: agentsv1alpha1.SandboxPoolSpec{Replicas: 2},
	}}}.build()
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pool, env).
		WithStatusSubresource(&agentsv1alpha1.SandboxPool{}).
		Build()
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC).Local()

	snap := &Snapshot{
		Pool:   pool,
		Env:    env,
		Prober: &fakeProber{accepted: 5, result: agentsv1alpha1.PoolScaleUpAttemptEnough},
		Now:    now,
	}
	m := NewMutator(snap)
	m.ScaleUpAttempt(2, 5)

	if err := m.Commit(context.Background(), c, nil); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	gotEnv := &agentsv1alpha1.SandboxEnv{}
	_ = c.Get(context.Background(), client.ObjectKey{Namespace: env.Namespace, Name: env.Name}, gotEnv)
	if v := gotEnv.Spec.Clusters[0].Members[0].Spec.Replicas; v != 5 {
		t.Errorf("Env Member.Spec.Replicas = %d, want 5", v)
	}
	gotPool := &agentsv1alpha1.SandboxPool{}
	_ = c.Get(context.Background(), client.ObjectKey{Namespace: pool.Namespace, Name: pool.Name}, gotPool)
	as := gotPool.Status.AutoScaling
	if as == nil || as.LastScaleUpTime == nil {
		t.Fatalf("expected LastScaleUpTime set, got %+v", as)
	}
	if as.LastScaleUpAttemptResult != agentsv1alpha1.PoolScaleUpAttemptEnough {
		t.Errorf("Result = %q, want Enough", as.LastScaleUpAttemptResult)
	}
	if as.ScaleUpErrorMessage != "" {
		t.Errorf("ScaleUpErrorMessage = %q, want empty", as.ScaleUpErrorMessage)
	}
}

func TestMutator_ResolveScaleUpAttempt_Insufficient_FullReject(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "p", replicas: 2}.build()
	env := envFixture{members: []agentsv1alpha1.EnvClusterMember{{
		Name: pool.Name,
		Spec: agentsv1alpha1.SandboxPoolSpec{Replicas: 2},
	}}}.build()
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pool, env).
		WithStatusSubresource(&agentsv1alpha1.SandboxPool{}).
		Build()

	snap := &Snapshot{
		Pool: pool,
		Env:  env,
		// Plugin says nothing extra accepted.
		Prober: &fakeProber{accepted: 2, result: agentsv1alpha1.PoolScaleUpAttemptInsufficient, errMsg: "no headroom"},
		Now:    time.Now(),
	}
	m := NewMutator(snap)
	m.ScaleUpAttempt(2, 5)

	if err := m.Commit(context.Background(), c, nil); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	gotEnv := &agentsv1alpha1.SandboxEnv{}
	_ = c.Get(context.Background(), client.ObjectKey{Namespace: env.Namespace, Name: env.Name}, gotEnv)
	if v := gotEnv.Spec.Clusters[0].Members[0].Spec.Replicas; v != 2 {
		t.Errorf("Env Member.Spec.Replicas = %d, want unchanged 2", v)
	}
	gotPool := &agentsv1alpha1.SandboxPool{}
	_ = c.Get(context.Background(), client.ObjectKey{Namespace: pool.Namespace, Name: pool.Name}, gotPool)
	as := gotPool.Status.AutoScaling
	if as == nil || as.LastScaleUpAttemptTime == nil {
		t.Fatalf("expected LastScaleUpAttemptTime set, got %+v", as)
	}
	if as.LastScaleUpTime != nil {
		t.Error("LastScaleUpTime should remain nil when nothing was accepted")
	}
	if as.LastScaleUpAttemptResult != agentsv1alpha1.PoolScaleUpAttemptInsufficient {
		t.Errorf("Result = %q, want Insufficient", as.LastScaleUpAttemptResult)
	}
	if as.ScaleUpErrorMessage != "no headroom" {
		t.Errorf("ScaleUpErrorMessage = %q, want %q", as.ScaleUpErrorMessage, "no headroom")
	}
}

func TestMutator_ResolveScaleUpAttempt_PartialAccept(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "p", replicas: 2}.build()
	env := envFixture{members: []agentsv1alpha1.EnvClusterMember{{
		Name: pool.Name,
		Spec: agentsv1alpha1.SandboxPoolSpec{Replicas: 2},
	}}}.build()
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pool, env).
		WithStatusSubresource(&agentsv1alpha1.SandboxPool{}).
		Build()

	snap := &Snapshot{
		Pool: pool,
		Env:  env,
		// Plugin admits 4 out of requested 7 — partial accept reports
		// Insufficient today (JustRight is the future refinement).
		Prober: &fakeProber{accepted: 4, result: agentsv1alpha1.PoolScaleUpAttemptInsufficient, errMsg: "cap at 4"},
		Now:    time.Now(),
	}
	m := NewMutator(snap)
	m.ScaleUpAttempt(2, 7)

	if err := m.Commit(context.Background(), c, nil); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	gotEnv := &agentsv1alpha1.SandboxEnv{}
	_ = c.Get(context.Background(), client.ObjectKey{Namespace: env.Namespace, Name: env.Name}, gotEnv)
	if v := gotEnv.Spec.Clusters[0].Members[0].Spec.Replicas; v != 4 {
		t.Errorf("Env Member.Spec.Replicas = %d, want partial 4", v)
	}
	gotPool := &agentsv1alpha1.SandboxPool{}
	_ = c.Get(context.Background(), client.ObjectKey{Namespace: pool.Namespace, Name: pool.Name}, gotPool)
	as := gotPool.Status.AutoScaling
	if as.LastScaleUpTime == nil {
		t.Error("partial accept should stamp LastScaleUpTime (we did grow)")
	}
	if as.LastScaleUpAttemptResult != agentsv1alpha1.PoolScaleUpAttemptInsufficient {
		t.Errorf("Result = %q, want Insufficient", as.LastScaleUpAttemptResult)
	}
}

func TestMutator_ResolveScaleUpAttempt_NoProber_TrivialAccept(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "p", replicas: 1}.build()
	env := envFixture{members: []agentsv1alpha1.EnvClusterMember{{
		Name: pool.Name,
		Spec: agentsv1alpha1.SandboxPoolSpec{Replicas: 1},
	}}}.build()
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pool, env).
		WithStatusSubresource(&agentsv1alpha1.SandboxPool{}).
		Build()

	snap := &Snapshot{Pool: pool, Env: env, Now: time.Now()} // no Prober wired
	m := NewMutator(snap)
	m.ScaleUpAttempt(1, 3)

	if err := m.Commit(context.Background(), c, nil); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	gotEnv := &agentsv1alpha1.SandboxEnv{}
	_ = c.Get(context.Background(), client.ObjectKey{Namespace: env.Namespace, Name: env.Name}, gotEnv)
	if v := gotEnv.Spec.Clusters[0].Members[0].Spec.Replicas; v != 3 {
		t.Errorf("nil-Prober path should trivially accept the target: got %d, want 3", v)
	}
}

func TestCommit_EmitEvent(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "p"}.build()
	env := envFixture{
		members: []agentsv1alpha1.EnvClusterMember{{
			Name: pool.Name,
			Spec: agentsv1alpha1.SandboxPoolSpec{Replicas: 2},
		}},
	}.build()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, env).Build()
	rec := &fakeEventRecorder{}

	m := NewMutator(&Snapshot{Pool: pool, Env: env})
	m.SetTargetReplicas(3) // ensures Commit has at least one write to flush
	m.EmitEvent(corev1.EventTypeNormal, "ScaleUp", "AutoscalerScaleUp", "scaled from %d to %d", 2, 3)

	if err := m.Commit(context.Background(), c, rec); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if got, want := len(rec.events), 1; got != want {
		t.Fatalf("recorded %d events, want %d", got, want)
	}
	if want := "Normal AutoscalerScaleUp ScaleUp scaled from 2 to 3"; rec.events[0] != want {
		t.Errorf("event = %q, want %q", rec.events[0], want)
	}
}

// ---------- Commit guards ----------

func TestCommit_NilGuards(t *testing.T) {
	scheme := newTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Nil Mutator.
	if err := (*Mutator)(nil).Commit(context.Background(), c, nil); err == nil {
		t.Error("expected error from nil Mutator")
	}
	// Nil Snapshot.Pool.
	m := NewMutator(&Snapshot{})
	if err := m.Commit(context.Background(), c, nil); err == nil {
		t.Error("expected error from nil Snapshot.Pool")
	}
	// Nil client.
	m2 := NewMutator(&Snapshot{Pool: poolFixture{name: "p"}.build()})
	if err := m2.Commit(context.Background(), nil, nil); err == nil {
		t.Error("expected error from nil client")
	}
}

func TestCommit_EmptyMutator_NoOp(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "p"}.build()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()

	m := NewMutator(&Snapshot{Pool: pool})
	if m.HasWrites() {
		t.Fatal("precondition: expected no writes")
	}
	if err := m.Commit(context.Background(), c, nil); err != nil {
		t.Fatalf("Commit on empty mutator: %v", err)
	}
}

// ---------- Scale-down transition ----------

func TestScaleDownTransition_RoundTrip(t *testing.T) {
	m := NewMutator(&Snapshot{Pool: poolFixture{name: "p"}.build()})
	if tr := m.ScaleDownTransition(); tr.Kind != ScaleDownNoTransition {
		t.Errorf("default transition = %+v, want NoTransition", tr)
	}
	m.SetScaleDownTransition(ScaleDownTransition{Kind: ScaleDownStarted, StartReplicas: 7})
	if tr := m.ScaleDownTransition(); tr.Kind != ScaleDownStarted || tr.StartReplicas != 7 {
		t.Errorf("transition = %+v, want Started{7}", tr)
	}
}

// A Completed/Aborted cycle stages no spec/status write, only a
// transition — HasWrites must still report true so Commit runs and the
// Completed event is emitted.
func TestHasWrites_TransitionOnly(t *testing.T) {
	m := NewMutator(&Snapshot{Pool: poolFixture{name: "p"}.build()})
	if m.HasWrites() {
		t.Fatal("precondition: no writes")
	}
	m.SetScaleDownTransition(ScaleDownTransition{Kind: ScaleDownCompleted})
	if !m.HasWrites() {
		t.Error("a transition-only cycle must report HasWrites so Commit emits its event")
	}
}

// The three scale-down lifecycle events round-trip through Commit with the
// expected recorder formatting.
func TestCommit_EmitsScaleDownLifecycleEvents(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "p"}.build()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()
	rec := &fakeEventRecorder{}

	m := NewMutator(&Snapshot{Pool: pool})
	m.SetScaleDownTransition(ScaleDownTransition{Kind: ScaleDownCompleted})
	m.EmitEvent(corev1.EventTypeNormal, "ScaleDown", "AutoscalerScaleDownCompleted",
		"completed scale-down of %s/%s from %d to %d (removed %d replicas in %s)",
		"ns", "p", 5, 0, 5, 3*time.Minute)

	if err := m.Commit(context.Background(), c, rec); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if got, want := len(rec.events), 1; got != want {
		t.Fatalf("recorded %d events, want %d", got, want)
	}
	want := "Normal AutoscalerScaleDownCompleted ScaleDown completed scale-down of ns/p from 5 to 0 (removed 5 replicas in 3m0s)"
	if rec.events[0] != want {
		t.Errorf("event = %q, want %q", rec.events[0], want)
	}
}
