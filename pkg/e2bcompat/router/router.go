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

// Package router provides E2B-compatible HTTP route registration.
package router

import (
	"github.com/gin-gonic/gin"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	e2bgen "github.com/scitix/agent-sandbox/pkg/e2bcompat/gen"
	"github.com/scitix/agent-sandbox/pkg/e2bcompat/handlers"
)

// Services bundles all service dependencies for E2B router handlers.
type Services struct {
	Sandbox service.SandboxService
	APIKey  service.APIKeyService
	// Forwarder enables cross-cluster forwarding via E2B API.
	// localClusterID is embedded in the forwarder itself; no separate field needed.
	Forwarder *service.CrossClusterForwarder
}

// Setup registers all E2B-compatible routes on the given Gin engine using the
// generated StrictServerInterface. Routes are also registered at the PrivatePathPrefix
// path for compatibility with private path mode.
func Setup(r *gin.Engine, svcs Services, k8sClient client.Client, authMw gin.HandlerFunc, gatewayDomain string) {
	srv := handlers.NewServer(handlers.Services{
		Sandbox:   svcs.Sandbox,
		APIKey:    svcs.APIKey,
		Forwarder: svcs.Forwarder,
	}, k8sClient, gatewayDomain)

	strictHandler := e2bgen.NewStrictHandler(srv, nil)

	// Register all routes with auth middleware at the standard path.
	// Note: /health is registered by the generated code and the auth middleware
	// skips it automatically (see middleware.NewE2BAuthMiddleware).
	e2bgen.RegisterHandlersWithOptions(r, strictHandler, e2bgen.GinServerOptions{
		Middlewares: []e2bgen.MiddlewareFunc{
			e2bgen.MiddlewareFunc(authMw),
		},
	})
}
