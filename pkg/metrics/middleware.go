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

package metrics

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
)

// counts and latencies.  api should be "native" or "e2b" to distinguish
// between the two API servers; this avoids path-collision ambiguity when both
// servers expose routes with identical patterns (e.g. /sandboxes/:id).
func GinPrometheusMiddleware(api string) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		// Use FullPath() to get the route template (e.g. "/v1/sandboxes/:sandboxId")
		// rather than the concrete URL, to avoid high-cardinality label values.
		path := c.FullPath()
		if path == "" {
			path = "unknown"
		}

		labels := prometheus.Labels{
			"method":      c.Request.Method,
			"path":        path,
			"status_code": strconv.Itoa(c.Writer.Status()),
			"api":         api,
		}
		HTTPRequestsTotal.With(labels).Inc()
		HTTPRequestDuration.With(labels).Observe(time.Since(start).Seconds())
	}
}
