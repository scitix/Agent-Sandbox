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

func TestSplitSandboxID(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantCID   string
		wantRawID string
	}{
		{
			name:      "plain UUID",
			input:     "01961e3a-7d6b-7c8e-8a3b-1234567890ab",
			wantCID:   "",
			wantRawID: "01961e3a-7d6b-7c8e-8a3b-1234567890ab",
		},
		{
			name:      "prefixed with cluster ID",
			input:     "cluster-b.01961e3a-7d6b-7c8e-8a3b-1234567890ab",
			wantCID:   "cluster-b",
			wantRawID: "01961e3a-7d6b-7c8e-8a3b-1234567890ab",
		},
		{
			name:      "short remainder (not a UUID)",
			input:     "abc.short",
			wantCID:   "",
			wantRawID: "abc.short",
		},
		{
			name:      "empty string",
			input:     "",
			wantCID:   "",
			wantRawID: "",
		},
		{
			name:      "no dot",
			input:     "justanid",
			wantCID:   "",
			wantRawID: "justanid",
		},
		{
			name:      "dot at position 0",
			input:     ".01961e3a-7d6b-7c8e-8a3b-1234567890ab",
			wantCID:   "",
			wantRawID: "01961e3a-7d6b-7c8e-8a3b-1234567890ab",
		},
		{
			name:      "multi-segment cluster ID",
			input:     "cluster1.01961e3a-7d6b-7c8e-8a3b-1234567890ab",
			wantCID:   "cluster1",
			wantRawID: "01961e3a-7d6b-7c8e-8a3b-1234567890ab",
		},
		{
			name:      "remainder exactly 36 chars",
			input:     "x.012345678901234567890123456789012345",
			wantCID:   "x",
			wantRawID: "012345678901234567890123456789012345",
		},
		{
			name:      "remainder 35 chars (too short)",
			input:     "x.01234567890123456789012345678901234",
			wantCID:   "",
			wantRawID: "x.01234567890123456789012345678901234",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCID, gotRawID := SplitSandboxID(tt.input)
			if gotCID != tt.wantCID || gotRawID != tt.wantRawID {
				t.Errorf("SplitSandboxID(%q) = (%q, %q), want (%q, %q)",
					tt.input, gotCID, gotRawID, tt.wantCID, tt.wantRawID)
			}
		})
	}
}
