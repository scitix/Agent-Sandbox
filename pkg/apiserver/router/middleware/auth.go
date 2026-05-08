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
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
)

const (
	// APIKeyHeader is the HTTP header name for the API key.
	APIKeyHeader = "AGENTBOX-API-KEY"
	// AuthContextKey is the gin context key used to store AuthInfo.
	AuthContextKey   = "auth"
	DefaultNamespace = "default"

	// ImpersonateTeamHeader and ImpersonateUserHeader allow admin callers to
	// act on behalf of a specific user. When both headers are present and the
	// caller has admin role, the auth middleware resolves the target user's
	// namespace via IAM and overwrites AuthInfo accordingly. Non-admin callers
	// that supply these headers have them silently ignored.
	ImpersonateTeamHeader = "X-Impersonate-Team"
	ImpersonateUserHeader = "X-Impersonate-User"
)

// iamJWTClaims holds the custom claims embedded in IAM JWTs issued by the BFF.
type iamJWTClaims struct {
	Sub        string `json:"sub"`
	Name       string `json:"name"`
	Email      string `json:"email"`
	Role       string `json:"role"`
	Team       string `json:"team"`
	AuthMethod string `json:"authMethod"`
	User       string `json:"user"`
	jwt.RegisteredClaims
}

// applyImpersonation reads X-Impersonate-Team and X-Impersonate-User request
// headers. If both are present and the caller has admin role, it calls
// iamSvc.ResolveNamespace to look up the target user's namespace and then
// overwrites the caller's AuthInfo with the target identity. This allows admins
// to operate on behalf of any user via ordinary endpoints without needing
// separate admin-proxy routes.
//
// Behaviour summary:
//   - Both headers present, caller is admin, IAM OK  → AuthInfo overwritten, returns true
//   - Both headers present, caller is admin, IAM nil  → 503, returns false
//   - Both headers present, caller is admin, IAM err  → 503, returns false
//   - Only one header present (either team or user)   → headers ignored, returns true
//   - Both headers present, caller is non-admin       → headers silently ignored, returns true
//   - No headers present                              → no-op, returns true
func applyImpersonation(c *gin.Context, auth *domain.AuthInfo, iamSvc service.IAMService) bool {
	team := strings.TrimSpace(c.GetHeader(ImpersonateTeamHeader))
	user := strings.TrimSpace(c.GetHeader(ImpersonateUserHeader))
	if team == "" || user == "" {
		return true
	}
	if auth.Role != apikey.RoleAdmin {
		// Silently ignore impersonation headers for non-admin callers.
		return true
	}
	if iamSvc == nil {
		writeError(c, http.StatusServiceUnavailable, "IAM service not configured for impersonation")
		return false
	}
	ns, appErr := iamSvc.ResolveNamespace(c.Request.Context(), team, user)
	if appErr != nil {
		writeError(c, http.StatusServiceUnavailable, appErr.Message)
		return false
	}
	auth.Team = team
	auth.User = user
	auth.Namespace = ns
	c.Set(AuthContextKey, *auth)
	return true
}

// NewAuthenticateMiddleware returns a gin.HandlerFunc that validates the API key or IAM JWT
// and injects AuthInfo into the request context.
//
// adminKeyMgr handles admin key comparison; keyStore validates tenant tokens.
// jwtSecret is the HS256 secret shared with the BFF; when empty, JWT auth is skipped.
// iamSvc is used to resolve the namespace for JWT-authenticated users; may be nil
// (falls back to "default" namespace when absent).
// When adminKeyMgr is nil, the middleware runs in dev mode and grants
// anonymous-admin access to all requests.
//
// If the route has AdminKeyAuthScopes set (via oapi-codegen's per-route security
// annotation), the middleware additionally enforces that the caller has admin role.
//
// Admin callers may include X-Impersonate-Team and X-Impersonate-User request headers
// to act on behalf of a specific user. When both headers are present, the middleware
// resolves the target user's namespace via iamSvc and overwrites AuthInfo with the
// target identity. Non-admin callers that supply these headers have them silently ignored.
func NewAuthenticateMiddleware(adminKeyMgr *apikey.AdminKeyManager, keyStore apikey.KeyStore, jwtSecret string, iamSvc service.IAMService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Dev mode: no admin key manager → grant anonymous admin.
		if adminKeyMgr == nil {
			c.Set(AuthContextKey, domain.AuthInfo{
				Namespace:  DefaultNamespace,
				Role:       apikey.RoleAdmin,
				User:       "anonymous-admin",
				AuthMethod: "apikey",
			})
			// Dev mode has no IAM; impersonation headers are silently ignored.
			c.Next()
			return
		}

		// ── Try Bearer JWT first ──────────────────────────────────────────────
		if jwtSecret != "" {
			authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
			if strings.HasPrefix(authHeader, "Bearer ") {
				tokenStr := authHeader[len("Bearer "):]
				claims := &iamJWTClaims{}
				token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
					if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
						return nil, jwt.ErrSignatureInvalid
					}
					return []byte(jwtSecret), nil
				})
				if err != nil || !token.Valid {
					writeError(c, http.StatusUnauthorized, "invalid or expired jwt token")
					return
				}

				role := claims.Role
				if role == "" {
					role = apikey.RoleTenant
				}
				user := claims.User
				if user == "" {
					user = claims.Sub
				}
				team := claims.Team

				// Resolve namespace from backend (with cache) based on team+user.
				ns := DefaultNamespace
				if iamSvc != nil && user != "" {
					resolved, nsErr := iamSvc.ResolveNamespace(c.Request.Context(), team, user)
					if nsErr == nil {
						ns = resolved
					}
				}

				auth := domain.AuthInfo{
					Namespace:  ns,
					Role:       role,
					User:       user,
					Team:       team,
					Email:      claims.Email,
					Name:       claims.Name,
					AuthMethod: "jwt",
				}

				c.Set(AuthContextKey, auth)

				// Apply impersonation before admin scope enforcement so that
				// an impersonated identity can still pass the scope check.
				if !applyImpersonation(c, &auth, iamSvc) {
					return
				}

				// Enforce admin role for admin-only routes.
				if _, hasAdminScope := c.Get(gen.AdminKeyAuthScopes); hasAdminScope {
					if auth.Role != apikey.RoleAdmin {
						writeError(c, http.StatusForbidden, "admin access required")
						return
					}
				}

				c.Next()
				return
			}
		}

		// ── Fall back to API Key auth ─────────────────────────────────────────
		providedKey := strings.TrimSpace(c.GetHeader(APIKeyHeader))
		if providedKey == "" {
			writeError(c, http.StatusUnauthorized, "missing api key")
			return
		}

		// Check admin key first (constant-time).
		if adminKeyMgr.IsAdminKey(providedKey) {
			auth := domain.AuthInfo{
				Namespace:  DefaultNamespace,
				Role:       apikey.RoleAdmin,
				User:       "admin",
				Team:       "admin",
				AuthMethod: "apikey",
			}
			c.Set(AuthContextKey, auth)
			if !applyImpersonation(c, &auth, iamSvc) {
				return
			}
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

		// Tenant API keys must have team and user set.
		if meta.Team == "" || meta.User == "" {
			writeError(c, http.StatusUnauthorized, "api key is missing team or user metadata")
			return
		}

		// When the key's stored namespace is empty (global keys created via
		// Manager), resolve the effective namespace locally via IAM based on
		// the key's team+user metadata. This allows each Worker cluster to
		// derive the correct namespace for its own environment.
		ns := meta.Namespace
		if ns == "" && iamSvc != nil {
			resolved, nsErr := iamSvc.ResolveNamespace(c.Request.Context(), meta.Team, meta.User)
			if nsErr == nil {
				ns = resolved
			}
		}
		if ns == "" {
			ns = DefaultNamespace
		}

		c.Set(AuthContextKey, domain.AuthInfo{
			Namespace:  ns,
			Role:       meta.Role,
			User:       meta.User,
			Team:       meta.Team,
			QuotaURL:   meta.QuotaURL,
			AuthMethod: "apikey",
		})

		// If this route requires admin access (AdminKeyAuth security scheme),
		// enforce the admin role inline — no separate adminMiddleware needed.
		if _, hasAdminScope := c.Get(gen.AdminKeyAuthScopes); hasAdminScope {
			auth := AuthFromContext(c)
			if auth.Role != apikey.RoleAdmin {
				writeError(c, http.StatusForbidden, "admin api key required")
				return
			}
		}

		// Apply impersonation for tenant keys too — silently ignored for non-admin roles.
		tenantAuth := AuthFromContext(c)
		if !applyImpersonation(c, &tenantAuth, iamSvc) {
			return
		}

		c.Next()
	}
}

// NewAuthenticateMiddlewareFromKey is a convenience wrapper that constructs
// an AdminKeyManager from a raw admin key string before calling
// NewAuthenticateMiddleware. Pass an empty adminKey to enable dev mode.
func NewAuthenticateMiddlewareFromKey(adminKey string, keyStore apikey.KeyStore) gin.HandlerFunc {
	if adminKey == "" {
		// Dev mode: return middleware that always grants anonymous admin.
		return func(c *gin.Context) {
			c.Set(AuthContextKey, domain.AuthInfo{
				Namespace:  DefaultNamespace,
				Role:       apikey.RoleAdmin,
				User:       "anonymous-admin",
				AuthMethod: "apikey",
			})
			c.Next()
		}
	}
	adminKeyMgr := apikey.NewAdminKeyManager(adminKey)
	return NewAuthenticateMiddleware(adminKeyMgr, keyStore, "", nil)
}

// NewRequireAdminMiddleware returns a gin.HandlerFunc that rejects non-admin callers.
func NewRequireAdminMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := AuthFromContext(c)
		if auth.Role != apikey.RoleAdmin {
			writeError(c, http.StatusForbidden, "admin api key required")
			return
		}
		c.Next()
	}
}

// AuthFromContext retrieves AuthInfo from the gin context. Falls back to a
// default with Namespace="default" if not set or type-assert fails.
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
	c.AbortWithStatusJSON(statusCode, gen.ErrorResponse{Error: message})
}
