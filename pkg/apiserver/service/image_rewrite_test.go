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

package service

import (
	"testing"
)

const (
	testEUImage = "eu-docker.pkg.dev/myproject/myimage:v1.0"
	testUSImage = "us-docker.pkg.dev/myproject/myimage:v1.0"
)

// fakeRegistryStore is a test double for RegistryStore.
type fakeRegistryStore struct {
	// hostToMeta maps host → (clusterID, type)
	hostToMeta map[string][2]string
	// typeToHost maps "clusterID:type" → host
	typeToHost map[string]string
}

func (f *fakeRegistryStore) LookupRegistry(host string) (clusterID, typ string, ok bool) {
	if m, found := f.hostToMeta[host]; found {
		return m[0], m[1], true
	}
	return "", "", false
}

func (f *fakeRegistryStore) RegistryForType(clusterID, typ string) (host string, ok bool) {
	h, ok := f.typeToHost[clusterID+":"+typ]
	return h, ok
}

// buildFakeStore creates a fakeRegistryStore from a map of clusterID → []RegistryEntry.
// Each RegistryEntry is represented as {host, type}.
type registryEntrySpec struct{ host, typ string }

func buildFakeStore(clusters map[string][]registryEntrySpec) *fakeRegistryStore {
	store := &fakeRegistryStore{
		hostToMeta: make(map[string][2]string),
		typeToHost: make(map[string]string),
	}
	for clusterID, entries := range clusters {
		seenTypes := make(map[string]struct{})
		for _, e := range entries {
			if e.host == "" {
				continue
			}
			store.hostToMeta[e.host] = [2]string{clusterID, e.typ}
			key := clusterID + ":" + e.typ
			if _, seen := seenTypes[e.typ]; !seen {
				seenTypes[e.typ] = struct{}{}
				store.typeToHost[key] = e.host
			}
		}
	}
	return store
}

func TestRewriteImageForCluster_PublicRegistry(t *testing.T) {
	store := buildFakeStore(map[string][]registryEntrySpec{
		"us-east": {{host: "us-docker.pkg.dev", typ: "gar"}},
		"eu-west": {{host: "eu-docker.pkg.dev", typ: "gar"}},
	})

	// docker.io / ghcr.io are not in any cluster registry → no rewrite
	cases := []string{
		"ubuntu:22.04",
		"docker.io/library/ubuntu:22.04",
		"ghcr.io/org/repo:v1.0",
		"quay.io/prometheus/prometheus:v2.40.0",
		"nginx:latest",
	}
	for _, img := range cases {
		got := RewriteImageForCluster(img, "eu-west", store)
		if got != img {
			t.Errorf("public image %q should not be rewritten, got %q", img, got)
		}
	}
}

func TestRewriteImageForCluster_SameCluster(t *testing.T) {
	store := buildFakeStore(map[string][]registryEntrySpec{
		"eu-west": {{host: "eu-docker.pkg.dev", typ: "gar"}},
	})

	img := testEUImage
	got := RewriteImageForCluster(img, "eu-west", store)
	if got != img {
		t.Errorf("same-cluster image should not be rewritten, got %q", got)
	}
}

func TestRewriteImageForCluster_CrossClusterRewrites(t *testing.T) {
	store := buildFakeStore(map[string][]registryEntrySpec{
		"us-east": {{host: "us-docker.pkg.dev", typ: "gar"}},
		"eu-west": {{host: "eu-docker.pkg.dev", typ: "gar"}},
	})

	cases := []struct {
		image    string
		wantHost string
	}{
		{
			image:    "us-docker.pkg.dev/myproject/myimage:v1.0",
			wantHost: "eu-docker.pkg.dev",
		},
		{
			image: "us-docker.pkg.dev/myproject/myimage@sha256:e3b0c44298fc1c149afbf4c8996fb924" +
				"27ae41e4649b934ca495991b7852b855",
			wantHost: "eu-docker.pkg.dev",
		},
		{
			image:    "us-docker.pkg.dev/myproject/deep/path/image:latest",
			wantHost: "eu-docker.pkg.dev",
		},
	}
	for _, tc := range cases {
		got := RewriteImageForCluster(tc.image, "eu-west", store)
		expectedPrefix := tc.wantHost
		if got[:len(expectedPrefix)] != expectedPrefix {
			t.Errorf("image %q: expected rewritten to start with %q, got %q", tc.image, expectedPrefix, got)
		}
		// Path after host must be preserved
		originalPath := tc.image[len("us-docker.pkg.dev"):]
		gotPath := got[len(tc.wantHost):]
		if originalPath != gotPath {
			t.Errorf("image %q: path changed after rewrite: want %q got %q", tc.image, originalPath, gotPath)
		}
	}
}

func TestRewriteImageForCluster_NoMatchingTypeInCurrentCluster(t *testing.T) {
	store := buildFakeStore(map[string][]registryEntrySpec{
		"us-east": {{host: "us-docker.pkg.dev", typ: "gar"}},
		"eu-west": {{host: "eu.registry.company.com", typ: "internal"}}, // different type
	})

	img := testUSImage
	got := RewriteImageForCluster(img, "eu-west", store)
	// eu-west has no "gar" registry → keep original
	if got != img {
		t.Errorf("no matching type: expected original %q, got %q", img, got)
	}
}

func TestRewriteImageForCluster_EmptyTypeGroup(t *testing.T) {
	// Registries without type (empty string) only match each other
	store := buildFakeStore(map[string][]registryEntrySpec{
		"us-east": {{host: "us.private.registry.io", typ: ""}},
		"eu-west": {{host: "eu.private.registry.io", typ: ""}},
	})

	img := "us.private.registry.io/myimage:v1"
	got := RewriteImageForCluster(img, "eu-west", store)
	want := "eu.private.registry.io/myimage:v1"
	if got != want {
		t.Errorf("empty-type rewrite: want %q, got %q", want, got)
	}
}

func TestRewriteImageForCluster_MultipleTypesPerCluster(t *testing.T) {
	store := buildFakeStore(map[string][]registryEntrySpec{
		"us-east": {
			{host: "us-docker.pkg.dev", typ: "gar"},
			{host: "us.internal.registry.io", typ: "internal"},
		},
		"eu-west": {
			{host: "eu-docker.pkg.dev", typ: "gar"},
			{host: "eu.internal.registry.io", typ: "internal"},
		},
	})

	cases := []struct {
		image string
		want  string
	}{
		{"us-docker.pkg.dev/project/img:v1", "eu-docker.pkg.dev/project/img:v1"},
		{"us.internal.registry.io/team/img:v2", "eu.internal.registry.io/team/img:v2"},
	}
	for _, tc := range cases {
		got := RewriteImageForCluster(tc.image, "eu-west", store)
		if got != tc.want {
			t.Errorf("multi-type: image %q: want %q, got %q", tc.image, tc.want, got)
		}
	}
}

func TestRewriteImageForCluster_NilStore(t *testing.T) {
	img := testUSImage
	got := RewriteImageForCluster(img, "eu-west", nil)
	if got != img {
		t.Errorf("nil store: expected original %q, got %q", img, got)
	}
}

func TestRewriteImageForCluster_EmptyImage(t *testing.T) {
	store := buildFakeStore(map[string][]registryEntrySpec{
		"eu-west": {{host: "eu-docker.pkg.dev", typ: "gar"}},
	})
	got := RewriteImageForCluster("", "eu-west", store)
	if got != "" {
		t.Errorf("empty image: want empty, got %q", got)
	}
}

func TestRewriteImageForCluster_EmptyClusterID(t *testing.T) {
	store := buildFakeStore(map[string][]registryEntrySpec{
		"us-east": {{host: "us-docker.pkg.dev", typ: "gar"}},
	})
	img := "us-docker.pkg.dev/project/img:v1"
	got := RewriteImageForCluster(img, "", store)
	if got != img {
		t.Errorf("empty clusterID: expected original %q, got %q", img, got)
	}
}

func TestRegistryHost(t *testing.T) {
	cases := []struct {
		image string
		want  string
	}{
		// Explicit host — extracted as-is
		{"us-docker.pkg.dev/project/img:v1", "us-docker.pkg.dev"},
		{"registry.example.com:5000/myimage:v1", "registry.example.com:5000"},
		{
			"eu-docker.pkg.dev/project/img@sha256:e3b0c44298fc1c149afbf4c8996fb924" +
				"27ae41e4649b934ca495991b7852b855",
			"eu-docker.pkg.dev",
		},
		// Short names — docker.io is implicit, should return ""
		{"ubuntu:22.04", ""},
		{"nginx:latest", ""},
		// docker.io explicit — raw string starts with "docker.io"
		{"docker.io/library/ubuntu:22.04", "docker.io"},
		// ghcr.io — explicit host
		{"ghcr.io/org/repo:v1", "ghcr.io"},
	}
	for _, tc := range cases {
		got := registryHost(tc.image)
		if got != tc.want {
			t.Errorf("registryHost(%q) = %q, want %q", tc.image, got, tc.want)
		}
	}
}

func TestReplaceRegistryHost(t *testing.T) {
	cases := []struct {
		image   string
		oldHost string
		newHost string
		want    string
	}{
		{
			"us-docker.pkg.dev/project/img:v1",
			"us-docker.pkg.dev",
			"eu-docker.pkg.dev",
			"eu-docker.pkg.dev/project/img:v1",
		},
		{
			"registry.example.com:5000/img:v1",
			"registry.example.com:5000",
			"eu.registry.io",
			"eu.registry.io/img:v1",
		},
		// Prefix not present — no change
		{
			"other.registry.io/img:v1",
			"us-docker.pkg.dev",
			"eu-docker.pkg.dev",
			"other.registry.io/img:v1",
		},
	}
	for _, tc := range cases {
		got := replaceRegistryHost(tc.image, tc.oldHost, tc.newHost)
		if got != tc.want {
			t.Errorf("replaceRegistryHost(%q, %q, %q) = %q, want %q",
				tc.image, tc.oldHost, tc.newHost, got, tc.want)
		}
	}
}
