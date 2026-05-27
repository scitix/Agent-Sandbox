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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/schedule"
)

// ---------- fixtures ----------

const (
	testNS      = "team-a"
	testEnvName = "env-1"
	testGroup   = "sci.c22-2"
)

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("corev1 scheme: %v", err)
	}
	if err := agentsv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("agentsv1alpha1 scheme: %v", err)
	}
	return s
}

// poolFixture builds a SandboxPool that is associated with the test Env
// via the LabelEnv label. Caller can override name + replicas + idle.
type poolFixture struct {
	name     string
	replicas int32
	idle     int32
	envLabel string // when empty, defaults to testEnvName; "-" disables labelling
}

func (f poolFixture) build() *agentsv1alpha1.SandboxPool {
	envLabel := f.envLabel
	if envLabel == "" {
		envLabel = testEnvName
	}
	labels := map[string]string{}
	if envLabel != "-" {
		labels[agentsv1alpha1.LabelEnv] = envLabel
	}
	return &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:       f.name,
			Namespace:  testNS,
			Labels:     labels,
			Generation: 1,
		},
		Spec: agentsv1alpha1.SandboxPoolSpec{Replicas: f.replicas},
		Status: agentsv1alpha1.SandboxPoolStatus{
			IdleReplicas: f.idle,
		},
	}
}

// envFixture builds a SandboxEnv whose single local cluster carries the
// given members. Each member references the shared test group.
type envFixture struct {
	members []agentsv1alpha1.EnvClusterMember
	groups  []agentsv1alpha1.EnvAutoscalingGroup
}

func (f envFixture) build() *agentsv1alpha1.SandboxEnv {
	env := &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{Name: testEnvName, Namespace: testNS},
		Spec: agentsv1alpha1.SandboxEnvSpec{
			Clusters: []agentsv1alpha1.EnvClusterSpec{{
				ClusterID: "local",
				Members:   f.members,
			}},
		},
	}
	if len(f.groups) > 0 {
		env.Spec.Autoscaling = &agentsv1alpha1.EnvAutoscalingSpec{Groups: f.groups}
	}
	return env
}

func makeMember(name string) agentsv1alpha1.EnvClusterMember {
	return agentsv1alpha1.EnvClusterMember{
		Name:   name,
		Config: agentsv1alpha1.EnvClusterMemberConfig{ScalingGroup: testGroup},
	}
}

func makeGroup(enabled bool) agentsv1alpha1.EnvAutoscalingGroup {
	return agentsv1alpha1.EnvAutoscalingGroup{
		Name:    testGroup,
		Enabled: enabled,
	}
}

// fakeTracker is an in-memory LastCreateTracker double.
type fakeTracker struct{ data map[string]time.Time }

func (f *fakeTracker) Get(ns, name string) (time.Time, bool) {
	t, ok := f.data[ns+"/"+name]
	return t, ok
}

// nilSchedulers satisfies SchedulerLookup with always-nil. Used when a
// test asserts the "no scheduler registered" branch.
type nilSchedulers struct{}

func (nilSchedulers) GetScheduler(string, string) *schedule.PoolScheduler { return nil }

// staticClock implements Clock with a fixed wall-clock.
type staticClock struct{ t time.Time }

func (s staticClock) Now() time.Time { return s.t }

// ---------- helper tests ----------

func TestResolveEnvName(t *testing.T) {
	cases := []struct {
		name     string
		pool     *agentsv1alpha1.SandboxPool
		wantName string
		wantOK   bool
	}{
		{
			name: "label_present",
			pool: &agentsv1alpha1.SandboxPool{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{agentsv1alpha1.LabelEnv: "env-x"},
				},
			},
			wantName: "env-x",
			wantOK:   true,
		},
		{
			name: "owner_ref_only",
			pool: &agentsv1alpha1.SandboxPool{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						{Kind: "SandboxEnv", Name: "env-owner"},
					},
				},
			},
			wantName: "env-owner",
			wantOK:   true,
		},
		{
			name: "label_wins_over_owner_ref",
			pool: &agentsv1alpha1.SandboxPool{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{agentsv1alpha1.LabelEnv: "from-label"},
					OwnerReferences: []metav1.OwnerReference{
						{Kind: "SandboxEnv", Name: "from-owner"},
					},
				},
			},
			wantName: "from-label",
			wantOK:   true,
		},
		{
			name:     "neither_present",
			pool:     &agentsv1alpha1.SandboxPool{ObjectMeta: metav1.ObjectMeta{Name: "orphan"}},
			wantName: "",
			wantOK:   false,
		},
		{
			name: "owner_ref_not_env_kind",
			pool: &agentsv1alpha1.SandboxPool{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						{Kind: "Deployment", Name: "irrelevant"},
					},
				},
			},
			wantName: "",
			wantOK:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotName, gotOK := resolveEnvName(tc.pool)
			if gotName != tc.wantName || gotOK != tc.wantOK {
				t.Errorf("resolveEnvName = (%q, %v), want (%q, %v)", gotName, gotOK, tc.wantName, tc.wantOK)
			}
		})
	}
}

func TestResolveLastCreate(t *testing.T) {
	now := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	older := now.Add(-time.Hour)

	cases := []struct {
		name      string
		annValue  string
		tracker   LastCreateTracker
		wantNil   bool
		wantValue time.Time
	}{
		{
			name:    "neither_source",
			wantNil: true,
		},
		{
			name:      "annotation_only",
			annValue:  older.Format(time.RFC3339),
			wantValue: older,
		},
		{
			name:      "tracker_wins_over_annotation",
			annValue:  older.Format(time.RFC3339),
			tracker:   &fakeTracker{data: map[string]time.Time{testNS + "/p": now}},
			wantValue: now,
		},
		{
			name:    "tracker_present_but_no_entry_falls_back_to_annotation",
			tracker: &fakeTracker{data: map[string]time.Time{}},
			// Annotation present.
			annValue:  older.Format(time.RFC3339),
			wantValue: older,
		},
		{
			name:     "malformed_annotation_returns_nil",
			annValue: "not-a-time",
			wantNil:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := &agentsv1alpha1.SandboxPool{ObjectMeta: metav1.ObjectMeta{Namespace: testNS, Name: "p"}}
			if tc.annValue != "" {
				pool.Annotations = map[string]string{agentsv1alpha1.LastSandboxCreateTimeAnnotationKey: tc.annValue}
			}
			got := resolveLastCreate(pool, tc.tracker)
			switch {
			case tc.wantNil && got != nil:
				t.Errorf("expected nil, got %v", got)
			case !tc.wantNil && got == nil:
				t.Errorf("expected non-nil, got nil")
			case !tc.wantNil && !got.Equal(tc.wantValue):
				t.Errorf("got %v, want %v", got, tc.wantValue)
			}
		})
	}
}

func TestSnapshotHelpers(t *testing.T) {
	enabled := makeGroup(true)
	disabled := makeGroup(false)

	t.Run("IsAutoscalingEnabled_nil_group", func(t *testing.T) {
		s := &Snapshot{}
		if s.IsAutoscalingEnabled() {
			t.Error("expected false when Group is nil")
		}
	})
	t.Run("IsAutoscalingEnabled_disabled_group", func(t *testing.T) {
		s := &Snapshot{Group: &disabled}
		if s.IsAutoscalingEnabled() {
			t.Error("expected false when Group.Enabled is false")
		}
	})
	t.Run("IsAutoscalingEnabled_enabled_group", func(t *testing.T) {
		s := &Snapshot{Group: &enabled}
		if !s.IsAutoscalingEnabled() {
			t.Error("expected true when Group.Enabled is true")
		}
	})

	t.Run("GroupDesiredTotal_and_GroupIdleTotal", func(t *testing.T) {
		s := &Snapshot{
			SiblingPools: []*agentsv1alpha1.SandboxPool{
				poolFixture{name: "a", replicas: 2, idle: 1}.build(),
				poolFixture{name: "b", replicas: 3, idle: 2}.build(),
			},
		}
		if got := s.GroupDesiredTotal(); got != 5 {
			t.Errorf("GroupDesiredTotal = %d, want 5", got)
		}
		if got := s.GroupIdleTotal(); got != 3 {
			t.Errorf("GroupIdleTotal = %d, want 3", got)
		}
	})

	t.Run("IsReactiveDemand", func(t *testing.T) {
		// No scheduler snap → never reactive.
		s0 := &Snapshot{}
		if s0.IsReactiveDemand() {
			t.Error("expected false with nil PoolSchedSnap")
		}
		// Queue len 0 → not reactive even with no idle.
		s1 := &Snapshot{PoolSchedSnap: &schedule.Snapshot{QueueLen: 0, IdleReady: 0}}
		if s1.IsReactiveDemand() {
			t.Error("expected false when QueueLen == 0")
		}
		// Queue len 1 but idle available → not reactive.
		s2 := &Snapshot{PoolSchedSnap: &schedule.Snapshot{QueueLen: 1, IdleReady: 2}}
		if s2.IsReactiveDemand() {
			t.Error("expected false when IdleReady > 0")
		}
		// Queue len > 0 and idle == 0 → reactive.
		s3 := &Snapshot{PoolSchedSnap: &schedule.Snapshot{QueueLen: 1, IdleReady: 0}}
		if !s3.IsReactiveDemand() {
			t.Error("expected true when QueueLen > 0 and IdleReady == 0")
		}
	})

	t.Run("OldestIdleAge", func(t *testing.T) {
		_, ok := (&Snapshot{}).OldestIdleAge()
		if ok {
			t.Error("expected false on empty IdlePodAges")
		}
		s := &Snapshot{IdlePodAges: []time.Duration{30 * time.Minute, 5 * time.Minute}}
		age, ok := s.OldestIdleAge()
		if !ok || age != 30*time.Minute {
			t.Errorf("got (%v, %v), want (30m, true)", age, ok)
		}
	})
}

// ---------- Loader.Load happy path ----------

func TestLoad_HappyPath(t *testing.T) {
	scheme := newTestScheme(t)
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)

	// Two sibling Pools in the same group, plus one in a different group
	// to confirm filtering. Self is "p-a".
	self := poolFixture{name: "p-a", replicas: 1, idle: 1}.build()
	sibling := poolFixture{name: "p-b", replicas: 2, idle: 0}.build()
	other := poolFixture{name: "p-c", replicas: 1, idle: 1}.build()
	// Annotate self with a last-create timestamp 10m ago to exercise the
	// annotation fallback path.
	self.Annotations = map[string]string{
		agentsv1alpha1.LastSandboxCreateTimeAnnotationKey: now.Add(-10 * time.Minute).Format(time.RFC3339),
	}

	// Idle pod for self created 20m ago — should appear in IdlePodAges.
	idlePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "p-a-idle-0",
			Namespace: testNS,
			Labels: map[string]string{
				agentsv1alpha1.SandboxPoolLabelKey:  "p-a",
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseIdle,
			},
			CreationTimestamp: metav1.NewTime(now.Add(-20 * time.Minute)),
		},
	}

	env := envFixture{
		members: []agentsv1alpha1.EnvClusterMember{
			makeMember("p-a"),
			makeMember("p-b"),
			{
				Name:   "p-c",
				Config: agentsv1alpha1.EnvClusterMemberConfig{ScalingGroup: "different-group"},
			},
		},
		groups: []agentsv1alpha1.EnvAutoscalingGroup{makeGroup(true)},
	}.build()

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(env, self, sibling, other, idlePod).
		Build()

	loader := &Loader{
		Client:     c,
		Schedulers: nilSchedulers{},
		LastCreate: &fakeTracker{},
		Clock:      staticClock{t: now},
	}

	snap, err := loader.Load(context.Background(), self)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if snap.Env == nil || snap.Env.Name != testEnvName {
		t.Fatalf("expected Env loaded, got %+v", snap.Env)
	}
	if snap.MemberConfig == nil || snap.MemberConfig.ScalingGroup != testGroup {
		t.Fatalf("expected MemberConfig.ScalingGroup=%q, got %+v", testGroup, snap.MemberConfig)
	}
	if snap.Group == nil || snap.Group.Name != testGroup {
		t.Fatalf("expected Group loaded, got %+v", snap.Group)
	}
	if !snap.IsAutoscalingEnabled() {
		t.Fatal("expected IsAutoscalingEnabled true")
	}
	if got := len(snap.SiblingPools); got != 2 {
		t.Fatalf("expected 2 sibling pools (p-a, p-b), got %d: %v", got, poolNames(snap.SiblingPools))
	}
	if snap.SiblingPools[0].Name != "p-a" || snap.SiblingPools[1].Name != "p-b" {
		t.Fatalf("expected sorted [p-a, p-b], got %v", poolNames(snap.SiblingPools))
	}
	// Self must be the caller's pointer (so callers see their pre-edit version).
	if snap.SiblingPools[0] != self {
		t.Error("expected SiblingPools[0] to be the caller's *self pointer")
	}
	if snap.LastCreateAt == nil || !snap.LastCreateAt.Equal(now.Add(-10*time.Minute)) {
		t.Fatalf("expected LastCreateAt 10m ago, got %v", snap.LastCreateAt)
	}
	if len(snap.IdlePodAges) != 1 {
		t.Fatalf("expected 1 idle pod age, got %d", len(snap.IdlePodAges))
	}
	if snap.IdlePodAges[0] != 20*time.Minute {
		t.Errorf("expected age 20m, got %v", snap.IdlePodAges[0])
	}
	if snap.GroupDesiredTotal() != 3 {
		t.Errorf("GroupDesiredTotal = %d, want 3 (1+2)", snap.GroupDesiredTotal())
	}
}

// ---------- Loader.Load edge cases ----------

func TestLoad_NoEnvLabel_NoOwnerRef(t *testing.T) {
	scheme := newTestScheme(t)
	self := poolFixture{name: "orphan", envLabel: "-"}.build()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(self).Build()

	snap, err := (&Loader{Client: c}).Load(context.Background(), self)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if snap.Env != nil || snap.Group != nil || snap.MemberConfig != nil {
		t.Errorf("expected Env/Group/MemberConfig all nil for orphan pool, got %+v", snap)
	}
	if snap.IsAutoscalingEnabled() {
		t.Error("expected IsAutoscalingEnabled false for orphan")
	}
}

func TestLoad_EnvNotFound_SoftMiss(t *testing.T) {
	scheme := newTestScheme(t)
	self := poolFixture{name: "p-a"}.build()
	// Note: env is NOT seeded.
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(self).Build()

	snap, err := (&Loader{Client: c}).Load(context.Background(), self)
	if err != nil {
		t.Fatalf("Load returned error on NotFound; want nil: %v", err)
	}
	if snap.Env != nil {
		t.Error("expected Env nil when env not found")
	}
}

func TestLoad_EnvFound_NoMatchingMember(t *testing.T) {
	scheme := newTestScheme(t)
	self := poolFixture{name: "p-z"}.build() // not in env members
	env := envFixture{
		members: []agentsv1alpha1.EnvClusterMember{makeMember("p-a")},
		groups:  []agentsv1alpha1.EnvAutoscalingGroup{makeGroup(true)},
	}.build()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(env, self).Build()

	snap, err := (&Loader{Client: c}).Load(context.Background(), self)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if snap.Env == nil {
		t.Fatal("expected Env loaded")
	}
	if snap.MemberConfig != nil {
		t.Error("expected MemberConfig nil when self not in member list")
	}
	if snap.Group != nil {
		t.Error("expected Group nil when MemberConfig nil")
	}
}

func TestLoad_GroupNotConfigured(t *testing.T) {
	scheme := newTestScheme(t)
	self := poolFixture{name: "p-a"}.build()
	env := envFixture{
		members: []agentsv1alpha1.EnvClusterMember{makeMember("p-a")},
		// No autoscaling.groups configured.
	}.build()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(env, self).Build()

	snap, err := (&Loader{Client: c}).Load(context.Background(), self)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if snap.MemberConfig == nil || snap.MemberConfig.ScalingGroup != testGroup {
		t.Errorf("expected MemberConfig loaded, got %+v", snap.MemberConfig)
	}
	if snap.Group != nil {
		t.Error("expected Group nil when autoscaling.groups is empty")
	}
	if snap.IsAutoscalingEnabled() {
		t.Error("expected IsAutoscalingEnabled false")
	}
}

func TestLoad_DisabledGroup_SkipsIdlePodList(t *testing.T) {
	scheme := newTestScheme(t)
	self := poolFixture{name: "p-a"}.build()
	idlePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "p-a-idle-0",
			Namespace: testNS,
			Labels: map[string]string{
				agentsv1alpha1.SandboxPoolLabelKey:  "p-a",
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseIdle,
			},
			CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour)),
		},
	}
	env := envFixture{
		members: []agentsv1alpha1.EnvClusterMember{makeMember("p-a")},
		groups:  []agentsv1alpha1.EnvAutoscalingGroup{makeGroup(false)},
	}.build()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(env, self, idlePod).Build()

	snap, err := (&Loader{Client: c}).Load(context.Background(), self)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if snap.Group == nil {
		t.Fatal("expected Group loaded")
	}
	if snap.IsAutoscalingEnabled() {
		t.Fatal("expected disabled")
	}
	if len(snap.IdlePodAges) != 0 {
		t.Errorf("expected no idle pod ages when group disabled, got %d", len(snap.IdlePodAges))
	}
}

func TestLoad_RejectsNilInputs(t *testing.T) {
	scheme := newTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	if _, err := (*Loader)(nil).Load(context.Background(), &agentsv1alpha1.SandboxPool{}); err == nil {
		t.Error("expected error from nil Loader")
	}
	if _, err := (&Loader{Client: c}).Load(context.Background(), nil); err == nil {
		t.Error("expected error from nil Pool")
	}
	if _, err := (&Loader{}).Load(context.Background(), &agentsv1alpha1.SandboxPool{}); err == nil {
		t.Error("expected error from Loader with nil Client")
	}
}

// ---------- helpers ----------

func poolNames(pools []*agentsv1alpha1.SandboxPool) []string {
	out := make([]string, len(pools))
	for i, p := range pools {
		out[i] = p.Name
	}
	return out
}

// Silence unused-import lint when only one branch references client pkg.
var _ = client.IgnoreNotFound
