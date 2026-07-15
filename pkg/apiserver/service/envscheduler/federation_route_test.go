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
	"testing"

	"k8s.io/apimachinery/pkg/types"
)

type fakeFederationView struct {
	localIdle    int32
	localCanGrow bool
	bestClustr   string
	bestPool     string
	bestIdle     int32
	bestFound    bool
}

func (f fakeFederationView) LocalIdle(_, _, _ string) int32   { return f.localIdle }
func (f fakeFederationView) LocalCanGrow(_, _, _ string) bool { return f.localCanGrow }
func (f fakeFederationView) BestForeignMember(_, _, _ string) (string, string, int32, bool) {
	return f.bestClustr, f.bestPool, f.bestIdle, f.bestFound
}

func TestSelectForeignTarget(t *testing.T) {
	key := types.NamespacedName{Namespace: "ns", Name: "env"}

	cases := []struct {
		name        string
		fed         FederationView
		wantCluster string
		wantPool    string
	}{
		{"no federation view → local", nil, "", ""},
		{"local has idle → local", fakeFederationView{localIdle: 3, bestClustr: "cluster-b", bestPool: "p", bestIdle: 9, bestFound: true}, "", ""},
		{"local empty, foreign has idle → forward to pinned pool", fakeFederationView{localIdle: 0, bestClustr: "cluster-b", bestPool: "p-b", bestIdle: 4, bestFound: true}, "cluster-b", "p-b"},
		{"local empty, no schedulable foreign → local (park/autoscale)", fakeFederationView{localIdle: 0, bestFound: false}, "", ""},
		{"foreign scale-up only, local can also grow → keep local", fakeFederationView{localIdle: 0, localCanGrow: true, bestClustr: "cluster-b", bestPool: "p-b", bestIdle: 0, bestFound: true}, "", ""},
		{"foreign scale-up only, local cannot grow → forward", fakeFederationView{localIdle: 0, localCanGrow: false, bestClustr: "cluster-b", bestPool: "p-b", bestIdle: 0, bestFound: true}, "cluster-b", "p-b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := New("cluster-a", nil, nil)
			m.SetFederationView(tc.fed)
			gotCluster, gotPool, ok := m.SelectForeignTarget(key, "")
			if gotCluster != tc.wantCluster || gotPool != tc.wantPool {
				t.Fatalf("SelectForeignTarget = (%q,%q), want (%q,%q)", gotCluster, gotPool, tc.wantCluster, tc.wantPool)
			}
			if ok != (tc.wantCluster != "") {
				t.Fatalf("ok = %v, want %v", ok, tc.wantCluster != "")
			}
		})
	}
}
