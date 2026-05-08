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
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/scitix/agent-sandbox/pkg/api/protocol"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
)

// sendKeySnapshot sends all existing API key Secrets as a key_snapshot frame.
func (m *SyncManager) sendKeySnapshot(ctx context.Context, sc *clusterSyncConn) error {
	if m.deps.KeyStore == nil {
		return nil
	}
	metas, err := m.deps.KeyStore.List(ctx)
	if err != nil {
		return fmt.Errorf("list secrets: %w", err)
	}
	items := make([]protocol.Frame, 0, len(metas))
	for _, meta := range metas {
		items = append(items, metaToFrame(meta))
	}
	return sc.send(protocol.Frame{Type: protocol.FrameKeySnapshot, Items: items})
}

// handleKeyCreate creates a key on master and broadcasts to all Workers.
// When the frame carries TokenHash + HashPrefix it operates in import/promote
// mode (idempotent, no new token generated).
func (m *SyncManager) handleKeyCreate(ctx context.Context, sc *clusterSyncConn, frame protocol.Frame) {
	// Import/promote mode: caller supplies an existing hash.
	if frame.TokenHash != "" && frame.HashPrefix != "" {
		m.handleKeyImportFromWS(ctx, sc, frame)
		return
	}

	if m.deps.KeyStore == nil {
		_ = sc.send(protocol.Frame{ID: frame.ID, Type: protocol.FrameKeyCreateResp, OK: false,
			Error: "key store not configured", HTTPStatus: 503})
		return
	}

	// Check per-user limit.
	if m.deps.MaxPerUser > 0 {
		count, err := m.deps.KeyStore.CountUserKeys(ctx, frame.Namespace, frame.User)
		if err != nil {
			log.Printf("syncManager: count user keys error: %v", err)
			_ = sc.send(protocol.Frame{ID: frame.ID, Type: protocol.FrameKeyCreateResp, OK: false,
				Error: "internal error", HTTPStatus: 500})
			return
		}
		if count >= m.deps.MaxPerUser {
			_ = sc.send(protocol.Frame{ID: frame.ID, Type: protocol.FrameKeyCreateResp, OK: false,
				Error: fmt.Sprintf("exceeded max keys per user (%d)", m.deps.MaxPerUser), HTTPStatus: 409})
			return
		}
	}

	var expiresAt time.Time
	if frame.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, frame.ExpiresAt); err == nil {
			expiresAt = t
		}
	}

	role := frame.Role
	if role == "" {
		role = apikey.RoleTenant
	}

	meta := apikey.KeyMetadata{
		Namespace:   frame.Namespace,
		User:        frame.User,
		Team:        frame.Team,
		Role:        role,
		Description: frame.Description,
		IssuedAt:    time.Now().UTC(),
		ExpiresAt:   expiresAt,
	}

	rawToken, keyID, err := m.deps.KeyStore.Create(ctx, meta)
	if err != nil {
		log.Printf("syncManager: create key error: %v", err)
		_ = sc.send(protocol.Frame{ID: frame.ID, Type: protocol.FrameKeyCreateResp, OK: false,
			Error: "failed to create key", HTTPStatus: 500})
		return
	}

	// Retrieve the full hash for broadcasting.
	createdMeta, getErr := m.deps.KeyStore.Get(ctx, keyID)
	if getErr != nil {
		log.Printf("syncManager: get created key %s error: %v", keyID, getErr)
	}

	// Broadcast key_sync to all Workers (including the requesting one).
	syncF := protocol.Frame{Type: protocol.FrameKeySync}
	if createdMeta != nil {
		syncF = metaToFrame(*createdMeta)
		syncF.Type = protocol.FrameKeySync
	}
	m.broadcast(syncF)

	// Send success response to the requesting Worker.
	issuedAtStr := meta.IssuedAt.UTC().Format(time.RFC3339)
	hashPrefix := ""
	if len(keyID) > len("agentbox-apikey-") {
		hashPrefix = keyID[len("agentbox-apikey-"):]
	}
	_ = sc.send(protocol.Frame{
		ID:         frame.ID,
		Type:       protocol.FrameKeyCreateResp,
		OK:         true,
		RawToken:   rawToken,
		KeyID:      keyID,
		HashPrefix: hashPrefix,
		IssuedAt:   issuedAtStr,
	})
}

// handleKeyDelete deletes a key from master and broadcasts to all Workers.
func (m *SyncManager) handleKeyDelete(ctx context.Context, sc *clusterSyncConn, frame protocol.Frame) {
	if m.deps.KeyStore == nil {
		_ = sc.send(protocol.Frame{ID: frame.ID, Type: protocol.FrameKeyDeleteResp, OK: false,
			Error: "key store not configured", HTTPStatus: 503})
		return
	}
	if frame.Name == "" {
		_ = sc.send(protocol.Frame{ID: frame.ID, Type: protocol.FrameKeyDeleteResp, OK: false,
			Error: "name is required", HTTPStatus: 400})
		return
	}

	if err := m.deps.KeyStore.Delete(ctx, frame.Name); err != nil {
		if err == apikey.ErrTokenNotFound {
			_ = sc.send(protocol.Frame{ID: frame.ID, Type: protocol.FrameKeyDeleteResp, OK: false,
				Error: "api key not found", HTTPStatus: 404})
			return
		}
		_ = sc.send(protocol.Frame{ID: frame.ID, Type: protocol.FrameKeyDeleteResp, OK: false,
			Error: "failed to delete key", HTTPStatus: 500})
		return
	}

	m.broadcast(protocol.Frame{Type: protocol.FrameKeyDeleteSync, Name: frame.Name})
	_ = sc.send(protocol.Frame{ID: frame.ID, Type: protocol.FrameKeyDeleteResp, OK: true})
}

// handleKeyImportFromWS handles import/promote frames sent via WebSocket
// (FrameKeyCreate with TokenHash + HashPrefix set). It stores the existing
// hash in the master KeyStore (idempotent) and broadcasts to all Workers.
func (m *SyncManager) handleKeyImportFromWS(ctx context.Context, sc *clusterSyncConn, frame protocol.Frame) {
	if m.deps.KeyStore == nil {
		_ = sc.send(protocol.Frame{ID: frame.ID, Type: protocol.FrameKeyCreateResp, OK: false,
			Error: "key store not configured", HTTPStatus: 503})
		return
	}

	var expiresAt time.Time
	if frame.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, frame.ExpiresAt); err == nil {
			expiresAt = t
		}
	}
	issuedAt := time.Now().UTC()
	if frame.IssuedAt != "" {
		if t, err := time.Parse(time.RFC3339, frame.IssuedAt); err == nil {
			issuedAt = t
		}
	}

	role := frame.Role
	if role == "" {
		role = apikey.RoleTenant
	}

	meta := apikey.KeyMetadata{
		Namespace:   frame.Namespace,
		User:        frame.User,
		Team:        frame.Team,
		Role:        role,
		Description: frame.Description,
		QuotaURL:    frame.QuotaURL,
		IssuedAt:    issuedAt,
		ExpiresAt:   expiresAt,
		RawToken:    frame.RawToken,
	}

	if err := m.deps.KeyStore.CreateFromHash(ctx, meta, frame.TokenHash, frame.HashPrefix); err != nil {
		log.Printf("syncManager: WS import error: %v", err)
		_ = sc.send(protocol.Frame{ID: frame.ID, Type: protocol.FrameKeyCreateResp, OK: false,
			Error: "failed to import key", HTTPStatus: 500})
		return
	}

	keyID := "agentbox-apikey-" + frame.HashPrefix
	createdMeta, _ := m.deps.KeyStore.Get(ctx, keyID)
	syncF := protocol.Frame{Type: protocol.FrameKeySync}
	if createdMeta != nil {
		syncF = metaToFrame(*createdMeta)
		syncF.Type = protocol.FrameKeySync
	}
	m.broadcast(syncF)

	_ = sc.send(protocol.Frame{
		ID:         frame.ID,
		Type:       protocol.FrameKeyCreateResp,
		OK:         true,
		KeyID:      keyID,
		HashPrefix: frame.HashPrefix,
		IssuedAt:   issuedAt.UTC().Format(time.RFC3339),
	})
}

// metaToFrame converts a KeyMetadata to a protocol.Frame for key_sync / key_snapshot items.
func metaToFrame(meta apikey.KeyMetadata) protocol.Frame {
	f := protocol.Frame{
		TokenHash:   meta.TokenHash,
		Namespace:   meta.Namespace,
		Role:        meta.Role,
		User:        meta.User,
		Team:        meta.Team,
		QuotaURL:    meta.QuotaURL,
		Description: meta.Description,
		RawToken:    meta.RawToken,
	}
	if !meta.IssuedAt.IsZero() {
		f.IssuedAt = meta.IssuedAt.UTC().Format(time.RFC3339)
	}
	if !meta.ExpiresAt.IsZero() {
		f.ExpiresAt = meta.ExpiresAt.UTC().Format(time.RFC3339)
	}
	// Derive hashPrefix from secret name: "agentbox-apikey-{hashPrefix}".
	shortName := meta.KeyID
	if i := strings.LastIndex(meta.KeyID, "/"); i >= 0 {
		shortName = meta.KeyID[i+1:]
	}
	f.Name = shortName
	const prefix = "agentbox-apikey-"
	if strings.HasPrefix(shortName, prefix) {
		f.HashPrefix = shortName[len(prefix):]
	}
	return f
}
