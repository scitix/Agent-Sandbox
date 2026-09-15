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

package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
)

// A body that carries a field the schema does not have is refused, not decoded
// with the field dropped. `additionalProperties: false` says so; nothing used to
// enforce it, so a mistyped field was a success with the value silently missing.
func TestStrictBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	group := engine.Group("/v1", StrictBody())
	group.POST("/envs", func(c *gin.Context) {
		var body gen.CreateSandboxEnvRequest
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, gin.H{"name": body.Name})
	})

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/envs", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		return w
	}

	t.Run("a body the schema has is bound as usual", func(t *testing.T) {
		w := post(`{"name":"demo","templateRef":{"name":"e2b-envd"}}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
	})

	t.Run("an unknown top-level field is refused by name", func(t *testing.T) {
		w := post(`{"name":"demo","templateRef":{"name":"e2b-envd"},"override":{}}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 — the field was silently dropped", w.Code)
		}
		if !strings.Contains(w.Body.String(), "override") {
			t.Fatalf("the refusal should name the field, got %s", w.Body.String())
		}
	})

	t.Run("so is one nested inside", func(t *testing.T) {
		w := post(`{"name":"demo","templateRef":{"name":"e2b-envd","versionPinned":"1"}}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (a typo one level down is the same bug)", w.Code)
		}
	})

	t.Run("an empty body still reaches the handler", func(t *testing.T) {
		// The middleware is about fields, not about presence: "name is required"
		// is the handler's sentence to write, and it says more than this can.
		w := post(``)
		if w.Code == http.StatusNotFound {
			t.Fatalf("the route should still be reached, got %d", w.Code)
		}
	})
}
