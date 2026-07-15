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

func cap1(cluster, pool, group string, idle int32, at time.Time) Capacity {
	return Capacity{ClusterID: cluster, Namespace: "ns", EnvName: "env", MemberPool: pool, ScalingGroup: group, Idle: idle, ObservedAt: at}
}

func TestLocalIdleSumsMatchingGroup(t *testing.T) {
	now := time.Unix(1000, 0)
	r := NewRegistry("cluster-a", 30*time.Second)
	r.SetClock(func() time.Time { return now })

	r.Upsert([]Capacity{
		cap1("cluster-a", "p-cpu", "cpu", 2, now),
		cap1("cluster-a", "p-gpu", "gpu", 5, now),
		cap1("cluster-b", "p-cpu", "cpu", 9, now), // foreign, ignored
	})

	if got := r.LocalIdle("ns", "env", ""); got != 7 { // all local members
		t.Fatalf("LocalIdle(all) = %d, want 7", got)
	}
	if got := r.LocalIdle("ns", "env", "cpu"); got != 2 { // only cpu group
		t.Fatalf("LocalIdle(cpu) = %d, want 2", got)
	}
}

func TestBestForeignMemberPicksMaxIdlePool(t *testing.T) {
	now := time.Unix(1000, 0)
	r := NewRegistry("cluster-a", 30*time.Second)
	r.SetClock(func() time.Time { return now })

	r.Upsert([]Capacity{
		cap1("cluster-a", "p-local", "cpu", 50, now), // local, ignored
		cap1("cluster-b", "p-b1", "cpu", 2, now),
		cap1("cluster-c", "p-c1", "cpu", 6, now),
		cap1("cluster-c", "p-c-gpu", "gpu", 8, now),                    // wrong group when cpu requested
		cap1("cluster-d", "p-d1", "cpu", 99, now.Add(-40*time.Second)), // stale
	})

	cluster, pool, idle, ok := r.BestForeignMember("ns", "env", "cpu")
	if !ok || cluster != "cluster-c" || pool != "p-c1" || idle != 6 {
		t.Fatalf("BestForeignMember(cpu) = (%s,%s,%d,%v), want (cluster-c,p-c1,6,true)", cluster, pool, idle, ok)
	}

	// No group constraint → gpu member (idle 8) wins.
	cluster, pool, idle, ok = r.BestForeignMember("ns", "env", "")
	if !ok || cluster != "cluster-c" || pool != "p-c-gpu" || idle != 8 {
		t.Fatalf("BestForeignMember(all) = (%s,%s,%d,%v), want (cluster-c,p-c-gpu,8,true)", cluster, pool, idle, ok)
	}
}

func TestBestForeignMemberNoneWhenAllZeroOrLocal(t *testing.T) {
	now := time.Unix(1000, 0)
	r := NewRegistry("cluster-a", 30*time.Second)
	r.SetClock(func() time.Time { return now })

	r.Upsert([]Capacity{
		cap1("cluster-a", "p-local", "cpu", 10, now), // local
		cap1("cluster-b", "p-b1", "cpu", 0, now),     // foreign, no idle
	})
	if _, _, _, ok := r.BestForeignMember("ns", "env", ""); ok {
		t.Fatalf("expected no foreign target")
	}
}

func TestTTLExpiry(t *testing.T) {
	base := time.Unix(1000, 0)
	cur := base
	r := NewRegistry("cluster-a", 30*time.Second)
	r.SetClock(func() time.Time { return cur })

	r.Upsert([]Capacity{cap1("cluster-b", "p-b1", "cpu", 5, base)})
	if _, _, _, ok := r.BestForeignMember("ns", "env", ""); !ok {
		t.Fatalf("expected fresh foreign target before expiry")
	}
	cur = base.Add(31 * time.Second)
	if _, _, _, ok := r.BestForeignMember("ns", "env", ""); ok {
		t.Fatalf("expected no target after TTL expiry")
	}
	if snap := r.Snapshot(); len(snap) != 0 {
		t.Fatalf("expected empty snapshot after expiry, got %d", len(snap))
	}
}

func TestUpsertReplacesSameMember(t *testing.T) {
	now := time.Unix(1000, 0)
	r := NewRegistry("cluster-a", 30*time.Second)
	r.SetClock(func() time.Time { return now })

	r.Upsert([]Capacity{cap1("cluster-b", "p-b1", "cpu", 1, now)})
	r.Upsert([]Capacity{cap1("cluster-b", "p-b1", "cpu", 9, now)})
	if _, _, idle, ok := r.BestForeignMember("ns", "env", ""); !ok || idle != 9 {
		t.Fatalf("expected latest idle 9, got %d (ok=%v)", idle, ok)
	}
}
