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

package federation

import (
	"testing"
	"time"
)

func TestForeignForEnvExcludesLocalCluster(t *testing.T) {
	now := time.Unix(1000, 0)
	r := NewRegistry("cluster-a", 30*time.Second)
	r.SetClock(func() time.Time { return now })

	r.Upsert([]Capacity{
		{ClusterID: "cluster-a", Namespace: "ns", EnvName: "env", ScalingGroup: "", Idle: 3, ObservedAt: now},
		{ClusterID: "cluster-b", Namespace: "ns", EnvName: "env", ScalingGroup: "", Idle: 5, ObservedAt: now},
		{ClusterID: "cluster-b", Namespace: "ns", EnvName: "other", ScalingGroup: "", Idle: 9, ObservedAt: now},
	})

	got := r.ForeignForEnv("ns", "env")
	if len(got) != 1 {
		t.Fatalf("expected 1 foreign record, got %d: %+v", len(got), got)
	}
	if got[0].ClusterID != "cluster-b" || got[0].Idle != 5 {
		t.Fatalf("unexpected foreign record: %+v", got[0])
	}
}

func TestForeignIdleAggregatesFreshOnly(t *testing.T) {
	now := time.Unix(1000, 0)
	r := NewRegistry("cluster-a", 30*time.Second)
	r.SetClock(func() time.Time { return now })

	r.Upsert([]Capacity{
		// Fresh foreign entries in the same group.
		{ClusterID: "cluster-b", Namespace: "ns", EnvName: "env", ScalingGroup: "cpu", Idle: 2, ObservedAt: now},
		{ClusterID: "cluster-c", Namespace: "ns", EnvName: "env", ScalingGroup: "cpu", Idle: 4, ObservedAt: now},
		// Stale entry (observed 40s ago, TTL 30s) must be excluded.
		{ClusterID: "cluster-d", Namespace: "ns", EnvName: "env", ScalingGroup: "cpu", Idle: 100, ObservedAt: now.Add(-40 * time.Second)},
		// Different group must be excluded.
		{ClusterID: "cluster-b", Namespace: "ns", EnvName: "env", ScalingGroup: "gpu", Idle: 7, ObservedAt: now},
		// Local cluster must be excluded.
		{ClusterID: "cluster-a", Namespace: "ns", EnvName: "env", ScalingGroup: "cpu", Idle: 50, ObservedAt: now},
	})

	if got := r.ForeignIdle("ns", "env", "cpu"); got != 6 {
		t.Fatalf("expected foreign idle 6, got %d", got)
	}
}

func TestTTLExpiry(t *testing.T) {
	base := time.Unix(1000, 0)
	cur := base
	r := NewRegistry("cluster-a", 30*time.Second)
	r.SetClock(func() time.Time { return cur })

	r.Upsert([]Capacity{
		{ClusterID: "cluster-b", Namespace: "ns", EnvName: "env", Idle: 5, ObservedAt: base},
	})

	if got := r.ForeignIdle("ns", "env", ""); got != 5 {
		t.Fatalf("expected 5 before expiry, got %d", got)
	}

	cur = base.Add(31 * time.Second) // advance past TTL
	if got := r.ForeignIdle("ns", "env", ""); got != 0 {
		t.Fatalf("expected 0 after TTL expiry, got %d", got)
	}
	if snap := r.Snapshot(); len(snap) != 0 {
		t.Fatalf("expected empty snapshot after expiry, got %d", len(snap))
	}
}

func TestUpsertReplacesSameKey(t *testing.T) {
	now := time.Unix(1000, 0)
	r := NewRegistry("cluster-a", 30*time.Second)
	r.SetClock(func() time.Time { return now })

	r.Upsert([]Capacity{{ClusterID: "cluster-b", Namespace: "ns", EnvName: "env", Idle: 1, ObservedAt: now}})
	r.Upsert([]Capacity{{ClusterID: "cluster-b", Namespace: "ns", EnvName: "env", Idle: 9, ObservedAt: now}})

	if got := r.ForeignIdle("ns", "env", ""); got != 9 {
		t.Fatalf("expected latest value 9, got %d", got)
	}
}
