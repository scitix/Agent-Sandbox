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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const testRealKey = "Bearer real-key"

func secretsFixture() Secrets {
	return Secrets{
		SandboxID: "sbx-1",
		Rules: []InjectRule{{
			Host:                   "api.openai.com",
			Headers:                []InjectHeader{{Name: "Authorization", Value: testRealKey, Mode: ModeOverride}},
			SubstitutePlaceholders: []string{"agbx_ph_decoy0000000000"},
			PathPrefixes:           []string{"/v1/"},
		}},
		Substitutions: map[string]string{"agbx_ph_decoy0000000000": "real-key"},
	}
}

func TestMatch_ExactHostAndDefaultPorts(t *testing.T) {
	s := secretsFixture()
	for _, tc := range []struct {
		host string
		port int
		want bool
	}{
		{"api.openai.com", 443, true},
		{"api.openai.com", 80, true},
		{"API.OpenAI.com", 443, true},  // case-insensitive
		{"api.openai.com.", 443, true}, // trailing dot is the same name
		{"api.openai.com", 8443, false},
		{"evil.api.openai.com", 443, false},
		{"openai.com", 443, false},
	} {
		if got := s.Intercepts(tc.host, tc.port); got != tc.want {
			t.Errorf("Match(%q,%d)=%v, want %v", tc.host, tc.port, got, tc.want)
		}
	}
}

func TestApply_OverrideReplacesSandboxHeader(t *testing.T) {
	s := secretsFixture()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer sandbox-supplied")

	out := s.Apply(req, s.MatchAll("api.openai.com", 443))
	if out.HeadersSet != 1 {
		t.Fatalf("HeadersSet=%d, want 1", out.HeadersSet)
	}
	if got := req.Header.Get("Authorization"); got != testRealKey {
		t.Fatalf("Authorization=%q, want the injected value", got)
	}
}

func TestApply_IfAbsentRespectsSandboxHeader(t *testing.T) {
	s := secretsFixture()
	s.Rules[0].Headers[0].Mode = ModeIfAbsent
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer agent-own-key")

	out := s.Apply(req, s.MatchAll("api.openai.com", 443))
	if out.HeadersSet != 0 {
		t.Fatalf("HeadersSet=%d, want 0 — IfAbsent must not overwrite", out.HeadersSet)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer agent-own-key" {
		t.Fatalf("Authorization=%q, want the agent's own value preserved", got)
	}
}

func TestApply_IfAbsentInjectsWhenMissing(t *testing.T) {
	s := secretsFixture()
	s.Rules[0].Headers[0].Mode = ModeIfAbsent
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)

	s.Apply(req, s.MatchAll("api.openai.com", 443))
	if got := req.Header.Get("Authorization"); got != testRealKey {
		t.Fatalf("Authorization=%q, want the injected value", got)
	}
}

func TestApply_SubstitutesPlaceholder(t *testing.T) {
	s := secretsFixture()
	s.Rules[0].Headers = nil // substitution only
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer agbx_ph_decoy0000000000")

	out := s.Apply(req, s.MatchAll("api.openai.com", 443))
	if out.Substituted != 1 {
		t.Fatalf("Substituted=%d, want 1", out.Substituted)
	}
	if got := req.Header.Get("Authorization"); got != testRealKey {
		t.Fatalf("Authorization=%q, want the decoy replaced by the real value", got)
	}
}

// A placeholder must only be swapped on a host whose rule allows it, otherwise
// the real credential could be steered to an unrelated destination.
func TestApply_NoSubstitutionWhenRuleDoesNotAllowIt(t *testing.T) {
	s := secretsFixture()
	s.Rules[0].SubstitutePlaceholders = nil
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("X-Token", "agbx_ph_decoy0000000000")

	out := s.Apply(req, s.MatchAll("api.openai.com", 443))
	if out.Substituted != 0 {
		t.Fatalf("Substituted=%d, want 0", out.Substituted)
	}
	if got := req.Header.Get("X-Token"); got != "agbx_ph_decoy0000000000" {
		t.Fatalf("X-Token=%q, want the decoy left untouched", got)
	}
}

func TestApply_PathPrefixNarrowing(t *testing.T) {
	s := secretsFixture()
	req := httptest.NewRequest(http.MethodGet, "/admin/keys", nil)

	out := s.Apply(req, s.MatchAll("api.openai.com", 443))
	if !out.Skipped {
		t.Fatal("expected the request to be skipped: /admin/keys is outside pathPrefixes")
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatal("no header should have been injected on a skipped request")
	}
}

func TestApply_MethodNarrowing(t *testing.T) {
	s := secretsFixture()
	s.Rules[0].Methods = []string{"POST"}
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)

	if out := s.Apply(req, s.MatchAll("api.openai.com", 443)); !out.Skipped {
		t.Fatal("GET should be skipped when the rule only covers POST")
	}
}

// Substitution runs before injection so "decoy first, header as fallback" is
// expressible: Override wins over whatever substitution produced.
func TestApply_OverrideWinsOverSubstitution(t *testing.T) {
	s := secretsFixture()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer agbx_ph_decoy0000000000")

	s.Apply(req, s.MatchAll("api.openai.com", 443))
	if got := req.Header.Get("Authorization"); got != testRealKey {
		t.Fatalf("Authorization=%q", got)
	}
}

func TestLoadSecrets_MissingFileIsEmptyNotError(t *testing.T) {
	s, err := LoadSecrets(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if s.Enabled() {
		t.Fatal("missing file must mean no injection")
	}
}

func TestWriteSecrets_OwnerOnlyPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	if err := WriteSecrets(path, secretsFixture()); err != nil {
		t.Fatalf("WriteSecrets: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode=%o, want 600 — the file holds credentials and the CA key", perm)
	}
	got, err := LoadSecrets(path)
	if err != nil {
		t.Fatalf("LoadSecrets: %v", err)
	}
	if len(got.Rules) != 1 || got.Rules[0].Host != "api.openai.com" {
		t.Fatalf("round-trip lost the rules: %+v", got)
	}
}

// Release must leave nothing behind: a recycled pod cannot inherit the previous
// sandbox's credentials, CA key, or decoy map.
func TestRemoveSecrets_WipesAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	if err := WriteSecrets(path, secretsFixture()); err != nil {
		t.Fatalf("WriteSecrets: %v", err)
	}
	if err := RemoveSecrets(path); err != nil {
		t.Fatalf("RemoveSecrets: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("secrets file still present after reset")
	}
	if err := RemoveSecrets(path); err != nil {
		t.Fatalf("second RemoveSecrets should be a no-op, got %v", err)
	}
	s, _ := LoadSecrets(path)
	if s.Enabled() || len(s.Substitutions) != 0 {
		t.Fatal("wiped config must expose no rules and no substitutions")
	}
}

// A host may carry several rules; every one of them must apply. Stopping at the
// first match would silently drop the rest.
func TestApply_AllRulesForAHostApply(t *testing.T) {
	s := Secrets{
		Rules: []InjectRule{
			{Host: "hub.example.com", Headers: []InjectHeader{{Name: "Authorization", Value: "Bearer t1"}}},
			{Host: "hub.example.com", Headers: []InjectHeader{{Name: "X-Trace", Value: "on"}}},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/x", nil)
	out := s.Apply(req, s.MatchAll("hub.example.com", 443))
	if out.HeadersSet != 2 {
		t.Fatalf("HeadersSet=%d, want 2 (both rules)", out.HeadersSet)
	}
	if req.Header.Get("Authorization") == "" || req.Header.Get("X-Trace") == "" {
		t.Fatalf("only some rules applied: %v", req.Header)
	}
}

// Narrowing is per rule: a rule whose path prefix excludes the request is
// skipped while its sibling still applies.
func TestApply_PerRuleNarrowing(t *testing.T) {
	s := Secrets{
		Rules: []InjectRule{
			{Host: "hub.example.com", PathPrefixes: []string{"/admin/"},
				Headers: []InjectHeader{{Name: "X-Admin", Value: "1"}}},
			{Host: "hub.example.com",
				Headers: []InjectHeader{{Name: "Authorization", Value: "Bearer t1"}}},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/x", nil)
	out := s.Apply(req, s.MatchAll("hub.example.com", 443))
	if out.Skipped {
		t.Fatal("at least one rule applied, so the request is not skipped")
	}
	if req.Header.Get("X-Admin") != "" {
		t.Fatal("a rule outside its path prefix must not apply")
	}
	if req.Header.Get("Authorization") != "Bearer t1" {
		t.Fatal("the unnarrowed sibling rule should still apply")
	}
}
