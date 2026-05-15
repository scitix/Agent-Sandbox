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

package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
	"github.com/scitix/agent-sandbox/pkg/wsproxy/config"
	"github.com/scitix/agent-sandbox/pkg/wsproxy/server"
)

// newTestStore builds a cluster.Store with a single fake entry.
func newTestStore(id, rawURL string) *cluster.Store {
	s := cluster.NewStore()
	s.Set([]cluster.ClusterEntry{{ID: id, Name: id, URL: rawURL}})
	return s
}

// terminalHandler extracts the underlying handler from a terminal server for
// use with httptest — we don't actually start the server.
func terminalHandler(store *cluster.Store) http.Handler {
	cfg := &config.Config{ListenAddr: ":9003"}
	srv := server.NewTerminalServer(cfg, store)
	return srv.Handler
}

func TestTerminalProxy_InvalidPath(t *testing.T) {
	store := cluster.NewStore()
	h := terminalHandler(store)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/not-a-ws-path", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestTerminalProxy_MissingToken(t *testing.T) {
	store := newTestStore("c1", "http://10.0.0.1:8080")
	h := terminalHandler(store)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/ws/clusters/c1/sandboxes/sb1/terminal", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestTerminalProxy_ClusterNotFound(t *testing.T) {
	store := cluster.NewStore() // empty store
	h := terminalHandler(store)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/ws/clusters/unknown/sandboxes/sb1/terminal?token=abc", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

func TestTerminalProxy_PingEndpoint(t *testing.T) {
	store := cluster.NewStore()
	h := terminalHandler(store)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("body = %q, want ok", rec.Body.String())
	}
}

func TestParseTerminalPath(t *testing.T) {
	cases := []struct {
		path     string
		wantOK   bool
		wantCID  string
		wantSBID string
	}{
		{"/ws/clusters/c1/sandboxes/sb1/terminal", true, "c1", "sb1"},
		{"/prefix/ws/clusters/myCluster/sandboxes/mySandbox/terminal", true, "myCluster", "mySandbox"},
		{"/ws/clusters/c1/sandboxes/sb1/other", false, "", ""},
		{"/notws/clusters/c1/sandboxes/sb1/terminal", false, "", ""},
		{"/ws/clusters/c1/sandboxes/terminal", false, "", ""},
	}

	for _, tc := range cases {
		cid, sbid, ok := server.ParseTerminalPath(tc.path)
		if ok != tc.wantOK {
			t.Errorf("path=%q: ok=%v, want %v", tc.path, ok, tc.wantOK)
			continue
		}
		if ok && (cid != tc.wantCID || sbid != tc.wantSBID) {
			t.Errorf("path=%q: got (%q,%q), want (%q,%q)",
				tc.path, cid, sbid, tc.wantCID, tc.wantSBID)
		}
	}
}
