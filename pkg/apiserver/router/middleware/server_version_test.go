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
	"testing"

	"github.com/gin-gonic/gin"
)

// The header must be present on every response, including when the version is
// the empty string. gin's Context.Header() deletes a header it is handed an
// empty value for, so an unguarded pass-through sent no header at all — which
// is what made the console read every cluster as "unknown" even though the
// request had reached a healthy backend.
func TestServerVersionMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name    string
		version string
		want    string
	}{
		{name: "an image tag survives verbatim", version: "develop-abc1234", want: "develop-abc1234"},
		{name: "a semver survives verbatim", version: "0.2.0", want: "0.2.0"},
		{name: "an unset version is named, not dropped", version: "", want: "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Use(NewServerVersionMiddleware(tc.version))
			r.GET("/ping", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))

			if got := w.Header().Get(ServerVersionHeader); got != tc.want {
				t.Errorf("%s = %q, want %q", ServerVersionHeader, got, tc.want)
			}
		})
	}
}
