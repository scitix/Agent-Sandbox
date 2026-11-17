// Copyright 2026 ScitiX
// SPDX-License-Identifier: Apache-2.0
package instancetype

import "testing"

func TestFrozenNodeSelector(t *testing.T) {
	if got, err := FrozenNodeSelector(nil); err != nil || got != nil {
		t.Fatalf("legacy member changed: %v %v", got, err)
	}
	for _, raw := range []string{"", "null", "{}", `{"invalid key":"v"}`, `{"example.com/model":"bad value"}`} {
		if _, err := FrozenNodeSelector(map[string]string{NodeSelectorSnapshotAnnotation: raw}); err == nil {
			t.Fatalf("accepted invalid snapshot %q", raw)
		}
	}
	got, err := FrozenNodeSelector(map[string]string{NodeSelectorSnapshotAnnotation: `{"example.com/model":"model-a"}`})
	if err != nil || got["example.com/model"] != "model-a" {
		t.Fatalf("snapshot not decoded: %v %v", got, err)
	}
}
