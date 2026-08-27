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

	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
)

// TestEnvVolumesFromGen_ReadOnlyNormalisation is the safety-critical case: an
// omitted readOnly on the wire must be stored as an explicit true. Leaving it
// nil would work today (IsReadOnly treats nil as read-only) but the stored CR
// would be ambiguous to anything reading it without that accessor.
func TestEnvVolumesFromGen_ReadOnlyNormalisation(t *testing.T) {
	cases := []struct {
		name string
		wire *bool
		want bool
	}{
		{"omitted", nil, true},
		{"explicit true", ptr.To(true), true},
		{"explicit false", ptr.To(false), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := envVolumesFromGen(&[]gen.EnvVolumeMount{
				{ClaimName: "c", MountPath: "/volume/c", ReadOnly: tc.wire},
			})
			if len(got) != 1 {
				t.Fatalf("want 1 entry, got %d", len(got))
			}
			if got[0].ReadOnly == nil {
				t.Fatal("ReadOnly must be normalised to an explicit pointer, got nil")
			}
			if *got[0].ReadOnly != tc.want {
				t.Errorf("ReadOnly = %v, want %v", *got[0].ReadOnly, tc.want)
			}
			if got[0].IsReadOnly() != tc.want {
				t.Errorf("IsReadOnly() = %v, want %v", got[0].IsReadOnly(), tc.want)
			}
		})
	}
}

// TestEnvVolumesFromGen_AbsentVsEmpty: PATCH replaces the overrides block
// wholesale, but the CRD still has to distinguish "no volumes declared" from
// "an explicitly empty list", because a non-nil empty slice is what a caller
// sends to clear the mounts.
func TestEnvVolumesFromGen_AbsentVsEmpty(t *testing.T) {
	if got := envVolumesFromGen(nil); got != nil {
		t.Errorf("absent volumes must map to nil, got %#v", got)
	}
	got := envVolumesFromGen(&[]gen.EnvVolumeMount{})
	if got == nil {
		t.Error("an explicitly empty list must stay distinguishable from absent (non-nil, len 0)")
	}
	if len(got) != 0 {
		t.Errorf("want an empty slice, got %d entries", len(got))
	}
}

func TestEnvVolumesFromGen_SubPath(t *testing.T) {
	got := envVolumesFromGen(&[]gen.EnvVolumeMount{
		{ClaimName: "a", MountPath: "/volume/a"},
		{ClaimName: "b", MountPath: "/volume/b", SubPath: ptr.To("inner/dir")},
	})
	if got[0].SubPath != "" {
		t.Errorf("absent subPath must be empty, got %q", got[0].SubPath)
	}
	if got[1].SubPath != "inner/dir" {
		t.Errorf("subPath = %q, want %q", got[1].SubPath, "inner/dir")
	}
}

// TestEnvOverridesFromGen_CarriesVolumes guards the conversion CLAUDE.md's SOP
// exists for: the field is defined everywhere but the projection function
// forgets to assign it, and the feature silently does nothing.
func TestEnvOverridesFromGen_CarriesVolumes(t *testing.T) {
	out, err := envOverridesFromGen(&gen.EnvOverrides{
		Volumes: &[]gen.EnvVolumeMount{
			{ClaimName: "ds", MountPath: "/volume/ds", ReadOnly: ptr.To(false)},
		},
	})
	if err != nil {
		t.Fatalf("envOverridesFromGen: %v", err)
	}
	if len(out.Volumes) != 1 {
		t.Fatalf("volumes did not survive the gen -> CRD projection, got %#v", out.Volumes)
	}
	v := out.Volumes[0]
	if v.ClaimName != "ds" || v.MountPath != "/volume/ds" || v.IsReadOnly() {
		t.Errorf("unexpected projection result: %+v", v)
	}
}
