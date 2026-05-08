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

// Package middleware provides E2B-compatible authentication middleware.
// It supports both X-API-Key (E2B standard) and AGENTBOX-API-KEY (backward-compatible).
package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	e2bgen "github.com/scitix/agent-sandbox/pkg/e2bcompat/gen"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
)

const (
	// E2BAPIKeyHeader is the E2B standard API key header.
	E2BAPIKeyHeader = "X-API-Key"

	// LegacyAPIKeyHeader is the legacy AgentBox API key header.
	LegacyAPIKeyHeader = "AGENTBOX-API-KEY"

	// AuthContextKey is the gin context key used to store AuthInfo.
	AuthContextKey = "auth"

	// DefaultNamespace is the default namespace when none is specified.
	DefaultNamespace = "default"
)

// NewE2BAuthMiddleware returns a gin.HandlerFunc that validates the API key
// using E2B-compatible header names and injects AuthInfo into the request context.
//
// It accepts both X-API-Key (E2B standard) and AGENTBOX-API-KEY (backward-compatible).
// When adminKeyMgr is nil, the middleware runs in dev mode and grants anonymous-admin access.
//
// iamSvc is used to resolve the namespace for global API keys (those with an empty
// namespace in the Secret). When non-nil and the key's namespace is empty, the
// middleware derives the effective namespace from the key's team+user metadata via
// IAM. This mirrors the behaviour of the native AgentBox auth middleware.
func NewE2BAuthMiddleware(adminKeyMgr *apikey.AdminKeyManager, keyStore apikey.KeyStore, iamSvc service.IAMService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Health endpoint is public; skip authentication.
		if c.Request.URL.Path == "/health" || strings.HasSuffix(c.Request.URL.Path, "/health") {
			c.Next()
			return
		}

		// Dev mode: no admin key manager → grant anonymous admin.
		if adminKeyMgr == nil {
			c.Set(AuthContextKey, domain.AuthInfo{
				Namespace: DefaultNamespace,
				Role:      apikey.RoleAdmin,
				User:      "anonymous-admin",
			})
			c.Next()
			return
		}

		// Check E2B-standard header first, then fall back to legacy header.
		providedKey := strings.TrimSpace(c.GetHeader(E2BAPIKeyHeader))
		if providedKey == "" {
			providedKey = strings.TrimSpace(c.GetHeader(LegacyAPIKeyHeader))
		}

		if providedKey == "" {
			writeError(c, http.StatusUnauthorized, "missing api key")
			return
		}

		// Check admin key first (constant-time comparison).
		if adminKeyMgr.IsAdminKey(providedKey) {
			c.Set(AuthContextKey, domain.AuthInfo{
				Namespace: DefaultNamespace,
				Role:      apikey.RoleAdmin,
				User:      "admin",
			})
			c.Next()
			return
		}

		// Fall back to KeyStore tenant validation.
		if keyStore == nil {
			writeError(c, http.StatusUnauthorized, "invalid api key")
			return
		}

		meta, err := keyStore.Validate(c.Request.Context(), providedKey)
		if err != nil {
			writeError(c, http.StatusUnauthorized, "invalid api key")
			return
		}

		// When the key's stored namespace is empty (global keys created via
		// Manager), resolve the effective namespace via IAM based on the key's
		// team+user metadata. This mirrors the native AgentBox auth middleware
		// and allows each Worker cluster to derive the correct namespace.
		ns := meta.Namespace
		if ns == "" && iamSvc != nil && meta.Team != "" && meta.User != "" {
			resolved, nsErr := iamSvc.ResolveNamespace(c.Request.Context(), meta.Team, meta.User)
			if nsErr == nil {
				ns = resolved
			}
		}
		if ns == "" {
			ns = DefaultNamespace
		}

		c.Set(AuthContextKey, domain.AuthInfo{
			Namespace: ns,
			Role:      meta.Role,
			User:      meta.User,
			Team:      meta.Team,
			QuotaURL:  meta.QuotaURL,
		})
		c.Next()
	}
}

// AuthFromContext retrieves AuthInfo from the gin context.
// Falls back to a default with Namespace="default" if not set.
func AuthFromContext(c *gin.Context) domain.AuthInfo {
	value, ok := c.Get(AuthContextKey)
	if !ok {
		return domain.AuthInfo{Namespace: DefaultNamespace}
	}
	auth, ok := value.(domain.AuthInfo)
	if !ok {
		return domain.AuthInfo{Namespace: DefaultNamespace}
	}
	if auth.Namespace == "" {
		auth.Namespace = DefaultNamespace
	}
	return auth
}

func writeError(c *gin.Context, statusCode int, message string) {
	c.AbortWithStatusJSON(statusCode, e2bgen.Error{Code: int32(statusCode), Message: message})
}
