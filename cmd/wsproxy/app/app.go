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
//     connections to the appropriate Worker cluster's sandbox terminal
//     endpoint.
//   - Sync manager + internal HTTP API (default :9004, enabled when
//     AGENTBOX_SYNC_TOKEN is set): maintains persistent WebSocket connections
//     to every Worker cluster and exposes /internal/* endpoints for the
//     Dashboard BFF to manage global API keys and SandboxTemplates.
//
// Downstream (closed-source) distributions should import this package and
// invoke Run() from their thin main() wrapper so they stay in sync with
// upstream wiring changes.
package wsproxy

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
	"github.com/scitix/agent-sandbox/pkg/wsproxy/syncmgr"
)

const defaultClustersFile = "/etc/agentbox/clusters.yaml"

var upgrader = websocket.Upgrader{
	CheckOrigin:     func(_ *http.Request) bool { return true },
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
}

var dialer = websocket.Dialer{
	HandshakeTimeout: 10 * time.Second,
}

// bridgeConns copies messages bidirectionally between two WebSocket connections
// until one of them closes or returns an error.
func bridgeConns(a, b *websocket.Conn) {
	done := make(chan struct{}, 2)
	copyFn := func(src, dst *websocket.Conn) {
		defer func() { done <- struct{}{} }()
		for {
			msgType, msg, err := src.ReadMessage()
			if err != nil {
				return
			}
			if err := dst.WriteMessage(msgType, msg); err != nil {
				return
			}
		}
	}
	go copyFn(a, b)
	go copyFn(b, a)
	<-done
}

// proxyHandler proxies WebSocket terminal connections from the Dashboard to the
// target Worker cluster.  It expects the path:
//
//	/ws/clusters/{clusterID}/sandboxes/{sandboxId}/terminal
//	(or with an arbitrary prefix before "/ws/")
func proxyHandler(store *cluster.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		log.Printf("wsproxy: incoming request path=%q", p)

		const wsMarker = "/ws/"
		idx := strings.Index(p, wsMarker)
		if idx == -1 {
			if !strings.HasSuffix(p, "/ws") {
				log.Printf("wsproxy: path %q does not contain /ws/, returning 404", p)
				http.Error(w, "invalid path", http.StatusNotFound)
				return
			}
			p = ""
		} else {
			p = p[idx+len(wsMarker)-1:]
		}

		parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
		log.Printf("wsproxy: parsed path parts=%v", parts)
		if len(parts) != 5 || parts[0] != "clusters" || parts[2] != "sandboxes" || parts[4] != "terminal" {
			log.Printf("wsproxy: invalid path structure, parts=%v", parts)
			http.Error(w, "invalid path", http.StatusNotFound)
			return
		}
		clusterID := parts[1]
		sandboxID := parts[3]

		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "token query parameter is required", http.StatusUnauthorized)
			return
		}

		entry, ok := store.Get(clusterID)
		if !ok {
			allIDs := func() []string {
				ids := make([]string, 0)
				for _, e := range store.All() {
					ids = append(ids, e.ID)
				}
				return ids
			}()
			log.Printf("wsproxy: cluster %q not found (known clusters: %v)", clusterID, allIDs)
			http.Error(w, fmt.Sprintf("cluster %q not found", clusterID), http.StatusBadGateway)
			return
		}

		upstream, err := url.Parse(entry.URL)
		if err != nil {
			log.Printf("wsproxy: invalid cluster URL %q: %v", entry.URL, err)
			http.Error(w, "invalid cluster URL", http.StatusInternalServerError)
			return
		}
		upstream.Scheme = toWSScheme(upstream.Scheme)
		upstream.Path = path.Join(upstream.Path, "v1", "sandboxes", sandboxID, "terminal")
		q := url.Values{"token": []string{token}}
		upstream.RawQuery = q.Encode()

		log.Printf("wsproxy: dialing upstream %s", upstream.String())

		upstreamHeader := http.Header{}
		for k, v := range entry.Headers {
			upstreamHeader.Set(k, v)
		}
		upstreamConn, _, err := dialer.Dial(upstream.String(), upstreamHeader)
		if err != nil {
			log.Printf("wsproxy: dial %s failed: %v", upstream, err)
			http.Error(w, "upstream connection failed", http.StatusBadGateway)
			return
		}
		defer upstreamConn.Close() //nolint:errcheck

		clientConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("wsproxy: client upgrade failed: %v", err)
			return
		}
		defer clientConn.Close() //nolint:errcheck

		log.Printf("wsproxy: proxying terminal cluster=%s sandbox=%s", clusterID, sandboxID)
		bridgeConns(clientConn, upstreamConn)
		log.Printf("wsproxy: terminal session ended cluster=%s sandbox=%s", clusterID, sandboxID)
	}
}

func toWSScheme(scheme string) string {
	switch scheme {
	case "https":
		return "wss"
	case "wss", "ws":
		return scheme
	default:
		return "ws"
	}
}

// buildScheme returns the runtime.Scheme used by the k8s client. The wsproxy
// only needs core types plus the agents.navix.sh CRDs (for SandboxTemplate
// sync). Kept small to minimize binary size.
func buildScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(agentsv1alpha1.AddToScheme(s))
	return s
}

// Run is the single entry point for cmd/wsproxy. It reads configuration from
// environment variables, starts the terminal proxy, and optionally starts the
// sync manager + internal HTTP API. Blocks forever.
func Run() {
	listenAddr := os.Getenv("WSPROXY_LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":9003"
	}

	clustersFilePath := os.Getenv("CLUSTERS_CONFIG_PATH")
	if clustersFilePath == "" {
		clustersFilePath = defaultClustersFile
	}

	// Use the shared cluster.Store so both the terminal proxy and sync manager
	// read from the same in-memory view and benefit from Gateway fields.
	store := cluster.NewStore()
	if err := store.LoadFromFile(clustersFilePath); err != nil {
		log.Printf("wsproxy: initial cluster config load failed (continuing): %v", err)
	}

	// Terminal WS proxy.
	mux := http.NewServeMux()
	mux.HandleFunc("/", proxyHandler(store))
	mux.HandleFunc("/ping", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "ok") //nolint:errcheck
	})

	log.Printf("wsproxy: listening on %s", listenAddr)
	go func() {
		if err := http.ListenAndServe(listenAddr, mux); err != nil { //nolint:gosec
			log.Fatalf("wsproxy: listen error: %v", err)
		}
	}()

	syncToken := os.Getenv("AGENTBOX_SYNC_TOKEN")
	managerToken := os.Getenv("AGENTBOX_MANAGER_TOKEN")

	var sm *syncmgr.SyncManager

	if syncToken != "" {
		internalAddr := os.Getenv("WSPROXY_INTERNAL_ADDR")
		if internalAddr == "" {
			internalAddr = ":9004"
		}

		maxPerUser := 0
		if v := os.Getenv("AGENTBOX_MAX_KEYS_PER_USER"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				maxPerUser = n
			}
		}

		apikeyNamespace := os.Getenv("AGENTBOX_APIKEY_NAMESPACE")
		if apikeyNamespace == "" {
			apikeyNamespace = "agentbox-system"
		}

		cfg := ctrl.GetConfigOrDie()
		k8sClient, err := client.New(cfg, client.Options{Scheme: buildScheme()})
		if err != nil {
			log.Fatalf("wsproxy: failed to create k8s client: %v", err)
		}

		ks := apikey.NewSecretKeyStore(apikey.SecretKeyStoreConfig{
			Client:           k8sClient,
			SecretsNamespace: apikeyNamespace,
			CacheTTL:         time.Minute,
		})

		templateSvc := service.NewSandboxTemplateService(k8sClient)

		sm = syncmgr.New(store, syncToken, managerToken, syncmgr.Deps{
			KeyStore:        ks,
			TemplateClient:  k8sClient,
			TemplateService: templateSvc,
			MaxPerUser:      maxPerUser,
		})

		internalSrv := &http.Server{
			Addr:              internalAddr,
			Handler:           sm.InternalAPIHandler(),
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() {
			log.Printf("wsproxy: internal API listening on %s", internalAddr)
			if err := internalSrv.ListenAndServe(); err != nil {
				log.Fatalf("wsproxy: internal API listen error: %v", err)
			}
		}()

		ctx := context.Background()
		go sm.Run(ctx)

		log.Printf("wsproxy: global key sync enabled (max-keys-per-user=%d)", maxPerUser)
	}

	// Reload cluster config every 30 s.  After each reload, broadcast updated
	// ClusterEntry data (including Gateway fields) to all connected Workers.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if err := store.LoadFromFile(clustersFilePath); err != nil {
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
