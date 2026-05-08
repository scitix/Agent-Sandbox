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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
)

func TestNewCrossClusterForwarder_NilWhenDisabled(t *testing.T) {
	store := cluster.NewStore()

	// nil store → nil forwarder
	if f := NewCrossClusterForwarder(nil, "local"); f != nil {
		t.Fatal("expected nil forwarder when store is nil")
	}
	// empty localClusterID → nil forwarder
	if f := NewCrossClusterForwarder(store, ""); f != nil {
		t.Fatal("expected nil forwarder when localClusterID is empty")
	}
	// both set → non-nil
	if f := NewCrossClusterForwarder(store, "local"); f == nil {
		t.Fatal("expected non-nil forwarder")
	}
}

// newTestForwarder creates a CrossClusterForwarder pointing at a httptest.Server.
func newTestForwarder(t *testing.T, handler http.Handler) (*CrossClusterForwarder, *httptest.Server) {
	t.Helper()
	ts := httptest.NewServer(handler)

	store := cluster.NewStore()
	store.Set([]cluster.ClusterEntry{
		{
			ID: "cluster-b",
			Gateway: &cluster.GatewayConfig{
				NativeURL: ts.URL + "/api",
				E2BURL:    ts.URL,
				DataURL:   ts.URL + "/data",
				Headers:   map[string]string{"X-Gateway-Token": "gw-secret"},
			},
		},
	})

	f := NewCrossClusterForwarder(store, "cluster-a")
	if f == nil {
		t.Fatal("expected non-nil forwarder")
	}
	return f, ts
}

// newGinContext creates a gin.Context backed by a real HTTP request and recorder.
func newGinContext(method, target string, body io.Reader) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	gc, _ := gin.CreateTestContext(w)
	gc.Request = httptest.NewRequest(method, target, body)
	return gc, w
}

func TestForward_ClusterNotFound(t *testing.T) {
	f, ts := newTestForwarder(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not have been called")
	}))
	defer ts.Close()

	gc, w := newGinContext(http.MethodGet, "/v1/sandboxes/sbx-1", nil)
	f.Forward(gc, "nonexistent", URLKindNative, nil)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

func TestForward_TransparentProxy(t *testing.T) {
	var gotMethod, gotPath, gotGatewayToken, gotSourceCluster, gotAPIKey string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotGatewayToken = r.Header.Get("X-Gateway-Token")
		gotSourceCluster = r.Header.Get("X-Source-Cluster")
		gotAPIKey = r.Header.Get("AGENTBOX-API-KEY")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"sandboxId":"cluster-b.abc"}`))
	})

	f, ts := newTestForwarder(t, handler)
	defer ts.Close()

	// The gin request path does NOT include the ControlPlanePrefix ("/api") —
	// the router strips it before dispatching. NativeAPIBaseURL() = ts.URL+"/api",
	// so the forwarded URL becomes ts.URL+"/api"+"/v1/sandboxes/cluster-b.abc".
	gc, w := newGinContext(http.MethodGet, "/v1/sandboxes/cluster-b.abc", nil)
	gc.Request.Header.Set("AGENTBOX-API-KEY", "agbx_testkey")
	gc.Request.Header.Set("Authorization", "Bearer tok")

	f.Forward(gc, "cluster-b", URLKindNative, nil)

	// Verify the upstream received the right request.
	if gotMethod != http.MethodGet {
		t.Errorf("expected GET, got %s", gotMethod)
	}
	if gotPath != "/api/v1/sandboxes/cluster-b.abc" {
		t.Errorf("unexpected upstream path: %s", gotPath)
	}
	if gotGatewayToken != "gw-secret" {
		t.Errorf("expected gateway token, got %q", gotGatewayToken)
	}
	if gotSourceCluster != "cluster-a" {
		t.Errorf("expected X-Source-Cluster=cluster-a, got %q", gotSourceCluster)
	}
	if gotAPIKey != "agbx_testkey" {
		t.Errorf("expected api key forwarded, got %q", gotAPIKey)
	}

	// Verify the response was written to the gin writer.
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "cluster-b.abc") {
		t.Errorf("expected body to contain sandbox ID, got: %s", body)
	}
}

func TestForward_RemoteStatusPassedThrough(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	})

	f, ts := newTestForwarder(t, handler)
	defer ts.Close()

	gc, w := newGinContext(http.MethodGet, "/v1/sandboxes/cluster-b.missing", nil)
	f.Forward(gc, "cluster-b", URLKindNative, nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 passed through, got %d", w.Code)
	}
}

func TestForward_E2BURLKind(t *testing.T) {
	var gotPath string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	})

	f, ts := newTestForwarder(t, handler)
	defer ts.Close()

	gc, w := newGinContext(http.MethodPost, "/v1/sandboxes", strings.NewReader(`{}`))
	gc.Request.Header.Set("Content-Type", "application/json")
	f.Forward(gc, "cluster-b", URLKindE2B, nil)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", w.Code)
	}
	if gotPath != "/v1/sandboxes" {
		t.Errorf("unexpected upstream path: %s", gotPath)
	}
}

// TestForward_MergedNativeHeaders verifies that NativeHeaders overrides common
// Headers for a URLKindNative request and does not leak for E2B calls.
func TestForward_MergedNativeHeaders(t *testing.T) {
	var gotAuth, gotShared, gotNative string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("X-Gateway-Token")
		gotShared = r.Header.Get("X-Shared")
		gotNative = r.Header.Get("X-Native-Only")
		w.WriteHeader(http.StatusOK)
	})

	ts := httptest.NewServer(handler)
	defer ts.Close()

	store := cluster.NewStore()
	store.Set([]cluster.ClusterEntry{
		{
			ID: "cluster-b",
			Gateway: &cluster.GatewayConfig{
				NativeURL:     ts.URL + "/api",
				E2BURL:        ts.URL,
				Headers:       map[string]string{"X-Gateway-Token": "common", "X-Shared": "yes"},
				NativeHeaders: map[string]string{"X-Gateway-Token": "native-only", "X-Native-Only": "n"},
				E2BHeaders:    map[string]string{"X-Gateway-Token": "e2b-only"},
			},
		},
	})
	f := NewCrossClusterForwarder(store, "cluster-a")

	// Native request: expect the native override to win and X-Native-Only to be present.
	gc, _ := newGinContext(http.MethodGet, "/v1/sandboxes", nil)
	f.Forward(gc, "cluster-b", URLKindNative, nil)
	if gotAuth != "native-only" {
		t.Errorf("native: X-Gateway-Token = %q, want native-only", gotAuth)
	}
	if gotShared != "yes" {
		t.Errorf("native: X-Shared = %q, want yes", gotShared)
	}
	if gotNative != "n" {
		t.Errorf("native: X-Native-Only = %q, want n", gotNative)
	}

	// E2B request on the same forwarder: NativeHeaders must NOT leak; E2BHeaders wins.
	gotAuth, gotShared, gotNative = "", "", ""
	gc2, _ := newGinContext(http.MethodGet, "/v1/sandboxes", nil)
	f.Forward(gc2, "cluster-b", URLKindE2B, nil)
	if gotAuth != "e2b-only" {
		t.Errorf("e2b: X-Gateway-Token = %q, want e2b-only", gotAuth)
	}
	if gotNative != "" {
		t.Errorf("e2b: X-Native-Only leaked, got %q", gotNative)
	}
}
