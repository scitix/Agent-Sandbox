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
	nativegen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
	"github.com/scitix/agent-sandbox/pkg/utils/httpctx"
	wsproxygen "github.com/scitix/agent-sandbox/pkg/wsproxy/gen"
)

// ── ListMyApiKeys ─────────────────────────────────────────────────────────────

func (s *templateServer) ListMyApiKeys(
	ctx context.Context,
	request wsproxygen.ListMyApiKeysRequestObject,
) (wsproxygen.ListMyApiKeysResponseObject, error) {
	if s.m.deps.KeyStore == nil {
		return wsproxygen.ListMyApiKeys503JSONResponse{Error: "key store not configured"}, nil
	}

	auth := httpctx.AuthFrom(ctx)
	isAdmin := auth.Role == apikey.RoleAdmin

	// Admin may filter by team/user; tenants are always scoped to their own identity.
	var team, user string
	if isAdmin {
		if request.Params.Team != nil {
			team = *request.Params.Team
		}
		if request.Params.User != nil {
			user = *request.Params.User
		}
	} else {
		team = auth.Team
		user = auth.User
	}

	metas, err := s.m.deps.KeyStore.ListByTeamAndUser(ctx, team, user)
	if err != nil {
		log.Printf("syncManager: api-keys list error: %v", err)
		return wsproxygen.ListMyApiKeys503JSONResponse{Error: "failed to list keys"}, nil
	}

	items := keyMetasToGenItems(metas)
	total := len(items)
	return wsproxygen.ListMyApiKeys200JSONResponse{
		Items:  items,
		Total:  total,
		Limit:  0,
		Offset: 0,
	}, nil
}

// ── CreateApiKey ─────────────────────────────────────────────────────────────

func (s *templateServer) CreateApiKey(
	ctx context.Context,
	request wsproxygen.CreateApiKeyRequestObject,
) (wsproxygen.CreateApiKeyResponseObject, error) {
	if s.m.deps.KeyStore == nil {
		return wsproxygen.CreateApiKey503JSONResponse{Error: "key store not configured"}, nil
	}

	auth := httpctx.AuthFrom(ctx)
	isAdmin := auth.Role == apikey.RoleAdmin
	body := request.Body

	// Impersonation: admin may create a key on behalf of another user.
	impTeam := derefStr(request.Params.XImpersonateTeam)
	impUser := derefStr(request.Params.XImpersonateUser)
	isImpersonating := isAdmin && impTeam != "" && impUser != ""

	// Import mode: both tokenHash and hashPrefix must be present.
	isImport := body.TokenHash != nil && *body.TokenHash != "" &&
		body.HashPrefix != nil && *body.HashPrefix != ""
	if isImport {
		return s.importApiKey(ctx, auth, body)
	}

	// Normal creation: derive user/team from JWT (or impersonation).
	targetUser := auth.User
	targetTeam := auth.Team
	targetNamespace := auth.Namespace
	role := auth.Role
	if isImpersonating {
		targetUser = impUser
		targetTeam = impTeam
		role = apikey.RoleTenant
	}

	if s.m.deps.MaxPerUser > 0 {
		count, err := s.m.deps.KeyStore.CountUserKeys(ctx, targetNamespace, targetUser)
		if err != nil {
			return wsproxygen.CreateApiKey503JSONResponse{Error: "internal error"}, nil
		}
		if count >= s.m.deps.MaxPerUser {
			return wsproxygen.CreateApiKey409JSONResponse{
				Error: fmt.Sprintf("exceeded max keys per user (%d)", s.m.deps.MaxPerUser),
			}, nil
		}
	}

	var expiresAt time.Time
	if body.ExpiresAt != nil {
		expiresAt = *body.ExpiresAt
	}

	meta := apikey.KeyMetadata{
		Namespace:   targetNamespace,
		User:        targetUser,
		Team:        targetTeam,
		Role:        role,
		Description: derefStr(body.Description),
		IssuedAt:    time.Now().UTC(),
		ExpiresAt:   expiresAt,
	}

	rawToken, keyID, err := s.m.deps.KeyStore.Create(ctx, meta)
	if err != nil {
		log.Printf("syncManager: api-key create error: %v", err)
		return wsproxygen.CreateApiKey503JSONResponse{Error: "failed to create key"}, nil
	}

	createdMeta, _ := s.m.deps.KeyStore.Get(ctx, keyID)
	if createdMeta != nil {
		syncF := metaToFrame(*createdMeta)
		syncF.Type = protocol.FrameKeySync
		s.m.broadcast(syncF)
	}

	return wsproxygen.CreateApiKey201JSONResponse{
		ApiKey:   rawToken,
		KeyId:    keyID,
		IssuedAt: meta.IssuedAt.UTC(),
		Role:     role,
		User:     &targetUser,
		Team:     &targetTeam,
	}, nil
}

func (s *templateServer) importApiKey(
	ctx context.Context,
	_ any,
	body *wsproxygen.CreateAPIKeyRequest,
) (wsproxygen.CreateApiKeyResponseObject, error) {
	issuedAt := time.Now().UTC()
	if body.IssuedAt != nil {
		issuedAt = *body.IssuedAt
	}
	var expiresAt time.Time
	if body.ExpiresAt != nil {
		expiresAt = *body.ExpiresAt
	}

	meta := apikey.KeyMetadata{
		Namespace:   derefStr(body.Namespace),
		User:        derefStr(body.User),
		Team:        derefStr(body.Team),
		Role:        apikey.RoleTenant,
		Description: derefStr(body.Description),
		QuotaURL:    derefStr(body.QuotaURL),
		IssuedAt:    issuedAt,
		ExpiresAt:   expiresAt,
	}

	if err := s.m.deps.KeyStore.CreateFromHash(ctx, meta, *body.TokenHash, *body.HashPrefix); err != nil {
		log.Printf("syncManager: api-key import error: %v", err)
		return wsproxygen.CreateApiKey503JSONResponse{Error: "failed to import key"}, nil
	}

	keyID := "agentbox-apikey-" + *body.HashPrefix
	createdMeta, _ := s.m.deps.KeyStore.Get(ctx, keyID)
	if createdMeta != nil {
		syncF := metaToFrame(*createdMeta)
		syncF.Type = protocol.FrameKeySync
		s.m.broadcast(syncF)
	}

	return wsproxygen.CreateApiKey201JSONResponse{
		ApiKey:   "",
		KeyId:    keyID,
		IssuedAt: issuedAt.UTC(),
		Role:     apikey.RoleTenant,
		User:     &meta.User,
		Team:     &meta.Team,
	}, nil
}

// ── DeleteApiKey ─────────────────────────────────────────────────────────────

func (s *templateServer) DeleteApiKey(
	ctx context.Context,
	request wsproxygen.DeleteApiKeyRequestObject,
) (wsproxygen.DeleteApiKeyResponseObject, error) {
	if s.m.deps.KeyStore == nil {
		return wsproxygen.DeleteApiKey503JSONResponse{Error: "key store not configured"}, nil
	}

	if err := s.m.deps.KeyStore.Delete(ctx, request.Name); err != nil {
		if err == apikey.ErrTokenNotFound {
			return wsproxygen.DeleteApiKey404JSONResponse{Error: "api key not found"}, nil
		}
		log.Printf("syncManager: api-key delete error: %v", err)
		return wsproxygen.DeleteApiKey503JSONResponse{Error: "failed to delete key"}, nil
	}
	s.m.broadcast(protocol.Frame{Type: protocol.FrameKeyDeleteSync, Name: request.Name})
	return wsproxygen.DeleteApiKey204Response{}, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func keyMetasToGenItems(metas []apikey.KeyMetadata) []nativegen.APIKeyItem {
	items := make([]nativegen.APIKeyItem, 0, len(metas))
	for _, m := range metas {
		shortName := m.KeyID
		if i := strings.LastIndex(m.KeyID, "/"); i >= 0 {
			shortName = m.KeyID[i+1:]
		}
		item := nativegen.APIKeyItem{
			KeyId:       shortName,
			User:        &m.User,
			Team:        &m.Team,
			Role:        m.Role,
			QuotaURL:    &m.QuotaURL,
			Description: &m.Description,
			IssuedAt:    m.IssuedAt,
			SyncSource:  &m.SyncSource,
		}
		if !m.ExpiresAt.IsZero() {
			item.ExpiresAt = &m.ExpiresAt
		}
		if m.RawToken != "" {
			item.RawToken = &m.RawToken
		}
		items = append(items, item)
	}
	return items
}
