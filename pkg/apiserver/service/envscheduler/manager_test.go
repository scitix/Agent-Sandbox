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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/schedule"
)

const localID = "local"

// --- fakes ------------------------------------------------------------------

// fakePools is a fake SchedulerLookup that hands out real PoolScheduler
// instances backed by a nil k8sClient. The schedulers' Run goroutine is
// never started, so Enqueue is the only meaningful operation.
type fakePools struct {
	schedulers map[string]*schedule.PoolScheduler
}

func newFakePools() *fakePools {
	return &fakePools{schedulers: make(map[string]*schedule.PoolScheduler)}
}

func (f *fakePools) GetScheduler(ns, poolName string) *schedule.PoolScheduler {
	return f.schedulers[ns+"/"+poolName]
}

func (f *fakePools) GetOrCreateScheduler(ns, poolName, _, _, _ string) *schedule.PoolScheduler {
	key := ns + "/" + poolName
	if s := f.schedulers[key]; s != nil {
		return s
	}
	// k8sClient nil — tests never start Run(), so the status writer never fires.
	s := schedule.NewPoolScheduler(ns, poolName, "", "", "", nil)
	f.schedulers[key] = s
	return s
}

// fakeEnvGetter returns Envs from an in-memory map.
type fakeEnvGetter struct {
	envs map[types.NamespacedName]*agentsv1alpha1.SandboxEnv
}

func (f *fakeEnvGetter) GetEnv(ns, name string) (*agentsv1alpha1.SandboxEnv, bool) {
	e, ok := f.envs[types.NamespacedName{Namespace: ns, Name: name}]
	return e, ok
}

// --- helpers ----------------------------------------------------------------

func makeEnv(name string, members ...agentsv1alpha1.EnvClusterMember) *agentsv1alpha1.SandboxEnv {
	return &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: name},
		Spec: agentsv1alpha1.SandboxEnvSpec{
			Clusters: []agentsv1alpha1.EnvClusterSpec{
				{ClusterID: localID, Members: members},
			},
		},
	}
}

func makeReq() *schedule.ClaimRequest {
	ch := make(chan schedule.ClaimResult, 1)
	return &schedule.ClaimRequest{ResultCh: ch}
}

// --- Resolve tests ----------------------------------------------------------

func TestResolve_BareNameHitsEnv(t *testing.T) {
	mgr := New(localID, newFakePools(), &fakeEnvGetter{})
	mgr.OnEnvUpsert(makeEnv("my-env", agentsv1alpha1.EnvClusterMember{Name: "my-env"}))

	r := mgr.Resolve("ns", "", "my-env")
	if r.Kind != ResolveEnv {
		t.Fatalf("Kind = %v, want ResolveEnv (%+v)", r.Kind, r)
	}
	if r.EnvKey.Name != "my-env" || r.EnvKey.Namespace != "ns" {
		t.Errorf("EnvKey = %+v", r.EnvKey)
	}
}

func TestResolve_BareNameMissingEnv_ReturnsNotFound(t *testing.T) {
	mgr := New(localID, newFakePools(), &fakeEnvGetter{})
	r := mgr.Resolve("ns", "", "nope")
	if r.Kind != ResolveNotFound {
		t.Errorf("Kind = %v, want ResolveNotFound", r.Kind)
	}
	if r.PoolName != "nope" {
		t.Errorf("PoolName carrying the requested name = %q, want %q", r.PoolName, "nope")
	}
}

func TestResolve_LocalExplicit_EnvNameHitsEnv(t *testing.T) {
	// "<localID>::<envName>" must go through Env member selection, not be taken
	// as a Pool name: this is the shape a cross-cluster create arrives in after
	// the origin forwarded it, so an Env reference has to survive the hop.
	mgr := New(localID, newFakePools(), &fakeEnvGetter{})
	mgr.OnEnvUpsert(makeEnv("my-env", agentsv1alpha1.EnvClusterMember{Name: "my-env-member"}))

	r := mgr.Resolve("ns", localID, "my-env")
	if r.Kind != ResolveEnv {
		t.Fatalf("Kind = %v, want ResolveEnv (%+v)", r.Kind, r)
	}
	if r.EnvKey.Name != "my-env" || r.EnvKey.Namespace != "ns" {
		t.Errorf("EnvKey = %+v", r.EnvKey)
	}
}

func TestResolve_LocalExplicit_UnknownEnvIsDirectPool(t *testing.T) {
	mgr := New(localID, newFakePools(), &fakeEnvGetter{})
	mgr.OnEnvUpsert(makeEnv("my-env", agentsv1alpha1.EnvClusterMember{Name: "my-env-member"}))

	// A member pool name is not an Env name, so it stays a direct Pool reference.
	r := mgr.Resolve("ns", localID, "my-env-member")
	if r.Kind != ResolveLocalPool {
		t.Errorf("Kind = %v, want ResolveLocalPool", r.Kind)
	}
	if r.PoolName != "my-env-member" {
		t.Errorf("PoolName = %q", r.PoolName)
	}
}

func TestResolve_CrossClusterExplicit(t *testing.T) {
	mgr := New(localID, newFakePools(), &fakeEnvGetter{})
	r := mgr.Resolve("ns", "cluster-b", "my-pool")
	if r.Kind != ResolveCrossCluster {
		t.Errorf("Kind = %v, want ResolveCrossCluster", r.Kind)
	}
	if r.ClusterID != "cluster-b" || r.PoolName != "my-pool" {
		t.Errorf("cluster/pool = %s/%s", r.ClusterID, r.PoolName)
	}
}

func TestResolve_EmptyTemplate(t *testing.T) {
	mgr := New(localID, newFakePools(), &fakeEnvGetter{})
	r := mgr.Resolve("ns", "", "")
	if r.Kind != ResolveNotFound {
		t.Errorf("empty template: Kind = %v, want ResolveNotFound", r.Kind)
	}
}

// --- Route single-member tests ---------------------------------------------

func TestRoute_SingleMember_HappyPath(t *testing.T) {
	pools := newFakePools()
	mgr := New(localID, pools, &fakeEnvGetter{})
	env := makeEnv("my-env", agentsv1alpha1.EnvClusterMember{Name: "my-env"})
	mgr.OnEnvUpsert(env)

	res := mgr.Route(context.Background(), types.NamespacedName{Namespace: "ns", Name: "my-env"}, makeReq())
	if res.Kind != RouteLocal {
		t.Fatalf("Kind = %v, want RouteLocal", res.Kind)
	}
	if res.Pool == nil {
		t.Fatal("Pool should be set")
	}
	if pools.GetScheduler("ns", "my-env") == nil {
		t.Errorf("expected scheduler to be created for ns/my-env")
	}
}

func TestRoute_NotFound_UnknownEnv(t *testing.T) {
	mgr := New(localID, newFakePools(), &fakeEnvGetter{})
	res := mgr.Route(context.Background(), types.NamespacedName{Namespace: "ns", Name: "ghost"}, makeReq())
	if res.Kind != RouteNotFound {
		t.Errorf("Kind = %v, want RouteNotFound", res.Kind)
	}
}

func TestRoute_NotFound_EnvWithNoLocalMembers(t *testing.T) {
	mgr := New(localID, newFakePools(), &fakeEnvGetter{})
	env := &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "env-only-remote"},
		Spec: agentsv1alpha1.SandboxEnvSpec{
			Clusters: []agentsv1alpha1.EnvClusterSpec{
				{ClusterID: "remote", Members: []agentsv1alpha1.EnvClusterMember{{Name: "p1"}}},
			},
		},
	}
	mgr.OnEnvUpsert(env)
	// The entry has 1 member but it's not local; single-member fast path skips,
	// routeMulti's pickLocalMembers filters everything out → RouteNotFound.
	res := mgr.Route(context.Background(), types.NamespacedName{Namespace: "ns", Name: "env-only-remote"}, makeReq())
	if res.Kind != RouteNotFound {
		t.Errorf("Kind = %v, want RouteNotFound for remote-only env", res.Kind)
	}
}

func TestRoute_SingleMember_QueueSaturated(t *testing.T) {
	// We don't want to fill reqCap=8192 in a unit test. Instead we monkey-patch
	// via direct PoolScheduler manipulation: build a manager whose schedulerLookup
	// returns a scheduler whose reqCh is already at capacity. Implemented by
	// re-creating the channel after constructing the scheduler — small trick
	// localised to this test.
	t.Skip("fast-path saturation needs a hook into reqCh capacity that we don't want to add for tests; covered by integration in scheduler stress tests")
}

func TestManager_OnEnvDelete(t *testing.T) {
	mgr := New(localID, newFakePools(), &fakeEnvGetter{})
	env := makeEnv("transient", agentsv1alpha1.EnvClusterMember{Name: "transient"})
	mgr.OnEnvUpsert(env)
	if r := mgr.Resolve("ns", "", "transient"); r.Kind != ResolveEnv {
		t.Fatalf("pre-delete: Kind = %v, want ResolveEnv", r.Kind)
	}
	mgr.OnEnvDelete(types.NamespacedName{Namespace: "ns", Name: "transient"})
	if r := mgr.Resolve("ns", "", "transient"); r.Kind != ResolveNotFound {
		t.Errorf("post-delete: Kind = %v, want ResolveNotFound", r.Kind)
	}
}

func TestManager_Snapshot(t *testing.T) {
	mgr := New(localID, newFakePools(), &fakeEnvGetter{})
	mgr.OnEnvUpsert(makeEnv("a", agentsv1alpha1.EnvClusterMember{Name: "a"}))
	mgr.OnEnvUpsert(makeEnv("b", agentsv1alpha1.EnvClusterMember{Name: "b"}))
	keys := mgr.Snapshot()
	if len(keys) != 2 {
		t.Errorf("expected 2 envs cached, got %d", len(keys))
	}
}

func TestOnEnvUpsert_NilIsNoop(t *testing.T) {
	mgr := New(localID, newFakePools(), &fakeEnvGetter{})
	mgr.OnEnvUpsert(nil)
	if got := mgr.Snapshot(); len(got) != 0 {
		t.Errorf("nil upsert should be a no-op, snapshot size = %d", len(got))
	}
}

// --- SelectPool scaling-group scoping ---------------------------------------

// memberInGroup builds a local member pool that belongs to the named
// autoscaling group.
func memberInGroup(name, group string) agentsv1alpha1.EnvClusterMember {
	return agentsv1alpha1.EnvClusterMember{
		Name:   name,
		Config: agentsv1alpha1.EnvClusterMemberConfig{ScalingGroup: group},
	}
}

func TestSelectPool_NoGroup_Unconstrained(t *testing.T) {
	mgr := New(localID, newFakePools(), &fakeEnvGetter{})
	mgr.OnEnvUpsert(makeEnv("env", memberInGroup("small", "1c2Gi"), memberInGroup("big", "2c4Gi")))
	got := mgr.SelectPool(types.NamespacedName{Namespace: "ns", Name: "env"}, "")
	// With no constraint either member is acceptable; the framework must pick one.
	if got != "small" && got != "big" {
		t.Errorf("unconstrained SelectPool = %q, want one of small/big", got)
	}
}

func TestSelectPool_GroupScoping_PicksInGroup(t *testing.T) {
	mgr := New(localID, newFakePools(), &fakeEnvGetter{})
	mgr.OnEnvUpsert(makeEnv("env",
		memberInGroup("small-a", "1c2Gi"),
		memberInGroup("small-b", "1c2Gi"),
		memberInGroup("big", "2c4Gi"),
	))
	got := mgr.SelectPool(types.NamespacedName{Namespace: "ns", Name: "env"}, "1c2Gi")
	if got != "small-a" && got != "small-b" {
		t.Errorf("SelectPool(1c2Gi) = %q, want an in-group pool (small-a/small-b), never big", got)
	}
}

func TestSelectPool_GroupScoping_NoMatchReturnsEmpty(t *testing.T) {
	mgr := New(localID, newFakePools(), &fakeEnvGetter{})
	mgr.OnEnvUpsert(makeEnv("env", memberInGroup("small", "1c2Gi"), memberInGroup("big", "2c4Gi")))
	if got := mgr.SelectPool(types.NamespacedName{Namespace: "ns", Name: "env"}, "8c16Gi"); got != "" {
		t.Errorf("SelectPool for absent group = %q, want \"\" (hard constraint, no fallback)", got)
	}
}

func TestSelectPool_GroupScoping_SingleMemberGuard(t *testing.T) {
	mgr := New(localID, newFakePools(), &fakeEnvGetter{})
	mgr.OnEnvUpsert(makeEnv("env", memberInGroup("only", "1c2Gi")))
	key := types.NamespacedName{Namespace: "ns", Name: "env"}
	if got := mgr.SelectPool(key, "1c2Gi"); got != "only" {
		t.Errorf("single member in requested group: SelectPool = %q, want only", got)
	}
	if got := mgr.SelectPool(key, "2c4Gi"); got != "" {
		t.Errorf("single member in a different group: SelectPool = %q, want \"\"", got)
	}
	if got := mgr.SelectPool(key, ""); got != "only" {
		t.Errorf("single member, no constraint: SelectPool = %q, want only", got)
	}
}
