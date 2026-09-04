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

// Package e2bcompat provides an E2B-compatible HTTP API server for AgentBox.
// It runs on an independent port and maps E2B SDK calls to AgentBox operations.
package e2bcompat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apimiddleware "github.com/scitix/agent-sandbox/pkg/apiserver/router/middleware"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service/federation"
	"github.com/scitix/agent-sandbox/pkg/e2bcompat/router"
	"github.com/scitix/agent-sandbox/pkg/e2bcompat/router/middleware"
	"github.com/scitix/agent-sandbox/pkg/metrics"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
	"github.com/scitix/agent-sandbox/pkg/utils/httplog"
	"github.com/scitix/agent-sandbox/pkg/utils/logclient"
	"github.com/scitix/agent-sandbox/pkg/utils/promclient"
)

const shutdownTimeout = 5 * time.Second

// Config holds the configuration for the E2B-compatible API server.
type Config struct {
	// BindAddress is the TCP address the HTTP server listens on (e.g. ":8090").
	BindAddress string
	// Domain is the gateway domain name returned to SDK clients for building
	// connection URLs. Can be a hostname (e.g. "my.gateway.com") or host:port.
	// When empty, the domain field in API responses will be null.
	Domain string
	// ServerVersion is stamped on every response via X-AgentBox-Server-Version.
	ServerVersion string
	// LocalClusterID identifies this cluster in federation records, so the
	// template listing can tell its own Envs from other clusters'.
	LocalClusterID string
	// MetricsSelector returns the PromQL label matcher that identifies this
	// cluster's series in a shared metrics backend, e.g. `cluster="foo"`.
	// Evaluated per query, because the cluster config it comes from is live.
	MetricsSelector func() string
	// LogFilters returns the central-log-service filters scoping queries to
	// this cluster (region / cluster labels). Evaluated per query.
	LogFilters func() map[string]string
}

// Server is the E2B-compatible HTTP API server.
type Server struct {
	httpServer *http.Server
}

// New creates and configures the E2B-compatible HTTP server.
// keyStore, adminKeyMgr, and iamSvc are shared with the native API server so
// that both servers validate keys against the same store without duplicating
// the Secret-backed cache.
// sandboxSvc is the shared SandboxService instance; passing the same instance
// avoids duplicate per-pool scheduler goroutines and ensures a single Shutdown path.
func New(cfg Config, k8sClient client.Client,
	keyStore apikey.KeyStore, adminKeyMgr *apikey.AdminKeyManager, iamSvc service.IAMService,
	sandboxSvc service.SandboxService, forwarder *service.CrossClusterForwarder,
	fedRegistry *federation.Registry, metricsClient *promclient.Client,
	vaultSvc service.VaultService, centralLogs *logclient.Client) *Server {

	gin.SetMode(gin.ReleaseMode)
	binding.EnableDecoderDisallowUnknownFields = false // E2B SDK may send extra fields
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(httplog.RequestID())
	r.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		Formatter: httplog.GinLogFormatter("[E2B]"),
		SkipPaths: []string{"/health"},
	}))
	r.Use(metrics.GinPrometheusMiddleware("e2b"))
	r.Use(apimiddleware.NewServerVersionMiddleware(cfg.ServerVersion))

	svcs := router.Services{
		Sandbox:         sandboxSvc,
		APIKey:          service.NewAPIKeyService(keyStore),
		Vault:           vaultSvc,
		Forwarder:       forwarder,
		Federation:      fedRegistry,
		LocalClusterID:  cfg.LocalClusterID,
		Metrics:         metricsClient,
		MetricsSelector: cfg.MetricsSelector,
		CentralLogs:     centralLogs,
		LogFilters:      cfg.LogFilters,
	}

	authMw := middleware.NewE2BAuthMiddleware(adminKeyMgr, keyStore, iamSvc)

	router.Setup(r, svcs, k8sClient, authMw, cfg.Domain)

	return &Server{
		httpServer: &http.Server{
			Addr:              cfg.BindAddress,
			Handler:           r,
			ReadHeaderTimeout: 5 * time.Second,
		},
	}
}

// Start runs the server until ctx is cancelled, then gracefully shuts down.
func (s *Server) Start(ctx context.Context) error {
	log := ctrl.Log.WithName("e2b-api-server")
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error(err, "Failed to shut down E2B API server")
		}
	}()

	log.Info("Starting E2B-compatible API server", "address", s.httpServer.Addr)
	err := s.httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		<-shutdownDone
		return nil
	}
	return fmt.Errorf("e2b-api-server: %w", err)
}
