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

package sandboxenv

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
)

// TestComputeScaleUpDelta_AcrossModes exercises the group-aware delta math.
// The aggregate-mode semantics are critical to multi-member Envs: "Default"
// growth means the group as a whole adds half its size, not each Pool.
func TestComputeScaleUpDelta_AcrossModes(t *testing.T) {
	tests := []struct {
		name      string
		aggregate int32
		mode      agentsv1alpha1.PoolScaleUpMode
		maxR      int32
		want      int32
	}{
		{"conservative from zero", 0, agentsv1alpha1.PoolScaleUpModeConservative, 0, 1},
		{"conservative from 10", 10, agentsv1alpha1.PoolScaleUpModeConservative, 0, 1},
		{"conservative bounded by maxR", 10, agentsv1alpha1.PoolScaleUpModeConservative, 10, 0},
		{"default from zero", 0, agentsv1alpha1.PoolScaleUpModeDefault, 0, 1},
		{"default from 10 (+max(1,ceil(5)))=15", 10, agentsv1alpha1.PoolScaleUpModeDefault, 0, 5},
		{"default capped to maxR", 10, agentsv1alpha1.PoolScaleUpModeDefault, 12, 2},
		{"aggressive from zero", 0, agentsv1alpha1.PoolScaleUpModeAggressive, 0, 1},
		{"aggressive doubling", 8, agentsv1alpha1.PoolScaleUpModeAggressive, 0, 8},
		{"aggressive capped", 8, agentsv1alpha1.PoolScaleUpModeAggressive, 10, 2},
		{"empty mode falls back to Default", 4, "", 0, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			group := &agentsv1alpha1.EnvAutoscalingGroup{
				ScaleUpPolicy: &agentsv1alpha1.PoolScaleUpPolicy{Mode: tt.mode},
			}
			if got := computeScaleUpDelta(tt.aggregate, group, tt.maxR); got != tt.want {
				t.Errorf("computeScaleUpDelta(%d, %s, max=%d) = %d, want %d",
					tt.aggregate, tt.mode, tt.maxR, got, tt.want)
			}
		})
	}
}

// TestComputeScaleUpDelta_NilGroup confirms the helper is defensive when
// the Env has autoscaling enabled but no group-specific policy.
func TestComputeScaleUpDelta_NilGroup(t *testing.T) {
	if got := computeScaleUpDelta(10, nil, 0); got != 5 {
		t.Errorf("nil group: got %d, want default-mode 5", got)
	}
}

// TestGroupViewsByScalingGroup verifies members are partitioned by
// Config.ScalingGroup and that empty-group members are excluded — the
// per-group autoscaler relies on this routing.
func TestGroupViewsByScalingGroup(t *testing.T) {
	mk := func(name, sg string) memberView {
		return memberView{
			member: agentsv1alpha1.EnvClusterMember{
				Name:   name,
				Config: agentsv1alpha1.EnvClusterMemberConfig{ScalingGroup: sg},
			},
		}
	}
	views := []memberView{
		mk("a", "1c4Gi"),
		mk("b", "1c4Gi"),
		mk("c", "2c8Gi"),
		mk("d", ""), // not in any group → dropped
	}
	got := groupViewsByScalingGroup(views)
	if len(got["1c4Gi"]) != 2 {
		t.Errorf("group 1c4Gi: want 2 members, got %d", len(got["1c4Gi"]))
	}
	if len(got["2c8Gi"]) != 1 {
		t.Errorf("group 2c8Gi: want 1 member, got %d", len(got["2c8Gi"]))
	}
	if _, ok := got[""]; ok {
		t.Errorf("empty-group bucket must not be populated")
	}
}

// TestScaleUpPolicyOrDefault_ReadsSaturationCooldown verifies the new
// SaturationCooldownSeconds knob plumbs through.
func TestScaleUpPolicyOrDefault_ReadsSaturationCooldown(t *testing.T) {
	g := &agentsv1alpha1.EnvAutoscalingGroup{
		ScaleUpPolicy: &agentsv1alpha1.PoolScaleUpPolicy{
			CooldownSeconds:           20,
			IdleThresholdSeconds:      15,
			SaturationCooldownSeconds: 90,
		},
	}
	cd, idle, sat := scaleUpPolicyOrDefault(g)
	if cd != 20*time.Second {
		t.Errorf("cooldown = %v, want 20s", cd)
	}
	if idle != 15 {
		t.Errorf("idleThresholdSeconds = %d, want 15", idle)
	}
	if sat != 90*time.Second {
		t.Errorf("saturationCooldown = %v, want 90s", sat)
	}
}

func TestScaleUpPolicyOrDefault_AppliesDefaults(t *testing.T) {
	cd, idle, sat := scaleUpPolicyOrDefault(nil)
	if cd != time.Duration(defaultScaleUpCooldownSeconds)*time.Second {
		t.Errorf("nil group cooldown = %v", cd)
	}
	if idle != defaultScaleUpIdleThresholdSeconds {
		t.Errorf("nil group idle threshold = %d", idle)
	}
	if sat != time.Duration(defaultSaturationCooldownSeconds)*time.Second {
		t.Errorf("nil group saturation cooldown = %v", sat)
	}
}

// TestApplyMemberAttemptResult_UpsertsNewMember confirms the helper creates
// an ObservedMember entry when no prior observation exists.
func TestApplyMemberAttemptResult_UpsertsNewMember(t *testing.T) {
	local := &agentsv1alpha1.EnvClusterStatus{ClusterID: "local"}
	until := metav1.NewTime(time.Now().Add(time.Minute))
	applyMemberAttemptResult(local, memberStatusUpdate{
		name:           "p1",
		attemptResult:  "InsufficientResources",
		saturatedUntil: &until,
		message:        "scheduler full",
	})
	if len(local.ObservedMembers) != 1 {
		t.Fatalf("expected 1 ObservedMember, got %d", len(local.ObservedMembers))
	}
	om := local.ObservedMembers[0]
	if om.LastScaleUpAttemptResult != "InsufficientResources" {
		t.Errorf("result = %q", om.LastScaleUpAttemptResult)
	}
	if om.SaturatedUntil == nil {
		t.Error("SaturatedUntil should be set")
	}
	if om.ScaleUpErrorMessage != "scheduler full" {
		t.Errorf("message = %q", om.ScaleUpErrorMessage)
	}
}

// TestApplyMemberAttemptResult_ClearSaturated confirms a successful probe
// wipes a prior SaturatedUntil.
func TestApplyMemberAttemptResult_ClearSaturated(t *testing.T) {
	until := metav1.NewTime(time.Now().Add(time.Minute))
	local := &agentsv1alpha1.EnvClusterStatus{
		ClusterID: "local",
		ObservedMembers: []agentsv1alpha1.EnvObservedMember{
			{Name: "p1", SaturatedUntil: &until, LastScaleUpAttemptResult: "InsufficientResources"},
		},
	}
	applyMemberAttemptResult(local, memberStatusUpdate{
		name:           "p1",
		attemptResult:  "Success",
		clearSaturated: true,
	})
	om := local.ObservedMembers[0]
	if om.SaturatedUntil != nil {
		t.Errorf("SaturatedUntil should be nil after Success+clearSaturated")
	}
	if om.LastScaleUpAttemptResult != "Success" {
		t.Errorf("result = %q", om.LastScaleUpAttemptResult)
	}
}

// TestApplyMemberAttemptResult_InternalErrorPreservesSaturation: an internal
// error shouldn't *clear* a prior saturation marker but also doesn't *add*
// one — the next reconcile retries the same member.
func TestApplyMemberAttemptResult_InternalErrorPreservesSaturation(t *testing.T) {
	until := metav1.NewTime(time.Now().Add(time.Minute))
	local := &agentsv1alpha1.EnvClusterStatus{
		ClusterID: "local",
		ObservedMembers: []agentsv1alpha1.EnvObservedMember{
			{Name: "p1", SaturatedUntil: &until},
		},
	}
	applyMemberAttemptResult(local, memberStatusUpdate{
		name:          "p1",
		attemptResult: "InternalError",
		message:       "rpc broken",
	})
	if local.ObservedMembers[0].SaturatedUntil == nil {
		t.Error("InternalError should not clear a pre-existing SaturatedUntil")
	}
	if local.ObservedMembers[0].LastScaleUpAttemptResult != "InternalError" {
		t.Errorf("result = %q", local.ObservedMembers[0].LastScaleUpAttemptResult)
	}
}

// TestPickScaleDownCandidate covers the group-aware scale-down ordering
// strategy: priority desc → oldestIdleSince asc → name asc, plus the
// timeout-not-yet-met requeue path.
func TestPickScaleDownCandidate(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	mk := func(name string, priority int32, idleAgo time.Duration) scaleDownCandidate {
		pri := priority
		return scaleDownCandidate{
			view: memberView{
				member: agentsv1alpha1.EnvClusterMember{
					Name: name,
					Config: agentsv1alpha1.EnvClusterMemberConfig{
						ScaleDownPriority: &pri,
					},
				},
			},
			oldestIdleSince: now.Add(-idleAgo),
		}
	}
	cases := []struct {
		name        string
		candidates  []scaleDownCandidate
		idleTimeout time.Duration
		wantWinner  string // "" means !ok
		wantWait    time.Duration
	}{
		{
			name:        "empty candidate set",
			candidates:  nil,
			idleTimeout: time.Minute,
			wantWinner:  "",
			wantWait:    0,
		},
		{
			name: "priority desc picks higher value",
			candidates: []scaleDownCandidate{
				mk("low", 0, 10*time.Minute),
				mk("high", 10, 10*time.Minute),
			},
			idleTimeout: time.Minute,
			wantWinner:  "high",
		},
		{
			name: "priority tie: oldest idle first",
			candidates: []scaleDownCandidate{
				mk("young", 0, 2*time.Minute),
				mk("old", 0, 10*time.Minute),
			},
			idleTimeout: time.Minute,
			wantWinner:  "old",
		},
		{
			name: "priority + idle tie: name lexicographic",
			candidates: []scaleDownCandidate{
				mk("b", 0, 10*time.Minute),
				mk("a", 0, 10*time.Minute),
			},
			idleTimeout: time.Minute,
			wantWinner:  "a",
		},
		{
			name: "priority desc beats older idle in lower bucket",
			candidates: []scaleDownCandidate{
				mk("lowPri-old", 0, 30*time.Minute),
				mk("highPri-young", 10, 2*time.Minute),
			},
			idleTimeout: time.Minute,
			wantWinner:  "highPri-young",
		},
		{
			name: "no candidate meets timeout → requeue min remaining",
			candidates: []scaleDownCandidate{
				mk("a", 0, 10*time.Second), // 50s short
				mk("b", 0, 30*time.Second), // 30s short ← min
			},
			idleTimeout: time.Minute,
			wantWinner:  "",
			wantWait:    30 * time.Second,
		},
		{
			name: "user yaml scenario: two members, default priority, alphabetical wins",
			candidates: []scaleDownCandidate{
				mk("abc-1c15gi-ondemand", 0, 10*time.Minute),
				mk("abc-1c15gi-shared", 0, 10*time.Minute),
			},
			idleTimeout: 5 * time.Minute,
			wantWinner:  "abc-1c15gi-ondemand",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, wait, ok := pickScaleDownCandidate(tc.candidates, now, tc.idleTimeout)
			switch {
			case tc.wantWinner == "" && ok:
				t.Fatalf("expected !ok, got winner=%q", got.view.member.Name)
			case tc.wantWinner != "" && !ok:
				t.Fatalf("expected winner=%q, got !ok (wait=%v)", tc.wantWinner, wait)
			case tc.wantWinner != "" && got.view.member.Name != tc.wantWinner:
				t.Fatalf("winner = %q, want %q", got.view.member.Name, tc.wantWinner)
			case tc.wantWinner == "" && tc.wantWait != 0 && wait != tc.wantWait:
				t.Fatalf("wait = %v, want %v", wait, tc.wantWait)
			}
		})
	}
}

// TestPickScaleDownCandidate_PriorityFallsBackToPriorityField confirms that
// when ScaleDownPriority is nil the EnvClusterMemberConfig.Priority field
// takes effect with the inverted "higher shrinks first" direction.
func TestPickScaleDownCandidate_PriorityFallsBackToPriorityField(t *testing.T) {
	now := time.Now()
	mk := func(name string, priority int32) scaleDownCandidate {
		return scaleDownCandidate{
			view: memberView{
				member: agentsv1alpha1.EnvClusterMember{
					Name:   name,
					Config: agentsv1alpha1.EnvClusterMemberConfig{Priority: priority},
				},
			},
			oldestIdleSince: now.Add(-10 * time.Minute),
		}
	}
	got, _, ok := pickScaleDownCandidate(
		[]scaleDownCandidate{mk("preferred", 0), mk("disposable", 10)},
		now,
		time.Minute,
	)
	if !ok || got.view.member.Name != "disposable" {
		t.Fatalf("expected disposable (Priority=10) to shrink first, got ok=%v name=%q",
			ok, got.view.member.Name)
	}
}

func TestTruncErr(t *testing.T) {
	if got := truncErr(nil); got != "" {
		t.Errorf("nil error: got %q", got)
	}
	short := &domain.AppError{Message: "short"}
	if got := truncErr(short); got != "short" {
		t.Errorf("short err: got %q", got)
	}
	long := &domain.AppError{Message: string(make([]byte, 500))}
	got := truncErr(long)
	if len(got) > 300 {
		t.Errorf("long err not truncated: len = %d", len(got))
	}
}
