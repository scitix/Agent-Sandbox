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

package approval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// A key in `gated` that names a route the router does not have is not a wrong
// answer — it is no answer. `Lookup` matches the gin route template exactly, so
// a rename upstream (PATCH → PUT, say) turns the gate off for that whole
// operation, silently, and the write it was meant to hold simply happens.
//
// This is not hypothetical: `env.update` was pinned to `PATCH /v1/envs/:name`
// for as long as the spec has had no PATCH on that path, so an agent credential
// could rewrite any env's overrides without asking anybody.
func TestEveryGatedRouteExistsInTheSpec(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "openapi", "native", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read the spec: %v", err)
	}
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse the spec: %v", err)
	}
	// The spec spells a parameter `{name}` and gin spells it `:name`, so the
	// two are compared in the router's spelling — the one `gated` is keyed on.
	braces := regexp.MustCompile(`\{([^}/]+)\}`)
	routes := map[string]map[string]json.RawMessage{}
	for path, ops := range doc.Paths {
		routes[braces.ReplaceAllString(path, ":$1")] = ops
	}

	for key := range gated {
		method, route, ok := strings.Cut(key, " ")
		if !ok {
			t.Errorf("gated key %q is not `METHOD /route`", key)
			continue
		}
		// The registry is keyed on the mounted route (`/v1/...`); the spec
		// spells the same endpoint without the mount, which the generated router
		// adds as its BaseURL.
		path, ok := routes[route]
		if !ok {
			path, ok = routes[strings.TrimPrefix(route, "/v1")]
		}
		if !ok {
			t.Errorf("gated route %q does not exist in the spec — the gate is off, not wrong", route)
			continue
		}
		if _, ok := path[strings.ToLower(method)]; !ok {
			t.Errorf("gated route %s %s does not exist in the spec — the gate is off, not wrong", method, route)
		}
	}
}

// The two operations whose absence caused this test to be written, named
// explicitly so a future rename has to change this line too.
func TestTheEnvWriteIsGated(t *testing.T) {
	op, ok := Lookup("PUT", "/v1/envs/:name")
	if !ok {
		t.Fatal("PUT /v1/envs/:name must be gated")
	}
	if op.ID != "env.update" {
		t.Fatalf("want env.update, got %q", op.ID)
	}
}
