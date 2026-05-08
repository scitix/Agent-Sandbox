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

package domain

import "time"

// KeyMetadata holds the full metadata for an API key, mirroring what is
// stored in the backing Kubernetes Secret.
type KeyMetadata struct {
	// KeyID is the fully qualified secret identifier: "<namespace>/<name>".
	KeyID       string
	Namespace   string
	Role        string
	User        string
	Team        string
	QuotaURL    string
	Description string
	IssuedAt    time.Time
	ExpiresAt   time.Time // zero value means no expiry
	// SyncSource is "global" when the key was created/synced via ws-proxy.
	// Empty means locally-created or a legacy resource — both are treated as non-global.
	SyncSource string
	// RawToken is the full raw API key recovered from storage. Empty for legacy keys
	// that pre-date plaintext storage. Exposed via API to authorised callers for recovery.
	RawToken string
}

// CreateAPIKeyInput carries parameters for issuing a new API key.
type CreateAPIKeyInput struct {
	Namespace   string
	User        string
	Team        string
	Description string
	ExpiresAt   time.Time // zero means no expiry
	// Import mode fields (admin-only).
	// When TokenHash is non-empty the key is imported using the given hash
	// (via CreateFromHash) instead of generating a new random token.
	TokenHash  string
	HashPrefix string
	IssuedAt   time.Time // preserve original issue time (import mode)
	QuotaURL   string
}

// APIKeyResult is the result of a successful Create operation.
// RawToken is the opaque token and is returned only on creation.
type APIKeyResult struct {
	RawToken string // shown once only
	KeyMetadata
}

// APIKeyItem holds the displayable metadata for a single API key (no token).
type APIKeyItem struct {
	KeyMetadata
	// ShortName is the Kubernetes Secret name (without namespace prefix).
	ShortName string
}

// ListAPIKeysResult is the result of a List operation.
type ListAPIKeysResult struct {
	Items []APIKeyItem
}

// DeleteAPIKeyInput identifies which key to delete.
type DeleteAPIKeyInput struct {
	// KeyID is the secret name (without namespace prefix).
	KeyID string
}
