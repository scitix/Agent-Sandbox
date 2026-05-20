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

package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	"github.com/scitix/agent-sandbox/pkg/wsmux"
)

// syncPongTimeout mirrors the value in pkg/wsproxy/syncmgr/manager.go.
// The Worker side resets its read deadline every time it receives data
// (including automatic Pong replies). If no Ping arrives within this window
// the WebSocket is treated as dead and the underlying yamux session tears
// itself down, which in turn ends the gRPC ClientConn.
const syncPongTimeout = 90 * time.Second

var syncWSUpgrader = websocket.Upgrader{
	CheckOrigin:     func(_ *http.Request) bool { return true },
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
}

// SyncWSHandler handles the /v1/ws/sync endpoint where ws-proxy dials in.
//
// The handshake is unchanged from the v1 protocol — Hub still dials with
// AGENTBOX-SYNC-TOKEN, the URL and the WebSocket Upgrade are untouched. Once
// upgraded, this handler stops reading frames itself: it hands the
// *websocket.Conn to wsmux.DialGRPC (Worker is the gRPC client / yamux
// client) and blocks on the resulting yamux session until it closes.
func SyncWSHandler(syncSvc service.SyncService, syncToken string) gin.HandlerFunc {
	log := ctrl.Log.WithName("sync-ws")

	return func(c *gin.Context) {
		if syncToken == "" {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "sync not configured"})
			return
		}

		// Machine-to-machine shared secret; constant-time comparison is
		// unnecessary because the secret is not user-derived.
		if c.GetHeader("AGENTBOX-SYNC-TOKEN") != syncToken {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid sync token"})
			return
		}

		conn, err := syncWSUpgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Error(err, "Failed to upgrade WS connection for sync")
			return
		}
		defer conn.Close() //nolint:errcheck

		// Configure WS ping/pong handling. ws-proxy (Hub) sends periodic
		// Pings; gorilla auto-Pongs and the PingHandler resets the read
		// deadline so a stalled upstream is detected promptly.
		_ = conn.SetReadDeadline(time.Now().Add(syncPongTimeout))
		conn.SetPingHandler(func(appData string) error {
			_ = conn.SetReadDeadline(time.Now().Add(syncPongTimeout))
			return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(10*time.Second))
		})

		grpcConn, session, err := wsmux.DialGRPC(conn)
		if err != nil {
			log.Error(err, "Failed to set up wsmux gRPC client")
			return
		}
		defer func() { _ = grpcConn.Close() }()
		defer func() { _ = session.Close() }()

		connID := syncSvc.OnConnect(grpcConn)
		log.Info("ws-proxy sync connection established", "connID", connID, "remoteAddr", c.Request.RemoteAddr)
		defer syncSvc.OnDisconnect(connID)

		// Block until the yamux session ends. yamux closes its CloseChan on
		// the underlying conn EOF or any internal teardown; the deferred
		// Close calls fall through and unblock the gRPC client.
		<-session.CloseChan()
		log.Info("ws-proxy sync connection closed", "connID", connID)
	}
}
