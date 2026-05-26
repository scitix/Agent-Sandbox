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

package apiserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/scitix/agent-sandbox/pkg/apiserver/router"
	"github.com/scitix/agent-sandbox/pkg/apiserver/router/middleware"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	"github.com/scitix/agent-sandbox/pkg/framework/plugins"
	instancetypeplugin "github.com/scitix/agent-sandbox/pkg/framework/providers/instancetype"
	quotaplugin "github.com/scitix/agent-sandbox/pkg/framework/providers/quota"
	"github.com/scitix/agent-sandbox/pkg/metrics"
	"github.com/scitix/agent-sandbox/pkg/store"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
	"github.com/scitix/agent-sandbox/pkg/utils/httplog"
)

const shutdownTimeout = 5 * time.Second

// Config holds the configuration for the API server.
type Config struct {
	// BindAddress is the TCP address the HTTP server listens on (e.g. ":8080").
	BindAddress string
	// KeyStore is the API key store used for authentication.
	KeyStore apikey.KeyStore
	// AdminKeyManager validates the shared admin key.  When nil, admin-key
	// authentication is disabled (dev mode).
	AdminKeyManager *apikey.AdminKeyManager
	// IAMService resolves team/user identities to Kubernetes namespaces and
	// handles JWT-based authentication issued by the BFF.
	IAMService service.IAMService
	// RestConfig is the Kubernetes REST config, used for WebSocket exec (terminal).
	// When nil, the terminal endpoint is disabled.
	RestConfig *rest.Config
	// Secret is the shared secret used to authenticate ws-proxy connections on
	// /v1/ws/sync. When empty, the sync endpoint is disabled.
	Secret string
	// ClusterConfigSink is called whenever a cluster_config_sync frame arrives from
	// ws-proxy. Typically a cluster.ConfigMapWriter that persists the routing table
	// so ExtProc can read it. May be nil when cluster config sync is not required.
	ClusterConfigSink service.ClusterConfigSink
	// Forwarder is the cross-cluster forwarder. When nil, cross-cluster requests
	// will be rejected. Typically constructed from cluster store + localClusterID.
	// The forwarder already carries the local cluster ID internally.
	Forwarder *service.CrossClusterForwarder
	// ClusterStore is the shared in-memory catalog of known clusters. It is
	// surfaced through GET /v1/clusters so SDK/CLI callers can discover valid
	// cross-cluster prefixes. May be nil in single-cluster deployments.
	ClusterStore *cluster.Store
	// LocalClusterID identifies the cluster serving this API server. Used to mark
	// the `local: true` entry in the /v1/clusters response. Empty in
	// single-cluster deployments where cross-cluster routing is disabled.
	LocalClusterID string
	// QuotaProvider selects the quota backend exposed by GET /v1/quotas and
	// inspected by the /v1/feature-gates endpoint. When nil, a Noop provider
	// is used (feature disabled).
	QuotaProvider quotaplugin.Provider
	// InstanceTypeProvider selects the InstanceType catalog backend exposed
	// by GET /v1/instancetypes and inspected by the /v1/feature-gates
	// endpoint. When nil, a Noop provider is used (feature disabled).
	InstanceTypeProvider instancetypeplugin.Provider
	// ServerVersion is the build-time version string stamped on every response
	// via X-AgentBox-Server-Version. Set from pkg/version.Version in app.go.
	ServerVersion string
}

// Server is the HTTP API server.
type Server struct {
	httpServer *http.Server
}

// New creates and configures the HTTP server with all service layers wired up.
// pluginManager may be nil (disables lifecycle plugins — open-source mode).
// sandboxSvc may be nil; when nil a default service is constructed internally.
// Pass a non-nil sandboxSvc to share the service with other components (e.g.
// SandboxPoolReconciler for idle-pod notifications).
func New(cfg Config, k8sClient client.Client, clientset kubernetes.Interface, sandboxStore store.SandboxStore,
	pluginManager *plugins.PluginManager, envoyGatewayBaseURL string,
	sandboxSvc service.SandboxService) *Server {

	log := ctrl.Log.WithName("api-server-init")

	gin.SetMode(gin.ReleaseMode)
	binding.EnableDecoderDisallowUnknownFields = true
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(httplog.RequestID())
	r.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		Formatter: httplog.GinLogFormatter("[RAW]"),
		SkipPaths: []string{"/ping", "/v1/ping"},
	}))
	r.Use(metrics.GinPrometheusMiddleware("native"))

	// Build SyncService when a sync token is configured.
	var syncSvc service.SyncService
	if cfg.Secret != "" {
		templateSvc := service.NewSandboxTemplateService(k8sClient)
		if cfg.ClusterConfigSink != nil {
			syncSvc = service.NewSyncServiceFull(cfg.KeyStore, templateSvc, cfg.ClusterConfigSink)
			log.Info("sync mode enabled: template, API key, and cluster config writes will be forwarded to ws-proxy")
		} else {
			syncSvc = service.NewSyncServiceWithTemplate(cfg.KeyStore, templateSvc)
			log.Info("sync mode enabled: template and API key writes will be forwarded to ws-proxy")
		}
	} else {
		log.Info("sync mode disabled: template and API key writes are local-only")
	}

	// Default providers to Noop when the caller left them nil so every
	// downstream service can assume a non-nil Provider.
	quotaProv := cfg.QuotaProvider
	if quotaProv == nil {
		quotaProv = quotaplugin.NewNoop()
	}
	instanceTypeProv := cfg.InstanceTypeProvider
	if instanceTypeProv == nil {
		instanceTypeProv = instancetypeplugin.NewNoop()
	}

	svcs := router.Services{
		Sandbox:              sandboxSvc,
		SandboxEnv:           service.NewSandboxEnvService(k8sClient, pluginManager, instanceTypeProv, quotaProv),
		SandboxTemplate:      service.NewSandboxTemplateService(k8sClient),
		APIKey:               service.NewAPIKeyServiceWithSync(cfg.KeyStore, syncSvc),
		Quota:                service.NewQuotaServiceFromProvider(quotaProv),
		Organization:         service.NewOrganizationService(k8sClient, cfg.KeyStore),
		IAM:                  cfg.IAMService,
		KubeClientset:        clientset,
		RestConfig:           cfg.RestConfig,
		Sync:                 syncSvc,
		SyncToken:            cfg.Secret,
		Forwarder:            cfg.Forwarder,
		Cluster:              service.NewClusterService(cfg.ClusterStore, cfg.LocalClusterID),
		QuotaProvider:        quotaProv,
		InstanceTypeProvider: instanceTypeProv,
		ServerVersion:        cfg.ServerVersion,
	}

	authMw := middleware.NewAuthenticateMiddleware(cfg.AdminKeyManager, cfg.KeyStore, cfg.Secret, svcs.IAM)

	router.Setup(r, svcs, authMw)

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
	log := ctrl.Log.WithName("api-server")
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error(err, "Failed to shut down API server")
		}
	}()

	log.Info("Starting API server", "address", s.httpServer.Addr)
	err := s.httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		<-shutdownDone
		return nil
	}
	return fmt.Errorf("api-server: %w", err)
}
