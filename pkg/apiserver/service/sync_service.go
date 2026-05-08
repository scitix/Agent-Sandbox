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

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	ctrl "sigs.k8s.io/controller-runtime"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/api/protocol"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
)

// newCorrelationID generates a new UUID v4 string for request correlation.
func newCorrelationID() string {
	return uuid.New().String()
}

// ErrSyncNotConnected is returned when an operation requires an active WS connection
// to ws-proxy but none is established.
var ErrSyncNotConnected = errors.New("ws-proxy sync not connected")

// ── Re-export protocol types for existing callers ────────────────────────────
//
// Downstream code (handlers, tests) already imports this package and references
// service.SyncFrameType, service.SyncEvent, etc.  Providing type aliases keeps
// those callers working without modification while the canonical definitions
// now live in pkg/api/protocol.

// SyncFrameType is an alias for protocol.FrameType.
type SyncFrameType = protocol.FrameType

// SyncEvent is an alias for protocol.Frame — the unified wire-format envelope.
type SyncEvent = protocol.Frame

// CreateKeyRequest is an alias for protocol.CreateKeyRequest.
type CreateKeyRequest = protocol.CreateKeyRequest

// CreateKeyResponse is an alias for protocol.CreateKeyResponse.
type CreateKeyResponse = protocol.CreateKeyResponse

// SyncHTTPError is an alias for protocol.HTTPError.
type SyncHTTPError = protocol.HTTPError

// Frame type constants — re-exported from the protocol package.
const (
	FrameKeySync       = protocol.FrameKeySync
	FrameKeyDeleteSync = protocol.FrameKeyDeleteSync
	FrameKeySnapshot   = protocol.FrameKeySnapshot
	FrameKeyCreateResp = protocol.FrameKeyCreateResp
	FrameKeyDeleteResp = protocol.FrameKeyDeleteResp

	FrameTemplateSnapshot   = protocol.FrameTemplateSnapshot
	FrameTemplateSync       = protocol.FrameTemplateSync
	FrameTemplateDeleteSync = protocol.FrameTemplateDeleteSync
	FrameTemplateCreateResp = protocol.FrameTemplateCreateResp
	FrameTemplateUpdateResp = protocol.FrameTemplateUpdateResp
	FrameTemplateDeleteResp = protocol.FrameTemplateDeleteResp

	FrameClusterConfigSync     = protocol.FrameClusterConfigSync
	FrameClusterConfigSnapshot = protocol.FrameClusterConfigSnapshot
)

// ── ClusterConfigSink ─────────────────────────────────────────────────────────

// ClusterConfigSink receives ClusterConfig snapshots from ws-proxy and persists
// them (e.g. to a Kubernetes ConfigMap) so that other Worker components such
// as ExtProc and the in-process DNS resolver can read the latest state.
//
// Implementations must treat a zero-valued snapshot (no clusters and no host
// aliases) as a no-op so that a transient empty push cannot erase an already
// populated configuration.
type ClusterConfigSink interface {
	ApplyClusterConfig(ctx context.Context, cfg cluster.ClusterConfig) error
}

// ── SyncService interface ─────────────────────────────────────────────────────

// SyncService manages the full-duplex WS channel between a Worker and ws-proxy.
// It handles inbound sync pushes (key_sync, key_delete_sync, key_snapshot,
// template_sync, template_delete_sync, template_snapshot) and
// outbound synchronous requests (key_create, key_delete, template_create,
// template_update, template_delete).
type SyncService interface {
	// OnConnect registers the ws-proxy WS connection. The caller is responsible
	// for reading from the connection; the service only writes to it.
	// It returns a connection ID that must be passed to OnDisconnect so that
	// a stale connection's cleanup does not accidentally clear a newer one.
	OnConnect(conn *websocket.Conn) uint64

	// OnDisconnect clears the active connection only if connID matches the
	// current connection, and cancels all pending requests. This prevents a
	// stale connection's deferred cleanup from clobbering a newer connection.
	OnDisconnect(connID uint64)

	// RequestCreate sends a key_create frame to ws-proxy and waits for the
	// key_create_resp (up to ctx deadline). Returns ErrSyncNotConnected when
	// no connection is active.
	RequestCreate(ctx context.Context, req CreateKeyRequest) (*CreateKeyResponse, error)

	// RequestDelete sends a key_delete frame to ws-proxy and waits for the
	// key_delete_resp. Returns ErrSyncNotConnected when no connection is active.
	RequestDelete(ctx context.Context, name string) error

	// RequestTemplateCreate sends a template_create frame to ws-proxy and waits
	// for template_create_resp. Returns ErrSyncNotConnected when no connection is
	// active.
	RequestTemplateCreate(ctx context.Context, raw json.RawMessage) error

	// RequestTemplateUpdate sends a template_update frame to ws-proxy and waits
	// for template_update_resp. Returns ErrSyncNotConnected when no connection is
	// active.
	RequestTemplateUpdate(ctx context.Context, raw json.RawMessage) error

	// RequestTemplateDelete sends a template_delete frame to ws-proxy and waits
	// for template_delete_resp. Returns ErrSyncNotConnected when no connection is
	// active.
	RequestTemplateDelete(ctx context.Context, name string) error

	// HandleIncoming processes a frame received from ws-proxy. Responses to
	// pending requests are routed via the pendingMap; push frames are applied
	// to the key store or template service.
	HandleIncoming(ctx context.Context, event SyncEvent) error
}

// ── pendingEntry ─────────────────────────────────────────────────────────────

type pendingEntry struct {
	ch chan protocol.Frame
}

// ── syncServiceImpl ───────────────────────────────────────────────────────────

type syncServiceImpl struct {
	store apikey.KeyStore
	log   interface {
		Info(msg string, keysAndValues ...any)
		Error(err error, msg string, keysAndValues ...any)
	}

	mu     sync.RWMutex
	conn   *websocket.Conn // nil when disconnected
	connID uint64          // monotonically increasing connection generation

	pending sync.Map // correlationID(string) → *pendingEntry

	templateSvc SandboxTemplateService // may be nil when template sync is not configured
	clusterSink ClusterConfigSink      // may be nil when cluster config sync is not configured
}

// NewSyncService creates a new SyncService.
// store is the local Worker KeyStore used to apply sync events.
func NewSyncService(store apikey.KeyStore) SyncService {
	return &syncServiceImpl{
		store: store,
		log:   ctrl.Log.WithName("sync-service"),
	}
}

// NewSyncServiceWithTemplate creates a new SyncService that also handles
// SandboxTemplate sync events, applying them via templateSvc.
func NewSyncServiceWithTemplate(store apikey.KeyStore, templateSvc SandboxTemplateService) SyncService {
	return &syncServiceImpl{
		store:       store,
		templateSvc: templateSvc,
		log:         ctrl.Log.WithName("sync-service"),
	}
}

// NewSyncServiceFull creates a SyncService with all optional components wired in.
// Use this constructor when both SandboxTemplate sync and ClusterConfig sync are
// required (the typical production setup in Worker clusters that participate in
// multi-cluster routing).
func NewSyncServiceFull(store apikey.KeyStore, templateSvc SandboxTemplateService, clusterSink ClusterConfigSink) SyncService {
	return &syncServiceImpl{
		store:       store,
		templateSvc: templateSvc,
		clusterSink: clusterSink,
		log:         ctrl.Log.WithName("sync-service"),
	}
}

// ── OnConnect / OnDisconnect ──────────────────────────────────────────────────

func (s *syncServiceImpl) OnConnect(conn *websocket.Conn) uint64 {
	s.mu.Lock()
	if s.conn != nil {
		s.log.Info("ws-proxy connection replaced; closing previous connection")
	}
	s.connID++
	id := s.connID
	s.conn = conn
	s.mu.Unlock()
	remoteAddr := ""
	if conn != nil {
		remoteAddr = conn.RemoteAddr().String()
	}
	s.log.Info("ws-proxy connection established", "connID", id, "remoteAddr", remoteAddr)
	return id
}

func (s *syncServiceImpl) OnDisconnect(connID uint64) {
	s.mu.Lock()
	if s.connID != connID {
		// A newer connection has been registered; do not clear it.
		s.mu.Unlock()
		s.log.Info("ws-proxy OnDisconnect skipped; stale connection", "staleConnID", connID, "currentConnID", s.connID)
		return
	}
	s.conn = nil
	s.mu.Unlock()
	s.log.Info("ws-proxy connection lost; cancelling pending requests", "connID", connID)

	// Drain all pending requests with a synthetic error response.
	s.pending.Range(func(key, value any) bool {
		entry := value.(*pendingEntry)
		entry.ch <- protocol.Frame{
			ID:         key.(string),
			Type:       protocol.FrameKeyCreateResp,
			OK:         false,
			Error:      "ws-proxy disconnected",
			HTTPStatus: 503,
		}
		s.pending.Delete(key)
		return true
	})
}

// ── RequestCreate ─────────────────────────────────────────────────────────────

func (s *syncServiceImpl) RequestCreate(ctx context.Context, req CreateKeyRequest) (*CreateKeyResponse, error) {
	id, ch, err := s.sendFrame(protocol.Frame{
		Type:        protocol.FrameKeyCreate,
		Namespace:   req.Namespace,
		User:        req.User,
		Team:        req.Team,
		Role:        req.Role,
		Description: req.Description,
		ExpiresAt:   req.ExpiresAt,
		// import/promote fields (zero values are omitempty, no-op for normal creates)
		TokenHash:  req.TokenHash,
		HashPrefix: req.HashPrefix,
		IssuedAt:   req.IssuedAt,
		QuotaURL:   req.QuotaURL,
		RawToken:   req.RawToken,
	})
	if err != nil {
		return nil, err
	}
	defer s.pending.Delete(id)

	select {
	case resp := <-ch:
		if !resp.OK {
			if resp.HTTPStatus != 0 {
				return nil, &protocol.HTTPError{Status: resp.HTTPStatus, Message: resp.Error}
			}
			return nil, errors.New(resp.Error)
		}
		return &CreateKeyResponse{
			RawToken:   resp.RawToken,
			KeyID:      resp.KeyID,
			TokenHash:  resp.TokenHash,
			HashPrefix: resp.HashPrefix,
			IssuedAt:   resp.IssuedAt,
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ── RequestDelete ─────────────────────────────────────────────────────────────

func (s *syncServiceImpl) RequestDelete(ctx context.Context, name string) error {
	id, ch, err := s.sendFrame(protocol.Frame{
		Type: protocol.FrameKeyDelete,
		Name: name,
	})
	if err != nil {
		return err
	}
	defer s.pending.Delete(id)

	select {
	case resp := <-ch:
		if !resp.OK {
			if resp.HTTPStatus != 0 {
				return &protocol.HTTPError{Status: resp.HTTPStatus, Message: resp.Error}
			}
			return errors.New(resp.Error)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ── RequestTemplateCreate ─────────────────────────────────────────────────────

func (s *syncServiceImpl) RequestTemplateCreate(ctx context.Context, raw json.RawMessage) error {
	id, ch, err := s.sendFrame(protocol.Frame{
		Type:         protocol.FrameTemplateCreate,
		TemplateFull: raw,
	})
	if err != nil {
		return err
	}
	defer s.pending.Delete(id)

	select {
	case resp := <-ch:
		if !resp.OK {
			if resp.HTTPStatus != 0 {
				return &protocol.HTTPError{Status: resp.HTTPStatus, Message: resp.Error}
			}
			return errors.New(resp.Error)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ── RequestTemplateUpdate ─────────────────────────────────────────────────────

func (s *syncServiceImpl) RequestTemplateUpdate(ctx context.Context, raw json.RawMessage) error {
	id, ch, err := s.sendFrame(protocol.Frame{
		Type:         protocol.FrameTemplateUpdate,
		TemplateFull: raw,
	})
	if err != nil {
		return err
	}
	defer s.pending.Delete(id)

	select {
	case resp := <-ch:
		if !resp.OK {
			if resp.HTTPStatus != 0 {
				return &protocol.HTTPError{Status: resp.HTTPStatus, Message: resp.Error}
			}
			return errors.New(resp.Error)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ── RequestTemplateDelete ─────────────────────────────────────────────────────

func (s *syncServiceImpl) RequestTemplateDelete(ctx context.Context, name string) error {
	id, ch, err := s.sendFrame(protocol.Frame{
		Type: protocol.FrameTemplateDelete,
		Name: name,
	})
	if err != nil {
		return err
	}
	defer s.pending.Delete(id)

	select {
	case resp := <-ch:
		if !resp.OK {
			if resp.HTTPStatus != 0 {
				return &protocol.HTTPError{Status: resp.HTTPStatus, Message: resp.Error}
			}
			return errors.New(resp.Error)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ── HandleIncoming ────────────────────────────────────────────────────────────

func (s *syncServiceImpl) HandleIncoming(ctx context.Context, event SyncEvent) error {
	switch event.Type {
	case FrameKeyCreateResp, FrameKeyDeleteResp:
		if v, ok := s.pending.Load(event.ID); ok {
			entry := v.(*pendingEntry)
			entry.ch <- event
		}

	case FrameKeySync:
		return s.applyKeySync(ctx, event)

	case FrameKeyDeleteSync:
		if event.Name == "" {
			return nil
		}
		if err := s.store.Delete(ctx, event.Name); err != nil {
			// Ignore not-found; we may have never had this key.
			if !errors.Is(err, apikey.ErrTokenNotFound) {
				s.log.Error(err, "failed to delete key from sync", "name", event.Name)
			}
		}

	case FrameKeySnapshot:
		for _, item := range event.Items {
			if err := s.applyKeySync(ctx, item); err != nil {
				s.log.Error(err, "snapshot apply error", "name", item.Name)
			}
		}

	// ── Template frames ───────────────────────────────────────────────────────

	case FrameTemplateCreateResp, FrameTemplateUpdateResp, FrameTemplateDeleteResp:
		if v, ok := s.pending.Load(event.ID); ok {
			entry := v.(*pendingEntry)
			entry.ch <- event
		}

	case FrameTemplateSync:
		return s.applyTemplateSync(ctx, event)

	case FrameTemplateDeleteSync:
		return s.applyTemplateDelete(ctx, event)

	case FrameTemplateSnapshot:
		for _, item := range event.Items {
			if err := s.applyTemplateSync(ctx, item); err != nil {
				s.log.Error(err, "template snapshot apply error", "templateFull", string(item.TemplateFull))
			}
		}
		if s.templateSvc != nil {
			knownNames := make(map[string]struct{}, len(event.Items))
			for _, item := range event.Items {
				tmpl, err := frameToTemplate(item)
				if err != nil || tmpl == nil {
					continue
				}
				knownNames[tmpl.Name] = struct{}{}
			}
			if appErr := s.templateSvc.StripStaleGlobalLabels(ctx, knownNames); appErr != nil {
				s.log.Error(errors.New(appErr.Message), "failed to strip stale global labels")
			}
		}

	// ── ClusterConfig frames ──────────────────────────────────────────────────

	case FrameClusterConfigSync, FrameClusterConfigSnapshot:
		return s.applyClusterConfig(ctx, event)
	}
	return nil
}

// applyKeySync writes a single key_sync item to the local key store.
func (s *syncServiceImpl) applyKeySync(ctx context.Context, e SyncEvent) error {
	if e.TokenHash == "" || e.HashPrefix == "" {
		return nil
	}

	meta := apikey.KeyMetadata{
		Namespace:   e.Namespace,
		Role:        e.Role,
		User:        e.User,
		Team:        e.Team,
		QuotaURL:    e.QuotaURL,
		Description: e.Description,
		RawToken:    e.RawToken,
	}
	if e.IssuedAt != "" {
		if t, err := time.Parse(time.RFC3339, e.IssuedAt); err == nil {
			meta.IssuedAt = t
		}
	}
	if e.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, e.ExpiresAt); err == nil {
			meta.ExpiresAt = t
		}
	}

	return s.store.CreateFromHash(ctx, meta, e.TokenHash, e.HashPrefix)
}

// applyTemplateSync writes a single template_sync item to the local template service.
func (s *syncServiceImpl) applyTemplateSync(ctx context.Context, e SyncEvent) error {
	if s.templateSvc == nil {
		return nil
	}
	tmpl, err := frameToTemplate(e)
	if err != nil {
		return fmt.Errorf("applyTemplateSync: %w", err)
	}
	if tmpl == nil {
		// Graceful skip: TemplateFull was empty (e.g. during a rolling upgrade).
		return nil
	}
	if appErr := s.templateSvc.CreateOrUpdate(ctx, tmpl); appErr != nil {
		return fmt.Errorf("applyTemplateSync CreateOrUpdate %q: %s", tmpl.Name, appErr.Message)
	}
	s.log.Info("synced template", "name", tmpl.Name)
	return nil
}

// applyTemplateDelete deletes a template locally on receipt of a template_delete_sync frame.
func (s *syncServiceImpl) applyTemplateDelete(ctx context.Context, e SyncEvent) error {
	if s.templateSvc == nil {
		return nil
	}
	if e.Name == "" {
		return nil
	}
	if appErr := s.templateSvc.Delete(ctx, e.Name); appErr != nil {
		// Ignore not-found — we may have never had this template.
		if appErr.Code == domain.ErrCodeNotFound {
			return nil
		}
		return fmt.Errorf("applyTemplateDelete %q: %s", e.Name, appErr.Message)
	}
	s.log.Info("deleted template from sync", "name", e.Name)
	return nil
}

// frameToTemplate converts a protocol.Frame carrying a TemplateFull field into a
// *agentsv1alpha1.SandboxTemplate ready for CreateOrUpdate.
// Returns nil (no error) when TemplateFull is empty, allowing graceful skip
// during rolling upgrades where an older ws-proxy may not populate the field.
func frameToTemplate(e protocol.Frame) (*agentsv1alpha1.SandboxTemplate, error) {
	if len(e.TemplateFull) == 0 {
		return nil, nil // graceful skip: upgrade window or empty frame
	}
	var tmpl agentsv1alpha1.SandboxTemplate
	if err := json.Unmarshal(e.TemplateFull, &tmpl); err != nil {
		return nil, fmt.Errorf("unmarshal full SandboxTemplate: %w", err)
	}
	return &tmpl, nil
}

// applyClusterConfig processes a cluster_config_sync or cluster_config_snapshot
// frame from ws-proxy. It deserialises the full ClusterConfig snapshot and
// forwards it to the ClusterConfigSink (typically a ConfigMapWriter) for
// persistence.
//
// Safety rules:
//   - Missing/empty ConfigSnapshot → no-op (never clears existing config).
//   - Malformed snapshot → error returned to the caller; sink left untouched.
//   - Zero-valued snapshot (no clusters, no host aliases) → treated as a
//     transient empty push and skipped, mirroring the pre-snapshot behaviour.
func (s *syncServiceImpl) applyClusterConfig(ctx context.Context, event SyncEvent) error {
	if len(event.ConfigSnapshot) == 0 {
		s.log.Info("cluster_config_sync: empty snapshot, skipping update")
		return nil
	}

	var cfg cluster.ClusterConfig
	if err := json.Unmarshal(event.ConfigSnapshot, &cfg); err != nil {
		s.log.Error(err, "cluster_config_sync: failed to unmarshal snapshot")
		return err
	}

	// Drop clusters without an ID defensively; do not drop the whole batch.
	valid := cfg.Clusters[:0]
	for i, e := range cfg.Clusters {
		if e.ID == "" {
			s.log.Info("cluster_config_sync: entry has empty ID, skipping", "index", i)
			continue
		}
		valid = append(valid, e)
	}
	cfg.Clusters = valid

	if len(cfg.Clusters) == 0 && len(cfg.HostAliases) == 0 {
		s.log.Info("cluster_config_sync: snapshot carries no actionable data, skipping")
		return nil
	}

	if s.clusterSink == nil {
		return nil
	}

	if err := s.clusterSink.ApplyClusterConfig(ctx, cfg); err != nil {
		s.log.Error(err, "cluster_config_sync: failed to apply cluster config")
		return err
	}

	return nil
}

// ── internal helpers ──────────────────────────────────────────────────────────

// sendFrame writes a protocol.Frame to the WS connection and registers a
// pending channel for the response. Returns the correlationID and channel.
func (s *syncServiceImpl) sendFrame(frame protocol.Frame) (string, chan protocol.Frame, error) {
	s.mu.RLock()
	conn := s.conn
	s.mu.RUnlock()
	if conn == nil {
		s.log.Info("sendFrame: no active ws-proxy connection", "frameType", string(frame.Type))
		return "", nil, ErrSyncNotConnected
	}

	id := newCorrelationID()
	frame.ID = id

	ch := make(chan protocol.Frame, 1)
	s.pending.Store(id, &pendingEntry{ch: ch})

	s.mu.RLock()
	activeConn := s.conn
	s.mu.RUnlock()
	if activeConn == nil {
		s.pending.Delete(id)
		return "", nil, ErrSyncNotConnected
	}

	if err := activeConn.WriteJSON(frame); err != nil {
		s.pending.Delete(id)
		return "", nil, err
	}
	return id, ch, nil
}
