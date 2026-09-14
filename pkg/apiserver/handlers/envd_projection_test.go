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

package handlers

import (
	"testing"

	"k8s.io/utils/ptr"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
)

// verbose defaults to on, so only "off" is worth storing. The wire carries the
// resolved value either way — which means an explicit true must come back as
// "unset", or a console that reads an Env and saves it unchanged would write
// the default into every CR and roll every pool.
func TestEnvdFromGen_CanonicalisesTheDefaultAway(t *testing.T) {
	cases := []struct {
		name string
		in   *gen.EnvdSpec
		want *bool // nil = expect a nil CRD spec
	}{
		{"absent", nil, nil},
		{"no opinion", &gen.EnvdSpec{}, nil},
		{"explicitly on is the default", &gen.EnvdSpec{Verbose: ptr.To(true)}, nil},
		{"explicitly off is stored", &gen.EnvdSpec{Verbose: ptr.To(false)}, ptr.To(false)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := envdFromGen(tc.in)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("expected no stored spec, got %+v", got)
				}
				return
			}
			if got == nil || got.Verbose == nil || *got.Verbose != *tc.want {
				t.Fatalf("expected verbose=%v to be stored, got %+v", *tc.want, got)
			}
		})
	}
}

// The stored spec and the effective value have to agree in both directions:
// envdToGen (pkg/apiserver/service) always sends the resolved value, and this
// side must map that back to the same effective value — otherwise reading an
// Env and saving it unchanged would alter the pod template and roll its pools.
func TestEnvdFromGen_PreservesTheEffectiveValue(t *testing.T) {
	for _, stored := range []*agentsv1alpha1.EnvdSpec{
		nil,
		{Verbose: ptr.To(true)},
		{Verbose: ptr.To(false)},
	} {
		want := stored.VerboseEnabled()
		// What envdToGen puts on the wire: the resolved value, always explicit.
		wire := &gen.EnvdSpec{Verbose: ptr.To(want)}
		if got := envdFromGen(wire).VerboseEnabled(); got != want {
			t.Fatalf("round trip changed the effective value: %v -> %v", want, got)
		}
	}
}
