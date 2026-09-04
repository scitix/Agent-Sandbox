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
	"io"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service/federation"
	syncv1 "github.com/scitix/agent-sandbox/pkg/proto/sandbox/sync/v1"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
)

// ErrSyncNotConnected is returned when an operation requires an active sync
// session to ws-proxy but none is established.
var ErrSyncNotConnected = errors.New("ws-proxy sync not connected")

// SyncHTTPError carries an HTTP-style status code translated from a gRPC
// status code. It is the cross-package error type that
// pkg/apiserver/handlers/server.go uses to map sync forwarding failures into
// the appropriate native API responses.
type SyncHTTPError struct {
	Status  int
	Message string
}

func (e *SyncHTTPError) Error() string { return e.Message }

// CreateKeyRequest carries the parameters for forwarding an API key
// CreateKey RPC to the Hub.
type CreateKeyRequest struct {
	Namespace   string
	User        string
	Team        string
	Role        string
	Description string
	QuotaURL    string
	ExpiresAt   string // RFC3339; empty = never expires

	// Import/promote mode: when TokenHash + HashPrefix are set the Hub calls
	// CreateFromHash instead of generating a new token.
	TokenHash  string
	HashPrefix string
	IssuedAt   string // RFC3339
	RawToken   string // plaintext token for promote
}

// CreateKeyResponse is the parsed result of a successful Hub-side CreateKey.
type CreateKeyResponse struct {
	RawToken   string
	KeyID      string
	TokenHash  string
	HashPrefix string
	IssuedAt   string // RFC3339
}

// ClusterConfigSink receives ClusterConfig snapshots from ws-proxy and
// persists them (e.g. to a Kubernetes ConfigMap) so that other Worker
// components (ExtProc, in-process DNS resolver, ...) can read the latest
// state. Implementations must treat a zero-valued snapshot (no clusters and
// no host aliases) as a no-op.
type ClusterConfigSink interface {
	ApplyClusterConfig(ctx context.Context, cfg cluster.ClusterConfig) error
}

// SyncService manages the gRPC client connection to ws-proxy. It exposes
// synchronous request methods for the apiserver business layer and runs
// background goroutines that consume the three Watch* streams to keep the
// local KeyStore / SandboxTemplate / ClusterConfig state up to date.
type SyncService interface {
	// OnConnect registers a new ClientConn (produced by wsmux.DialGRPC after
	// a fresh /v1/ws/sync upgrade). Returns a connection generation ID that
	// must be passed back to OnDisconnect so that a stale teardown does not
	// race against a newer connect.
	OnConnect(conn *grpc.ClientConn) uint64

	// OnDisconnect tears down all background Watch goroutines associated
	// with connID and clears the ClientConn — but only if connID still
	// matches the currently-registered conn.
	OnDisconnect(connID uint64)

	RequestCreate(ctx context.Context, req CreateKeyRequest) (*CreateKeyResponse, error)
	RequestDelete(ctx context.Context, name string) error
	RequestTemplateCreate(ctx context.Context, raw json.RawMessage) error
	RequestTemplateUpdate(ctx context.Context, raw json.RawMessage) error
	RequestTemplateDelete(ctx context.Context, name string) error
}

// syncServiceImpl is the live SyncService backed by a gRPC ClientConn.
type syncServiceImpl struct {
	store apikey.KeyStore
	log   interface {
		Info(msg string, keysAndValues ...any)
		Error(err error, msg string, keysAndValues ...any)
	}

	mu     sync.RWMutex
	conn   *grpc.ClientConn
	connID uint64
	cancel context.CancelFunc // cancels the Watch goroutines for the current conn

	keyClient    syncv1.APIKeyServiceClient
	tmplClient   syncv1.TemplateServiceClient
	configClient syncv1.ClusterConfigServiceClient
	fedClient    syncv1.FederationServiceClient

	templateSvc SandboxTemplateService // may be nil when template sync is not configured
	clusterSink ClusterConfigSink      // may be nil when cluster config sync is not configured

	// Cross-cluster capacity federation. All nil when federation is not
	// configured (single-cluster deployments), in which case the report/watch
	// goroutines are not started.
	vaultClient       syncv1.VaultServiceClient
	vaultSink         VaultSink
	fedRegistry       *federation.Registry
	fedSource         federation.CapacitySource
	localClusterID    string
	fedReportInterval time.Duration
	// fedMetricsAt throttles federation gauge republishing. Accessed only from
	// the single runWatchFederation goroutine, so it needs no lock.
	fedMetricsAt time.Time
}

// NewSyncService creates a new SyncService.
func NewSyncService(store apikey.KeyStore) SyncService {
	return &syncServiceImpl{
		store: store,
		log:   ctrl.Log.WithName("sync-service"),
	}
}

// NewSyncServiceWithTemplate creates a SyncService that also handles
// SandboxTemplate sync events.
func NewSyncServiceWithTemplate(store apikey.KeyStore, templateSvc SandboxTemplateService) SyncService {
	return &syncServiceImpl{
		store:       store,
		templateSvc: templateSvc,
		log:         ctrl.Log.WithName("sync-service"),
	}
}

// NewSyncServiceFull creates a SyncService with all optional components wired in.
func NewSyncServiceFull(store apikey.KeyStore, templateSvc SandboxTemplateService, clusterSink ClusterConfigSink) SyncService {
	return &syncServiceImpl{
		store:       store,
		templateSvc: templateSvc,
		clusterSink: clusterSink,
		log:         ctrl.Log.WithName("sync-service"),
	}
}

// ── OnConnect / OnDisconnect ──────────────────────────────────────────────────

func (s *syncServiceImpl) OnConnect(conn *grpc.ClientConn) uint64 {
	s.mu.Lock()
	if s.conn != nil {
		s.log.Info("ws-proxy connection replaced; tearing down previous")
		if s.cancel != nil {
			s.cancel()
		}
	}
	s.connID++
	id := s.connID
	s.conn = conn
	s.keyClient = syncv1.NewAPIKeyServiceClient(conn)
	s.vaultClient = syncv1.NewVaultServiceClient(conn)
	s.tmplClient = syncv1.NewTemplateServiceClient(conn)
	s.configClient = syncv1.NewClusterConfigServiceClient(conn)
	s.fedClient = syncv1.NewFederationServiceClient(conn)
	fedEnabled := s.fedRegistry != nil && s.fedSource != nil

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.mu.Unlock()

	s.log.Info("ws-proxy connection established", "connID", id)

	// Background Watch goroutines: each consumes its server-stream until it
	// errors or the context is cancelled. On unexpected stream end we log and
	// exit; the outer reconnect loop in handlers/sync.go produces a new
	// ClientConn (and a new OnConnect) when a fresh WS dial succeeds.
	go s.runWatchKeys(ctx, id)
	go s.runWatchVault(ctx, id)
	go s.runWatchTemplates(ctx, id)
	go s.runWatchClusterConfig(ctx, id)
	if fedEnabled {
		go s.runWatchFederation(ctx, id)
		go s.runReportFederation(ctx, id)
	}
	return id
}

func (s *syncServiceImpl) OnDisconnect(connID uint64) {
	s.mu.Lock()
	if s.connID != connID {
		s.mu.Unlock()
		s.log.Info("ws-proxy OnDisconnect skipped; stale", "staleConnID", connID, "currentConnID", s.connID)
		return
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.conn = nil
	s.keyClient = nil
	s.vaultClient = nil
	s.tmplClient = nil
	s.configClient = nil
	s.fedClient = nil
	s.mu.Unlock()
	s.log.Info("ws-proxy connection lost", "connID", connID)
}

// currentClients snapshots the unary-RPC clients alongside the underlying
// ClientConn (used by callers to detect "not connected"). The Watch* clients
// are read directly inside their goroutines so they intentionally do not flow
// through here.
func (s *syncServiceImpl) currentClients() (kc syncv1.APIKeyServiceClient, tc syncv1.TemplateServiceClient, conn *grpc.ClientConn) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.keyClient, s.tmplClient, s.conn
}

// ── unary requests forwarded to the Hub ──────────────────────────────────────

func (s *syncServiceImpl) RequestCreate(ctx context.Context, req CreateKeyRequest) (*CreateKeyResponse, error) {
	kc, _, conn := s.currentClients()
	if conn == nil {
		return nil, ErrSyncNotConnected
	}
	pbReq := &syncv1.CreateKeyRequest{
		Namespace:   req.Namespace,
		User:        req.User,
		Team:        req.Team,
		Role:        req.Role,
		Description: req.Description,
		QuotaUrl:    req.QuotaURL,
		TokenHash:   req.TokenHash,
		HashPrefix:  req.HashPrefix,
		RawToken:    req.RawToken,
	}
	if req.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, req.ExpiresAt); err == nil {
			pbReq.ExpiresAt = timestamppb.New(t)
		}
	}
	if req.IssuedAt != "" {
		if t, err := time.Parse(time.RFC3339, req.IssuedAt); err == nil {
			pbReq.IssuedAt = timestamppb.New(t)
		}
	}
	resp, err := kc.CreateKey(ctx, pbReq)
	if err != nil {
		return nil, translateGRPCError(err)
	}
	out := &CreateKeyResponse{
		RawToken:   resp.RawToken,
		KeyID:      resp.KeyId,
		TokenHash:  resp.TokenHash,
		HashPrefix: resp.HashPrefix,
	}
	if resp.IssuedAt != nil {
		out.IssuedAt = resp.IssuedAt.AsTime().UTC().Format(time.RFC3339)
	}
	return out, nil
}

func (s *syncServiceImpl) RequestDelete(ctx context.Context, name string) error {
	kc, _, conn := s.currentClients()
	if conn == nil {
		return ErrSyncNotConnected
	}
	_, err := kc.DeleteKey(ctx, &syncv1.DeleteKeyRequest{KeyId: name})
	if err != nil {
		return translateGRPCError(err)
	}
	return nil
}

func (s *syncServiceImpl) RequestTemplateCreate(ctx context.Context, raw json.RawMessage) error {
	_, tc, conn := s.currentClients()
	if conn == nil {
		return ErrSyncNotConnected
	}
	_, err := tc.CreateTemplate(ctx, &syncv1.CreateTemplateRequest{TemplateJson: raw})
	if err != nil {
		return translateGRPCError(err)
	}
	return nil
}

func (s *syncServiceImpl) RequestTemplateUpdate(ctx context.Context, raw json.RawMessage) error {
	_, tc, conn := s.currentClients()
	if conn == nil {
		return ErrSyncNotConnected
	}
	_, err := tc.UpdateTemplate(ctx, &syncv1.UpdateTemplateRequest{TemplateJson: raw})
	if err != nil {
		return translateGRPCError(err)
	}
	return nil
}

func (s *syncServiceImpl) RequestTemplateDelete(ctx context.Context, name string) error {
	_, tc, conn := s.currentClients()
	if conn == nil {
		return ErrSyncNotConnected
	}
	_, err := tc.DeleteTemplate(ctx, &syncv1.DeleteTemplateRequest{Name: name})
	if err != nil {
		return translateGRPCError(err)
	}
	return nil
}

// ── Watch goroutines: consume server-streams, apply to local state ───────────

func (s *syncServiceImpl) runWatchKeys(ctx context.Context, connID uint64) {
	s.mu.RLock()
	kc := s.keyClient
	s.mu.RUnlock()
	if kc == nil {
		return
	}
	stream, err := kc.WatchKeys(ctx, &syncv1.WatchKeysRequest{})
	if err != nil {
		s.log.Error(err, "WatchKeys subscribe failed", "connID", connID)
		return
	}
	for {
		ev, err := stream.Recv()
		if err != nil {
			if !errors.Is(err, io.EOF) && status.Code(err) != codes.Canceled {
				s.log.Error(err, "WatchKeys recv error", "connID", connID)
			}
			return
		}
		s.dispatchKeyEvent(ctx, ev)
	}
}

func (s *syncServiceImpl) dispatchKeyEvent(ctx context.Context, ev *syncv1.KeyEvent) {
	switch k := ev.Kind.(type) {
	case *syncv1.KeyEvent_Snapshot:
		for _, item := range k.Snapshot.Items {
			if err := s.applyKeyUpsert(ctx, item); err != nil {
				s.log.Error(err, "key snapshot apply error", "secretName", item.SecretName)
			}
		}
	case *syncv1.KeyEvent_Upsert:
		if err := s.applyKeyUpsert(ctx, k.Upsert); err != nil {
			s.log.Error(err, "key upsert apply error", "secretName", k.Upsert.SecretName)
		}
	case *syncv1.KeyEvent_Delete:
		if k.Delete.SecretName == "" {
			return
		}
		if err := s.store.Delete(ctx, k.Delete.SecretName); err != nil &&
			!errors.Is(err, apikey.ErrTokenNotFound) {
			s.log.Error(err, "failed to delete key from sync", "name", k.Delete.SecretName)
		}
	}
}

func (s *syncServiceImpl) applyKeyUpsert(ctx context.Context, m *syncv1.APIKeyMetadata) error {
	if m == nil || m.TokenHash == "" || m.HashPrefix == "" {
		return nil
	}
	meta := apikey.KeyMetadata{
		Namespace:   m.Namespace,
		Role:        m.Role,
		User:        m.User,
		Team:        m.Team,
		QuotaURL:    m.QuotaUrl,
		Description: m.Description,
		RawToken:    m.RawToken,
	}
	if m.IssuedAt != nil {
		meta.IssuedAt = m.IssuedAt.AsTime()
	}
	if m.ExpiresAt != nil {
		meta.ExpiresAt = m.ExpiresAt.AsTime()
	}
	return s.store.CreateFromHash(ctx, meta, m.TokenHash, m.HashPrefix)
}

func (s *syncServiceImpl) runWatchTemplates(ctx context.Context, connID uint64) {
	s.mu.RLock()
	tc := s.tmplClient
	s.mu.RUnlock()
	if tc == nil {
		return
	}
	stream, err := tc.WatchTemplates(ctx, &syncv1.WatchTemplatesRequest{})
	if err != nil {
		s.log.Error(err, "WatchTemplates subscribe failed", "connID", connID)
		return
	}
	for {
		ev, err := stream.Recv()
		if err != nil {
			if !errors.Is(err, io.EOF) && status.Code(err) != codes.Canceled {
				s.log.Error(err, "WatchTemplates recv error", "connID", connID)
			}
			return
		}
		s.dispatchTemplateEvent(ctx, ev)
	}
}

func (s *syncServiceImpl) dispatchTemplateEvent(ctx context.Context, ev *syncv1.TemplateEvent) {
	switch k := ev.Kind.(type) {
	case *syncv1.TemplateEvent_Snapshot:
		// Apply each item; afterwards strip the "global" label off any
		// locally-cached template that the Hub no longer knows about.
		knownNames := make(map[string]struct{}, len(k.Snapshot.TemplateJsons))
		for _, raw := range k.Snapshot.TemplateJsons {
			tmpl, err := jsonToTemplate(raw)
			if err != nil || tmpl == nil {
				s.log.Error(err, "template snapshot decode error")
				continue
			}
			knownNames[tmpl.Name] = struct{}{}
			if appErr := s.applyTemplate(ctx, tmpl); appErr != nil {
				s.log.Error(errors.New(appErr.Message), "template snapshot apply error", "name", tmpl.Name)
			}
		}
		if s.templateSvc != nil {
			if appErr := s.templateSvc.StripStaleGlobalLabels(ctx, knownNames); appErr != nil {
				s.log.Error(errors.New(appErr.Message), "strip stale global labels failed")
			}
		}
	case *syncv1.TemplateEvent_Upsert:
		tmpl, err := jsonToTemplate(k.Upsert.TemplateJson)
		if err != nil || tmpl == nil {
			s.log.Error(err, "template upsert decode error")
			return
		}
		if appErr := s.applyTemplate(ctx, tmpl); appErr != nil {
			s.log.Error(errors.New(appErr.Message), "template upsert apply error", "name", tmpl.Name)
		}
	case *syncv1.TemplateEvent_Delete:
		if k.Delete.Name == "" || s.templateSvc == nil {
			return
		}
		if appErr := s.templateSvc.Delete(ctx, k.Delete.Name); appErr != nil &&
			appErr.Code != domain.ErrCodeNotFound {
			s.log.Error(errors.New(appErr.Message), "template delete apply error", "name", k.Delete.Name)
		}
	}
}

func (s *syncServiceImpl) applyTemplate(ctx context.Context, tmpl *agentsv1alpha1.SandboxTemplate) *domain.AppError {
	if s.templateSvc == nil {
		return nil
	}
	return s.templateSvc.CreateOrUpdate(ctx, tmpl)
}

func (s *syncServiceImpl) runWatchClusterConfig(ctx context.Context, connID uint64) {
	s.mu.RLock()
	cc := s.configClient
	s.mu.RUnlock()
	if cc == nil {
		return
	}
	stream, err := cc.WatchClusterConfig(ctx, &syncv1.WatchClusterConfigRequest{})
	if err != nil {
		s.log.Error(err, "WatchClusterConfig subscribe failed", "connID", connID)
		return
	}
	for {
		ev, err := stream.Recv()
		if err != nil {
			if !errors.Is(err, io.EOF) && status.Code(err) != codes.Canceled {
				s.log.Error(err, "WatchClusterConfig recv error", "connID", connID)
			}
			return
		}
		s.applyClusterConfigEvent(ctx, ev)
	}
}

func (s *syncServiceImpl) applyClusterConfigEvent(ctx context.Context, ev *syncv1.ClusterConfigEvent) {
	if ev == nil || ev.Snapshot == nil {
		s.log.Info("cluster_config event: empty snapshot, skipping")
		return
	}
	cfg := protoToClusterConfig(ev.Snapshot)

	// Defensive: drop entries with empty IDs but keep the rest.
	valid := cfg.Clusters[:0]
	for i, e := range cfg.Clusters {
		if e.ID == "" {
			s.log.Info("cluster_config: entry has empty ID, skipping", "index", i)
			continue
		}
		valid = append(valid, e)
	}
	cfg.Clusters = valid

	if len(cfg.Clusters) == 0 && len(cfg.HostAliases) == 0 {
		s.log.Info("cluster_config: snapshot carries no actionable data, skipping")
		return
	}
	if s.clusterSink == nil {
		return
	}
	if err := s.clusterSink.ApplyClusterConfig(ctx, cfg); err != nil {
		s.log.Error(err, "cluster_config: apply failed")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// jsonToTemplate decodes a JSON-encoded SandboxTemplate sent over the wire.
// Returns (nil, nil) for an empty body so a transient empty event during a
// rolling upgrade does not look like an error.
func jsonToTemplate(raw []byte) (*agentsv1alpha1.SandboxTemplate, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var tmpl agentsv1alpha1.SandboxTemplate
	if err := json.Unmarshal(raw, &tmpl); err != nil {
		return nil, fmt.Errorf("unmarshal SandboxTemplate: %w", err)
	}
	return &tmpl, nil
}

// translateGRPCError maps a gRPC status into the *SyncHTTPError contract that
// upstream handlers expect, preserving the existing HTTP status table used by
// pkg/apiserver/handlers/server.go and apikey_service.go.
func translateGRPCError(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	httpStatus := grpcCodeToHTTP(st.Code())
	if httpStatus == 0 {
		return err
	}
	return &SyncHTTPError{Status: httpStatus, Message: st.Message()}
}

// grpcCodeToHTTP mirrors the table used by the v1 protocol.Frame wire format
// so existing translation logic in handlers/server.go (200/400/404/409/429/503)
// works unchanged.
func grpcCodeToHTTP(c codes.Code) int {
	switch c {
	case codes.OK:
		return 200
	case codes.InvalidArgument:
		return 400
	case codes.NotFound:
		return 404
	case codes.AlreadyExists:
		return 409
	case codes.ResourceExhausted:
		return 429
	case codes.Unavailable:
		return 503
	case codes.Unauthenticated:
		return 401
	case codes.PermissionDenied:
		return 403
	case codes.DeadlineExceeded:
		return 504
	default:
		return 500
	}
}

// protoToClusterConfig translates a proto ClusterConfig into the in-memory
// cluster.ClusterConfig type. Mirrors syncmgr/grpc_convert.go on the Hub side.
func protoToClusterConfig(p *syncv1.ClusterConfig) cluster.ClusterConfig {
	out := cluster.ClusterConfig{
		Clusters:    make([]cluster.ClusterEntry, 0, len(p.Clusters)),
		HostAliases: make([]corev1.HostAlias, 0, len(p.HostAliases)),
	}
	for _, c := range p.Clusters {
		entry := cluster.ClusterEntry{
			ID:       c.Id,
			Name:     c.Name,
			URL:      c.Url,
			Selector: c.Selector,
			Headers:  c.Headers,
			Visible:  c.Visible,
		}
		if c.Gateway != nil {
			entry.Gateway = &cluster.GatewayConfig{
				NativeURL:     c.Gateway.NativeUrl,
				E2BURL:        c.Gateway.E2BUrl,
				DataURL:       c.Gateway.DataUrl,
				Headers:       c.Gateway.Headers,
				NativeHeaders: c.Gateway.NativeHeaders,
				E2BHeaders:    c.Gateway.E2BHeaders,
				DataHeaders:   c.Gateway.DataHeaders,
			}
		}
		for _, r := range c.Registries {
			entry.Registries = append(entry.Registries, cluster.RegistryEntry{
				Host: r.Host,
				Type: r.Type,
			})
		}
		out.Clusters = append(out.Clusters, entry)
	}
	for _, ha := range p.HostAliases {
		out.HostAliases = append(out.HostAliases, corev1.HostAlias{
			IP:        ha.Ip,
			Hostnames: append([]string(nil), ha.Hostnames...),
		})
	}
	return out
}
