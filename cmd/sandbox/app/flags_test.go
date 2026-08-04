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

package controller

import "testing"

func TestParseOptionalBool(t *testing.T) {
	// The empty case is the point of the whole helper: the manifest carries
	// `--egress-legacy-sidecar={{.VAR}}`, and an unset deploy variable
	// substitutes to nothing. Defaulting off beats CrashLooping the operator.
	for _, in := range []string{"", " ", "false", "FALSE", "0", "no", "off"} {
		got, err := parseOptionalBool(in)
		if err != nil || got {
			t.Errorf("parseOptionalBool(%q) = %v, %v; want false, nil", in, got, err)
		}
	}
	for _, in := range []string{"true", "TRUE", "True", "1", "yes", "on"} {
		got, err := parseOptionalBool(in)
		if err != nil || !got {
			t.Errorf("parseOptionalBool(%q) = %v, %v; want true, nil", in, got, err)
		}
	}
	// Anything else is reported rather than read as "off", so an unsubstituted
	// placeholder or a typo shows up in the log instead of silently disabling the
	// feature. "<no value>" is what text/template renders for a variable the
	// release platform never declared.
	for _, in := range []string{"<no value>", "{{.YOUR_EGRESS_LEGACY_SIDECAR}}", "ture", "enabled"} {
		if _, err := parseOptionalBool(in); err == nil {
			t.Errorf("parseOptionalBool(%q) should have failed", in)
		}
	}
}
