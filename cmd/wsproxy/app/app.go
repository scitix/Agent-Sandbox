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
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
	"github.com/scitix/agent-sandbox/pkg/wsproxy/config"
	"github.com/scitix/agent-sandbox/pkg/wsproxy/notify"
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

	var (
		sm *syncmgr.SyncManager
		// Shared by the console API (create-an-env-with-the-agent) and the
	)

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
			ConsoleBaseURL:         cfg.ConsoleBaseURL,
		})

		// Templates edited outside the internal API (kubectl, a restore) reach
		// the workers through this watch; the API path broadcasts on its own.
		go sm.WatchTemplateCRs(context.Background(), k8sClient)

		notifySvc := notify.New(notify.Params{
			Client:           k8sClient,
			Namespace:        cfg.APIKeyNamespace,
			ConfigMapName:    cfg.NotificationConfigMap,
			PrometheusURL:    cfg.PrometheusURL,
			PrometheusToken:  cfg.PrometheusToken,
			FeishuWebhookURL: cfg.FeishuWebhookURL,
			Clusters:         store,
		})
		if notifySvc.Enabled() {
			go notifySvc.Run(context.Background())
			log.Printf("wsproxy: notification service enabled (configmap=%s/%s)", cfg.APIKeyNamespace, cfg.NotificationConfigMap)
		} else {
			log.Printf("wsproxy: notification service disabled (no prometheus-url configured)")
		}

		routerDeps := server.RouterDeps{
			SyncManager:  sm,
			AdminKeyMgr:  adminKeyMgr,
			KeyStore:     ks,
			JWTSecret:    cfg.Secret,
			ManagerToken: cfg.Secret,
			Notify:       notifySvc,
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

// buildK8sClient creates a controller-runtime client for the in-cluster config.
func buildK8sClient() client.WithWatch {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(agentsv1alpha1.AddToScheme(s))

	cfg := ctrl.GetConfigOrDie()
	// WithWatch rather than a plain client: the sync manager watches
	// SandboxTemplates so an edit made outside the internal API still reaches
	// the workers. It satisfies client.Client, so every other caller is
	// unaffected.
	k8sClient, err := client.NewWithWatch(cfg, client.Options{Scheme: s})
	if err != nil {
		log.Fatalf("wsproxy: failed to create k8s client: %v", err)
	}
	return k8sClient
}
