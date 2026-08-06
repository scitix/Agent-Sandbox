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

package notify

import (
	"testing"

	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
)

// A cluster ID is this platform's handle for a config entry; the `cluster`
// label value is what the scrape pipeline stamps on the series. Matching on
// the ID returns no series at all — silently, since an empty result vector is
// indistinguishable from a quiet cluster — so every report metric renders
// "N/A". These pin the ID-vs-label-value distinction down.
func TestClusterLabelValue(t *testing.T) {
	tests := []struct {
		name  string
		entry cluster.ClusterEntry
		want  string
	}{
		{
			name:  "reads the value out of the selector",
			entry: cluster.ClusterEntry{ID: "bar", Selector: `cluster="prod-bar"`},
			want:  "prod-bar",
		},
		{
			name:  "keeps only the cluster label from a multi-label selector",
			entry: cluster.ClusterEntry{ID: "baz", Selector: `cluster="prod-baz",region="us-west"`},
			want:  "prod-baz",
		},
		{
			name:  "tolerates whitespace around the equals sign",
			entry: cluster.ClusterEntry{ID: "foo", Selector: `cluster = "prod-foo"`},
			want:  "prod-foo",
		},
		{
			name:  "falls back to the ID when there is no selector",
			entry: cluster.ClusterEntry{ID: "plain"},
			want:  "plain",
		},
		{
			name:  "falls back to the ID when the selector pins another label",
			entry: cluster.ClusterEntry{ID: "manager", Selector: `manager="hub"`},
			want:  "manager",
		},
		{
			name:  "does not match a label merely ending in cluster",
			entry: cluster.ClusterEntry{ID: "x", Selector: `subcluster="nope"`},
			want:  "x",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clusterLabelValue(tt.entry); got != tt.want {
				t.Errorf("clusterLabelValue() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCombinedMatcher(t *testing.T) {
	tests := []struct {
		name     string
		clusters []reportCluster
		want     string
	}{
		{
			name: "joins label values, not IDs",
			clusters: []reportCluster{
				{ID: "foo", LabelValue: "prod-foo"},
				{ID: "bar", LabelValue: "prod-bar"},
			},
			want: "prod-foo|prod-bar",
		},
		{
			name:     "escapes regex metacharacters so a value cannot act as a pattern",
			clusters: []reportCluster{{ID: "a", LabelValue: "a.b+c"}, {ID: "b", LabelValue: "plain"}},
			want:     `a\.b\+c|plain`,
		},
		{
			name:     "single cluster needs no alternation",
			clusters: []reportCluster{{ID: "only", LabelValue: "prod-only"}},
			want:     "prod-only",
		},
		{
			name:     "empty set yields an empty body",
			clusters: nil,
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := combinedMatcher(tt.clusters); got != tt.want {
				t.Errorf("combinedMatcher() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClusterIDsProjectsDisplayIDs(t *testing.T) {
	got := clusterIDs([]reportCluster{
		{ID: "foo", LabelValue: "prod-foo"},
		{ID: "bar", LabelValue: "prod-bar"},
	})
	want := []string{"foo", "bar"}
	if len(got) != len(want) {
		t.Fatalf("clusterIDs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("clusterIDs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
