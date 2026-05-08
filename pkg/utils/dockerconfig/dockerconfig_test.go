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

package dockerconfig

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestBuildAndParse(t *testing.T) {
	creds := []RegistryCredential{
		{Registry: "https://index.docker.io/v1/", Username: "alice", Password: "s3cret"},
		{Registry: "ghcr.io", Username: "bob", Password: "ghcr-pw"},
	}
	data, err := Build(creds)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var cfg DockerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(cfg.Auths) != 2 {
		t.Fatalf("expected 2 auths, got %d", len(cfg.Auths))
	}
	docker := cfg.Auths["https://index.docker.io/v1/"]
	wantAuth := base64.StdEncoding.EncodeToString([]byte("alice:s3cret"))
	if docker.Auth != wantAuth {
		t.Fatalf("auth mismatch: got %q want %q", docker.Auth, wantAuth)
	}
	parsed, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("expected 2 parsed, got %d", len(parsed))
	}
	// Must contain both registries with correct creds
	seen := map[string]RegistryCredential{}
	for _, c := range parsed {
		seen[c.Registry] = c
	}
	if got := seen["https://index.docker.io/v1/"]; got.Username != "alice" || got.Password != "s3cret" {
		t.Fatalf("docker cred wrong: %+v", got)
	}
	if got := seen["ghcr.io"]; got.Username != "bob" || got.Password != "ghcr-pw" {
		t.Fatalf("ghcr cred wrong: %+v", got)
	}
}

func TestBuildRejectsEmpty(t *testing.T) {
	if _, err := Build(nil); err == nil {
		t.Fatal("expected error for empty creds")
	}
	if _, err := Build([]RegistryCredential{{Registry: "", Username: "u", Password: "p"}}); err == nil {
		t.Fatal("expected error for empty registry")
	}
	if _, err := Build([]RegistryCredential{{Registry: "r", Username: "", Password: "p"}}); err == nil {
		t.Fatal("expected error for empty username")
	}
	if _, err := Build([]RegistryCredential{{Registry: "r", Username: "u", Password: ""}}); err == nil {
		t.Fatal("expected error for empty password")
	}
}

func TestBuildRejectsDuplicates(t *testing.T) {
	creds := []RegistryCredential{
		{Registry: "ghcr.io", Username: "a", Password: "b"},
		{Registry: "ghcr.io", Username: "c", Password: "d"},
	}
	if _, err := Build(creds); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestParseFallbackToAuthField(t *testing.T) {
	// When username/password are missing but auth is present, we decode from auth.
	auth := base64.StdEncoding.EncodeToString([]byte("carol:pw"))
	payload := []byte(`{"auths":{"ghcr.io":{"auth":"` + auth + `"}}}`)
	parsed, err := Parse(payload)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("expected 1, got %d", len(parsed))
	}
	if parsed[0].Username != "carol" || parsed[0].Password != "pw" {
		t.Fatalf("decoded wrong: %+v", parsed[0])
	}
}
