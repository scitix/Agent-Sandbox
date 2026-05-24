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

package quota

import "testing"

// TestDeriveDefaultShortName covers the common scitix QuotaURL shapes the
// Env Reconciler will see when naming member pools.
func TestDeriveDefaultShortName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty input", "", ""},
		{"single segment", "ondemand", "ondemand"},
		{"two segments", "math.exclusive", "exclusive"},
		{"scitix-style three segments", "zxli.ai-lab.math.exclusive", "exclusive"},
		{"upgrader example", "upgrader.autoupg.test.ondemand", "ondemand"},
		{"with scheme + host", "https://quota.scitix.ai/v1/zxli.ai-lab.math.exclusive", "exclusive"},
		{"with query string", "math.ondemand?team=foo", "ondemand"},
		{"mixed case is lowercased", "tenant.Pool.SPOT", "spot"},
		{"underscore becomes dash", "team_x.spot_a", "spot-a"},
		{"trailing dot is empty segment", "team.pool.", ""},
		{"only punctuation", "...", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DeriveDefaultShortName(tt.in); got != tt.want {
				t.Errorf("DeriveDefaultShortName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestNoop_DeriveShortName confirms the Noop provider delegates to the
// open-source default extractor.
func TestNoop_DeriveShortName(t *testing.T) {
	n := Noop{}
	if got := n.DeriveShortName("x.y.spot"); got != "spot" {
		t.Errorf("Noop.DeriveShortName: got %q", got)
	}
}
