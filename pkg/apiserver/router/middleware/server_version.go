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

import "github.com/gin-gonic/gin"

const ServerVersionHeader = "X-AgentBox-Server-Version"

// NewServerVersionMiddleware returns a middleware that stamps every response
// with the running server version in the X-AgentBox-Server-Version header.
// Clients can inspect this header (e.g. on /ping before auth) to detect
// version mismatches before making write requests.
func NewServerVersionMiddleware(version string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header(ServerVersionHeader, version)
		c.Next()
	}
}
