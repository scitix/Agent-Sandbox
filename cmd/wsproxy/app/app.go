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

// Package wsproxy contains the bootstrap logic for the wsproxy sidecar
// (cmd/wsproxy). It hosts two endpoints:
//
//   - Terminal WebSocket proxy (default :9003): routes dashboard terminal
//     connections to the appropriate Worker cluster's sandbox terminal endpoint.
//   - Sync manager + internal HTTP API (default :9004, enabled when
//     AGENTBOX_SECRET is set): maintains persistent WebSocket connections
//     to every Worker cluster and exposes management endpoints for API keys,
//     SandboxTemplates, and cluster config.
//
// Downstream distributions should import this package and invoke Run() from
// their thin main() wrapper to stay in sync with upstream wiring changes.
package wsproxy

import (
	"context"
	"flag"
	"log"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/gin-gonic/gin"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/router/middleware"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	"github.com/scitix/agent-sandbox/pkg/controllers/managedagent"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
	"github.com/scitix/agent-sandbox/pkg/wsproxy/config"
	"github.com/scitix/agent-sandbox/pkg/wsproxy/server"
	"github.com/scitix/agent-sandbox/pkg/wsproxy/syncmgr"
)

// Run is the single entry point for cmd/wsproxy. It parses flags (with env-var
// defaults), assembles the terminal proxy and optional sync manager layers,
// and blocks until the process is killed.
func Run() {
	cfg := config.FromFlags(flag.CommandLine)
	flag.Parse()

	if err := cfg.Validate(); err != nil {
		log.Fatalf("wsproxy: configuration error: %v", err)
	}

	if cfg.AdminKey == "" {
		log.Printf("wsproxy: WARNING: AGENTBOX_ADMIN_KEY is not set — running in dev mode" +
			" (all requests authenticated as admin)")
	}

	// ── Cluster store ─────────────────────────────────────────────────────────

	store := cluster.NewStore()
	if err := store.LoadFromFile(cfg.ClustersFilePath); err != nil {
		log.Printf("wsproxy: initial cluster config load failed (continuing): %v", err)
	}

	// ── Layer 1: Terminal WebSocket proxy (:9003) ─────────────────────────────

	terminalSrv := server.NewTerminalServer(cfg, store)
	go func() {
		log.Printf("wsproxy: terminal proxy listening on %s", cfg.ListenAddr)
		if err := terminalSrv.ListenAndServe(); err != nil {
			log.Fatalf("wsproxy: terminal proxy error: %v", err)
		}
	}()

	// ── Layer 2: Sync manager + internal API (:9004) ──────────────────────────

	var sm *syncmgr.SyncManager

	if cfg.SyncEnabled() {
		k8sClient := buildK8sClient()

		ks := apikey.NewSecretKeyStore(apikey.SecretKeyStoreConfig{
			Client:           k8sClient,
			SecretsNamespace: cfg.APIKeyNamespace,
			CacheTTL:         time.Minute,
		})

		adminKeyMgr := apikey.NewAdminKeyManager(cfg.AdminKey)
		templateSvc := service.NewSandboxTemplateService(k8sClient)

		sm = syncmgr.New(store, cfg.Secret, cfg.Secret, syncmgr.Deps{
			KeyStore:               ks,
			AdminKeyMgr:            adminKeyMgr,
			TemplateClient:         k8sClient,
			TemplateService:        templateSvc,
			MaxPerUser:             cfg.MaxKeysPerUser,
			JWTSecret:              cfg.Secret,
			ImagesCatalogNamespace: cfg.APIKeyNamespace,
			ImagesCatalogConfigMap: cfg.ImagesCatalogConfigMap,
		})

		routerDeps := server.RouterDeps{
			SyncManager:  sm,
			AdminKeyMgr:  adminKeyMgr,
			KeyStore:     ks,
			JWTSecret:    cfg.Secret,
			ManagerToken: cfg.Secret,
		}
		if cfg.ManagedAgentEnabled {
			ns := cfg.ManagedAgentNamespace
			if ns == "" {
				ns = cfg.APIKeyNamespace
			}
			routerDeps.ManagedAgentAPI = &server.ManagedAgentAPI{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Namespace: ns,
			}
		}
		if cfg.ManagedAgentEnabled && cfg.ManagedAgentGatewayAddr != "" {
			startManagedAgentGateway(cfg, k8sClient,
				middleware.NewAuthenticateMiddleware(adminKeyMgr, ks, cfg.Secret, nil))
		}
		internalSrv := server.NewInternalServer(cfg, routerDeps)
		go func() {
			log.Printf("wsproxy: internal API listening on %s", cfg.InternalAddr)
			if err := internalSrv.ListenAndServe(); err != nil {
				log.Fatalf("wsproxy: internal API error: %v", err)
			}
		}()

		ctx := context.Background()
		go sm.Run(ctx)

		log.Printf("wsproxy: sync manager enabled (max-keys-per-user=%d)", cfg.MaxKeysPerUser)
	}

	// ── ManagedAgent controller (control plane only) ──────────────────────────

	if cfg.ManagedAgentEnabled {
		startManagedAgentController(cfg, handsProvisioner(cfg, store))
	}

	// ── Cluster config reload (30s) ───────────────────────────────────────────

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if err := store.LoadFromFile(cfg.ClustersFilePath); err != nil {
				log.Printf("wsproxy: cluster config reload failed: %v", err)
				continue
			}
			if sm != nil {
				sm.BroadcastClusterConfig()
			}
		}
	}()

	select {}
}

// handsProvisioner lets the ManagedAgent controller derive SandboxEnvs on
// worker clusters.
//
// It returns nil without an admin key rather than a provisioner that fails
// every call: the worker rejects an unauthenticated create, and an agent using
// hands.envRef or hands.external must keep working on a control plane that was
// never given one. The controller reports the missing capability on the
// objects that actually need it.
func handsProvisioner(cfg *config.Config, store *cluster.Store) managedagent.HandsProvisioner {
	if cfg.AdminKey == "" {
		log.Printf("wsproxy: hands.auto disabled (AGENTBOX_ADMIN_KEY is not set)")
		return nil
	}
	return managedagent.NewRESTHandsProvisioner(func(clusterID string) (managedagent.ClusterEndpoint, bool) {
		entry, ok := store.Get(clusterID)
		if !ok {
			return managedagent.ClusterEndpoint{}, false
		}
		return managedagent.ClusterEndpoint{
			BaseURL: entry.URL,
			// The worker's ingress routes on Host. When the entry addresses it
			// by IP, dropping this header lands every call on the default
			// backend, which 404s while the address itself looks right.
			HostHeader: entry.Headers["Host"],
		}, true
	}, cfg.AdminKey)
}

// startManagedAgentController runs the ManagedAgent controller in the
// background.
//
// It is deliberately isolated from the terminal proxy: a panic in reconciliation
// must not take down the WebSocket path that dashboard terminals depend on, and
// a controller that cannot start must leave the proxy serving. Both failure
// modes therefore log and leave the rest of the process alone rather than
// exiting.
func startManagedAgentController(cfg *config.Config, hands managedagent.HandsProvisioner) {
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("wsproxy: ManagedAgent controller panicked and will stay down: %v", rec)
			}
		}()

		// Without a logger, controller-runtime discards everything the
		// controller reports — including the error from a failed reconcile, so a
		// stuck object gives no clue why.
		ctrl.SetLogger(zap.New(zap.UseDevMode(false)))

		s := runtime.NewScheme()
		utilruntime.Must(clientgoscheme.AddToScheme(s))
		utilruntime.Must(agentsv1alpha1.AddToScheme(s))

		opts := ctrl.Options{
			Scheme: s,
			// The proxy owns :9003/:9004; the controller must not bind
			// anything of its own.
			Metrics:                metricsserver.Options{BindAddress: "0"},
			HealthProbeBindAddress: "0",
			LeaderElection:         true,
			LeaderElectionID:       "managedagent.agents.navix.sh",
			// The Lease lives beside the objects the controller manages, so a
			// namespace-scoped deployment needs no cluster-wide grant.
			LeaderElectionNamespace: cfg.ManagedAgentNamespace,
		}
		if cfg.ManagedAgentNamespace != "" {
			// Narrowing the cache to one namespace keeps the controller's RBAC
			// to Roles and keeps its memory proportional to that namespace
			// rather than to every Deployment in the cluster.
			opts.Cache = cache.Options{
				DefaultNamespaces: map[string]cache.Config{cfg.ManagedAgentNamespace: {}},
			}
		} else {
			opts.LeaderElectionNamespace = cfg.APIKeyNamespace
		}

		mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), opts)
		if err != nil {
			log.Printf("wsproxy: ManagedAgent controller not started: %v", err)
			return
		}
		if err := (&managedagent.Reconciler{
			Client:        mgr.GetClient(),
			Scheme:        mgr.GetScheme(),
			Hands:         hands,
			ProxyService:  cfg.ManagedAgentProxyService,
			PublicBaseURL: cfg.ManagedAgentPublicBaseURL,
		}).SetupWithManager(mgr); err != nil {
			log.Printf("wsproxy: ManagedAgent controller setup failed: %v", err)
			return
		}

		log.Printf("wsproxy: ManagedAgent controller starting (namespace=%q)",
			cfg.ManagedAgentNamespace)
		if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
			log.Printf("wsproxy: ManagedAgent controller stopped: %v", err)
		}
	}()
}

// buildK8sClient creates a controller-runtime client for the in-cluster config.
func buildK8sClient() client.Client {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(agentsv1alpha1.AddToScheme(s))

	cfg := ctrl.GetConfigOrDie()
	k8sClient, err := client.New(cfg, client.Options{Scheme: s})
	if err != nil {
		log.Fatalf("wsproxy: failed to create k8s client: %v", err)
	}
	return k8sClient
}

// startManagedAgentGateway serves published agents on their own listener.
//
// It is separate from the internal API on purpose: the internal API trusts a
// manager token and must never be routable from outside, so an ingress can only
// be pointed at a port that authenticates every request on its own.
func startManagedAgentGateway(cfg *config.Config, k8sClient client.Client, auth gin.HandlerFunc) {
	ns := cfg.ManagedAgentNamespace
	if ns == "" {
		ns = cfg.APIKeyNamespace
	}
	gw := server.NewManagedAgentGateway(k8sClient, ns)
	srv := server.NewManagedAgentGatewayServer(cfg.ManagedAgentGatewayAddr, gw, auth)
	go func() {
		log.Printf("wsproxy: managed-agent gateway listening on %s (namespace=%q)",
			cfg.ManagedAgentGatewayAddr, ns)
		if err := srv.ListenAndServe(); err != nil {
			log.Printf("wsproxy: managed-agent gateway stopped: %v", err)
		}
	}()
}
