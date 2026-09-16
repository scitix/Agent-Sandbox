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

package service

import (
	"sort"
	"testing"

	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
)

// The spec says which fields are fixed; the service has to agree.
//
// `x-immutable` is what the CLI renders as "fixed at create", what the console
// reads to lock a form field, and what a client is told to preserve — and the
// 400 that enforces it is written out by hand, per field, in Go. Those are the
// two halves of one rule, and nothing else connects them: a field that gains
// the marker without gaining a check is a body the server accepts and silently
// rewrites, and a check whose marker went away is a 400 nobody can act on.
// This reads the embedded spec and holds the two lists together.
func TestEnvFixedFieldsMatchTheSpec(t *testing.T) {
	want := immutableFieldsIn(t, "UpsertSandboxEnvRequest")
	got := append([]string(nil), envFixedFields...)
	sort.Strings(got)
	if len(want) != len(got) || !equalStrings(want, got) {
		t.Fatalf("spec says %v are fixed, the service checks %v", want, got)
	}
}

// immutableFieldsIn returns the sorted property names marked `x-immutable`.
func immutableFieldsIn(t *testing.T, schema string) []string {
	t.Helper()
	doc, err := gen.GetSwagger()
	if err != nil {
		t.Fatalf("swagger: %v", err)
	}
	s, ok := doc.Components.Schemas[schema]
	if !ok || s.Value == nil {
		t.Fatalf("no schema %q in the embedded spec", schema)
	}
	var out []string
	for name, prop := range s.Value.Properties {
		if prop.Value == nil {
			continue
		}
		if v, ok := prop.Value.Extensions["x-immutable"]; ok && v == true {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
