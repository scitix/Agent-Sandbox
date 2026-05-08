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

package apikey

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// LabelType marks Secrets managed by agentbox API key store.
	LabelType = "agentbox.io/type"
	// LabelTypeValue is the label value for API key secrets.
	LabelTypeValue = "api-key"
	// LabelNamespace stores the tenant namespace in the secret label.
	LabelNamespace = "agentbox.io/namespace"
	// LabelUser stores the user in the secret label.
	LabelUser = "agentbox.io/user"
	// LabelTeam stores the team in the secret label.
	LabelTeam = "agentbox.io/team"
	// LabelTokenHashPrefix is the first 16 hex chars of SHA-256(rawToken) for label selector.
	LabelTokenHashPrefix = "agentbox.io/token-hash"

	// LabelSyncSource marks the origin of the API key Secret.
	// "global" means the key was created/synced via ws-proxy (global key manager).
	// Resources without this label (locally-created or legacy) are treated as non-global.
	LabelSyncSource       = "agentbox.io/sync-source"
	LabelSyncSourceGlobal = "global"

	// secretNamePrefix is the prefix for API key secret names.
	secretNamePrefix = "agentbox-apikey-"

	// Token prefix for all API keys.
	TokenPrefix = "agbx_"

	// defaultSecretsNamespace is the default namespace for API key secrets.
	defaultSecretsNamespace = "agentbox-system"
	// defaultCacheTTL is the default TTL for the in-memory cache.
	defaultCacheTTL = time.Minute
)

var (
	// ErrTokenNotFound is returned when the provided token does not match any stored key.
	ErrTokenNotFound = errors.New("api key not found")
	// ErrTokenExpired is returned when the token is found but has passed its expiry time.
	ErrTokenExpired = errors.New("api key expired")
)

// KeyMetadata holds the full metadata stored in a Kubernetes Secret for one API key.
type KeyMetadata struct {
	// KeyID is the fully qualified secret identifier: "<namespace>/<name>".
	KeyID       string    `json:"keyId"`
	Namespace   string    `json:"namespace"`
	Role        string    `json:"role"`
	User        string    `json:"user"`
	Team        string    `json:"team"`
	QuotaURL    string    `json:"quotaURL"`
	Description string    `json:"description"`
	IssuedAt    time.Time `json:"issuedAt"`
	ExpiresAt   time.Time `json:"expiresAt"` // zero = never expires
	// SyncSource is "global" when the key was created/synced via ws-proxy.
	// Empty means locally-created or a legacy resource — both are treated as non-global.
	SyncSource string `json:"syncSource,omitempty"`
	// TokenHash is the full SHA-256 hex hash of the raw token, extracted from
	// the Secret data. It is needed for sync (CreateFromHash) but MUST NOT be
	// exposed via any external API — hence json:"-".
	TokenHash string `json:"-"`
	// RawToken is the full opaque token (agbx_...) stored in the Secret's "apikey"
	// data field. Populated only for keys created after plaintext storage was
	// introduced. Empty for legacy keys. MUST NOT be exposed in JSON — json:"-".
	RawToken string `json:"-"`
}

// KeyStore defines the operations for managing opaque API keys backed by
// Kubernetes Secrets.
type KeyStore interface {
	// Create generates a new random token, stores its metadata as a K8s Secret,
	// and returns the raw token (shown only once) plus the secret name (KeyID short form).
	Create(ctx context.Context, meta KeyMetadata) (rawToken, keyID string, err error)

	// CreateFromHash writes a Secret using an already-computed tokenHash/hashPrefix
	// (e.g. when importing a key from another cluster). It is idempotent:
	// if the Secret already exists the call returns nil.
	CreateFromHash(ctx context.Context, meta KeyMetadata, tokenHash, hashPrefix string) error

	// Validate resolves rawToken to its KeyMetadata, using an in-memory cache.
	// Returns ErrTokenNotFound if no matching secret exists.
	// Returns ErrTokenExpired if the key has passed its ExpiresAt time.
	Validate(ctx context.Context, rawToken string) (*KeyMetadata, error)

	// List returns all API key metadata.
	List(ctx context.Context) ([]KeyMetadata, error)

	// ListByTeamAndUser returns API key metadata filtered by team and/or user
	// labels. Pass empty strings to skip a filter dimension.
	ListByTeamAndUser(ctx context.Context, team, user string) ([]KeyMetadata, error)

	// Get returns the metadata for the secret identified by keyID (secret name,
	// without namespace prefix).
	Get(ctx context.Context, keyID string) (*KeyMetadata, error)

	// Delete revokes the API key identified by keyID (secret name).
	Delete(ctx context.Context, keyID string) error
}

// ───────────────────────────────────────────────────────────────────────────────
// cacheEntry

type cacheEntry struct {
	meta      KeyMetadata
	fullHash  string    // hex(SHA-256(rawToken)) – used for eviction
	expiresAt time.Time // cache entry expiry (based on CacheTTL, not key expiry)
}

// ───────────────────────────────────────────────────────────────────────────────
// SecretKeyStoreConfig

// SecretKeyStoreConfig configures the SecretKeyStore.
type SecretKeyStoreConfig struct {
	// Client is the Kubernetes client used to read/write Secrets.
	Client client.Client
	// SecretsNamespace is the namespace where API key secrets are stored.
	// Defaults to "agentbox-system" if empty.
	SecretsNamespace string
	// CacheTTL is how long a successful Validate result is cached in memory.
	// Defaults to 1 minute if zero.
	CacheTTL time.Duration
}

// ───────────────────────────────────────────────────────────────────────────────
// SecretKeyStore

// SecretKeyStore implements KeyStore backed by Kubernetes Secrets.
type SecretKeyStore struct {
	client    client.Client
	namespace string
	cacheTTL  time.Duration

	mu    sync.RWMutex
	cache map[string]*cacheEntry // key: fullHash (hex SHA-256)
}

// NewSecretKeyStore creates a new SecretKeyStore.
func NewSecretKeyStore(cfg SecretKeyStoreConfig) *SecretKeyStore {
	ns := cfg.SecretsNamespace
	if ns == "" {
		ns = defaultSecretsNamespace
	}
	ttl := cfg.CacheTTL
	if ttl <= 0 {
		ttl = defaultCacheTTL
	}
	return &SecretKeyStore{
		client:    cfg.Client,
		namespace: ns,
		cacheTTL:  ttl,
		cache:     make(map[string]*cacheEntry),
	}
}

// ── Create ────────────────────────────────────────────────────────────────────

// Create generates a new opaque token "agbx_<base64url>", stores its SHA-256
// hash and all metadata in a Kubernetes Secret, then returns the raw token.
// The raw token is shown only once; the caller must persist it.
func (s *SecretKeyStore) Create(ctx context.Context, meta KeyMetadata) (rawToken, keyID string, err error) {
	// Generate 32 random bytes → base64url token.
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("apikey: generate token: %w", err)
	}
	rawToken = TokenPrefix + hex.EncodeToString(raw)

	// Compute SHA-256 hash.
	fullHash := tokenHash(rawToken)
	hashPrefix := fullHash[:16] // first 16 hex chars for label selector

	// Derive secret name from hash prefix.
	secretName := secretNamePrefix + hashPrefix

	now := time.Now().UTC()
	if meta.IssuedAt.IsZero() {
		meta.IssuedAt = now
	}
	if meta.Role == "" {
		meta.Role = RoleTenant
	}

	data := map[string][]byte{
		"token":       []byte(fullHash),
		"apikey":      []byte(rawToken), // plaintext stored for recovery; protected by K8s RBAC
		"namespace":   []byte(meta.Namespace),
		"role":        []byte(meta.Role),
		"user":        []byte(meta.User),
		"team":        []byte(meta.Team),
		"quotaURL":    []byte(meta.QuotaURL),
		"description": []byte(meta.Description),
		"issuedAt":    []byte(meta.IssuedAt.UTC().Format(time.RFC3339)),
	}
	if !meta.ExpiresAt.IsZero() {
		data["expiresAt"] = []byte(meta.ExpiresAt.UTC().Format(time.RFC3339))
	}

	labels := map[string]string{
		LabelType:            LabelTypeValue,
		LabelNamespace:       meta.Namespace,
		LabelTokenHashPrefix: hashPrefix,
	}
	if meta.User != "" {
		labels[LabelUser] = meta.User
	}
	if meta.Team != "" {
		labels[LabelTeam] = meta.Team
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: s.namespace,
			Labels:    labels,
		},
		Data: data,
	}

	if err = s.client.Create(ctx, secret); err != nil {
		return "", "", fmt.Errorf("apikey: create secret: %w", err)
	}

	keyID = secretName
	return rawToken, keyID, nil
}

// ── Validate ──────────────────────────────────────────────────────────────────

// Validate resolves rawToken → KeyMetadata via in-memory cache, falling back
// to a Kubernetes Secret lookup.
func (s *SecretKeyStore) Validate(ctx context.Context, rawToken string) (*KeyMetadata, error) {
	fullHash := tokenHash(rawToken)

	// Fast path: cache hit.
	if meta := s.cacheGet(fullHash); meta != nil {
		if !meta.ExpiresAt.IsZero() && time.Now().UTC().After(meta.ExpiresAt) {
			s.cacheEvict(fullHash)
			return nil, ErrTokenExpired
		}
		return meta, nil
	}

	// Slow path: K8s lookup.
	hashPrefix := fullHash[:16]
	labelSel := client.MatchingLabels{
		LabelType:            LabelTypeValue,
		LabelTokenHashPrefix: hashPrefix,
	}

	var secretList corev1.SecretList
	if err := s.client.List(ctx, &secretList, client.InNamespace(s.namespace), labelSel); err != nil {
		return nil, fmt.Errorf("apikey: list secrets: %w", err)
	}

	for i := range secretList.Items {
		secret := &secretList.Items[i]
		storedHash := string(secret.Data["token"])
		if storedHash != fullHash {
			continue
		}
		// Found a match.
		meta, err := metadataFromSecret(secret)
		if err != nil {
			return nil, fmt.Errorf("apikey: parse secret %s: %w", secret.Name, err)
		}
		if !meta.ExpiresAt.IsZero() && time.Now().UTC().After(meta.ExpiresAt) {
			return nil, ErrTokenExpired
		}
		s.cachePut(fullHash, meta)
		return meta, nil
	}

	return nil, ErrTokenNotFound
}

// ── List ──────────────────────────────────────────────────────────────────────

// List returns metadata for all API key secrets.
func (s *SecretKeyStore) List(ctx context.Context) ([]KeyMetadata, error) {
	labelSel := client.MatchingLabels{LabelType: LabelTypeValue}

	var secretList corev1.SecretList
	if err := s.client.List(ctx, &secretList, client.InNamespace(s.namespace), labelSel); err != nil {
		return nil, fmt.Errorf("apikey: list secrets: %w", err)
	}

	metas := make([]KeyMetadata, 0, len(secretList.Items))
	for i := range secretList.Items {
		meta, err := metadataFromSecret(&secretList.Items[i])
		if err != nil {
			continue // skip malformed secrets
		}
		metas = append(metas, *meta)
	}
	return metas, nil
}

// ListByTeamAndUser returns API key metadata filtered by team and/or user
// labels at the Kubernetes level (label selector). Pass empty strings to skip
// a filter dimension.
func (s *SecretKeyStore) ListByTeamAndUser(ctx context.Context, team, user string) ([]KeyMetadata, error) {
	labelSel := client.MatchingLabels{LabelType: LabelTypeValue}
	if team != "" {
		labelSel[LabelTeam] = team
	}
	if user != "" {
		labelSel[LabelUser] = user
	}

	var secretList corev1.SecretList
	if err := s.client.List(ctx, &secretList, client.InNamespace(s.namespace), labelSel); err != nil {
		return nil, fmt.Errorf("apikey: list secrets by team/user: %w", err)
	}

	metas := make([]KeyMetadata, 0, len(secretList.Items))
	for i := range secretList.Items {
		meta, err := metadataFromSecret(&secretList.Items[i])
		if err != nil {
			continue
		}
		metas = append(metas, *meta)
	}
	return metas, nil
}

// ── Get ───────────────────────────────────────────────────────────────────────

// Get returns metadata for a single API key secret by its name (without namespace prefix).
func (s *SecretKeyStore) Get(ctx context.Context, keyID string) (*KeyMetadata, error) {
	var secret corev1.Secret
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: s.namespace, Name: keyID}, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, ErrTokenNotFound
		}
		return nil, fmt.Errorf("apikey: get secret %s: %w", keyID, err)
	}

	// Verify it's an API key secret.
	if secret.Labels[LabelType] != LabelTypeValue {
		return nil, ErrTokenNotFound
	}

	meta, err := metadataFromSecret(&secret)
	if err != nil {
		return nil, fmt.Errorf("apikey: parse secret %s: %w", keyID, err)
	}
	return meta, nil
}

// ── CountUserKeys ─────────────────────────────────────────────────────────────

// CountUserKeys returns the number of API key Secrets for the given namespace+user pair.
func (s *SecretKeyStore) CountUserKeys(ctx context.Context, namespace, user string) (int, error) {
	labelSel := client.MatchingLabels{
		LabelType:      LabelTypeValue,
		LabelNamespace: namespace,
	}
	if user != "" {
		labelSel[LabelUser] = user
	}

	var secretList corev1.SecretList
	if err := s.client.List(ctx, &secretList, client.InNamespace(s.namespace), labelSel); err != nil {
		return 0, fmt.Errorf("apikey: count user keys: %w", err)
	}
	return len(secretList.Items), nil
}

// ── CreateFromHash ────────────────────────────────────────────────────────────

// CreateFromHash writes a Secret using an already-computed tokenHash/hashPrefix
// (e.g. when syncing a key created by the master cluster BFF). It is idempotent:
// if the Secret already exists the call returns nil.
func (s *SecretKeyStore) CreateFromHash(ctx context.Context, meta KeyMetadata, tokenHash, hashPrefix string) error {
	secretName := secretNamePrefix + hashPrefix

	// Idempotency check: if the secret already exists, ensure it carries the
	// syncSource=global label (a promote operation may be updating a locally-
	// created Secret that lacks this label).
	var existing corev1.Secret
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: s.namespace, Name: secretName}, &existing); err == nil {
		if existing.Labels[LabelSyncSource] != LabelSyncSourceGlobal {
			patch := client.MergeFrom(existing.DeepCopy())
			if existing.Labels == nil {
				existing.Labels = map[string]string{}
			}
			existing.Labels[LabelSyncSource] = LabelSyncSourceGlobal
			return s.client.Patch(ctx, &existing, patch)
		}
		return nil
	}

	now := time.Now().UTC()
	if meta.IssuedAt.IsZero() {
		meta.IssuedAt = now
	}
	if meta.Role == "" {
		meta.Role = RoleTenant
	}

	data := map[string][]byte{
		"token":       []byte(tokenHash),
		"namespace":   []byte(meta.Namespace),
		"role":        []byte(meta.Role),
		"user":        []byte(meta.User),
		"team":        []byte(meta.Team),
		"quotaURL":    []byte(meta.QuotaURL),
		"description": []byte(meta.Description),
		"issuedAt":    []byte(meta.IssuedAt.UTC().Format(time.RFC3339)),
	}
	if !meta.ExpiresAt.IsZero() {
		data["expiresAt"] = []byte(meta.ExpiresAt.UTC().Format(time.RFC3339))
	}
	if meta.RawToken != "" {
		data["apikey"] = []byte(meta.RawToken)
	}

	labels := map[string]string{
		LabelType:            LabelTypeValue,
		LabelNamespace:       meta.Namespace,
		LabelTokenHashPrefix: hashPrefix,
		LabelSyncSource:      LabelSyncSourceGlobal,
	}
	if meta.User != "" {
		labels[LabelUser] = meta.User
	}
	if meta.Team != "" {
		labels[LabelTeam] = meta.Team
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: s.namespace,
			Labels:    labels,
		},
		Data: data,
	}

	if err := s.client.Create(ctx, secret); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil // idempotent
		}
		return fmt.Errorf("apikey: create-from-hash secret: %w", err)
	}
	return nil
}

// ── Delete ────────────────────────────────────────────────────────────────────

// Delete revokes the API key by deleting its Kubernetes Secret.
// It reads the secret first to evict the cache entry precisely.
func (s *SecretKeyStore) Delete(ctx context.Context, keyID string) error {
	var secret corev1.Secret
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: s.namespace, Name: keyID}, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return ErrTokenNotFound
		}
		return fmt.Errorf("apikey: get secret for delete %s: %w", keyID, err)
	}

	// Verify it's an API key secret to avoid accidental deletion.
	if secret.Labels[LabelType] != LabelTypeValue {
		return ErrTokenNotFound
	}

	// Evict cache entry using the full hash stored in the secret.
	if fullHash := string(secret.Data["token"]); fullHash != "" {
		s.cacheEvict(fullHash)
	}

	if err := s.client.Delete(ctx, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return ErrTokenNotFound
		}
		return fmt.Errorf("apikey: delete secret %s: %w", keyID, err)
	}
	return nil
}

// ───────────────────────────────────────────────────────────────────────────────
// cache helpers

func (s *SecretKeyStore) cacheGet(fullHash string) *KeyMetadata {
	s.mu.RLock()
	entry, ok := s.cache[fullHash]
	s.mu.RUnlock()
	if !ok {
		return nil
	}
	if time.Now().After(entry.expiresAt) {
		// Cache TTL expired – evict and force re-fetch.
		s.cacheEvict(fullHash)
		return nil
	}
	cp := entry.meta
	return &cp
}

func (s *SecretKeyStore) cachePut(fullHash string, meta *KeyMetadata) {
	s.mu.Lock()
	s.cache[fullHash] = &cacheEntry{
		meta:      *meta,
		fullHash:  fullHash,
		expiresAt: time.Now().Add(s.cacheTTL),
	}
	s.mu.Unlock()
}

func (s *SecretKeyStore) cacheEvict(fullHash string) {
	s.mu.Lock()
	delete(s.cache, fullHash)
	s.mu.Unlock()
}

// ───────────────────────────────────────────────────────────────────────────────
// helpers

// tokenHash computes hex(SHA-256(rawToken)).
func tokenHash(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}

// metadataFromSecret deserialises a Secret into a KeyMetadata.
func metadataFromSecret(secret *corev1.Secret) (*KeyMetadata, error) {
	meta := &KeyMetadata{
		KeyID:       secret.Namespace + "/" + secret.Name,
		Namespace:   string(secret.Data["namespace"]),
		Role:        string(secret.Data["role"]),
		User:        string(secret.Data["user"]),
		Team:        string(secret.Data["team"]),
		QuotaURL:    string(secret.Data["quotaURL"]),
		Description: string(secret.Data["description"]),
		TokenHash:   string(secret.Data["token"]),
		RawToken:    string(secret.Data["apikey"]),
		SyncSource:  secret.Labels[LabelSyncSource],
	}
	if meta.Role == "" {
		meta.Role = RoleTenant
	}

	if v := string(secret.Data["issuedAt"]); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return nil, fmt.Errorf("parse issuedAt %q: %w", v, err)
		}
		meta.IssuedAt = t
	}

	if v := string(secret.Data["expiresAt"]); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return nil, fmt.Errorf("parse expiresAt %q: %w", v, err)
		}
		meta.ExpiresAt = t
	}

	return meta, nil
}
