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
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	nativegen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
	"github.com/scitix/agent-sandbox/pkg/utils/httpctx"
	wsproxygen "github.com/scitix/agent-sandbox/pkg/wsproxy/gen"
)

// ── ListMyApiKeys ─────────────────────────────────────────────────────────────

func (s *Server) ListMyApiKeys(
	ctx context.Context,
	request wsproxygen.ListMyApiKeysRequestObject,
) (wsproxygen.ListMyApiKeysResponseObject, error) {
	deps := s.m.GetDeps()
	if deps.KeyStore == nil {
		return wsproxygen.ListMyApiKeys503JSONResponse{Error: "key store not configured"}, nil
	}

	auth := httpctx.AuthFrom(ctx)
	isAdmin := auth.Role == apikey.RoleAdmin

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

	metas, err := deps.KeyStore.ListByTeamAndUser(ctx, team, user)
	if err != nil {
		log.Printf("syncManager: api-keys list error: %v", err)
		return wsproxygen.ListMyApiKeys503JSONResponse{Error: "failed to list keys"}, nil
	}

	items := keyMetasToGenItems(metas)
	return wsproxygen.ListMyApiKeys200JSONResponse(items), nil
}

// ── CreateApiKey ─────────────────────────────────────────────────────────────

func (s *Server) CreateApiKey(
	ctx context.Context,
	request wsproxygen.CreateApiKeyRequestObject,
) (wsproxygen.CreateApiKeyResponseObject, error) {
	deps := s.m.GetDeps()
	if deps.KeyStore == nil {
		return wsproxygen.CreateApiKey503JSONResponse{Error: "key store not configured"}, nil
	}

	auth := httpctx.AuthFrom(ctx)
	isAdmin := auth.Role == apikey.RoleAdmin
	body := request.Body

	impTeam := derefStr(request.Params.XImpersonateTeam)
	impUser := derefStr(request.Params.XImpersonateUser)
	isImpersonating := isAdmin && impTeam != "" && impUser != ""

	isImport := body.TokenHash != nil && *body.TokenHash != "" &&
		body.HashPrefix != nil && *body.HashPrefix != ""
	if isImport {
		return s.importApiKey(ctx, body)
	}

	targetUser := auth.User
	targetTeam := auth.Team
	role := auth.Role
	if isImpersonating {
		targetUser = impUser
		targetTeam = impTeam
		role = apikey.RoleTenant
	}

	wantAgent := body.Mode != nil && *body.Mode == wsproxygen.Agent

	// The agent key is not one of a person's own keys, and is counted as
	// neither.
	//
	// It is issued FOR them rather than BY them — the assistant mints one the
	// first time they open a conversation — so charging it to an allowance they
	// did not spend is how someone ends up unable to make a third key of their
	// own because a chat window made one for them. That is not hypothetical: it
	// is exactly the 409 that surfaced mid-conversation before this existed.
	//
	// And there is only ever one. A second would be a second credential acting
	// unattended in the same person's name, with nothing to distinguish them in
	// an audit line; returning the existing one instead is both what the caller
	// wanted and the only answer that keeps that true under a race.
	if deps.MaxPerUser > 0 || wantAgent {
		keys, err := deps.KeyStore.ListByTeamAndUser(ctx, targetTeam, targetUser)
		if err != nil {
			return wsproxygen.CreateApiKey503JSONResponse{Error: "internal error"}, nil
		}
		own := 0
		for _, k := range keys {
			if k.RequireApproval {
				if wantAgent {
					return wsproxygen.CreateApiKey409JSONResponse{
						Error: "this user already has an agent key (" +
							shortKeyName(k.KeyID) + "); there is only ever one",
					}, nil
				}
				continue
			}
			own++
		}
		if !wantAgent && deps.MaxPerUser > 0 && own >= deps.MaxPerUser {
			return wsproxygen.CreateApiKey409JSONResponse{
				Error: fmt.Sprintf("exceeded max keys per user (%d)", deps.MaxPerUser),
			}, nil
		}
	}

	var expiresAt time.Time
	if body.ExpiresAt != nil {
		expiresAt = *body.ExpiresAt
	}

	// Global keys are stored with an empty namespace: each Worker cluster's
	// auth middleware resolves the effective namespace at request time from
	// the key's team+user metadata via its local IAM. Baking in a namespace
	// resolved on the master would pin the key to a namespace that may not
	// exist on the workers (tenant namespaces live only there).
	meta := apikey.KeyMetadata{
		User:        targetUser,
		Team:        targetTeam,
		Role:        role,
		Description: derefStr(body.Description),
		IssuedAt:    time.Now().UTC(),
		ExpiresAt:   expiresAt,
		// Absent means unrestricted — what every key was before this existed,
		// so an older client keeps issuing exactly what it always did.
		RequireApproval: wantAgent,
	}

	rawToken, keyID, err := deps.KeyStore.Create(ctx, meta)
	if err != nil {
		log.Printf("syncManager: api-key create error: %v", err)
		return wsproxygen.CreateApiKey503JSONResponse{Error: "failed to create key"}, nil
	}

	createdMeta, _ := deps.KeyStore.Get(ctx, keyID)
	if createdMeta != nil {
		s.m.BroadcastKeyMeta(*createdMeta)
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

func (s *Server) importApiKey(
	ctx context.Context,
	body *wsproxygen.CreateAPIKeyRequest,
) (wsproxygen.CreateApiKeyResponseObject, error) {
	deps := s.m.GetDeps()

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
		IssuedAt:    issuedAt,
		ExpiresAt:   expiresAt,
	}

	if err := deps.KeyStore.CreateFromHash(ctx, meta, *body.TokenHash, *body.HashPrefix); err != nil {
		log.Printf("syncManager: api-key import error: %v", err)
		return wsproxygen.CreateApiKey503JSONResponse{Error: "failed to import key"}, nil
	}

	keyID := "agentbox-apikey-" + *body.HashPrefix
	createdMeta, _ := deps.KeyStore.Get(ctx, keyID)
	if createdMeta != nil {
		s.m.BroadcastKeyMeta(*createdMeta)
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

func (s *Server) DeleteApiKey(
	ctx context.Context,
	request wsproxygen.DeleteApiKeyRequestObject,
) (wsproxygen.DeleteApiKeyResponseObject, error) {
	deps := s.m.GetDeps()
	if deps.KeyStore == nil {
		return wsproxygen.DeleteApiKey503JSONResponse{Error: "key store not configured"}, nil
	}

	if err := deps.KeyStore.Delete(ctx, request.Name); err != nil {
		if err == apikey.ErrTokenNotFound {
			return wsproxygen.DeleteApiKey404JSONResponse{Error: "api key not found"}, nil
		}
		log.Printf("syncManager: api-key delete error: %v", err)
		return wsproxygen.DeleteApiKey503JSONResponse{Error: "failed to delete key"}, nil
	}
	s.m.BroadcastKeyDelete(request.Name)
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
		// Stated for both modes: a console that badges only the gated ones
		// cannot tell "not gated" from "issued before this field existed".
		mode := nativegen.APIKeyItemModeUnrestricted
		if m.RequireApproval {
			mode = nativegen.APIKeyItemModeAgent
		}
		item.Mode = &mode
		items = append(items, item)
	}
	return items
}

// shortKeyName drops the namespace from a fully qualified KeyID so an error
// names the key the way the console's own list does.
func shortKeyName(keyID string) string {
	if i := strings.LastIndex(keyID, "/"); i >= 0 {
		return keyID[i+1:]
	}
	return keyID
}
