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
	"crypto/sha256"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
	"google.golang.org/grpc"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	syncv1 "github.com/scitix/agent-sandbox/pkg/proto/sandbox/sync/v1"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
	"github.com/scitix/agent-sandbox/pkg/wsmux"
	"github.com/scitix/agent-sandbox/pkg/wsproxy/wsdial"
)

// ── WebSocket keep-alive constants ───────────────────────────────────────────
//
// The yamux layer's own keepalive is disabled (see wsmux.defaultYamuxConfig);
// here we keep the v1 WS-level ping/pong cadence so gateways that idle-timeout
// HTTP/1.1 connections (e.g. Nginx default 60 s) hold the upgrade open.

const (
	syncPingInterval = 30 * time.Second
	syncPongTimeout  = 90 * time.Second

	// broadcastBuffer is the per-cluster, per-resource event channel capacity.
	// Events that cannot be enqueued are dropped and counted via
	// WSSyncEventsDroppedTotal. Sized for the worst plausible burst (a wide
	// snapshot replay) without unbounded memory growth.
	broadcastBuffer = 256
)

var wsDialer = websocket.Dialer{
	HandshakeTimeout: 10 * time.Second,
}

// ── clusterSyncConn ───────────────────────────────────────────────────────────

// clusterSyncConn owns the per-cluster sync session: the WebSocket transport,
// the yamux multiplexer running on top of it, the gRPC server that handles
// inbound RPCs (CreateKey / CreateTemplate / Watch* etc.), and three
// buffered event channels that broadcast routines write to and the active
// Watch* gRPC handlers drain into stream.Send().
//
// Lifecycle: created in dialCluster on connect, removed from the registry on
// disconnect (read-loop exit). The grpc.Server and yamux.Session are torn
// down together when the WebSocket dies.
type clusterSyncConn struct {
	clusterID string
	conn      *websocket.Conn
	session   *yamux.Session
	grpcSrv   *grpc.Server

	// Per-resource broadcast channels. Buffered so a slow Worker stalls only
	// its own stream — once full, new events are dropped (see broadcast*).
	keyCh   chan *syncv1.KeyEvent
	vaultCh chan *syncv1.VaultEvent
	tmplCh  chan *syncv1.TemplateEvent
	cfgCh   chan *syncv1.ClusterConfigEvent
	fedCh   chan *syncv1.FederationBroadcast

	done          chan struct{}
	connectedAt   time.Time // immutable after creation
	lastFrameAtNs int64     // atomic UnixNano of the most recent inbound event
}

// lastFrameAt returns the time of the last received frame, or zero time.
func (c *clusterSyncConn) lastFrameAt() time.Time {
	ns := atomic.LoadInt64(&c.lastFrameAtNs)
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// ── SyncManager ──────────────────────────────────────────────────────────────

// SyncManager maintains persistent WebSocket connections to every configured
// Worker cluster and exposes the three sync gRPC services on each one via
// yamux multiplexing.
type SyncManager struct {
	clusters     *cluster.Store
	deps         Deps
	syncToken    string
	managerToken string
	startedAt    time.Time

	mu                sync.RWMutex
	registry          map[string]*clusterSyncConn
	lastConnected     map[string]time.Time
	lastDisconnected  map[string]time.Time
	lastBroadcastHash [32]byte

	// fed holds the cross-cluster capacity soft state relayed between Workers.
	fed *federationStore

	// vault is the Hub-side source of truth for credential entries, fanned out
	// to every Worker (see grpc_vault_server.go).
	vault *vaultStore
}

// Deps bundles all optional dependencies injected into SyncManager.
type Deps struct {
	KeyStore        KeyStore
	AdminKeyMgr     *apikey.AdminKeyManager        // validates the shared admin key; nil = dev mode
	IAMService      service.IAMService             // namespace resolution for API key auth; may be nil
	TemplateClient  client.Client                  // websocket sync path: raw K8s ops + snapshot
	TemplateService service.SandboxTemplateService // internal HTTP API: business logic + rendered responses
	MaxPerUser      int
	JWTSecret       string // HS256 secret shared with the BFF; enables Bearer JWT auth on internal API

	// ImagesCatalogNamespace / ImagesCatalogConfigMap locate the images-catalog
	// ConfigMap that ws-proxy reads and writes. The chart points these at the
	// ConfigMap it actually owns; the binary falls back to the historical
	// "agentbox-system" / "agentbox-images-catalog" values when unset.
	ImagesCatalogNamespace string
	ImagesCatalogConfigMap string
}

// New creates a new SyncManager.
func New(clusters *cluster.Store, syncToken, managerToken string, deps Deps) *SyncManager {
	return &SyncManager{
		clusters:         clusters,
		vault:            newVaultStore(),
		deps:             deps,
		syncToken:        syncToken,
		managerToken:     managerToken,
		startedAt:        time.Now(),
		registry:         make(map[string]*clusterSyncConn),
		lastConnected:    make(map[string]time.Time),
		lastDisconnected: make(map[string]time.Time),
		fed:              newFederationStore(),
	}
}

// Run starts the sync manager loop: dial all known clusters and rescan every
// 30 s for newly added ones.
func (m *SyncManager) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Bootstrap the images-catalog ConfigMap before serving. When the chart
	// does not ship the object (imagesCatalog.manageConfigMap=false), ws-proxy
	// is its sole owner; when the chart does, this is a no-op.
	m.ensureImagesCatalog(ctx)

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

func (m *SyncManager) dialAll(_ context.Context) {
	for _, entry := range m.clusters.All() {
		m.mu.RLock()
		_, exists := m.registry[entry.ID]
		m.mu.RUnlock()
		if !exists {
			go m.dialCluster(entry)
		}
	}
}

// dialCluster establishes the per-cluster sync session: WS → yamux → gRPC.
func (m *SyncManager) dialCluster(entry cluster.ClusterEntry) {
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

	conn, err := wsdial.Dial(&wsDialer, u.String(), hdr)
	if err != nil {
		log.Printf("syncManager: dial cluster %s (%s) failed: %v (will retry in 30s)", entry.ID, u, err)
		return
	}

	sc := &clusterSyncConn{
		clusterID:   entry.ID,
		conn:        conn,
		keyCh:       make(chan *syncv1.KeyEvent, broadcastBuffer),
		vaultCh:     make(chan *syncv1.VaultEvent, broadcastBuffer),
		tmplCh:      make(chan *syncv1.TemplateEvent, broadcastBuffer),
		cfgCh:       make(chan *syncv1.ClusterConfigEvent, broadcastBuffer),
		fedCh:       make(chan *syncv1.FederationBroadcast, broadcastBuffer),
		done:        make(chan struct{}),
		connectedAt: time.Now(),
	}

	grpcSrv, session, err := wsmux.ServeGRPC(conn, func(s *grpc.Server) {
		syncv1.RegisterAPIKeyServiceServer(s, newAPIKeyServer(m, sc))
		syncv1.RegisterVaultServiceServer(s, newVaultServer(m, sc))
		syncv1.RegisterTemplateServiceServer(s, newTemplateServer(m, sc))
		syncv1.RegisterClusterConfigServiceServer(s, newClusterConfigServer(m, sc))
		syncv1.RegisterFederationServiceServer(s, newFederationServer(m, sc))
	})
	if err != nil {
		log.Printf("syncManager: ServeGRPC for %s failed: %v", entry.ID, err)
		_ = conn.Close()
		return
	}
	sc.grpcSrv = grpcSrv
	sc.session = session

	m.mu.Lock()
	m.registry[entry.ID] = sc
	m.lastConnected[entry.ID] = sc.connectedAt
	m.mu.Unlock()

	log.Printf("syncManager: connected to cluster %s", entry.ID)
	WSSyncConnectionsActive.Inc()
	WSSyncReconnectsTotal.WithLabelValues(entry.ID).Inc()

	// WS-level keepalive. yamux's own keepalive is off (see wsmux); this
	// keeps the WebSocket Upgrade alive across HTTP/1.1 gateway idle timeouts.
	_ = conn.SetReadDeadline(time.Now().Add(syncPongTimeout))
	conn.SetPongHandler(func(_ string) error {
		return conn.SetReadDeadline(time.Now().Add(syncPongTimeout))
	})

	// Ping ticker: dies when sc.done closes (see closeSession below).
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
					_ = sc.session.Close()
					return
				}
			case <-sc.done:
				return
			}
		}
	}()

	// Session lifetime monitor. We block on the yamux CloseChan (closes when
	// the underlying WS dies or grpcSrv stops) and then clean up registry +
	// metrics + the gRPC server + buffered channels.
	go func() {
		<-session.CloseChan()
		grpcSrv.GracefulStop()

		m.mu.Lock()
		// Only remove if this exact session is still the registered one
		// (defensive: a reconnect could have raced).
		if cur, ok := m.registry[entry.ID]; ok && cur == sc {
			delete(m.registry, entry.ID)
		}
		m.lastDisconnected[entry.ID] = time.Now()
		m.mu.Unlock()

		// A cluster that drops off must not linger in the federation store,
		// or new subscribers would receive its stale capacity. Purge it before
		// closing channels; surviving Workers also age it out on their own TTL.
		m.fed.purgeCluster(entry.ID)

		close(sc.done)
		// Drain channels by closing them; pending Watch loops exit via
		// the chan-closed case in their select.
		close(sc.keyCh)
		close(sc.tmplCh)
		close(sc.cfgCh)
		close(sc.fedCh)

		_ = conn.Close()
		WSSyncConnectionsActive.Dec()
		WSSyncDisconnectsTotal.WithLabelValues(entry.ID).Inc()
		log.Printf("syncManager: disconnected from cluster %s", entry.ID)
	}()
}

// ── Broadcast helpers ─────────────────────────────────────────────────────────
//
// Each helper enqueues one event onto every connected cluster's channel.
// Non-blocking: if a channel is full the event is dropped and the
// WSSyncEventsDroppedTotal counter is incremented. The Worker re-subscribing
// after a stall causes a fresh Snapshot to replay, so any dropped Upsert is
// recovered on the next reconnect.

func (m *SyncManager) snapshotConns() []*clusterSyncConn {
	m.mu.RLock()
	defer m.mu.RUnlock()
	conns := make([]*clusterSyncConn, 0, len(m.registry))
	for _, c := range m.registry {
		conns = append(conns, c)
	}
	return conns
}

func (m *SyncManager) broadcastKeyUpsert(meta *syncv1.APIKeyMetadata) {
	ev := &syncv1.KeyEvent{Kind: &syncv1.KeyEvent_Upsert{Upsert: meta}}
	for _, sc := range m.snapshotConns() {
		select {
		case sc.keyCh <- ev:
			WSSyncEventsTotal.WithLabelValues(sc.clusterID, "key_upsert").Inc()
		default:
			WSSyncEventsDroppedTotal.WithLabelValues(sc.clusterID, "key_upsert").Inc()
			log.Printf("syncManager: dropped key_upsert for cluster %s (buffer full)", sc.clusterID)
		}
	}
}

func (m *SyncManager) broadcastKeyDelete(secretName string) {
	ev := &syncv1.KeyEvent{Kind: &syncv1.KeyEvent_Delete{Delete: &syncv1.KeyDelete{SecretName: secretName}}}
	for _, sc := range m.snapshotConns() {
		select {
		case sc.keyCh <- ev:
			WSSyncEventsTotal.WithLabelValues(sc.clusterID, "key_delete").Inc()
		default:
			WSSyncEventsDroppedTotal.WithLabelValues(sc.clusterID, "key_delete").Inc()
			log.Printf("syncManager: dropped key_delete for cluster %s (buffer full)", sc.clusterID)
		}
	}
}

func (m *SyncManager) broadcastVaultUpsert(entry *syncv1.VaultEntry) {
	ev := &syncv1.VaultEvent{Kind: &syncv1.VaultEvent_Upsert{Upsert: entry}}
	for _, sc := range m.snapshotConns() {
		select {
		case sc.vaultCh <- ev:
			WSSyncEventsTotal.WithLabelValues(sc.clusterID, "vault_upsert").Inc()
		default:
			// Dropping is safe only because the next Watch re-subscription
			// starts with a full snapshot; the entry is not lost, just late.
			WSSyncEventsDroppedTotal.WithLabelValues(sc.clusterID, "vault_upsert").Inc()
			log.Printf("syncManager: dropped vault_upsert for cluster %s (buffer full)", sc.clusterID)
		}
	}
}

func (m *SyncManager) broadcastVaultDelete(namespace, user, name string) {
	ev := &syncv1.VaultEvent{Kind: &syncv1.VaultEvent_Delete{Delete: &syncv1.VaultDelete{
		Namespace: namespace, User: user, Name: name,
	}}}
	for _, sc := range m.snapshotConns() {
		select {
		case sc.vaultCh <- ev:
			WSSyncEventsTotal.WithLabelValues(sc.clusterID, "vault_delete").Inc()
		default:
			WSSyncEventsDroppedTotal.WithLabelValues(sc.clusterID, "vault_delete").Inc()
			log.Printf("syncManager: dropped vault_delete for cluster %s (buffer full)", sc.clusterID)
		}
	}
}

func (m *SyncManager) broadcastTemplateUpsert(raw []byte) {
	ev := &syncv1.TemplateEvent{Kind: &syncv1.TemplateEvent_Upsert{Upsert: &syncv1.TemplateUpsert{TemplateJson: raw}}}
	for _, sc := range m.snapshotConns() {
		select {
		case sc.tmplCh <- ev:
			WSSyncEventsTotal.WithLabelValues(sc.clusterID, "template_upsert").Inc()
		default:
			WSSyncEventsDroppedTotal.WithLabelValues(sc.clusterID, "template_upsert").Inc()
			log.Printf("syncManager: dropped template_upsert for cluster %s (buffer full)", sc.clusterID)
		}
	}
}

func (m *SyncManager) broadcastTemplateDelete(name string) {
	ev := &syncv1.TemplateEvent{Kind: &syncv1.TemplateEvent_Delete{Delete: &syncv1.TemplateDelete{Name: name}}}
	for _, sc := range m.snapshotConns() {
		select {
		case sc.tmplCh <- ev:
			WSSyncEventsTotal.WithLabelValues(sc.clusterID, "template_delete").Inc()
		default:
			WSSyncEventsDroppedTotal.WithLabelValues(sc.clusterID, "template_delete").Inc()
			log.Printf("syncManager: dropped template_delete for cluster %s (buffer full)", sc.clusterID)
		}
	}
}

// broadcastFederation fans one capacity batch out to every connected Worker,
// including the originating one (a Worker filters its own cluster out when it
// answers foreign-capacity queries, so the echo is harmless).
func (m *SyncManager) broadcastFederation(items []*syncv1.EnvCapacity) {
	if len(items) == 0 {
		return
	}
	ev := &syncv1.FederationBroadcast{Items: items}
	for _, sc := range m.snapshotConns() {
		select {
		case sc.fedCh <- ev:
			WSSyncEventsTotal.WithLabelValues(sc.clusterID, "federation").Inc()
		default:
			WSSyncEventsDroppedTotal.WithLabelValues(sc.clusterID, "federation").Inc()
			log.Printf("syncManager: dropped federation for cluster %s (buffer full)", sc.clusterID)
		}
	}
}

func (m *SyncManager) broadcastClusterConfig(snap cluster.ClusterConfig) {
	ev := &syncv1.ClusterConfigEvent{Snapshot: clusterConfigToProto(snap)}
	for _, sc := range m.snapshotConns() {
		select {
		case sc.cfgCh <- ev:
			WSSyncEventsTotal.WithLabelValues(sc.clusterID, "cluster_config").Inc()
		default:
			WSSyncEventsDroppedTotal.WithLabelValues(sc.clusterID, "cluster_config").Inc()
			log.Printf("syncManager: dropped cluster_config for cluster %s (buffer full)", sc.clusterID)
		}
	}
}

// ── Exported helpers used by the handlers subpackage ─────────────────────────

// BroadcastKeyMeta broadcasts an APIKey upsert event derived from an
// apikey.KeyMetadata. Used by the dashboard-facing /v1/api-keys handler in
// the handlers subpackage so a Hub-side create lands in every Worker.
func (m *SyncManager) BroadcastKeyMeta(meta apikey.KeyMetadata) {
	m.broadcastKeyUpsert(metaToProto(meta))
}

// BroadcastKeyDelete broadcasts an APIKey delete event by secret name.
func (m *SyncManager) BroadcastKeyDelete(secretName string) {
	m.broadcastKeyDelete(secretName)
}

// BroadcastTemplateUpsert broadcasts a SandboxTemplate upsert event carrying
// the full JSON-encoded template body.
func (m *SyncManager) BroadcastTemplateUpsert(raw []byte) {
	m.broadcastTemplateUpsert(raw)
}

// BroadcastTemplateDelete broadcasts a SandboxTemplate delete event by name.
func (m *SyncManager) BroadcastTemplateDelete(name string) {
	m.broadcastTemplateDelete(name)
}

// GetDeps returns the Deps struct for read-only access by the handlers subpackage.
func (m *SyncManager) GetDeps() Deps { return m.deps }

// LoadCatalog reads the images catalog from the master cluster's ConfigMap.
func (m *SyncManager) LoadCatalog(ctx context.Context) ([]ImageDataset, error) {
	return m.loadCatalog(ctx)
}

// SaveCatalog writes the images catalog to the master cluster's ConfigMap.
func (m *SyncManager) SaveCatalog(ctx context.Context, datasets []ImageDataset) error {
	return m.saveCatalog(ctx, datasets)
}

// BroadcastClusterConfig serialises the full cluster config snapshot and
// pushes it to every connected Worker via the per-cluster ClusterConfig
// stream. Suppresses the log line when the snapshot is identical to the last
// broadcast (steady-state ticks shouldn't spam the log).
func (m *SyncManager) BroadcastClusterConfig() {
	snap := m.currentSnapshot()
	if isEmptySnapshot(snap) {
		log.Printf("syncManager: BroadcastClusterConfig: snapshot is empty, skipping broadcast")
		return
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		log.Printf("syncManager: BroadcastClusterConfig: marshal error: %v", err)
		return
	}
	hash := sha256.Sum256(raw)
	m.mu.Lock()
	unchanged := hash == m.lastBroadcastHash
	if !unchanged {
		m.lastBroadcastHash = hash
	}
	m.mu.Unlock()
	m.broadcastClusterConfig(snap)
	if !unchanged {
		log.Printf("syncManager: broadcast cluster_config to all workers (clusters=%d, hostAliases=%d)",
			len(snap.Clusters), len(snap.HostAliases))
	}
}

// RegisterLegacyRoutes registers the legacy /internal/* HTTP handlers onto the
// provided Gin RouterGroup.
func (m *SyncManager) RegisterLegacyRoutes(rg *gin.RouterGroup) {
	rg.POST("/api-keys", m.handleInternalCreate)
	rg.GET("/api-keys", m.handleInternalList)
	rg.DELETE("/api-keys/:name", m.handleInternalDelete)
	rg.GET("/clusters/status", m.handleClusterStatus)
	rg.GET("/status", m.handleInternalStatus)
	rg.GET("/images-catalog", m.handleImagesCatalogList)
	rg.POST("/images-catalog", m.handleImagesCatalogUpsert)
	rg.PUT("/images-catalog/:id", m.handleImagesCatalogUpsert)
	rg.DELETE("/images-catalog/:id", m.handleImagesCatalogDelete)
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
