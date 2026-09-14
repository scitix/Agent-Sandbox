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

package logclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const sampleNDJSON = `{"_timestamp":"2026-09-04T10:00:00.5Z","container_name":"sandbox","log":"line one","pod_name":"p1"}
{"_timestamp":"2026-09-04T10:00:01Z","container_name":"sandbox","log":"line two","pod_name":"p1"}
{"_meta":true,"source":"external-logs"}
`

func TestQuery_ParsesNDJSONAndSkipsMeta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, sampleNDJSON)
	}))
	defer srv.Close()

	c := New(Config{URL: srv.URL, Token: "tok"})
	entries, err := c.Query(context.Background(), QueryOptions{
		PodName: "p1", Start: time.Now().Add(-time.Hour), End: time.Now()})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("the terminal meta line is not a log entry; got %d entries", len(entries))
	}
	if entries[0].Log != "line one" || entries[0].Timestamp.IsZero() {
		t.Fatalf("unexpected first entry: %+v", entries[0])
	}
}

// Scoping is not optional: pod names are unique per cluster, not globally, so
// an unscoped query can return another cluster's output for the same name.
func TestQuery_SendsPodAndClusterFilters(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, "")
	}))
	defer srv.Close()

	c := New(Config{URL: srv.URL, Token: "tok"})
	if _, err := c.Query(context.Background(), QueryOptions{
		PodName: "p1",
		Filters: map[string]string{"cluster": "prod-foo", "region": "region-a"},
		Start:   time.Unix(1000, 0), End: time.Unix(2000, 0)}); err != nil {
		t.Fatalf("query: %v", err)
	}

	filters, _ := got["filters"].(map[string]any)
	for _, key := range []string{"pod_name", "cluster", "region"} {
		m, ok := filters[key].(map[string]any)
		if !ok {
			t.Fatalf("filter %q missing: %+v", key, filters)
		}
		if m["op"] != "eq" {
			t.Fatalf("filter %q must be an equality match, got %v", key, m["op"])
		}
	}
	if got["kind"] != "container_stdout" {
		t.Fatalf("unexpected kind: %v", got["kind"])
	}
}

func TestQuery_SignedSchemeWhenAppIDIsSet(t *testing.T) {
	var hdrs http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdrs = r.Header.Clone()
		_, _ = io.WriteString(w, "")
	}))
	defer srv.Close()

	c := New(Config{URL: srv.URL, Token: "tok", AppID: "agent-sandbox"})
	if _, err := c.Query(context.Background(), QueryOptions{
		PodName: "p1", Start: time.Unix(0, 0), End: time.Unix(1, 0)}); err != nil {
		t.Fatalf("query: %v", err)
	}
	for _, h := range []string{"Signature", "Appid", "Timestamp", "Randstr"} {
		if hdrs.Get(h) == "" {
			t.Errorf("signed scheme must send %s", h)
		}
	}
	// The raw token must never travel; only the digest does.
	if strings.Contains(hdrs.Get("Signature"), "tok") {
		t.Error("signature must not embed the token")
	}
	if hdrs.Get("Authorization") != "" {
		t.Error("signed scheme must not also send a bearer token")
	}
}

func TestQuery_BearerSchemeWhenAppIDIsEmpty(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, "")
	}))
	defer srv.Close()

	c := New(Config{URL: srv.URL, Token: "tok"})
	if _, err := c.Query(context.Background(), QueryOptions{
		PodName: "p1", Start: time.Unix(0, 0), End: time.Unix(1, 0)}); err != nil {
		t.Fatalf("query: %v", err)
	}
	if auth != "Bearer tok" {
		t.Fatalf("unexpected auth header %q", auth)
	}
}

func TestQuery_RespectsLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, sampleNDJSON)
	}))
	defer srv.Close()

	c := New(Config{URL: srv.URL, Token: "tok"})
	entries, err := c.Query(context.Background(), QueryOptions{
		PodName: "p1", Start: time.Unix(0, 0), End: time.Unix(1, 0), Limit: 1})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected the limit to be honoured, got %d", len(entries))
	}
}

func TestReady_RequiresURLAndToken(t *testing.T) {
	if New(Config{}).Ready() {
		t.Error("an unconfigured client must not be ready")
	}
	if New(Config{URL: "https://x"}).Ready() {
		t.Error("a URL without a token is not usable")
	}
	if !New(Config{URL: "https://x", Token: "t"}).Ready() {
		t.Error("expected ready")
	}
}

func TestProjectFor(t *testing.T) {
	cases := []struct {
		name         string
		splitProject bool
		namespace    string
		want         string
	}{
		{"unsharded cluster leaves the endpoint's own project alone", false, "t-team-a", ""},
		{"unsharded cluster, platform namespace", false, "agentbox-system", ""},
		{"sharded cluster sends a tenant namespace to the tenant store", true, "t-team-a", TenantProject},
		{"sharded cluster sends everything else to the platform store", true, "agentbox-system", PlatformProject},
		{"the prefix must be a prefix, not a substring", true, "team-t-a", PlatformProject},
		{"an empty namespace is not a tenant", true, "", PlatformProject},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ProjectFor(tc.splitProject, tc.namespace); got != tc.want {
				t.Fatalf("ProjectFor(%v, %q) = %q, want %q", tc.splitProject, tc.namespace, got, tc.want)
			}
		})
	}
}

// Two `project` parameters in one URL is not an error the server reports; it
// picks one and answers 200 with whatever that store holds.
func TestWithProject_ReplacesRatherThanAppends(t *testing.T) {
	got, err := withProject("https://logs.example.com/download?project=default", "internal", "")
	if err != nil {
		t.Fatalf("withProject: %v", err)
	}
	if got != "https://logs.example.com/download?project=internal" {
		t.Fatalf("override must replace the endpoint's own project, got %q", got)
	}

	got, err = withProject("https://logs.example.com/download?project=default", "", "")
	if err != nil {
		t.Fatalf("withProject: %v", err)
	}
	if got != "https://logs.example.com/download?project=default" {
		t.Fatalf("no override must leave the endpoint untouched, got %q", got)
	}

	got, err = withProject("https://logs.example.com/download", "", "fallback")
	if err != nil {
		t.Fatalf("withProject: %v", err)
	}
	if got != "https://logs.example.com/download?project=fallback" {
		t.Fatalf("configured project must apply when the URL carries none, got %q", got)
	}
}

// An empty result is indistinguishable from a wrong question at the transport,
// so the caller has to be able to show what it asked.
func TestQueryOptions_Scope(t *testing.T) {
	opts := QueryOptions{
		PodName: "pod-1",
		Filters: map[string]string{"region": "region-a", "cluster": "prod-foo"},
		Start:   time.Unix(0, 0), End: time.Unix(60, 0),
	}
	got := opts.Scope("default")
	for _, want := range []string{
		"project=default", "cluster=prod-foo", "region=region-a", "pod_name=pod-1",
		"1970-01-01T00:00:00Z..1970-01-01T00:01:00Z",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("scope %q is missing %q", got, want)
		}
	}
	if s := (QueryOptions{Project: "internal"}).Scope("default"); !strings.Contains(s, "project=internal") {
		t.Errorf("the per-query override is what was actually sent, got %q", s)
	}
}

// One unparseable line must not cost the caller the rest of the output.
func TestDecodeNDJSON_SkipsMalformedLines(t *testing.T) {
	entries := decodeNDJSON(strings.NewReader(
		"{\"log\":\"good\"}\nnot json\n{\"log\":\"also good\"}\n"), 0)
	if len(entries) != 2 {
		t.Fatalf("expected the surrounding lines to survive, got %d", len(entries))
	}
}
