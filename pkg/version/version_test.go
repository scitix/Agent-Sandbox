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

package version

import "testing"

func TestResolve(t *testing.T) {
	cases := []struct {
		name  string
		env   string
		built string
		want  string
	}{
		{
			// The deployed case: the chart passes the image tag, which is not
			// semver and must survive verbatim.
			name:  "environment wins over the build-time version",
			env:   "develop-abc1234",
			built: "0.2.0",
			want:  "develop-abc1234",
		},
		{
			name:  "falls back to the build-time version when unset",
			env:   "",
			built: "0.2.0",
			want:  "0.2.0",
		},
		{
			// A Helm value left empty renders as an empty env var rather than
			// an absent one, and reporting "" would read as a broken server.
			name:  "treats a blank value as unset",
			env:   "   ",
			built: "0.2.0",
			want:  "0.2.0",
		},
		{
			name:  "trims the value it does use",
			env:   "  develop-abc1234\n",
			built: "0.2.0",
			want:  "develop-abc1234",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnvVersion, tc.env)
			prev := Version
			Version = tc.built
			defer func() { Version = prev }()

			if got := Resolve(); got != tc.want {
				t.Errorf("Resolve() = %q, want %q", got, tc.want)
			}
		})
	}
}
