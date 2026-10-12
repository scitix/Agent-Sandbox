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

package accesstoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

const (
	testSecret = "test-shared-secret"
	testID     = "01a12e1a-13f1-7da3-9436-fc351d93ccc1"
)

func hexHMAC(secret, content string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(content))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestEnvdTokenMatchesHMAC(t *testing.T) {
	g := New(testSecret)
	got := g.EnvdToken(testID)
	want := hexHMAC(testSecret, testID)
	if got != want {
		t.Fatalf("EnvdToken = %q, want %q", got, want)
	}
	if len(got) != 64 {
		t.Fatalf("EnvdToken length = %d, want 64 hex chars", len(got))
	}
}

func TestTrafficTokenUsesPrefix(t *testing.T) {
	g := New(testSecret)
	got := g.TrafficToken(testID)
	want := hexHMAC(testSecret, "sandbox-traffic-"+testID)
	if got != want {
		t.Fatalf("TrafficToken = %q, want %q", got, want)
	}
	// Traffic and envd tokens for the same sandbox must differ, or securing one
	// surface would leak the credential for the other.
	if got == g.EnvdToken(testID) {
		t.Fatal("TrafficToken must differ from EnvdToken for the same sandbox")
	}
}

func TestTokensAreDeterministicAcrossGenerators(t *testing.T) {
	// The operator, the E2B API and the gateway each build their own Generator
	// from the same secret; all three must derive identical tokens.
	a := New(testSecret)
	b := New(testSecret)
	if a.EnvdToken(testID) != b.EnvdToken(testID) {
		t.Fatal("EnvdToken differs between generators with the same secret")
	}
	if a.TrafficToken(testID) != b.TrafficToken(testID) {
		t.Fatal("TrafficToken differs between generators with the same secret")
	}
}

func TestDisabledGeneratorMintsNothing(t *testing.T) {
	g := New("")
	if g.Enabled() {
		t.Fatal("empty-secret generator must report disabled")
	}
	if g.EnvdToken(testID) != "" || g.TrafficToken(testID) != "" {
		t.Fatal("disabled generator must mint empty tokens")
	}
	if g.ValidateTraffic(testID, "anything") {
		t.Fatal("disabled generator must not validate any traffic token")
	}
	// A nil receiver must behave the same, not panic.
	var nilGen *Generator
	if nilGen.Enabled() || nilGen.EnvdToken(testID) != "" {
		t.Fatal("nil generator must be disabled and mint nothing")
	}
}

func TestValidateTraffic(t *testing.T) {
	g := New(testSecret)
	good := g.TrafficToken(testID)
	if !g.ValidateTraffic(testID, good) {
		t.Fatal("ValidateTraffic rejected the correct token")
	}
	if g.ValidateTraffic(testID, good+"x") {
		t.Fatal("ValidateTraffic accepted a wrong token")
	}
	if g.ValidateTraffic(testID, "") {
		t.Fatal("ValidateTraffic accepted an empty token")
	}
	// A token minted for a different sandbox must not validate.
	if g.ValidateTraffic(testID, g.TrafficToken("other-id")) {
		t.Fatal("ValidateTraffic accepted another sandbox's token")
	}
}

func TestIsInternalEnvdPath(t *testing.T) {
	cases := map[string]bool{
		"/init":                    true,
		"/freeze":                  true,
		"/unfreeze":                true,
		"/fsfreeze":                true,
		"/fsthaw":                  true,
		"/collapse":                true,
		"/upgrade":                 true,
		"init":                     true, // missing leading slash is normalised
		"/init/":                   true, // trailing slash cleaned
		"//init":                   true, // doubled slash cleaned
		"/files/../init":           true, // traversal cleaned
		"/files":                   false,
		"/process.Process/Start":   false,
		"/filesystem.Filesystem/X": false,
		"/health":                  false,
		"":                         false,
	}
	for p, want := range cases {
		if got := IsInternalEnvdPath(p); got != want {
			t.Errorf("IsInternalEnvdPath(%q) = %v, want %v", p, got, want)
		}
	}
}
