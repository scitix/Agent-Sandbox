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

package sandboxpool

import (
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestResolveMaxUnavailableCount(t *testing.T) {
	pct := func(s string) *intstr.IntOrString { v := intstr.FromString(s); return &v }
	abs := func(n int) *intstr.IntOrString { v := intstr.FromInt32(int32(n)); return &v }

	cases := []struct {
		name     string
		mu       *intstr.IntOrString
		desired  int
		want     int
	}{
		{"nil defaults to 20% of 10", nil, 10, 2},
		{"20% of 10", pct("20%"), 10, 2},
		{"percent rounds down", pct("25%"), 10, 2},
		{"percent floored at 1 for small pool", pct("20%"), 3, 1},
		{"percent floored at 1 when zero", pct("10%"), 1, 1},
		{"absolute value", abs(3), 10, 3},
		{"absolute floored at 1", abs(0), 10, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveMaxUnavailableCount(tc.mu, tc.desired); got != tc.want {
				t.Errorf("resolveMaxUnavailableCount(%v, %d) = %d, want %d", tc.mu, tc.desired, got, tc.want)
			}
		})
	}
}
