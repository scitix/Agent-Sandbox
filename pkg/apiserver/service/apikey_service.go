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
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
)

// APIKeyService defines business operations for API key management.
type APIKeyService interface {
	Create(ctx context.Context, input CreateAPIKeyInput) (*APIKeyResult, *domain.AppError)
	List(ctx context.Context) ([]APIKeyItem, *domain.AppError)
	ListByTeamAndUser(ctx context.Context, team, user string) ([]APIKeyItem, *domain.AppError)
	Get(ctx context.Context, keyID string) (*APIKeyItem, *domain.AppError)
	Delete(ctx context.Context, keyID string) *domain.AppError
	// Promote elevates a locally-created key to a global key by syncing it
	// through the ws-proxy manager to all Worker clusters.
	Promote(ctx context.Context, keyID string) *domain.AppError
}

type k8sAPIKeyService struct {
	store   apikey.KeyStore
	syncSvc SyncService // non-nil = WS-forwarding mode (global key management)
}

// NewAPIKeyService creates a new APIKeyService backed by the provided KeyStore.
// If store is nil, all operations will return a ServiceUnavailable error.
func NewAPIKeyService(store apikey.KeyStore) APIKeyService {
	return &k8sAPIKeyService{store: store}
}

// NewAPIKeyServiceWithSync creates a new APIKeyService that forwards Create/Delete
// operations to the master cluster via the provided SyncService WS channel.
// Local List/Get continue to use the local store.
func NewAPIKeyServiceWithSync(store apikey.KeyStore, syncSvc SyncService) APIKeyService {
	return &k8sAPIKeyService{store: store, syncSvc: syncSvc}
}

// ── Create ────────────────────────────────────────────────────────────────────

func (s *k8sAPIKeyService) Create(ctx context.Context, input CreateAPIKeyInput) (*APIKeyResult, *domain.AppError) {
	if s.store == nil {
		return nil, domain.NewServiceUnavailable("api key store is disabled")
	}

	// Import mode: create from an existing token hash (admin-only, idempotent).
	if input.TokenHash != "" && input.HashPrefix != "" {
		return s.createFromHash(ctx, input)
	}

	// WS-forwarding mode: delegate to master via SyncService.
	if s.syncSvc != nil {
		req := CreateKeyRequest{
			Namespace:   strings.TrimSpace(input.Namespace),
			User:        strings.TrimSpace(input.User),
			Team:        strings.TrimSpace(input.Team),
			Role:        apikey.RoleTenant,
			Description: strings.TrimSpace(input.Description),
		}
		if !input.ExpiresAt.IsZero() {
			req.ExpiresAt = input.ExpiresAt.UTC().Format(time.RFC3339)
		}

		resp, err := s.syncSvc.RequestCreate(ctx, req)
		if err != nil {
			if errors.Is(err, ErrSyncNotConnected) {
				return nil, domain.NewServiceUnavailable("global key manager unavailable")
			}
			var httpErr *SyncHTTPError
			if errors.As(err, &httpErr) {
				switch httpErr.Status {
				case 409:
					return nil, domain.NewConflict(httpErr.Message)
				case 429:
					return nil, domain.NewServiceUnavailable(httpErr.Message)
				}
			}
			return nil, domain.NewInternal(err.Error(), err)
		}

		var issuedAt time.Time
		if resp.IssuedAt != "" {
			if t, parseErr := time.Parse(time.RFC3339, resp.IssuedAt); parseErr == nil {
				issuedAt = t
			}
		}
		if issuedAt.IsZero() {
			issuedAt = time.Now().UTC()
		}

		meta := KeyMetadata{
			KeyID:       resp.KeyID,
			Namespace:   input.Namespace,
			Role:        apikey.RoleTenant,
			User:        input.User,
			Team:        input.Team,
			Description: input.Description,
			IssuedAt:    issuedAt,
			ExpiresAt:   input.ExpiresAt,
		}
		return &APIKeyResult{
			RawToken:    resp.RawToken,
			KeyMetadata: meta,
		}, nil
	}

	// Local mode.
	meta := apikey.KeyMetadata{
		Namespace:   strings.TrimSpace(input.Namespace),
		User:        strings.TrimSpace(input.User),
		Team:        strings.TrimSpace(input.Team),
		Description: strings.TrimSpace(input.Description),
		Role:        apikey.RoleTenant,
		IssuedAt:    time.Now().UTC(),
		ExpiresAt:   input.ExpiresAt,
	}

	rawToken, keyID, err := s.store.Create(ctx, meta)
	if err != nil {
		return nil, domain.NewInternal(err.Error(), err)
	}

	meta.KeyID = keyID
	return &APIKeyResult{
		RawToken:    rawToken,
		KeyMetadata: keyMetadataFromAPIKey(meta),
	}, nil
}

// ── List ──────────────────────────────────────────────────────────────────────

func (s *k8sAPIKeyService) List(ctx context.Context) ([]APIKeyItem, *domain.AppError) {
	if s.store == nil {
		return nil, domain.NewServiceUnavailable("api key store is disabled")
	}

	metas, err := s.store.List(ctx)
	if err != nil {
		return nil, domain.NewInternal(err.Error(), err)
	}

	items := make([]APIKeyItem, 0, len(metas))
	for _, m := range metas {
		items = append(items, APIKeyItem{
			KeyMetadata: keyMetadataFromAPIKey(m),
			ShortName:   shortName(m.KeyID),
		})
	}

	// Sort by team, then user, then issuedAt for consistent ordering.
	sort.Slice(items, func(i, j int) bool {
		if items[i].Team != items[j].Team {
			return items[i].Team < items[j].Team
		}
		if items[i].User != items[j].User {
			return items[i].User < items[j].User
		}
		return items[i].IssuedAt.Before(items[j].IssuedAt)
	})

	return items, nil
}

// ── ListByTeamAndUser ────────────────────────────────────────────────────────

func (s *k8sAPIKeyService) ListByTeamAndUser(ctx context.Context, team, user string) ([]APIKeyItem, *domain.AppError) {
	if s.store == nil {
		return nil, domain.NewServiceUnavailable("api key store is disabled")
	}

	metas, err := s.store.ListByTeamAndUser(ctx, team, user)
	if err != nil {
		return nil, domain.NewInternal(err.Error(), err)
	}

	items := make([]APIKeyItem, 0, len(metas))
	for _, m := range metas {
		items = append(items, APIKeyItem{
			KeyMetadata: keyMetadataFromAPIKey(m),
			ShortName:   shortName(m.KeyID),
		})
	}
	return items, nil
}

// ── Get ───────────────────────────────────────────────────────────────────────

func (s *k8sAPIKeyService) Get(ctx context.Context, keyID string) (*APIKeyItem, *domain.AppError) {
	if s.store == nil {
		return nil, domain.NewServiceUnavailable("api key store is disabled")
	}

	m, err := s.store.Get(ctx, keyID)
	if err != nil {
		if err == apikey.ErrTokenNotFound {
			return nil, domain.NewNotFound("api key not found")
		}
		return nil, domain.NewInternal(err.Error(), err)
	}

	return &APIKeyItem{
		KeyMetadata: keyMetadataFromAPIKey(*m),
		ShortName:   shortName(m.KeyID),
	}, nil
}

// ── Delete ────────────────────────────────────────────────────────────────────

func (s *k8sAPIKeyService) Delete(ctx context.Context, keyID string) *domain.AppError {
	if s.store == nil {
		return domain.NewServiceUnavailable("api key store is disabled")
	}

	// WS-forwarding mode: delegate to master via SyncService.
	if s.syncSvc != nil {
		err := s.syncSvc.RequestDelete(ctx, keyID)
		if err != nil {
			if errors.Is(err, ErrSyncNotConnected) {
				return domain.NewServiceUnavailable("global key manager unavailable")
			}
			var httpErr *SyncHTTPError
			if errors.As(err, &httpErr) {
				if httpErr.Status == 404 {
					return domain.NewNotFound("api key not found")
				}
			}
			return domain.NewInternal(err.Error(), err)
		}
		return nil
	}

	// Local mode.
	if err := s.store.Delete(ctx, keyID); err != nil {
		if err == apikey.ErrTokenNotFound {
			return domain.NewNotFound("api key not found")
		}
		return domain.NewInternal(err.Error(), err)
	}
	return nil
}

// ── Promote ───────────────────────────────────────────────────────────────────

func (s *k8sAPIKeyService) Promote(ctx context.Context, keyID string) *domain.AppError {
	if s.store == nil {
		return domain.NewServiceUnavailable("api key store is disabled")
	}
	if s.syncSvc == nil {
		return domain.NewServiceUnavailable("global key manager unavailable: not connected to ws-proxy")
	}

	m, err := s.store.Get(ctx, keyID)
	if err != nil {
		if err == apikey.ErrTokenNotFound {
			return domain.NewNotFound("api key not found")
		}
		return domain.NewInternal(err.Error(), err)
	}

	if m.SyncSource == apikey.LabelSyncSourceGlobal {
		return domain.NewConflict("key is already global")
	}

	// Derive hashPrefix from the secret name (format: "agentbox-apikey-{hashPrefix}").
	shortName := shortName(m.KeyID)
	const secretPrefix = "agentbox-apikey-"
	hashPrefix := ""
	if len(shortName) > len(secretPrefix) {
		hashPrefix = shortName[len(secretPrefix):]
	}
	if hashPrefix == "" || m.TokenHash == "" {
		return domain.NewInternal("key metadata is incomplete: missing tokenHash or hashPrefix", nil)
	}

	var issuedAt string
	if !m.IssuedAt.IsZero() {
		issuedAt = m.IssuedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	var expiresAt string
	if !m.ExpiresAt.IsZero() {
		expiresAt = m.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z")
	}

	req := CreateKeyRequest{
		Namespace:   m.Namespace,
		User:        m.User,
		Team:        m.Team,
		Role:        m.Role,
		Description: m.Description,
		QuotaURL:    m.QuotaURL,
		ExpiresAt:   expiresAt,
		TokenHash:   m.TokenHash,
		HashPrefix:  hashPrefix,
		IssuedAt:    issuedAt,
		RawToken:    m.RawToken,
	}
	if _, reqErr := s.syncSvc.RequestCreate(ctx, req); reqErr != nil {
		if errors.Is(reqErr, ErrSyncNotConnected) {
			return domain.NewServiceUnavailable("global key manager unavailable")
		}
		var httpErr *SyncHTTPError
		if errors.As(reqErr, &httpErr) {
			switch httpErr.Status {
			case 409:
				return domain.NewConflict(httpErr.Message)
			}
		}
		return domain.NewInternal(reqErr.Error(), reqErr)
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// keyMetadataFromAPIKey copies fields from the storage shape (apikey.KeyMetadata)
// into the service-layer KeyMetadata. The QuotaURL field is intentionally dropped
// here — it is preserved at the storage layer (for sync propagation) but is not
// exposed through the service surface anymore.
func keyMetadataFromAPIKey(m apikey.KeyMetadata) KeyMetadata {
	return KeyMetadata{
		KeyID:       m.KeyID,
		Namespace:   m.Namespace,
		Role:        m.Role,
		User:        m.User,
		Team:        m.Team,
		Description: m.Description,
		IssuedAt:    m.IssuedAt,
		ExpiresAt:   m.ExpiresAt,
		SyncSource:  m.SyncSource,
		RawToken:    m.RawToken,
	}
}

// shortName extracts the secret name from a fully qualified "ns/name" KeyID.
func shortName(keyID string) string {
	if i := strings.LastIndex(keyID, "/"); i >= 0 {
		return keyID[i+1:]
	}
	return keyID
}

// createFromHash handles import mode: writes a key using the provided
// token hash (idempotent via CreateFromHash). This is used for migrating
// existing keys from Worker clusters to the Manager.
func (s *k8sAPIKeyService) createFromHash(ctx context.Context, input CreateAPIKeyInput) (*APIKeyResult, *domain.AppError) {
	issuedAt := input.IssuedAt
	if issuedAt.IsZero() {
		issuedAt = time.Now().UTC()
	}

	meta := apikey.KeyMetadata{
		Namespace:   strings.TrimSpace(input.Namespace),
		User:        strings.TrimSpace(input.User),
		Team:        strings.TrimSpace(input.Team),
		Description: strings.TrimSpace(input.Description),
		Role:        apikey.RoleTenant,
		IssuedAt:    issuedAt,
		ExpiresAt:   input.ExpiresAt,
	}

	if err := s.store.CreateFromHash(ctx, meta, input.TokenHash, input.HashPrefix); err != nil {
		return nil, domain.NewInternal(err.Error(), err)
	}

	secretName := "agentbox-apikey-" + input.HashPrefix
	meta.KeyID = secretName

	return &APIKeyResult{
		RawToken:    "", // import mode: raw token is not available
		KeyMetadata: keyMetadataFromAPIKey(meta),
	}, nil
}
