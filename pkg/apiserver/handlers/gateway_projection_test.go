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

func TestGatewayFromGen(t *testing.T) {
	if gatewayFromGen(nil) != nil {
		t.Fatal("nil in => nil out")
	}
	if g := gatewayFromGen(&gen.GatewaySpec{Enabled: ptr.To(true)}); g == nil || !g.Enabled {
		t.Fatalf("enabled not mapped: %+v", g)
	}
}

// An explicit false and an omitted enabled both have to survive as a non-nil
// spec with Enabled=false. The Env reconciler compares the whole pointer for
// drift, so collapsing "present and off" into nil would make turning the
// gateway off look like no change at all.
func TestGatewayFromGen_DisabledIsStillPresent(t *testing.T) {
	for name, in := range map[string]*gen.GatewaySpec{
		"explicit false": {Enabled: ptr.To(false)},
		"omitted":        {},
	} {
		got := gatewayFromGen(in)
		if got == nil || got.Enabled {
			t.Errorf("%s: got %+v, want a non-nil spec with Enabled=false", name, got)
		}
	}
}
