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
	"fmt"
	"net/http"

	"github.com/Masterminds/semver/v3"
	"github.com/gin-gonic/gin"
)

const (
	// ClientVersionHeader is the HTTP header clients use to declare their version.
	ClientVersionHeader = "X-AgentBox-Client-Version"

	// MinClientVersion is the minimum client version required for write operations.
	// Clients below this version receive 426 Upgrade Required.
	//
	// Set to "0.0.0" during the grace period — all clients pass.
	// Bump this when a breaking API change is released (e.g. "0.2.0").
	MinClientVersion = "0.0.0"

	// DefaultClientVersion is assumed when the header is missing.
	// This makes old clients (that don't send the header) appear as version 0.0.0.
	DefaultClientVersion = "0.0.0"
)

// NewVersionCheckMiddleware returns a Gin middleware that enforces a minimum
// client version on write operations (POST, PUT, PATCH, DELETE).
//
// Exemptions:
//   - Requests without an AGENTBOX-API-KEY header are assumed to be
//     Dashboard/JWT users and are always allowed (they are deployed
//     alongside the server).
//   - Read-only methods (GET, HEAD, OPTIONS) are never blocked.
//
// When the client version is below MinClientVersion, the middleware
// responds with 426 Upgrade Required and a JSON error body.
func NewVersionCheckMiddleware() gin.HandlerFunc {
	minVersion := semver.MustParse(MinClientVersion)

	return func(c *gin.Context) {
		// Exempt non-SDK clients (Dashboard, JWT-auth browsers).
		// SDK/CLI clients always send AGENTBOX-API-KEY.
		if c.GetHeader(APIKeyHeader) == "" {
			c.Next()
			return
		}

		// Only enforce on write operations.
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
			return
		}

		// Read the client version header; default to 0.0.0 if missing.
		raw := c.GetHeader(ClientVersionHeader)
		if raw == "" {
			raw = DefaultClientVersion
		}

		clientVer, err := semver.NewVersion(raw)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUpgradeRequired, gin.H{
				"error":  fmt.Sprintf("invalid %s header: %q is not a valid semver", ClientVersionHeader, raw),
				"detail": fmt.Sprintf("Please upgrade your client to version >= %s", MinClientVersion),
			})
			return
		}

		if clientVer.LessThan(minVersion) {
			c.AbortWithStatusJSON(http.StatusUpgradeRequired, gin.H{
				"error":  fmt.Sprintf("client version %s is no longer supported, minimum required: %s", clientVer, MinClientVersion),
				"detail": "Please upgrade your abx CLI or Python SDK to the latest version.",
			})
			return
		}

		c.Next()
	}
}
