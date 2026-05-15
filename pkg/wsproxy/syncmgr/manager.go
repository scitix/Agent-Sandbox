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

package syncmgr

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/scitix/agent-sandbox/pkg/api/protocol"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
)

// ── WebSocket keep-alive constants ───────────────────────────────────────────

const (
	// syncPingInterval is how often ws-proxy sends a WebSocket Ping to Workers.
	// Must be well under any upstream proxy read timeout (e.g. Nginx default 60s).
	syncPingInterval = 30 * time.Second

	// syncPongTimeout is how long we wait for *any* read activity (including Pong)
	// before treating the connection as dead. Set to 3× the ping interval.
	syncPongTimeout = 90 * time.Second
)

var wsDialer = websocket.Dialer{
	HandshakeTimeout: 10 * time.Second,
}

// ── clusterSyncConn ───────────────────────────────────────────────────────────

// clusterSyncConn holds the state for one active WebSocket connection to a
// Worker cluster's /v1/ws/sync endpoint.
type clusterSyncConn struct {
	clusterID     string
	conn          *websocket.Conn
	mu            sync.Mutex
	done          chan struct{}
	connectedAt   time.Time // set once at connection time; immutable after creation
	lastFrameAtNs int64     // atomic Unix nanoseconds; 0 = no frame received yet
}

// lastFrameAt returns the time of the last received frame, or zero time.
func (c *clusterSyncConn) lastFrameAt() time.Time {
	ns := atomic.LoadInt64(&c.lastFrameAtNs)
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

func (c *clusterSyncConn) send(frame protocol.Frame) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.conn.WriteJSON(frame); err != nil {
		return err
	}
	WSSyncFramesTotal.WithLabelValues(c.clusterID, "tx").Inc()
	return nil
}

// ── SyncManager ──────────────────────────────────────────────────────────────

// SyncManager maintains persistent WebSocket connections to every configured
// Worker cluster and pushes API key, SandboxTemplate, and ClusterConfig updates.
type SyncManager struct {
	clusters     *cluster.Store
	deps         Deps
	syncToken    string
	managerToken string
	startedAt    time.Time

	mu               sync.RWMutex
	registry         map[string]*clusterSyncConn
	lastConnected    map[string]time.Time
	lastDisconnected map[string]time.Time
}

// Deps bundles all optional dependencies injected into SyncManager.
type Deps struct {
	KeyStore        KeyStore
	TemplateClient  client.Client                  // websocket sync path: raw K8s ops + snapshot
	TemplateService service.SandboxTemplateService // internal HTTP API: business logic + rendered responses
	MaxPerUser      int
	JWTSecret       string // HS256 secret shared with the BFF; enables Bearer JWT auth on internal API
}

// New creates a new SyncManager.
// clusters is the shared cluster store loaded from clusters.yaml.
// syncToken is sent in the AGENTBOX-SYNC-TOKEN header to authenticate with Workers.
// managerToken gates the internal HTTP API.
func New(clusters *cluster.Store, syncToken, managerToken string, deps Deps) *SyncManager {
	return &SyncManager{
		clusters:         clusters,
		deps:             deps,
		syncToken:        syncToken,
		managerToken:     managerToken,
		startedAt:        time.Now(),
		registry:         make(map[string]*clusterSyncConn),
		lastConnected:    make(map[string]time.Time),
		lastDisconnected: make(map[string]time.Time),
	}
}

// Run starts the sync manager loop: immediately dials all known clusters and
// then re-dials every 30 s to pick up newly added clusters.
func (m *SyncManager) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	m.dialAll(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.dialAll(ctx)
		}
	}
}

func (m *SyncManager) dialAll(ctx context.Context) {
	for _, entry := range m.clusters.All() {
		m.mu.RLock()
		_, exists := m.registry[entry.ID]
		m.mu.RUnlock()
		if !exists {
			go m.dialCluster(ctx, entry)
		}
	}
}

func (m *SyncManager) dialCluster(ctx context.Context, entry cluster.ClusterEntry) {
	u, err := url.Parse(entry.URL)
	if err != nil {
		log.Printf("syncManager: invalid cluster URL %q: %v", entry.URL, err)
		return
	}
	u.Scheme = toWSScheme(u.Scheme)
	u.Path = path.Join(u.Path, "v1", "ws", "sync")

	hdr := http.Header{}
	hdr.Set("AGENTBOX-SYNC-TOKEN", m.syncToken)
	for k, v := range entry.Headers {
		hdr.Set(k, v)
	}

	conn, _, err := wsDialer.Dial(u.String(), hdr)
	if err != nil {
		log.Printf("syncManager: dial cluster %s (%s) failed: %v (will retry in 30s)", entry.ID, u, err)
		return
	}

	sc := &clusterSyncConn{
		clusterID:   entry.ID,
		conn:        conn,
		done:        make(chan struct{}),
		connectedAt: time.Now(),
	}

	m.mu.Lock()
	m.registry[entry.ID] = sc
	m.lastConnected[entry.ID] = sc.connectedAt
	m.mu.Unlock()

	log.Printf("syncManager: connected to cluster %s", entry.ID)
	WSSyncConnectionsActive.Inc()
	WSSyncReconnectsTotal.WithLabelValues(entry.ID).Inc()

	// Send full snapshots on connect.
	if err := m.sendKeySnapshot(ctx, sc); err != nil {
		log.Printf("syncManager: key snapshot to cluster %s failed: %v", entry.ID, err)
	}
	if err := m.sendTemplateSnapshot(ctx, sc); err != nil {
		log.Printf("syncManager: template snapshot to cluster %s failed: %v", entry.ID, err)
	}
	if err := m.sendClusterConfigSnapshot(sc); err != nil {
		log.Printf("syncManager: cluster config snapshot to cluster %s failed: %v", entry.ID, err)
	}

	// Configure keep-alive.
	_ = conn.SetReadDeadline(time.Now().Add(syncPongTimeout))
	conn.SetPongHandler(func(_ string) error {
		return conn.SetReadDeadline(time.Now().Add(syncPongTimeout))
	})

	// Ping ticker.
	go func() {
		ticker := time.NewTicker(syncPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				deadline := time.Now().Add(10 * time.Second)
				if err := conn.WriteControl(websocket.PingMessage, nil, deadline); err != nil {
					log.Printf("syncManager: ping to cluster %s failed: %v", entry.ID, err)
					WSSyncPingFailuresTotal.WithLabelValues(entry.ID).Inc()
					conn.Close() //nolint:errcheck
					return
				}
			case <-sc.done:
				return
			}
		}
	}()

	// Read loop.
	go func() {
		defer func() {
			conn.Close() //nolint:errcheck
			m.mu.Lock()
			delete(m.registry, entry.ID)
			m.lastDisconnected[entry.ID] = time.Now()
			m.mu.Unlock()
			close(sc.done)
			WSSyncConnectionsActive.Dec()
			WSSyncDisconnectsTotal.WithLabelValues(entry.ID).Inc()
			log.Printf("syncManager: disconnected from cluster %s", entry.ID)
		}()
		for {
			var frame protocol.Frame
			if err := conn.ReadJSON(&frame); err != nil {
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					log.Printf("syncManager: read error from cluster %s: %v", entry.ID, err)
				}
				return
			}
			atomic.StoreInt64(&sc.lastFrameAtNs, time.Now().UnixNano())
			WSSyncFramesTotal.WithLabelValues(entry.ID, "rx").Inc()
			m.handleWorkerFrame(ctx, sc, frame)
		}
	}()
}

// handleWorkerFrame dispatches an inbound frame from a Worker.
func (m *SyncManager) handleWorkerFrame(ctx context.Context, sc *clusterSyncConn, frame protocol.Frame) {
	switch frame.Type {
	case protocol.FrameKeyCreate:
		m.handleKeyCreate(ctx, sc, frame)
	case protocol.FrameKeyDelete:
		m.handleKeyDelete(ctx, sc, frame)
	case protocol.FrameTemplateCreate:
		m.handleTemplateCreate(ctx, sc, frame)
	case protocol.FrameTemplateUpdate:
		m.handleTemplateUpdate(ctx, sc, frame)
	case protocol.FrameTemplateDelete:
		m.handleTemplateDelete(ctx, sc, frame)
	default:
		log.Printf("syncManager: unknown frame type %q from cluster %s", frame.Type, sc.clusterID)
	}
}

// broadcast sends a frame to all connected Worker clusters.
func (m *SyncManager) broadcast(frame protocol.Frame) {
	m.mu.RLock()
	conns := make([]*clusterSyncConn, 0, len(m.registry))
	for _, c := range m.registry {
		conns = append(conns, c)
	}
	m.mu.RUnlock()

	for _, c := range conns {
		if err := c.send(frame); err != nil {
			log.Printf("syncManager: broadcast to cluster %s failed: %v", c.clusterID, err)
		}
	}
}

// ClusterIDs returns a snapshot of known cluster IDs (for logging / status).
func (m *SyncManager) ClusterIDs() []string {
	all := m.clusters.All()
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
