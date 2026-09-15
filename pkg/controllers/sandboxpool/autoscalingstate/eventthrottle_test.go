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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

var throttleKey = types.NamespacedName{Namespace: testNS, Name: "p"}

func TestEventThrottle_SuppressesIdenticalRepeat(t *testing.T) {
	th := NewEventThrottle()
	now := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)

	if !th.Allow(throttleKey, "AutoscalerScaleUp", "increased replicas from 1 to 6", now) {
		t.Fatal("first event must be allowed")
	}
	// The autoscaler re-decides the same growth one cooldown window later.
	if th.Allow(throttleKey, "AutoscalerScaleUp", "increased replicas from 1 to 6", now.Add(30*time.Second)) {
		t.Error("identical repeat inside the window must be suppressed")
	}
	if th.Allow(throttleKey, "AutoscalerScaleUp", "increased replicas from 1 to 6", now.Add(repeatEventInterval-time.Second)) {
		t.Error("identical repeat just before the window closes must be suppressed")
	}
	// Past the window a stuck Pool reports again, so it stays visible.
	if !th.Allow(throttleKey, "AutoscalerScaleUp", "increased replicas from 1 to 6", now.Add(repeatEventInterval+time.Second)) {
		t.Error("identical repeat after the window must be allowed")
	}
}

// Progress must never be throttled: a Pool that is actually moving reports
// every step at full fidelity.
func TestEventThrottle_ChangedMessagePassesImmediately(t *testing.T) {
	th := NewEventThrottle()
	now := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)

	if !th.Allow(throttleKey, "AutoscalerScaleUp", "increased replicas from 1 to 6", now) {
		t.Fatal("first event must be allowed")
	}
	if !th.Allow(throttleKey, "AutoscalerScaleUp", "increased replicas from 6 to 9", now.Add(time.Second)) {
		t.Error("a different message is news and must pass immediately")
	}
	// ...and the new message becomes the one being suppressed.
	if th.Allow(throttleKey, "AutoscalerScaleUp", "increased replicas from 6 to 9", now.Add(2*time.Second)) {
		t.Error("the new message should now be the throttled one")
	}
}

func TestEventThrottle_ReasonsAreIndependent(t *testing.T) {
	th := NewEventThrottle()
	now := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)

	if !th.Allow(throttleKey, "ScalingUp", "same text", now) {
		t.Fatal("first event must be allowed")
	}
	if !th.Allow(throttleKey, "PoolRecovered", "same text", now) {
		t.Error("a different reason must not be suppressed by another reason's window")
	}
}

func TestEventThrottle_PoolsAreIndependent(t *testing.T) {
	th := NewEventThrottle()
	now := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)
	other := types.NamespacedName{Namespace: testNS, Name: "q"}

	if !th.Allow(throttleKey, "AutoscalerScaleUp", "msg", now) {
		t.Fatal("first event must be allowed")
	}
	if !th.Allow(other, "AutoscalerScaleUp", "msg", now) {
		t.Error("one Pool's window must not suppress another Pool's event")
	}
}

// A Pool recreated under the same name starts clean rather than inheriting the
// previous incarnation's suppression window.
func TestEventThrottle_ForgetClearsState(t *testing.T) {
	th := NewEventThrottle()
	now := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)

	th.Allow(throttleKey, "AutoscalerScaleUp", "msg", now)
	if th.Allow(throttleKey, "AutoscalerScaleUp", "msg", now.Add(time.Second)) {
		t.Fatal("precondition: repeat should be suppressed")
	}
	th.Forget(throttleKey)
	if !th.Allow(throttleKey, "AutoscalerScaleUp", "msg", now.Add(2*time.Second)) {
		t.Error("after Forget the next event must be allowed")
	}
}

// Entries for Pools that went quiet must not accumulate forever.
func TestEventThrottle_EvictsIdleEntries(t *testing.T) {
	th := NewEventThrottle()
	now := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)

	th.Allow(throttleKey, "AutoscalerScaleUp", "msg", now)
	// A later call for an unrelated Pool drives the sweep.
	th.Allow(types.NamespacedName{Namespace: testNS, Name: "other"}, "r", "m", now.Add(throttleEntryTTL+time.Minute))
	if _, ok := th.items[throttleKey]; ok {
		t.Error("entry untouched for longer than the TTL should have been evicted")
	}
}

// A nil throttle is the "not wired" case and must allow everything.
func TestEventThrottle_NilAllowsEverything(t *testing.T) {
	var th *EventThrottle
	now := time.Now()
	for i := range 3 {
		if !th.Allow(throttleKey, "r", "m", now.Add(time.Duration(i)*time.Second)) {
			t.Errorf("nil throttle must allow every event (call %d was suppressed)", i+1)
		}
	}
	th.Forget(throttleKey) // must not panic
}

// The throttle governs only whether an Event is posted. Suppressing the event
// must never suppress the replica write the event describes — otherwise
// rate-limiting the noise would also rate-limit the scaling.
func TestCommit_EventThrottle_SuppressesEventNotSpecWrite(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "p", replicas: 1}.build()
	env := envFixture{
		members: []agentsv1alpha1.EnvClusterMember{{
			Name: pool.Name,
			Spec: agentsv1alpha1.SandboxPoolSpec{Replicas: 1},
		}},
	}.build()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, env).Build()

	th := NewEventThrottle()
	now := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)
	rec := &fakeEventRecorder{}
	commit := func(target int32, at time.Time) {
		t.Helper()
		m := NewMutator(&Snapshot{Pool: pool, Env: env, Now: at, EventThrottle: th})
		m.SetTargetReplicas(target)
		m.EmitEvent(corev1.EventTypeNormal, "ScaleUp", "AutoscalerScaleUp", "increased replicas to %d", target)
		if err := m.Commit(context.Background(), c, rec); err != nil {
			t.Fatalf("Commit: %v", err)
		}
	}

	commit(6, now)
	commit(6, now.Add(30*time.Second)) // identical decision, one cooldown later

	if got, want := len(rec.events), 1; got != want {
		t.Errorf("recorded %d events, want %d (the repeat must be suppressed)", got, want)
	}
	got := &agentsv1alpha1.SandboxEnv{}
	if err := c.Get(context.Background(),
		client.ObjectKey{Namespace: env.Namespace, Name: env.Name}, got); err != nil {
		t.Fatalf("get env: %v", err)
	}
	if r := got.Spec.Clusters[0].Members[0].Spec.Replicas; r != 6 {
		t.Errorf("Member.Spec.Replicas = %d, want 6 — the spec write must survive event suppression", r)
	}
}
