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

	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
	"github.com/scitix/agent-sandbox/pkg/wsproxy/config"
	"github.com/scitix/agent-sandbox/pkg/wsproxy/server"
)

func newTestInternalServer() *http.Server {
	cfg := &config.Config{
		InternalAddr: ":9004",
		// Empty JWTSecret + ManagerToken → dev mode (anonymous admin)
	}
	return server.NewInternalServer(cfg, server.RouterDeps{})
}

func TestInternalServer_Ping(t *testing.T) {
	srv := newTestInternalServer()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != "ok" {
		t.Fatalf("expected body \"ok\", got %q", body)
	}
}

func TestInternalServer_Metrics(t *testing.T) {
	srv := newTestInternalServer()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestInternalServer_APIKeysRequiresAuth(t *testing.T) {
	// A non-empty AdminKey activates the real auth middleware (no dev-mode bypass).
	// With no auth header on the request, the middleware must return 401.
	cfg := &config.Config{
		InternalAddr: ":9004",
	}
	srv := server.NewInternalServer(cfg, server.RouterDeps{
		AdminKeyMgr: apikey.NewAdminKeyManager("test-admin-key"),
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/api-keys", nil)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}
