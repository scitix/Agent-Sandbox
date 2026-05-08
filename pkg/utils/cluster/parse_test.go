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

package cluster

import "testing"

func TestParsePoolRef(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  ParsedPoolRef
	}{
		{
			name:  "plain pool name",
			input: "my-pool",
			want:  ParsedPoolRef{PoolName: "my-pool"},
		},
		{
			name:  "pool with image override",
			input: "my-pool//ubuntu:22.04",
			want:  ParsedPoolRef{PoolName: "my-pool", ImageOverride: "ubuntu:22.04"},
		},
		{
			name:  "cluster and pool",
			input: "cluster-b::my-pool",
			want:  ParsedPoolRef{ClusterID: "cluster-b", PoolName: "my-pool"},
		},
		{
			name:  "cluster pool and image",
			input: "cluster-b::my-pool//docker.io/library/ubuntu:22.04",
			want:  ParsedPoolRef{ClusterID: "cluster-b", PoolName: "my-pool", ImageOverride: "docker.io/library/ubuntu:22.04"},
		},
		{
			name:  "whitespace trimmed",
			input: "  cluster-b :: my-pool // image ",
			want:  ParsedPoolRef{ClusterID: "cluster-b", PoolName: "my-pool", ImageOverride: "image"},
		},
		{
			name:  "empty string",
			input: "",
			want:  ParsedPoolRef{},
		},
		{
			name:  "empty cluster prefix",
			input: "::poolName",
			want:  ParsedPoolRef{PoolName: "poolName"},
		},
		{
			name:  "empty image override",
			input: "pool//",
			want:  ParsedPoolRef{PoolName: "pool"},
		},
		{
			name:  "cluster with empty pool",
			input: "cks::",
			want:  ParsedPoolRef{ClusterID: "cks"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParsePoolRef(tt.input)
			if got != tt.want {
				t.Errorf("ParsePoolRef(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}
