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
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
)

// syncPongTimeout mirrors the value in cmd/wsproxy/sync_manager.go.
// The Worker side resets its read deadline every time it receives data
// (including automatic Pong replies). If no Ping arrives within this
// window the connection is treated as dead.
const syncPongTimeout = 90 * time.Second

var syncWSUpgrader = websocket.Upgrader{
	CheckOrigin:     func(_ *http.Request) bool { return true },
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
}

// SyncWSHandler handles the /v1/ws/sync endpoint where ws-proxy dials in.
// Authentication is via the AGENTBOX-SYNC-TOKEN header which must equal syncToken.
// When syncToken is empty the endpoint returns 503 (sync not configured).
func SyncWSHandler(syncSvc service.SyncService, syncToken string) gin.HandlerFunc {
	log := ctrl.Log.WithName("sync-ws")

	return func(c *gin.Context) {
		if syncToken == "" {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "sync not configured"})
			return
		}

		// Validate sync token (constant-time comparison via simple equality is
		// acceptable here because we don't store the secret and this is not a
		// user-facing token, but rather a machine-to-machine shared secret).
		incomingToken := c.GetHeader("AGENTBOX-SYNC-TOKEN")
		if incomingToken != syncToken {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid sync token"})
			return
		}

		conn, err := syncWSUpgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Error(err, "Failed to upgrade WS connection for sync")
			return
		}
		defer conn.Close() //nolint:errcheck

		connID := syncSvc.OnConnect(conn)
		defer syncSvc.OnDisconnect(connID)

		log.Info("ws-proxy sync connection established", "connID", connID, "remoteAddr", c.Request.RemoteAddr)

		// Configure ping/pong for keep-alive.
		// ws-proxy sends periodic Pings; gorilla/websocket auto-replies with
		// Pong. We set a read deadline so that if pings stop arriving (dead
		// upstream), we close the connection promptly instead of hanging.
		_ = conn.SetReadDeadline(time.Now().Add(syncPongTimeout))
		conn.SetPingHandler(func(appData string) error {
			// Reset read deadline on every Ping received from ws-proxy.
			_ = conn.SetReadDeadline(time.Now().Add(syncPongTimeout))
			// Send Pong back (gorilla default behaviour, replicated explicitly).
			return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(10*time.Second))
		})

		ctx := c.Request.Context()

		// Read loop: handle inbound frames from ws-proxy.
		for {
			var event service.SyncEvent
			if err := conn.ReadJSON(&event); err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					log.Info("ws-proxy sync connection closed normally", "connID", connID)
				} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
					log.Error(err, "ws-proxy sync read timeout — no pings received within deadline; check that ws-proxy is sending pings", "connID", connID)
				} else {
					log.Error(err, "ws-proxy sync read error", "connID", connID)
				}
				return
			}
			if handleErr := syncSvc.HandleIncoming(ctx, event); handleErr != nil {
				log.Error(handleErr, "error handling sync event", "type", event.Type)
			}
		}
	}
}
