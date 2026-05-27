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

package envscheduler

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/schedule"
)

// loadSchedulerN enqueues `n` placeholder requests into the scheduler so
// Snapshot reports QueueLen = n. The requests never get dispatched (no Run
// goroutine, no idle pods); they sit in reqCh until the test cleans up.
func loadSchedulerN(t *testing.T, s *schedule.PoolScheduler, n int) {
	t.Helper()
	for i := range n {
		ch := make(chan schedule.ClaimResult, 1)
		req := &schedule.ClaimRequest{
			Ctx:      context.Background(),
			Deadline: time.Now().Add(time.Hour),
			ResultCh: ch,
		}
		if !s.Enqueue(req) {
			t.Fatalf("Enqueue %d/%d failed unexpectedly", i+1, n)
		}
	}
}

func mkMember(name string, priority int32) agentsv1alpha1.EnvClusterMember {
	return agentsv1alpha1.EnvClusterMember{Name: name, Config: agentsv1alpha1.EnvClusterMemberConfig{Priority: priority}}
}

// TestRouteMulti_PrefersLowerPriority confirms that priority dominates the
// load signal: even when "low-pri" has no queued requests and "high-pri" is
// loaded, the lower-priority *number* (= higher routing preference) wins.
func TestRouteMulti_PrefersLowerPriority(t *testing.T) {
	pools := newFakePools()
	mgr := New(localID, pools, &fakeEnvGetter{})

	env := makeEnv("e",
		mkMember("pri-high", 100),
		mkMember("pri-low", 0),
	)
	mgr.OnEnvUpsert(env)

	// Pre-create both schedulers; load pri-low to be busier than pri-high to
	// prove priority overrides load.
	pri := pools.GetOrCreateScheduler("ns", "pri-low", "", "")
	loadSchedulerN(t, pri, 3)

	req := makeReq()
	res := mgr.Route(context.Background(), types.NamespacedName{Namespace: "ns", Name: "e"}, req)
	if res.Kind != RouteLocal {
		t.Fatalf("Kind = %v", res.Kind)
	}
	// req should have landed on pri-low (priority=0)
	if got := pools.GetScheduler("ns", "pri-low").Snapshot().QueueLen; got != 4 {
		t.Errorf("pri-low QueueLen = %d, want 4 (had 3 pre-load + 1 routed)", got)
	}
}

// TestRouteMulti_PrefersHigherIdle ranks within the same priority by queue
// state. Two members have priority=0; the one with a busier queue should be
// skipped. (We can't easily preload "IdleReady" without a running scheduler
// goroutine, so this case exercises the QueueLen tie-break specifically.)
func TestRouteMulti_BreaksTieByLowerQueue(t *testing.T) {
	pools := newFakePools()
	mgr := New(localID, pools, &fakeEnvGetter{})
	env := makeEnv("e",
		mkMember("a", 0),
		mkMember("b", 0),
	)
	mgr.OnEnvUpsert(env)

	// Pre-load "a" so "b" looks better by QueueLen.
	loadSchedulerN(t, pools.GetOrCreateScheduler("ns", "a", "", ""), 5)
	// "b" gets one preload too so both exist; but fewer than "a".
	loadSchedulerN(t, pools.GetOrCreateScheduler("ns", "b", "", ""), 1)

	req := makeReq()
	mgr.Route(context.Background(), types.NamespacedName{Namespace: "ns", Name: "e"}, req)

	// req should have landed on "b" (lower queue): QueueLen now 2.
	if got := pools.GetScheduler("ns", "b").Snapshot().QueueLen; got != 2 {
		t.Errorf("expected b.QueueLen = 2 (had 1 + 1 routed), got %d", got)
	}
	if got := pools.GetScheduler("ns", "a").Snapshot().QueueLen; got != 5 {
		t.Errorf("expected a.QueueLen unchanged at 5, got %d", got)
	}
}

// TestRouteMulti_SkipsSaturatedMember demonstrates the saturation cache: a
// member whose ObservedMember.SaturatedUntil is in the future is held back
// from the primary candidate list. With a non-saturated alternate, the
// alternate wins regardless of its higher priority number.
func TestRouteMulti_SkipsSaturatedUntilFresh(t *testing.T) {
	pools := newFakePools()
	getter := &fakeEnvGetter{envs: map[types.NamespacedName]*agentsv1alpha1.SandboxEnv{}}
	mgr := New(localID, pools, getter)

	env := makeEnv("e",
		mkMember("preferred", 0),
		mkMember("backup", 100),
	)
	// Mark "preferred" saturated for the next hour.
	until := metav1.NewTime(time.Now().Add(time.Hour))
	env.Status.Clusters = []agentsv1alpha1.EnvClusterStatus{
		{
			ClusterID: localID,
			ObservedMembers: []agentsv1alpha1.EnvObservedMember{
				{Name: "preferred", SaturatedUntil: &until},
			},
		},
	}
	mgr.OnEnvUpsert(env)
	getter.envs[types.NamespacedName{Namespace: "ns", Name: "e"}] = env

	req := makeReq()
	mgr.Route(context.Background(), types.NamespacedName{Namespace: "ns", Name: "e"}, req)

	// The req must have gone to backup, not preferred. Routing won't even
	// create a scheduler for preferred (it stays in the stale tier and we
	// never fell through to it).
	if got := pools.GetScheduler("ns", "preferred"); got != nil && got.Snapshot().QueueLen != 0 {
		t.Errorf("preferred should not have received the request, QueueLen = %d", got.Snapshot().QueueLen)
	}
	if got := pools.GetScheduler("ns", "backup").Snapshot().QueueLen; got != 1 {
		t.Errorf("backup QueueLen = %d, want 1", got)
	}
}

// TestRouteMulti_AllSaturatedFallback: when every member is saturated, the
// router still tries them rather than failing — better an over-loaded Pool
// than 503.
func TestRouteMulti_AllSaturatedFallback(t *testing.T) {
	pools := newFakePools()
	getter := &fakeEnvGetter{envs: map[types.NamespacedName]*agentsv1alpha1.SandboxEnv{}}
	mgr := New(localID, pools, getter)

	env := makeEnv("e",
		mkMember("a", 0),
		mkMember("b", 100),
	)
	until := metav1.NewTime(time.Now().Add(time.Hour))
	env.Status.Clusters = []agentsv1alpha1.EnvClusterStatus{
		{
			ClusterID: localID,
			ObservedMembers: []agentsv1alpha1.EnvObservedMember{
				{Name: "a", SaturatedUntil: &until},
				{Name: "b", SaturatedUntil: &until},
			},
		},
	}
	mgr.OnEnvUpsert(env)
	getter.envs[types.NamespacedName{Namespace: "ns", Name: "e"}] = env

	req := makeReq()
	res := mgr.Route(context.Background(), types.NamespacedName{Namespace: "ns", Name: "e"}, req)
	if res.Kind != RouteLocal {
		t.Errorf("all-saturated should still route as last-ditch effort, got %v", res.Kind)
	}
	// Lower priority wins among saturated — "a" (priority 0) should take it.
	if got := pools.GetScheduler("ns", "a").Snapshot().QueueLen; got != 1 {
		t.Errorf("a QueueLen = %d, want 1 (saturated fallback)", got)
	}
}

// TestRouteMulti_ExpiredSaturationIsIgnored: SaturatedUntil in the past
// must NOT exclude the member from the fresh tier.
func TestRouteMulti_ExpiredSaturationIsIgnored(t *testing.T) {
	pools := newFakePools()
	getter := &fakeEnvGetter{envs: map[types.NamespacedName]*agentsv1alpha1.SandboxEnv{}}
	mgr := New(localID, pools, getter)

	env := makeEnv("e",
		mkMember("a", 0),
		mkMember("b", 100),
	)
	past := metav1.NewTime(time.Now().Add(-time.Hour))
	env.Status.Clusters = []agentsv1alpha1.EnvClusterStatus{
		{
			ClusterID: localID,
			ObservedMembers: []agentsv1alpha1.EnvObservedMember{
				{Name: "a", SaturatedUntil: &past},
			},
		},
	}
	mgr.OnEnvUpsert(env)
	getter.envs[types.NamespacedName{Namespace: "ns", Name: "e"}] = env

	req := makeReq()
	mgr.Route(context.Background(), types.NamespacedName{Namespace: "ns", Name: "e"}, req)
	if got := pools.GetScheduler("ns", "a").Snapshot().QueueLen; got != 1 {
		t.Errorf("a QueueLen = %d, want 1 (expired saturation should not exclude)", got)
	}
}

// TestRouteMulti_MaxedOutMemberSkippedInFavourOfGrowable proves the
// MaxedOutFilter is wired into the routing path: a Pool that's at its
// MaxReplicas with no idle is bypassed in favour of a same-priority
// sibling that can still grow. Reproduces the user-reported scenario
// where one Pool sat saturated and the other still had headroom.
func TestRouteMulti_MaxedOutMemberSkippedInFavourOfGrowable(t *testing.T) {
	pools := newFakePools()
	getter := &fakeEnvGetter{envs: map[types.NamespacedName]*agentsv1alpha1.SandboxEnv{}}
	mgr := New(localID, pools, getter)

	maxed := mkMember("maxed", 0)
	maxed.Config.MaxReplicas = ptrInt32(2)
	growable := mkMember("growable", 0)
	growable.Config.MaxReplicas = ptrInt32(10)

	env := makeEnv("e", maxed, growable)
	// Reflect "maxed" sitting at its cap via Env.Status.
	env.Status.Clusters = []agentsv1alpha1.EnvClusterStatus{
		{
			ClusterID: localID,
			ObservedMembers: []agentsv1alpha1.EnvObservedMember{
				{Name: "maxed", DesiredReplicas: 2},
				{Name: "growable", DesiredReplicas: 2},
			},
		},
	}
	mgr.OnEnvUpsert(env)
	getter.envs[types.NamespacedName{Namespace: "ns", Name: "e"}] = env

	mgr.Route(context.Background(), types.NamespacedName{Namespace: "ns", Name: "e"}, makeReq())
	if got := pools.GetScheduler("ns", "growable").Snapshot().QueueLen; got != 1 {
		t.Errorf("growable QueueLen = %d, want 1 (maxed should have been filtered)", got)
	}
	if got := pools.GetScheduler("ns", "maxed"); got != nil && got.Snapshot().QueueLen != 0 {
		t.Errorf("maxed should not have received the request, QueueLen = %d", got.Snapshot().QueueLen)
	}
}

func ptrInt32(v int32) *int32 { return &v }

// TestRouteMulti_DeterministicTiebreakerByName: two members tied on every
// rank dimension are split by lexicographic name order to avoid flakiness.
func TestRouteMulti_DeterministicTiebreakerByName(t *testing.T) {
	pools := newFakePools()
	mgr := New(localID, pools, &fakeEnvGetter{})
	env := makeEnv("e",
		mkMember("zzz", 0),
		mkMember("aaa", 0),
	)
	mgr.OnEnvUpsert(env)

	req := makeReq()
	mgr.Route(context.Background(), types.NamespacedName{Namespace: "ns", Name: "e"}, req)

	if got := pools.GetScheduler("ns", "aaa").Snapshot().QueueLen; got != 1 {
		t.Errorf("aaa QueueLen = %d, want 1 (alphabetic tie-break)", got)
	}
	if got := pools.GetScheduler("ns", "zzz"); got != nil && got.Snapshot().QueueLen != 0 {
		t.Errorf("zzz should not have received the request")
	}
}
