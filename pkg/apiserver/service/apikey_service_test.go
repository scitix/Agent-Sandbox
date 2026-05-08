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

package service_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/gorilla/websocket"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
)

// ── local-mode (no SyncService) tests ────────────────────────────────────────

func newFakeAPIKeyStore(t *testing.T) *apikey.SecretKeyStore {
	t.Helper()
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	return apikey.NewSecretKeyStore(apikey.SecretKeyStoreConfig{
		Client:           fake.NewClientBuilder().WithScheme(s).Build(),
		SecretsNamespace: "agentbox-system",
		CacheTTL:         time.Minute,
	})
}

func TestAPIKeyService_Create_Local(t *testing.T) {
	ks := newFakeAPIKeyStore(t)
	svc := service.NewAPIKeyService(ks)

	result, appErr := svc.Create(context.Background(), domain.CreateAPIKeyInput{
		Namespace:   "ns-a",
		User:        "alice",
		Team:        "eng",
		Description: "test",
	})
	if appErr != nil {
		t.Fatalf("Create() appErr = %v", appErr)
	}
	if result.RawToken == "" {
		t.Error("Create() RawToken is empty")
	}
	if result.User != "alice" {
		t.Errorf("User = %q, want %q", result.User, "alice")
	}
}

func TestAPIKeyService_Create_StoreNil(t *testing.T) {
	svc := service.NewAPIKeyService(nil)
	_, appErr := svc.Create(context.Background(), domain.CreateAPIKeyInput{})
	if appErr == nil {
		t.Error("Create() expected appErr, got nil")
	}
	if appErr.Code != domain.ErrCodeServiceUnavailable {
		t.Errorf("Code = %v, want ErrCodeServiceUnavailable", appErr.Code)
	}
}

func TestAPIKeyService_List(t *testing.T) {
	ks := newFakeAPIKeyStore(t)
	svc := service.NewAPIKeyService(ks)
	ctx := context.Background()

	for range 3 {
		_, appErr := svc.Create(ctx, domain.CreateAPIKeyInput{Namespace: "ns-list", User: "bob", Team: "eng"})
		if appErr != nil {
			t.Fatalf("Create() appErr = %v", appErr)
		}
	}
	_, appErr := svc.Create(ctx, domain.CreateAPIKeyInput{Namespace: "other-ns", User: "carol", Team: "sci"})
	if appErr != nil {
		t.Fatalf("Create() appErr = %v", appErr)
	}

	// List all keys.
	result, appErr := svc.List(ctx)
	if appErr != nil {
		t.Fatalf("List() appErr = %v", appErr)
	}
	if len(result.Items) != 4 {
		t.Errorf("List() = %d items, want 4", len(result.Items))
	}
}

func TestAPIKeyService_ListByTeamAndUser(t *testing.T) {
	ks := newFakeAPIKeyStore(t)
	svc := service.NewAPIKeyService(ks)
	ctx := context.Background()

	// Create keys for different team/user combos.
	for range 2 {
		_, appErr := svc.Create(ctx, domain.CreateAPIKeyInput{User: "alice", Team: "eng"})
		if appErr != nil {
			t.Fatalf("Create() appErr = %v", appErr)
		}
	}
	_, appErr := svc.Create(ctx, domain.CreateAPIKeyInput{User: "bob", Team: "eng"})
	if appErr != nil {
		t.Fatalf("Create() appErr = %v", appErr)
	}
	_, appErr = svc.Create(ctx, domain.CreateAPIKeyInput{User: "carol", Team: "sci"})
	if appErr != nil {
		t.Fatalf("Create() appErr = %v", appErr)
	}

	// Filter by team only.
	result, appErr := svc.ListByTeamAndUser(ctx, "eng", "")
	if appErr != nil {
		t.Fatalf("ListByTeamAndUser(eng, '') appErr = %v", appErr)
	}
	if len(result.Items) != 3 {
		t.Errorf("ListByTeamAndUser(eng, '') = %d items, want 3", len(result.Items))
	}

	// Filter by team + user.
	result, appErr = svc.ListByTeamAndUser(ctx, "eng", "alice")
	if appErr != nil {
		t.Fatalf("ListByTeamAndUser(eng, alice) appErr = %v", appErr)
	}
	if len(result.Items) != 2 {
		t.Errorf("ListByTeamAndUser(eng, alice) = %d items, want 2", len(result.Items))
	}

	// Filter by user only.
	result, appErr = svc.ListByTeamAndUser(ctx, "", "carol")
	if appErr != nil {
		t.Fatalf("ListByTeamAndUser('', carol) appErr = %v", appErr)
	}
	if len(result.Items) != 1 {
		t.Errorf("ListByTeamAndUser('', carol) = %d items, want 1", len(result.Items))
	}

	// No filter = all keys.
	result, appErr = svc.ListByTeamAndUser(ctx, "", "")
	if appErr != nil {
		t.Fatalf("ListByTeamAndUser('', '') appErr = %v", appErr)
	}
	if len(result.Items) != 4 {
		t.Errorf("ListByTeamAndUser('', '') = %d items, want 4", len(result.Items))
	}
}

func TestAPIKeyService_Get(t *testing.T) {
	ks := newFakeAPIKeyStore(t)
	svc := service.NewAPIKeyService(ks)
	ctx := context.Background()

	created, appErr := svc.Create(ctx, domain.CreateAPIKeyInput{
		Namespace: "ns-get", User: "dan", Team: "ops",
	})
	if appErr != nil {
		t.Fatalf("Create() appErr = %v", appErr)
	}

	got, appErr := svc.Get(ctx, created.KeyID)
	if appErr != nil {
		t.Fatalf("Get() appErr = %v", appErr)
	}
	if got.Team != "ops" {
		t.Errorf("Team = %q, want %q", got.Team, "ops")
	}
}

func TestAPIKeyService_Get_NotFound(t *testing.T) {
	ks := newFakeAPIKeyStore(t)
	svc := service.NewAPIKeyService(ks)

	_, appErr := svc.Get(context.Background(), "agentbox-apikey-nosuchkey")
	if appErr == nil {
		t.Error("Get() expected appErr, got nil")
	}
	if appErr.Code != domain.ErrCodeNotFound {
		t.Errorf("Code = %v, want ErrCodeNotFound", appErr.Code)
	}
}

func TestAPIKeyService_Delete(t *testing.T) {
	ks := newFakeAPIKeyStore(t)
	svc := service.NewAPIKeyService(ks)
	ctx := context.Background()

	created, appErr := svc.Create(ctx, domain.CreateAPIKeyInput{Namespace: "ns-del", User: "eve"})
	if appErr != nil {
		t.Fatalf("Create() appErr = %v", appErr)
	}

	appErr = svc.Delete(ctx, domain.DeleteAPIKeyInput{KeyID: created.KeyID})
	if appErr != nil {
		t.Fatalf("Delete() appErr = %v", appErr)
	}

	_, appErr = svc.Get(ctx, created.KeyID)
	if appErr == nil {
		t.Error("Get() after Delete expected appErr, got nil")
	}
	if appErr.Code != domain.ErrCodeNotFound {
		t.Errorf("Code = %v, want ErrCodeNotFound", appErr.Code)
	}
}

func TestAPIKeyService_Delete_NotFound(t *testing.T) {
	ks := newFakeAPIKeyStore(t)
	svc := service.NewAPIKeyService(ks)

	appErr := svc.Delete(context.Background(), domain.DeleteAPIKeyInput{KeyID: "agentbox-apikey-nosuch"})
	if appErr == nil {
		t.Error("Delete() expected appErr, got nil")
	}
	if appErr.Code != domain.ErrCodeNotFound {
		t.Errorf("Code = %v, want ErrCodeNotFound", appErr.Code)
	}
}

// ── WS-forwarding mode (with SyncService) tests ───────────────────────────────

// stubSyncService is a controllable test double for SyncService.
type stubSyncService struct {
	createResp *service.CreateKeyResponse
	createErr  error
	deleteErr  error
}

func (s *stubSyncService) OnConnect(_ *websocket.Conn) uint64 { return 0 }
func (s *stubSyncService) OnDisconnect(_ uint64)              {}
func (s *stubSyncService) HandleIncoming(_ context.Context, _ service.SyncEvent) error {
	return nil
}
func (s *stubSyncService) RequestCreate(_ context.Context, _ service.CreateKeyRequest) (*service.CreateKeyResponse, error) {
	return s.createResp, s.createErr
}
func (s *stubSyncService) RequestDelete(_ context.Context, _ string) error {
	return s.deleteErr
}

// Template operation stubs — no-op implementations satisfying the interface.
func (s *stubSyncService) RequestTemplateCreate(_ context.Context, _ json.RawMessage) error {
	return nil
}
func (s *stubSyncService) RequestTemplateUpdate(_ context.Context, _ json.RawMessage) error {
	return nil
}
func (s *stubSyncService) RequestTemplateDelete(_ context.Context, _ string) error {
	return nil
}

// Verify stubSyncService satisfies the interface at compile time.
var _ service.SyncService = (*stubSyncService)(nil)

func TestAPIKeyService_Create_SyncMode_Success(t *testing.T) {
	ks := newFakeAPIKeyStore(t)
	stub := &stubSyncService{
		createResp: &service.CreateKeyResponse{
			RawToken:   "agbx_synced",
			KeyID:      "agentbox-apikey-1234123412341234",
			HashPrefix: "1234123412341234",
			IssuedAt:   time.Now().UTC().Format(time.RFC3339),
		},
	}
	svc := service.NewAPIKeyServiceWithSync(ks, stub)

	result, appErr := svc.Create(context.Background(), domain.CreateAPIKeyInput{
		Namespace: "ns-sync", User: "frank",
	})
	if appErr != nil {
		t.Fatalf("Create() appErr = %v", appErr)
	}
	if result.RawToken != "agbx_synced" {
		t.Errorf("RawToken = %q, want %q", result.RawToken, "agbx_synced")
	}
}

func TestAPIKeyService_Create_SyncMode_NotConnected(t *testing.T) {
	ks := newFakeAPIKeyStore(t)
	stub := &stubSyncService{createErr: service.ErrSyncNotConnected}
	svc := service.NewAPIKeyServiceWithSync(ks, stub)

	_, appErr := svc.Create(context.Background(), domain.CreateAPIKeyInput{
		Namespace: "ns-sync", User: "grace",
	})
	if appErr == nil {
		t.Fatal("Create() expected appErr, got nil")
	}
	if appErr.Code != domain.ErrCodeServiceUnavailable {
		t.Errorf("Code = %v, want ErrCodeServiceUnavailable", appErr.Code)
	}
}

func TestAPIKeyService_Create_SyncMode_QuotaExceeded(t *testing.T) {
	ks := newFakeAPIKeyStore(t)
	stub := &stubSyncService{
		createErr: &service.SyncHTTPError{Status: 409, Message: "exceeded max keys per user"},
	}
	svc := service.NewAPIKeyServiceWithSync(ks, stub)

	_, appErr := svc.Create(context.Background(), domain.CreateAPIKeyInput{
		Namespace: "ns-quota", User: "henry",
	})
	if appErr == nil {
		t.Fatal("Create() expected appErr, got nil")
	}
	if appErr.Code != domain.ErrCodeConflict {
		t.Errorf("Code = %v, want ErrCodeConflict (409)", appErr.Code)
	}
}

func TestAPIKeyService_Delete_SyncMode_Success(t *testing.T) {
	ks := newFakeAPIKeyStore(t)
	stub := &stubSyncService{deleteErr: nil}
	svc := service.NewAPIKeyServiceWithSync(ks, stub)

	appErr := svc.Delete(context.Background(), domain.DeleteAPIKeyInput{KeyID: "agentbox-apikey-somekey"})
	if appErr != nil {
		t.Fatalf("Delete() appErr = %v", appErr)
	}
}

func TestAPIKeyService_Delete_SyncMode_NotConnected(t *testing.T) {
	ks := newFakeAPIKeyStore(t)
	stub := &stubSyncService{deleteErr: service.ErrSyncNotConnected}
	svc := service.NewAPIKeyServiceWithSync(ks, stub)

	appErr := svc.Delete(context.Background(), domain.DeleteAPIKeyInput{KeyID: "agentbox-apikey-somekey"})
	if appErr == nil {
		t.Fatal("Delete() expected appErr, got nil")
	}
	if appErr.Code != domain.ErrCodeServiceUnavailable {
		t.Errorf("Code = %v, want ErrCodeServiceUnavailable", appErr.Code)
	}
}

func TestAPIKeyService_Delete_SyncMode_NotFound(t *testing.T) {
	ks := newFakeAPIKeyStore(t)
	stub := &stubSyncService{
		deleteErr: &service.SyncHTTPError{Status: 404, Message: "api key not found"},
	}
	svc := service.NewAPIKeyServiceWithSync(ks, stub)

	appErr := svc.Delete(context.Background(), domain.DeleteAPIKeyInput{KeyID: "agentbox-apikey-somekey"})
	if appErr == nil {
		t.Fatal("Delete() expected appErr, got nil")
	}
	if appErr.Code != domain.ErrCodeNotFound {
		t.Errorf("Code = %v, want ErrCodeNotFound", appErr.Code)
	}
}
