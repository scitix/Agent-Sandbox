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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
)

// StrictBody refuses a write whose body carries a field the schema does not have.
//
// The spec says `additionalProperties: false` on the bodies that were just
// unified, but that is a claim about documentation until something enforces it:
// gin binds with encoding/json's defaults, which DROP an unknown field and
// report success. A caller who mistypes `override` gets a 200 and no error, and
// the only symptom is a setting that never took effect — which is the failure
// mode the one-body contract exists to remove.
//
// Registered per route, the same way the approval gate is and for the same
// reason: it needs the route template (`c.FullPath()`) to know which type to
// decode into. Scoped to the bodies that declare `additionalProperties: false`
// rather than switched on for the whole API: the rest of it has not promised
// this, and turning a tolerance off for endpoints nobody asked about is how a
// release breaks a client silently.
func StrictBody() gin.HandlerFunc {
	return func(c *gin.Context) {
		factory, guarded := strictBodies[c.Request.Method+" "+c.FullPath()]
		if !guarded {
			c.Next()
			return
		}
		raw, err := c.GetRawData()
		if err != nil {
			// Not this middleware's error to phrase: the wrapper below reads the
			// body itself and reports what it finds.
			c.Next()
			return
		}
		// GetRawData consumes the body. The generated wrapper still has to bind
		// it, so it goes straight back.
		c.Request.Body = io.NopCloser(bytes.NewReader(raw))
		if len(bytes.TrimSpace(raw)) == 0 {
			c.Next()
			return
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(factory()); err != nil {
			if field, ok := unknownField(err); ok {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
					"error": fmt.Sprintf(
						"unknown field %q: this body has no such field, and a field the schema does not have would be dropped without a word — check the spelling, or `abx %s --help` for the shape",
						field, helpFor(c.FullPath())),
				})
				return
			}
			// Everything else (a type error, truncated JSON) is the handler's to
			// report, with its own wording.
		}
		c.Next()
	}
}

// strictBodies names the guarded routes. A route the router does not have would
// simply never match — the middleware fails open, which is the same direction as
// the spec: the schema is the contract, and this only makes it enforced.
var strictBodies = map[string]func() any{
	"POST /v1/envs":                             func() any { return &gen.CreateSandboxEnvRequest{} },
	"PUT /v1/envs/:name":                        func() any { return &gen.UpdateSandboxEnvRequest{} },
	"POST /v1/envs/:name/sandboxpools":          func() any { return &gen.CreateEnvSandboxPoolRequest{} },
	"PUT /v1/envs/:name/sandboxpools/:poolName": func() any { return &gen.UpdateEnvSandboxPoolRequest{} },
}

// unknownField pulls the field name out of encoding/json's refusal.
func unknownField(err error) (string, bool) {
	const prefix = `json: unknown field "`
	msg := err.Error()
	if !strings.HasPrefix(msg, prefix) {
		return "", false
	}
	return strings.TrimSuffix(strings.TrimPrefix(msg, prefix), `"`), true
}

// helpFor turns a route template into the command a person would type.
func helpFor(route string) string {
	switch route {
	case "/v1/envs", "/v1/envs/:name":
		return "envs"
	case "/v1/envs/:name/sandboxpools", "/v1/envs/:name/sandboxpools/:poolName":
		return "envs <env> pools"
	default:
		return "envs"
	}
}
