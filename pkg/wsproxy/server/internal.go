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

// Package server provides the two HTTP servers for wsproxy:
//   - terminal.go: :9003 WebSocket terminal reverse-proxy
//   - internal.go: :9004 internal management API (Gin + OpenAPI-generated routes)
package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/apiserver/router/middleware"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
	"github.com/scitix/agent-sandbox/pkg/utils/httplog"
	"github.com/scitix/agent-sandbox/pkg/wsproxy/config"
	wsproxygen "github.com/scitix/agent-sandbox/pkg/wsproxy/gen"
	"github.com/scitix/agent-sandbox/pkg/wsproxy/notify"
	"github.com/scitix/agent-sandbox/pkg/wsproxy/syncmgr"
	"github.com/scitix/agent-sandbox/pkg/wsproxy/syncmgr/handlers"
)

// RouterDeps bundles all dependencies required to build the internal HTTP router.
type RouterDeps struct {
	SyncManager  *syncmgr.SyncManager
	AdminKeyMgr  *apikey.AdminKeyManager
	KeyStore     syncmgr.KeyStore
	JWTSecret    string
	ManagerToken string
	IAMService   service.IAMService

	// Notify serves the daily-report / idle-alert admin API. Nil when the
	// notification service is not configured (no Prometheus URL), in which
	// case those routes answer 503 rather than being unregistered.
	Notify *notify.Service

	// ManagedAgentAPI serves the console's ManagedAgent CRUD. Nil when the
	// ManagedAgent controller is not enabled, in which case the routes are not
	// registered at all rather than answering with an empty list — a console
	// pointed at a control plane without the feature should see a 404, not the
	// impression that the tenant simply owns no agents.
	ManagedAgentAPI *ManagedAgentAPI
}

// NewInternalServer creates the :9004 management HTTP server.
// It registers the OpenAPI-generated strict routes (templates, api-keys, images catalog)
// behind jwtOrManagerTokenMiddleware, plus legacy /internal/* routes behind the static
// manager-token middleware.
func NewInternalServer(cfg *config.Config, deps RouterDeps) *http.Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(httplog.RequestID())
	r.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		Formatter: httplog.GinLogFormatter("[WS]"),
		SkipPaths: []string{"/ping", "/metrics"},
	}))

	// /ping — public health-check.
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// /metrics — Prometheus scrapes without auth.
	r.GET("/metrics", gin.WrapH(syncmgr.MetricsHandler()))

	// OpenAPI-generated strict routes: templates, api-keys, images catalog.
	authMW := jwtOrManagerTokenMiddleware(
		deps.AdminKeyMgr, deps.KeyStore,
		deps.JWTSecret, deps.ManagerToken,
		deps.IAMService,
	)
	srv := handlers.New(deps.SyncManager, deps.Notify)
	strictHandler := wsproxygen.NewStrictHandler(srv, nil)
	wsproxygen.RegisterHandlersWithOptions(r, strictHandler, wsproxygen.GinServerOptions{
		BaseURL:     "",
		Middlewares: []wsproxygen.MiddlewareFunc{wsproxygen.MiddlewareFunc(authMW)},
	})

	// ManagedAgent CRUD. It sits behind the same JWT-or-manager-token gate as
	// the generated routes because the console forwards the caller's JWT and
	// tenant scoping is derived from it.
	if deps.ManagedAgentAPI != nil {
		deps.ManagedAgentAPI.RegisterManagedAgentRoutes(r.Group("/internal", authMW))
	}

	// Legacy /internal/* routes — static manager-token only.
	if deps.SyncManager != nil {
		legacy := r.Group("/internal", managerTokenMiddleware(deps.ManagerToken))
		deps.SyncManager.RegisterLegacyRoutes(legacy)
	}

	return &http.Server{
		Addr:    cfg.InternalAddr,
		Handler: r,
	}
}

// managerTokenMiddleware enforces the AGENTBOX-MANAGER-TOKEN header.
// When token is empty (dev mode), all requests pass through.
func managerTokenMiddleware(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if token != "" && c.GetHeader("AGENTBOX-MANAGER-TOKEN") != token {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}

// jwtOrManagerTokenMiddleware authenticates requests using (in priority order):
//  1. AGENTBOX-MANAGER-TOKEN header → admin
//  2. Bearer JWT (HS256) → role from claims
//  3. AGENTBOX-API-KEY header → role from key metadata
//
// When both jwtSecret and managerToken are empty (dev mode), all requests pass
// through as anonymous admin.
func jwtOrManagerTokenMiddleware(
	adminKeyMgr *apikey.AdminKeyManager,
	keyStore syncmgr.KeyStore,
	jwtSecret, managerToken string,
	iamSvc service.IAMService,
) gin.HandlerFunc {
	if jwtSecret == "" && managerToken == "" && adminKeyMgr == nil {
		return func(c *gin.Context) {
			c.Set(middleware.AuthContextKey, domain.AuthInfo{
				Namespace:  middleware.DefaultNamespace,
				Role:       apikey.RoleAdmin,
				User:       "anonymous-admin",
				AuthMethod: "apikey",
			})
			c.Next()
		}
	}

	authMiddleware := middleware.NewAuthenticateMiddleware(adminKeyMgr, keyStore, jwtSecret, iamSvc)
	return func(c *gin.Context) {
		if managerToken != "" && c.GetHeader("AGENTBOX-MANAGER-TOKEN") == managerToken {
			c.Set(middleware.AuthContextKey, domain.AuthInfo{
				Namespace:  middleware.DefaultNamespace,
				Role:       apikey.RoleAdmin,
				User:       "system",
				Team:       "system",
				AuthMethod: "apikey",
			})
			c.Next()
			return
		}
		authMiddleware(c)
	}
}
