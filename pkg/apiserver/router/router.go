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

package router

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/apiserver/handlers"
	"github.com/scitix/agent-sandbox/pkg/apiserver/router/middleware"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	quotaplugin "github.com/scitix/agent-sandbox/pkg/framework/providers/quota"
)

// Services bundles all service interfaces needed by the router.
type Services struct {
	Sandbox         service.SandboxService
	SandboxPool     service.SandboxPoolService
	SandboxEnv      service.SandboxEnvService
	SandboxTemplate service.SandboxTemplateService
	APIKey          service.APIKeyService
	Quota           service.QuotaService
	Organization    service.OrganizationService
	IAM             service.IAMService
	// KubeClientset and RestConfig are needed for the WebSocket terminal handler and log streaming.
	KubeClientset kubernetes.Interface
	RestConfig    *rest.Config
	// Sync and SyncToken enable the /v1/ws/sync endpoint for ws-proxy connections.
	// When SyncToken is empty the endpoint is disabled.
	Sync      service.SyncService
	SyncToken string
	// Forwarder enables cross-cluster forwarding at the handler layer.
	// localClusterID is embedded in the forwarder itself; no separate field needed.
	// When Forwarder is nil, cross-cluster requests are rejected.
	Forwarder *service.CrossClusterForwarder
	// Cluster serves the /v1/clusters discovery endpoint. Must always be non-nil
	// (single-cluster deployments can inject a service with nil store + empty
	// localID; the endpoint then returns an empty catalog).
	Cluster service.ClusterService
	// QuotaProvider drives the /v1/feature-gates endpoint.
	// Nil is accepted and treated as Noop downstream.
	QuotaProvider quotaplugin.Provider
	// ServerVersion is stamped onto every response via X-AgentBox-Server-Version.
	// Comes from pkg/version.Version (injected at build time via -ldflags).
	ServerVersion string
}

// Setup registers all routes on the provided gin.Engine.
// authMiddleware is the authenticate middleware; it also enforces admin access
// for routes that have AdminKeyAuth security (via oapi-codegen per-route c.Set).
func Setup(r *gin.Engine, svcs Services, authMiddleware gin.HandlerFunc) {
	// Stamp every response (including /ping) with the running server version.
	r.Use(middleware.NewServerVersionMiddleware(svcs.ServerVersion))
	srv := handlers.NewServer(handlers.Services{
		Sandbox:         svcs.Sandbox,
		SandboxPool:     svcs.SandboxPool,
		SandboxEnv:      svcs.SandboxEnv,
		SandboxTemplate: svcs.SandboxTemplate,
		APIKey:          svcs.APIKey,
		Quota:           svcs.Quota,
		Organization:    svcs.Organization,
		Sync:            svcs.Sync,
		Forwarder:       svcs.Forwarder,
		Cluster:         svcs.Cluster,
		QuotaProvider:   svcs.QuotaProvider,
	})

	strictHandler := gen.NewStrictHandler(srv, nil)

	// health check endpoint (no auth required)
	r.GET("/ping", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })

	// Embedded OpenAPI spec endpoint
	r.GET("/openapi.json", func(c *gin.Context) {
		swagger, err := gen.GetSwagger()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to serve spec"})
			return
		}
		c.JSON(http.StatusOK, swagger)
	})

	// All API routes are registered under /v1 with the generated router.
	// Per-route admin enforcement is handled inside authMiddleware by detecting
	// the AdminKeyAuthScopes context key set by oapi-codegen wrappers.
	// gen.MiddlewareFunc is func(*gin.Context); gin.HandlerFunc is also func(*gin.Context),
	// so they are the same underlying type — explicit cast is needed to satisfy the compiler.
	gen.RegisterHandlersWithOptions(r, strictHandler, gen.GinServerOptions{
		BaseURL: "/v1",
		Middlewares: []gen.MiddlewareFunc{
			gen.MiddlewareFunc(authMiddleware),
			gen.MiddlewareFunc(middleware.NewVersionCheckMiddleware()),
		},
	})

	// WebSocket terminal endpoint (outside oapi-codegen, requires HTTP Upgrade).
	// Auth is performed by token validation inside the handler (exec token).
	if svcs.KubeClientset != nil && svcs.RestConfig != nil {
		r.GET("/v1/sandboxes/:sandboxId/terminal", handlers.WSTerminalHandler(svcs.Sandbox, svcs.KubeClientset, svcs.RestConfig))
	}

	// NDJSON log streaming endpoint (outside oapi-codegen, streams response body).
	// Auth is enforced by authMiddleware (same as all /v1 routes).
	r.GET("/v1/sandboxes/:sandboxId/logs/stream",
		authMiddleware,
		handlers.LogsStreamHandler(svcs.Sandbox, svcs.KubeClientset, svcs.RestConfig))

	// WS sync endpoint for ws-proxy connections (outside oapi-codegen, no user auth).
	// ws-proxy dials in and authenticates using AGENTBOX-SYNC-TOKEN header.
	// Only registered when both SyncService and SyncToken are configured.
	if svcs.Sync != nil && svcs.SyncToken != "" {
		r.GET("/v1/ws/sync", handlers.SyncWSHandler(svcs.Sync, svcs.SyncToken))
	}
}
