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

package envmember

import (
	"sort"
	"testing"

	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
)

// The spec says which fields are fixed; this package has to agree.
//
// The member pool's shape is the sharpest case of it: `instanceType`,
// `multiplier` and `inlineResources` are not just immutable, they are what the
// pool is NAMED after — so a client that sends back a different shape is asking
// for a pool that does not exist. `x-immutable` is where the CLI and the
// console read that rule; the refusals below are where it is enforced. This is
// the test that keeps the two from drifting apart.
func TestMemberFixedFieldsMatchTheSpec(t *testing.T) {
	doc, err := gen.GetSwagger()
	if err != nil {
		t.Fatalf("swagger: %v", err)
	}
	s, ok := doc.Components.Schemas["UpsertSandboxPoolRequest"]
	if !ok || s.Value == nil {
		t.Fatal("no UpsertSandboxPoolRequest in the embedded spec")
	}
	var want []string
	for name, prop := range s.Value.Properties {
		if prop.Value == nil {
			continue
		}
		if v, ok := prop.Value.Extensions["x-immutable"]; ok && v == true {
			want = append(want, name)
		}
	}
	sort.Strings(want)
	got := append([]string(nil), memberFixedFields...)
	sort.Strings(got)
	if len(want) != len(got) {
		t.Fatalf("spec says %v are fixed, the service checks %v", want, got)
	}
	for i := range want {
		if want[i] != got[i] {
			t.Fatalf("spec says %v are fixed, the service checks %v", want, got)
		}
	}
}
