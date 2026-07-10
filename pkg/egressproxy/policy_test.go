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

package egressproxy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPolicy_MissingFileFailsClosed(t *testing.T) {
	p, err := LoadPolicy(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if !p.Enforce || !p.DisableEgress {
		t.Errorf("missing file must fail closed: got %+v", p)
	}
}

func TestLoadPolicy_EmptyFileFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := LoadPolicy(path)
	if err != nil || !p.DisableEgress {
		t.Errorf("empty file must fail closed without error: p=%+v err=%v", p, err)
	}
}

func TestLoadPolicy_CorruptFailsClosedWithError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := LoadPolicy(path)
	if err == nil {
		t.Error("corrupt file should return an error")
	}
	if !p.DisableEgress {
		t.Errorf("corrupt file must still fail closed: %+v", p)
	}
}

func TestWriteLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	want := Policy{
		SandboxID:      "sbx-1",
		Enforce:        true,
		AllowedDomains: []string{"pypi.org", "*.pythonhosted.org"},
		AllowedCIDRs:   []string{"8.8.8.8/32"},
	}
	if err := WritePolicy(path, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := LoadPolicy(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.SandboxID != want.SandboxID || !got.Enforce || len(got.AllowedDomains) != 2 || len(got.AllowedCIDRs) != 1 {
		t.Errorf("round trip mismatch: got %+v", got)
	}
}
