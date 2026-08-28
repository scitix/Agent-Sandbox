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

// Package server contains the two HTTP servers that wsproxy exposes:
//   - terminal.go  (:9003) — WebSocket terminal proxy toward Worker clusters
//   - internal.go  (:9004) — Internal management API for Dashboard BFF
package server

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
	"github.com/scitix/agent-sandbox/pkg/wsproxy/config"
	"github.com/scitix/agent-sandbox/pkg/wsproxy/wsdial"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin:     func(_ *http.Request) bool { return true },
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
}

var wsDialer = websocket.Dialer{
	HandshakeTimeout: 10 * time.Second,
}

// NewTerminalServer creates the :9003 HTTP server that proxies WebSocket
// terminal connections from the Dashboard to the target Worker cluster.
func NewTerminalServer(cfg *config.Config, store *cluster.Store) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", terminalProxyHandler(store))
	mux.HandleFunc("/ping", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "ok") //nolint:errcheck
	})
	return &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: mux,
	}
}

// terminalProxyHandler returns an http.HandlerFunc that proxies WebSocket
// terminal connections to the target Worker cluster. It expects the path:
//
//	/ws/clusters/{clusterID}/sandboxes/{sandboxID}/terminal
//	(or with an arbitrary prefix before "/ws/")
func terminalProxyHandler(store *cluster.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clusterID, sandboxID, ok := parseTerminalPath(r.URL.Path)
		if !ok {
			log.Printf("wsproxy: invalid terminal path %q", r.URL.Path)
			http.Error(w, "invalid path", http.StatusNotFound)
			return
		}

		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "token query parameter is required", http.StatusUnauthorized)
			return
		}

		entry, found := store.Get(clusterID)
		if !found {
			allIDs := clusterIDs(store)
			log.Printf("wsproxy: cluster %q not found (known: %v)", clusterID, allIDs)
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
		upstream.RawQuery = url.Values{"token": []string{token}}.Encode()

		log.Printf("wsproxy: dialing upstream %s", upstream)

		upstreamHeader := http.Header{}
		for k, v := range entry.Headers {
			upstreamHeader.Set(k, v)
		}
		upstreamConn, err := wsdial.Dial(&wsDialer, upstream.String(), upstreamHeader)
		if err != nil {
			log.Printf("wsproxy: dial %s failed: %v", upstream, err)
			http.Error(w, "upstream connection failed", http.StatusBadGateway)
			return
		}
		defer upstreamConn.Close() //nolint:errcheck

		clientConn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("wsproxy: client upgrade failed: %v", err)
			return
		}
		defer clientConn.Close() //nolint:errcheck

		log.Printf("wsproxy: proxying terminal cluster=%s sandbox=%s", clusterID, sandboxID)
		BridgeConns(clientConn, upstreamConn)
		log.Printf("wsproxy: terminal session ended cluster=%s sandbox=%s", clusterID, sandboxID)
	}
}

// BridgeConns copies messages bidirectionally between two WebSocket connections
// until one of them closes or returns an error.
func BridgeConns(a, b *websocket.Conn) {
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

// ParseTerminalPath parses a terminal proxy path of the form:
//
//	[<prefix>]/ws/clusters/{clusterID}/sandboxes/{sandboxID}/terminal
//
// Returns (clusterID, sandboxID, true) on success, ("", "", false) otherwise.
func ParseTerminalPath(rawPath string) (clusterID, sandboxID string, ok bool) {
	return parseTerminalPath(rawPath)
}

// parseTerminalPath is the internal implementation of ParseTerminalPath.
func parseTerminalPath(rawPath string) (clusterID, sandboxID string, ok bool) {
	const wsMarker = "/ws/"
	idx := strings.Index(rawPath, wsMarker)
	var sub string
	if idx == -1 {
		if !strings.HasSuffix(rawPath, "/ws") {
			return "", "", false
		}
		// path is exactly "/ws" with no trailing parts → invalid structure below
		sub = ""
	} else {
		sub = rawPath[idx+len(wsMarker)-1:] // keep leading /
	}

	parts := strings.Split(strings.TrimPrefix(sub, "/"), "/")
	if len(parts) != 5 ||
		parts[0] != "clusters" ||
		parts[2] != "sandboxes" ||
		parts[4] != "terminal" {
		return "", "", false
	}
	return parts[1], parts[3], true
}

// clusterIDs returns a snapshot of all cluster IDs in the store (for logging).
func clusterIDs(store *cluster.Store) []string {
	all := store.All()
	ids := make([]string, 0, len(all))
	for _, e := range all {
		ids = append(ids, e.ID)
	}
	return ids
}

// toWSScheme converts an http/https scheme to ws/wss.
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
