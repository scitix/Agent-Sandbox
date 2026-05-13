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

package cluster

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const (
	testClusterUSEast = "us-east"
	testClusterEUWest = "eu-west"
)

func TestGatewayConfigURLs(t *testing.T) {
	gw := &GatewayConfig{
		NativeURL: "https://native.cluster-b.internal",
		E2BURL:    "https://e2b.cluster-b.internal",
		DataURL:   "https://data.cluster-b.internal",
	}

	if got := gw.NativeAPIBaseURL(); got != "https://native.cluster-b.internal" {
		t.Errorf("NativeAPIBaseURL() = %q", got)
	}
	if got := gw.E2BAPIBaseURL(); got != "https://e2b.cluster-b.internal" {
		t.Errorf("E2BAPIBaseURL() = %q", got)
	}
	if got := gw.DataPlaneBaseURL(); got != "https://data.cluster-b.internal" {
		t.Errorf("DataPlaneBaseURL() = %q", got)
	}
}

func TestMergedHeaders(t *testing.T) {
	cases := []struct {
		name string
		gw   GatewayConfig
		kind URLKind
		want map[string]string
	}{
		{
			name: "both empty returns nil",
			gw:   GatewayConfig{},
			kind: URLKindNative,
			want: nil,
		},
		{
			name: "only common headers",
			gw:   GatewayConfig{Headers: map[string]string{"X-Auth": "shared"}},
			kind: URLKindData,
			want: map[string]string{"X-Auth": "shared"},
		},
		{
			name: "only per-plane headers",
			gw:   GatewayConfig{NativeHeaders: map[string]string{"X-Native": "n"}},
			kind: URLKindNative,
			want: map[string]string{"X-Native": "n"},
		},
		{
			name: "common + per-plane merged (disjoint keys)",
			gw: GatewayConfig{
				Headers:     map[string]string{"X-Auth": "shared"},
				DataHeaders: map[string]string{"X-Data": "d"},
			},
			kind: URLKindData,
			want: map[string]string{"X-Auth": "shared", "X-Data": "d"},
		},
		{
			name: "per-plane overrides common on key conflict",
			gw: GatewayConfig{
				Headers:     map[string]string{"X-Auth": "shared", "X-Other": "o"},
				DataHeaders: map[string]string{"X-Auth": "data-only"},
			},
			kind: URLKindData,
			want: map[string]string{"X-Auth": "data-only", "X-Other": "o"},
		},
		{
			name: "unrelated per-plane override does not leak",
			gw: GatewayConfig{
				Headers:       map[string]string{"X-Auth": "shared"},
				NativeHeaders: map[string]string{"X-Auth": "native-only"},
			},
			kind: URLKindData,
			want: map[string]string{"X-Auth": "shared"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.gw.MergedHeaders(tc.kind)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("MergedHeaders() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestStoreGetSet(t *testing.T) {
	s := NewStore()

	// Empty store
	if _, ok := s.Get("cluster-a"); ok {
		t.Error("expected Get to return false on empty store")
	}

	// Set entries
	s.Set([]ClusterEntry{
		{ID: "cluster-a", Name: "Cluster A"},
		{ID: "cluster-b", Name: "Cluster B", Gateway: &GatewayConfig{
			NativeURL: "https://native.cluster-b.internal",
			DataURL:   "https://data.cluster-b.internal",
			Headers:   map[string]string{"X-Gateway-Auth": "token"},
		}},
	})

	a, ok := s.Get("cluster-a")
	if !ok || a.Name != "Cluster A" {
		t.Errorf("Get(cluster-a) = %+v, %v", a, ok)
	}

	b, ok := s.Get("cluster-b")
	if !ok || b.Gateway == nil {
		t.Fatal("Get(cluster-b) missing gateway")
	}
	if b.Gateway.Headers["X-Gateway-Auth"] != "token" {
		t.Errorf("gateway header mismatch: %v", b.Gateway.Headers)
	}

	// Unknown cluster
	if _, ok := s.Get("cluster-c"); ok {
		t.Error("expected Get to return false for unknown cluster")
	}
}

func TestStoreSetReplacesEntries(t *testing.T) {
	s := NewStore()
	s.Set([]ClusterEntry{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}})
	s.Set([]ClusterEntry{{ID: "c", Name: "C"}})

	if _, ok := s.Get("a"); ok {
		t.Error("expected old entry 'a' to be gone after Set")
	}
	if _, ok := s.Get("c"); !ok {
		t.Error("expected new entry 'c' to be present")
	}
}

func TestStoreSetSkipsEmptyID(t *testing.T) {
	s := NewStore()
	s.Set([]ClusterEntry{{ID: "", Name: "NoID"}, {ID: "valid", Name: "Valid"}})

	all := s.All()
	if len(all) != 1 {
		t.Errorf("expected 1 entry, got %d", len(all))
	}
}

func TestStoreAll(t *testing.T) {
	s := NewStore()
	s.Set([]ClusterEntry{
		{ID: "a", Name: "A"},
		{ID: "b", Name: "B"},
	})

	all := s.All()
	if len(all) != 2 {
		t.Errorf("All() returned %d entries, want 2", len(all))
	}
}

func TestStoreLoadFromFile(t *testing.T) {
	content := `clusters:
  - id: "cluster-a"
    name: "Cluster A"
    url: "https://a.example.com"
    selector: 'cluster="cluster-a"'
    gateway:
      nativeURL: "https://native.cluster-a.internal"
      dataURL: "https://data.cluster-a.internal"
      headers:
        X-Gateway-Auth: "secret"
      dataHeaders:
        X-Gateway-Auth: "data-secret"
  - id: "cluster-b"
    name: "Cluster B"
    url: "https://b.example.com"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "clusters.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewStore()
	if err := s.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	a, ok := s.Get("cluster-a")
	if !ok {
		t.Fatal("cluster-a not found")
	}
	if a.Selector != `cluster="cluster-a"` {
		t.Errorf("unexpected selector: %q", a.Selector)
	}
	if a.Gateway == nil {
		t.Fatal("cluster-a gateway is nil")
	}
	if a.Gateway.NativeAPIBaseURL() != "https://native.cluster-a.internal" {
		t.Errorf("unexpected NativeAPIBaseURL: %s", a.Gateway.NativeAPIBaseURL())
	}
	if a.Gateway.Headers["X-Gateway-Auth"] != "secret" {
		t.Errorf("unexpected gateway header: %v", a.Gateway.Headers)
	}
	merged := a.Gateway.MergedHeaders(URLKindData)
	if merged["X-Gateway-Auth"] != "data-secret" {
		t.Errorf("expected data override to win, got %v", merged)
	}

	b, ok := s.Get("cluster-b")
	if !ok {
		t.Fatal("cluster-b not found")
	}
	if b.Gateway != nil {
		t.Error("cluster-b should have nil gateway")
	}
}

func TestStoreLoadFromFileNotFound(t *testing.T) {
	s := NewStore()
	if err := s.LoadFromFile("/nonexistent/path.yaml"); err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestStoreLoadFromFileInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("{{invalid yaml"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewStore()
	if err := s.LoadFromFile(path); err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestStoreLoadFromData(t *testing.T) {
	data := []byte(`clusters:
  - id: "cluster-x"
    name: "Cluster X"
    gateway:
      nativeURL: "https://native.x.internal"
      dataURL: "https://data.x.internal"
`)
	s := NewStore()
	if err := s.LoadFromData(data); err != nil {
		t.Fatalf("LoadFromData: %v", err)
	}
	x, ok := s.Get("cluster-x")
	if !ok {
		t.Fatal("cluster-x not found")
	}
	if x.Gateway == nil || x.Gateway.NativeAPIBaseURL() != "https://native.x.internal" {
		t.Errorf("unexpected entry: %+v", x)
	}
}

func TestStoreLoadFromDataEmpty(t *testing.T) {
	s := NewStore()
	s.Set([]ClusterEntry{{ID: "a", Name: "A"}})

	// Empty data should clear the store.
	if err := s.LoadFromData(nil); err != nil {
		t.Fatalf("LoadFromData(nil): %v", err)
	}
	if len(s.All()) != 0 {
		t.Errorf("expected 0 entries after empty load, got %d", len(s.All()))
	}

	// Empty string
	if err := s.LoadFromData([]byte("")); err != nil {
		t.Fatalf("LoadFromData(empty): %v", err)
	}
	if len(s.All()) != 0 {
		t.Errorf("expected 0 entries after empty string load, got %d", len(s.All()))
	}
}

func TestStoreLoadFromDataInvalidYAML(t *testing.T) {
	s := NewStore()
	if err := s.LoadFromData([]byte("{{invalid yaml")); err == nil {
		t.Error("expected error for invalid YAML")
	}
}

// ---------------------------------------------------------------------------
// Registry index tests
// ---------------------------------------------------------------------------

func TestStoreRegistryIndex_LookupAndReplace(t *testing.T) {
	s := NewStore()
	s.ApplyConfig(ClusterConfig{
		Clusters: []ClusterEntry{
			{
				ID: testClusterUSEast,
				Registries: []RegistryEntry{
					{Host: "us-docker.pkg.dev", Type: "gar"},
					{Host: "us.internal.registry.io", Type: "internal"},
				},
			},
			{
				ID: testClusterEUWest,
				Registries: []RegistryEntry{
					{Host: "eu-docker.pkg.dev", Type: "gar"},
					{Host: "eu.internal.registry.io", Type: "internal"},
				},
			},
		},
	})

	// LookupRegistry — known hosts
	clusterID, typ, ok := s.LookupRegistry("us-docker.pkg.dev")
	if !ok || clusterID != testClusterUSEast || typ != "gar" {
		t.Errorf("LookupRegistry(us-docker.pkg.dev) = (%q, %q, %v), want (us-east, gar, true)", clusterID, typ, ok)
	}

	clusterID, typ, ok = s.LookupRegistry("eu.internal.registry.io")
	if !ok || clusterID != testClusterEUWest || typ != "internal" {
		t.Errorf("LookupRegistry(eu.internal.registry.io) = (%q, %q, %v), want (eu-west, internal, true)", clusterID, typ, ok)
	}

	// LookupRegistry — unknown host (public registry)
	_, _, ok = s.LookupRegistry("ghcr.io")
	if ok {
		t.Error("LookupRegistry(ghcr.io) should return ok=false")
	}

	// RegistryForType — find replacement target
	host, ok := s.RegistryForType(testClusterEUWest, "gar")
	if !ok || host != "eu-docker.pkg.dev" {
		t.Errorf("RegistryForType(eu-west, gar) = (%q, %v), want (eu-docker.pkg.dev, true)", host, ok)
	}

	// RegistryForType — type not present in cluster
	_, ok = s.RegistryForType(testClusterEUWest, "nonexistent-type")
	if ok {
		t.Error("RegistryForType(eu-west, nonexistent-type) should return ok=false")
	}
}

func TestStoreRegistryIndex_EmptyType(t *testing.T) {
	s := NewStore()
	s.ApplyConfig(ClusterConfig{
		Clusters: []ClusterEntry{
			{ID: testClusterUSEast, Registries: []RegistryEntry{{Host: "us.private.io", Type: ""}}},
			{ID: testClusterEUWest, Registries: []RegistryEntry{{Host: "eu.private.io", Type: ""}}},
		},
	})

	clusterID, typ, ok := s.LookupRegistry("us.private.io")
	if !ok || clusterID != testClusterUSEast || typ != "" {
		t.Errorf("LookupRegistry(us.private.io) = (%q, %q, %v), want (us-east, '', true)", clusterID, typ, ok)
	}

	host, ok := s.RegistryForType(testClusterEUWest, "")
	if !ok || host != "eu.private.io" {
		t.Errorf("RegistryForType(eu-west, '') = (%q, %v), want (eu.private.io, true)", host, ok)
	}
}

func TestStoreRegistryIndex_DuplicateTypeFirstWins(t *testing.T) {
	s := NewStore()
	s.ApplyConfig(ClusterConfig{
		Clusters: []ClusterEntry{
			{
				ID: testClusterEUWest,
				Registries: []RegistryEntry{
					{Host: "eu-first.pkg.dev", Type: "gar"},
					{Host: "eu-second.pkg.dev", Type: "gar"}, // duplicate type, should be ignored
				},
			},
		},
	})

	host, ok := s.RegistryForType(testClusterEUWest, "gar")
	if !ok || host != "eu-first.pkg.dev" {
		t.Errorf("RegistryForType(eu-west, gar) = (%q, %v), want (eu-first.pkg.dev, true)", host, ok)
	}
	// Second host must still be looked up (it IS registered in hostIndex)
	clusterID, _, ok := s.LookupRegistry("eu-second.pkg.dev")
	if !ok || clusterID != testClusterEUWest {
		t.Errorf("LookupRegistry(eu-second.pkg.dev) = (%q, %v), want (eu-west, true)", clusterID, ok)
	}
}

func TestStoreRegistryIndex_RebuiltOnApplyConfig(t *testing.T) {
	s := NewStore()
	s.ApplyConfig(ClusterConfig{
		Clusters: []ClusterEntry{
			{ID: testClusterUSEast, Registries: []RegistryEntry{{Host: "us-docker.pkg.dev", Type: "gar"}}},
		},
	})

	// Initial state: us-docker.pkg.dev known
	_, _, ok := s.LookupRegistry("us-docker.pkg.dev")
	if !ok {
		t.Fatal("expected us-docker.pkg.dev to be in index after first ApplyConfig")
	}

	// Replace with new config — old entry must be gone
	s.ApplyConfig(ClusterConfig{
		Clusters: []ClusterEntry{
			{ID: testClusterEUWest, Registries: []RegistryEntry{{Host: "eu-docker.pkg.dev", Type: "gar"}}},
		},
	})

	_, _, ok = s.LookupRegistry("us-docker.pkg.dev")
	if ok {
		t.Error("us-docker.pkg.dev should be gone after second ApplyConfig")
	}
	_, _, ok = s.LookupRegistry("eu-docker.pkg.dev")
	if !ok {
		t.Error("eu-docker.pkg.dev should be present after second ApplyConfig")
	}
}

func TestStoreRegistryIndex_LoadFromFile(t *testing.T) {
	content := `clusters:
  - id: "us-east"
    name: "US East"
    registries:
      - host: "us-docker.pkg.dev"
        type: "gar"
      - host: "us.internal.registry.io"
        type: "internal"
  - id: "eu-west"
    name: "EU West"
    registries:
      - host: "eu-docker.pkg.dev"
        type: "gar"
`
	dir := t.TempDir()
	path := dir + "/clusters.yaml"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewStore()
	if err := s.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	clusterID, typ, ok := s.LookupRegistry("us-docker.pkg.dev")
	if !ok || clusterID != "us-east" || typ != "gar" {
		t.Errorf("LookupRegistry after LoadFromFile = (%q, %q, %v), want (us-east, gar, true)", clusterID, typ, ok)
	}

	host, ok := s.RegistryForType("eu-west", "gar")
	if !ok || host != "eu-docker.pkg.dev" {
		t.Errorf("RegistryForType after LoadFromFile = (%q, %v), want (eu-docker.pkg.dev, true)", host, ok)
	}
}
