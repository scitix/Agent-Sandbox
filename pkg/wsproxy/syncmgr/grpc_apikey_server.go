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
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	syncv1 "github.com/scitix/agent-sandbox/pkg/proto/sandbox/sync/v1"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
)

// apiKeyServer implements the Hub side of APIKeyService for a single
// connected Worker cluster. One instance per *clusterSyncConn.
type apiKeyServer struct {
	syncv1.UnimplementedAPIKeyServiceServer
	m  *SyncManager
	sc *clusterSyncConn
}

func newAPIKeyServer(m *SyncManager, sc *clusterSyncConn) *apiKeyServer {
	return &apiKeyServer{m: m, sc: sc}
}

// CreateKey covers both the "generate new token" and "import existing hash"
// paths. The import path triggers when both TokenHash and HashPrefix are set
// in the request — semantically identical to the v1 protocol.Frame behaviour.
func (s *apiKeyServer) CreateKey(ctx context.Context, req *syncv1.CreateKeyRequest) (*syncv1.CreateKeyResponse, error) {
	if s.m.deps.KeyStore == nil {
		return nil, status.Error(codes.Unavailable, "key store not configured")
	}

	if req.TokenHash != "" && req.HashPrefix != "" {
		return s.importKey(ctx, req)
	}

	// Enforce per-user limit (best-effort; race-prone like the v1 path).
	if s.m.deps.MaxPerUser > 0 {
		count, err := s.m.deps.KeyStore.CountUserKeys(ctx, req.Namespace, req.User)
		if err != nil {
			log.Printf("syncmgr/grpc: CreateKey count error: %v", err)
			return nil, status.Error(codes.Internal, "failed to count keys")
		}
		if count >= s.m.deps.MaxPerUser {
			return nil, status.Errorf(codes.AlreadyExists,
				"exceeded max keys per user (%d)", s.m.deps.MaxPerUser)
		}
	}

	role := req.Role
	if role == "" {
		role = apikey.RoleTenant
	}

	meta := apikey.KeyMetadata{
		Namespace:   req.Namespace,
		User:        req.User,
		Team:        req.Team,
		Role:        role,
		Description: req.Description,
		IssuedAt:    time.Now().UTC(),
		ExpiresAt:   protoToTime(req.ExpiresAt),
	}

	rawToken, keyID, err := s.m.deps.KeyStore.Create(ctx, meta)
	if err != nil {
		log.Printf("syncmgr/grpc: CreateKey create error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to create key: %v", err)
	}

	createdMeta, getErr := s.m.deps.KeyStore.Get(ctx, keyID)
	if getErr != nil {
		log.Printf("syncmgr/grpc: CreateKey get-after-create %s error: %v", keyID, getErr)
	}

	// Broadcast key_sync to every Worker (including the requesting one — the
	// idempotent CreateFromHash on receipt is a no-op when the local Secret
	// already exists).
	if createdMeta != nil {
		s.m.broadcastKeyUpsert(metaToProto(*createdMeta))
	}

	const prefix = "agentbox-apikey-"
	hashPrefix := ""
	if len(keyID) > len(prefix) {
		hashPrefix = keyID[len(prefix):]
	}
	return &syncv1.CreateKeyResponse{
		RawToken:   rawToken,
		KeyId:      keyID,
		HashPrefix: hashPrefix,
		IssuedAt:   timestampOrNil(meta.IssuedAt),
	}, nil
}

// importKey handles CreateKey when caller supplied TokenHash + HashPrefix.
// Idempotent: re-importing the same hash is a no-op.
func (s *apiKeyServer) importKey(ctx context.Context, req *syncv1.CreateKeyRequest) (*syncv1.CreateKeyResponse, error) {
	issuedAt := protoToTime(req.IssuedAt)
	if issuedAt.IsZero() {
		issuedAt = time.Now().UTC()
	}
	role := req.Role
	if role == "" {
		role = apikey.RoleTenant
	}

	meta := apikey.KeyMetadata{
		Namespace:   req.Namespace,
		User:        req.User,
		Team:        req.Team,
		Role:        role,
		Description: req.Description,
		QuotaURL:    req.QuotaUrl,
		IssuedAt:    issuedAt,
		ExpiresAt:   protoToTime(req.ExpiresAt),
		RawToken:    req.RawToken,
	}

	if err := s.m.deps.KeyStore.CreateFromHash(ctx, meta, req.TokenHash, req.HashPrefix); err != nil {
		log.Printf("syncmgr/grpc: importKey error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to import key: %v", err)
	}

	keyID := "agentbox-apikey-" + req.HashPrefix
	createdMeta, _ := s.m.deps.KeyStore.Get(ctx, keyID)
	if createdMeta != nil {
		s.m.broadcastKeyUpsert(metaToProto(*createdMeta))
	}

	return &syncv1.CreateKeyResponse{
		KeyId:      keyID,
		HashPrefix: req.HashPrefix,
		IssuedAt:   timestampOrNil(issuedAt),
	}, nil
}

func (s *apiKeyServer) DeleteKey(ctx context.Context, req *syncv1.DeleteKeyRequest) (*syncv1.DeleteKeyResponse, error) {
	if s.m.deps.KeyStore == nil {
		return nil, status.Error(codes.Unavailable, "key store not configured")
	}
	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}
	if err := s.m.deps.KeyStore.Delete(ctx, req.KeyId); err != nil {
		if err == apikey.ErrTokenNotFound {
			return nil, status.Error(codes.NotFound, "api key not found")
		}
		log.Printf("syncmgr/grpc: DeleteKey error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to delete key: %v", err)
	}
	s.m.broadcastKeyDelete(req.KeyId)
	return &syncv1.DeleteKeyResponse{}, nil
}

// WatchKeys streams every key currently held by the Hub (one Snapshot event)
// followed by every change (Upsert / Delete) until the client disconnects.
// One stream per Worker cluster is expected; closing it tears down the
// associated event channel so a stale subscription cannot leak goroutines.
func (s *apiKeyServer) WatchKeys(_ *syncv1.WatchKeysRequest, stream syncv1.APIKeyService_WatchKeysServer) error {
	ctx := stream.Context()

	// Send initial snapshot before subscribing so events that fire after the
	// List() call land in the channel and replay correctly.
	if s.m.deps.KeyStore != nil {
		metas, err := s.m.deps.KeyStore.List(ctx)
		if err != nil {
			return status.Errorf(codes.Internal, "list keys: %v", err)
		}
		snap := &syncv1.KeySnapshot{Items: make([]*syncv1.APIKeyMetadata, 0, len(metas))}
		for _, m := range metas {
			snap.Items = append(snap.Items, metaToProto(m))
		}
		if err := stream.Send(&syncv1.KeyEvent{Kind: &syncv1.KeyEvent_Snapshot{Snapshot: snap}}); err != nil {
			return err
		}
	}

	// Loop on the per-cluster event channel. The channel was created at
	// dialCluster() time and is closed in the read loop's defer.
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-s.sc.keyCh:
			if !ok {
				return nil
			}
			if err := stream.Send(ev); err != nil {
				return err
			}
		}
	}
}

// timestampOrNil returns a proto Timestamp for non-zero t, else nil. Used to
// keep API responses faithful to "unset" semantics rather than emitting epoch.
func timestampOrNil(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t.UTC())
}
