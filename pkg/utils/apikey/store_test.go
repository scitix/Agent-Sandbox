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

package apikey_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
)

func newFakeScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	return s
}

func newFakeStore() *apikey.SecretKeyStore {
	builder := fake.NewClientBuilder().WithScheme(newFakeScheme())
	return apikey.NewSecretKeyStore(apikey.SecretKeyStoreConfig{
		Client:           builder.Build(),
		SecretsNamespace: "agentbox-system",
		CacheTTL:         5 * time.Minute,
	})
}

func TestCreate(t *testing.T) {
	store := newFakeStore()
	ctx := context.Background()

	meta := apikey.KeyMetadata{
		Namespace:   "test-ns",
		User:        "alice",
		Team:        "eng",
		Description: "test key",
	}

	rawToken, keyID, err := store.Create(ctx, meta)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if rawToken == "" {
		t.Error("Create() rawToken is empty")
	}
	if keyID == "" {
		t.Error("Create() keyID is empty")
	}
	if len(rawToken) < len(apikey.TokenPrefix)+1 {
		t.Errorf("Create() rawToken %q too short", rawToken)
	}
}

func TestValidate_Found(t *testing.T) {
	store := newFakeStore()
	ctx := context.Background()

	meta := apikey.KeyMetadata{
		Namespace:   "test-ns",
		User:        "bob",
		Description: "validate test",
	}

	rawToken, _, err := store.Create(ctx, meta)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := store.Validate(ctx, rawToken)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got.Namespace != meta.Namespace {
		t.Errorf("Namespace = %q, want %q", got.Namespace, meta.Namespace)
	}
	if got.User != meta.User {
		t.Errorf("User = %q, want %q", got.User, meta.User)
	}
}

func TestValidate_CacheHit(t *testing.T) {
	store := newFakeStore()
	ctx := context.Background()

	meta := apikey.KeyMetadata{Namespace: "ns1", User: "carol"}
	rawToken, _, err := store.Create(ctx, meta)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// First call populates cache.
	if _, err := store.Validate(ctx, rawToken); err != nil {
		t.Fatalf("first Validate() error = %v", err)
	}

	// Second call should hit cache (no error expected even if K8s is unavailable).
	got, err := store.Validate(ctx, rawToken)
	if err != nil {
		t.Fatalf("second Validate() (cache hit) error = %v", err)
	}
	if got.User != meta.User {
		t.Errorf("User = %q, want %q", got.User, meta.User)
	}
}

func TestValidate_NotFound(t *testing.T) {
	store := newFakeStore()
	ctx := context.Background()

	_, err := store.Validate(ctx, "agbx_doesnotexist")
	if err != apikey.ErrTokenNotFound {
		t.Errorf("Validate() error = %v, want ErrTokenNotFound", err)
	}
}

func TestValidate_Expired(t *testing.T) {
	store := newFakeStore()
	ctx := context.Background()

	past := time.Now().UTC().Add(-1 * time.Hour)
	meta := apikey.KeyMetadata{
		Namespace: "ns-exp",
		User:      "dan",
		ExpiresAt: past,
	}

	rawToken, _, err := store.Create(ctx, meta)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err = store.Validate(ctx, rawToken)
	if err != apikey.ErrTokenExpired {
		t.Errorf("Validate() error = %v, want ErrTokenExpired", err)
	}
}

func TestList(t *testing.T) {
	store := newFakeStore()
	ctx := context.Background()

	// Create keys with different teams/users.
	_, _, err := store.Create(ctx, apikey.KeyMetadata{User: "u1", Team: "eng"})
	if err != nil {
		t.Fatalf("Create #1 error = %v", err)
	}
	_, _, err = store.Create(ctx, apikey.KeyMetadata{User: "u2", Team: "eng"})
	if err != nil {
		t.Fatalf("Create #2 error = %v", err)
	}
	_, _, err = store.Create(ctx, apikey.KeyMetadata{User: "u3", Team: "sci"})
	if err != nil {
		t.Fatalf("Create #3 error = %v", err)
	}

	all, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(all) != 3 {
		t.Errorf("List() = %d items, want 3", len(all))
	}
}

func TestListByTeamAndUser(t *testing.T) {
	store := newFakeStore()
	ctx := context.Background()

	_, _, err := store.Create(ctx, apikey.KeyMetadata{User: "alice", Team: "eng"})
	if err != nil {
		t.Fatalf("Create #1 error = %v", err)
	}
	_, _, err = store.Create(ctx, apikey.KeyMetadata{User: "bob", Team: "eng"})
	if err != nil {
		t.Fatalf("Create #2 error = %v", err)
	}
	_, _, err = store.Create(ctx, apikey.KeyMetadata{User: "carol", Team: "sci"})
	if err != nil {
		t.Fatalf("Create #3 error = %v", err)
	}

	// Filter by team.
	eng, err := store.ListByTeamAndUser(ctx, "eng", "")
	if err != nil {
		t.Fatalf("ListByTeamAndUser(eng, '') error = %v", err)
	}
	if len(eng) != 2 {
		t.Errorf("ListByTeamAndUser(eng, '') = %d items, want 2", len(eng))
	}

	// Filter by team + user.
	alice, err := store.ListByTeamAndUser(ctx, "eng", "alice")
	if err != nil {
		t.Fatalf("ListByTeamAndUser(eng, alice) error = %v", err)
	}
	if len(alice) != 1 {
		t.Errorf("ListByTeamAndUser(eng, alice) = %d items, want 1", len(alice))
	}

	// No filter = all.
	all, err := store.ListByTeamAndUser(ctx, "", "")
	if err != nil {
		t.Fatalf("ListByTeamAndUser('', '') error = %v", err)
	}
	if len(all) != 3 {
		t.Errorf("ListByTeamAndUser('', '') = %d items, want 3", len(all))
	}
}

func TestGet(t *testing.T) {
	store := newFakeStore()
	ctx := context.Background()

	meta := apikey.KeyMetadata{Namespace: "ns-get", User: "eve", Team: "ops"}
	_, keyID, err := store.Create(ctx, meta)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := store.Get(ctx, keyID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Team != meta.Team {
		t.Errorf("Team = %q, want %q", got.Team, meta.Team)
	}
}

func TestGet_NotFound(t *testing.T) {
	store := newFakeStore()
	ctx := context.Background()

	_, err := store.Get(ctx, "agentbox-apikey-nosuchsecret")
	if err != apikey.ErrTokenNotFound {
		t.Errorf("Get() error = %v, want ErrTokenNotFound", err)
	}
}

func TestDelete(t *testing.T) {
	store := newFakeStore()
	ctx := context.Background()

	meta := apikey.KeyMetadata{Namespace: "ns-del", User: "frank"}
	rawToken, keyID, err := store.Create(ctx, meta)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Validate first to populate cache.
	if _, err := store.Validate(ctx, rawToken); err != nil {
		t.Fatalf("Validate() before delete error = %v", err)
	}

	// Delete.
	if err := store.Delete(ctx, keyID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Validate should now return not found (cache evicted, K8s secret gone).
	_, err = store.Validate(ctx, rawToken)
	if err != apikey.ErrTokenNotFound {
		t.Errorf("Validate() after delete = %v, want ErrTokenNotFound", err)
	}
}

func TestDelete_NotFound(t *testing.T) {
	store := newFakeStore()
	ctx := context.Background()

	err := store.Delete(ctx, "agentbox-apikey-nosuchsecret")
	if err != apikey.ErrTokenNotFound {
		t.Errorf("Delete() error = %v, want ErrTokenNotFound", err)
	}
}

func TestDelete_NonAPIKeySecret(t *testing.T) {
	// Create a secret without the api-key label.
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agentbox-apikey-abcdef1234567890",
			Namespace: "agentbox-system",
			Labels:    map[string]string{"agentbox.io/type": "other"},
		},
		Data: map[string][]byte{"token": []byte("somehash")},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(newFakeScheme()).WithObjects(s).Build()
	store := apikey.NewSecretKeyStore(apikey.SecretKeyStoreConfig{
		Client:           fakeClient,
		SecretsNamespace: "agentbox-system",
	})

	err := store.Delete(context.Background(), s.Name)
	if err != apikey.ErrTokenNotFound {
		t.Errorf("Delete(non-apikey) = %v, want ErrTokenNotFound", err)
	}
}

func TestCreateFromHash(t *testing.T) {
	store := newFakeStore()
	ctx := context.Background()

	meta := apikey.KeyMetadata{
		Namespace:   "test-ns",
		User:        "alice",
		Team:        "eng",
		Description: "synced key",
		Role:        "tenant",
		IssuedAt:    time.Now().UTC(),
	}
	tokenHash := strings.Repeat("0", 64)
	hashPrefix := tokenHash[:16]

	if err := store.CreateFromHash(ctx, meta, tokenHash, hashPrefix); err != nil {
		t.Fatalf("CreateFromHash() error = %v", err)
	}

	// Validate: the token with the known hash should resolve correctly.
	got, err := store.Get(ctx, "agentbox-apikey-"+hashPrefix)
	if err != nil {
		t.Fatalf("Get() after CreateFromHash error = %v", err)
	}
	if got.User != meta.User {
		t.Errorf("User = %q, want %q", got.User, meta.User)
	}
	if got.Namespace != meta.Namespace {
		t.Errorf("Namespace = %q, want %q", got.Namespace, meta.Namespace)
	}
}

func TestCreateFromHash_Idempotent(t *testing.T) {
	store := newFakeStore()
	ctx := context.Background()

	meta := apikey.KeyMetadata{Namespace: "ns-idem", User: "bob"}
	tokenHash := "1111111111111111111111111111111111111111111111111111111111111111"
	hashPrefix := tokenHash[:16]

	// First call creates.
	if err := store.CreateFromHash(ctx, meta, tokenHash, hashPrefix); err != nil {
		t.Fatalf("first CreateFromHash() error = %v", err)
	}
	// Second call must be a no-op (idempotent).
	if err := store.CreateFromHash(ctx, meta, tokenHash, hashPrefix); err != nil {
		t.Fatalf("second CreateFromHash() (idempotent) error = %v", err)
	}

	// Only one Secret should exist.
	all, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(all) != 1 {
		t.Errorf("List() = %d items, want 1 (idempotent create)", len(all))
	}
}

func TestCountUserKeys(t *testing.T) {
	store := newFakeStore()
	ctx := context.Background()

	// No keys yet.
	n, err := store.CountUserKeys(ctx, "ns-count", "alice")
	if err != nil {
		t.Fatalf("CountUserKeys() error = %v", err)
	}
	if n != 0 {
		t.Errorf("CountUserKeys() = %d, want 0", n)
	}

	// Create two keys for alice and one for bob (same namespace).
	for range 2 {
		if _, _, err := store.Create(ctx, apikey.KeyMetadata{Namespace: "ns-count", User: "alice"}); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}
	if _, _, err := store.Create(ctx, apikey.KeyMetadata{Namespace: "ns-count", User: "bob"}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	n, err = store.CountUserKeys(ctx, "ns-count", "alice")
	if err != nil {
		t.Fatalf("CountUserKeys(alice) error = %v", err)
	}
	if n != 2 {
		t.Errorf("CountUserKeys(alice) = %d, want 2", n)
	}

	n, err = store.CountUserKeys(ctx, "ns-count", "bob")
	if err != nil {
		t.Fatalf("CountUserKeys(bob) error = %v", err)
	}
	if n != 1 {
		t.Errorf("CountUserKeys(bob) = %d, want 1", n)
	}
}

func TestGet_TokenHashPopulated(t *testing.T) {
	store := newFakeStore()
	ctx := context.Background()

	meta := apikey.KeyMetadata{
		Namespace:   "ns-hash",
		User:        "grace",
		Description: "token hash test",
	}

	rawToken, keyID, err := store.Create(ctx, meta)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	_ = rawToken

	got, err := store.Get(ctx, keyID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got.TokenHash == "" {
		t.Error("Get() TokenHash is empty, expected the SHA-256 hash of the raw token")
	}

	// TokenHash should be a 64-char hex string (SHA-256 produces 32 bytes = 64 hex chars).
	if len(got.TokenHash) != 64 {
		t.Errorf("Get() TokenHash length = %d, want 64", len(got.TokenHash))
	}
}

func TestKeyMetadata_JSONOmitsTokenHash(t *testing.T) {
	meta := apikey.KeyMetadata{
		KeyID:       "agentbox-system/agentbox-apikey-abc123",
		Namespace:   "test-ns",
		Role:        "tenant",
		User:        "alice",
		Team:        "eng",
		Description: "json test",
		IssuedAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		TokenHash:   "should_not_appear_in_json",
	}

	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	// TokenHash must NOT appear in JSON output (json:"-" tag).
	if _, ok := parsed["tokenHash"]; ok {
		t.Error("json.Marshal() should not include tokenHash (has json:\"-\" tag)")
	}
	if _, ok := parsed["TokenHash"]; ok {
		t.Error("json.Marshal() should not include TokenHash (has json:\"-\" tag)")
	}

	// Verify camelCase json tags are applied correctly.
	expectedKeys := []string{"keyId", "namespace", "role", "user", "team", "description", "issuedAt", "expiresAt"}
	for _, key := range expectedKeys {
		if _, ok := parsed[key]; !ok {
			t.Errorf("json.Marshal() missing expected key %q", key)
		}
	}

	// Verify no PascalCase keys are present (would indicate missing json tags).
	unexpectedKeys := []string{"KeyID", "Namespace", "Role", "User", "Team", "Description", "IssuedAt", "ExpiresAt", "QuotaURL"}
	for _, key := range unexpectedKeys {
		if _, ok := parsed[key]; ok {
			t.Errorf("json.Marshal() has unexpected PascalCase key %q (missing json tag?)", key)
		}
	}
}
