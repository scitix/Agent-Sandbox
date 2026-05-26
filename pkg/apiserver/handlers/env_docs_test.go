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
	"testing"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
)

// stubAPIKeyService implements just the ListByTeamAndUser method used by
// renderEnvDocs. Other methods panic so any unexpected call is caught.
type stubAPIKeyService struct {
	items   []service.APIKeyItem
	listErr *domain.AppError
}

var _ service.APIKeyService = (*stubAPIKeyService)(nil)

func (s *stubAPIKeyService) ListByTeamAndUser(context.Context, string, string) ([]service.APIKeyItem, *domain.AppError) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.items, nil
}

func (s *stubAPIKeyService) Create(context.Context, service.CreateAPIKeyInput) (*service.APIKeyResult, *domain.AppError) {
	panic("not implemented")
}
func (s *stubAPIKeyService) List(context.Context) ([]service.APIKeyItem, *domain.AppError) {
	panic("not implemented")
}
func (s *stubAPIKeyService) Get(context.Context, string) (*service.APIKeyItem, *domain.AppError) {
	panic("not implemented")
}
func (s *stubAPIKeyService) Delete(context.Context, string) *domain.AppError {
	panic("not implemented")
}
func (s *stubAPIKeyService) Promote(context.Context, string) *domain.AppError {
	panic("not implemented")
}

func newTestServer(stub *stubAPIKeyService) *Server {
	return &Server{apikey: stub}
}

func TestRenderEnvDocs_EmptyRaw(t *testing.T) {
	s := newTestServer(&stubAPIKeyService{})
	got, err := s.renderEnvDocs(context.Background(), "", "e", "c", domain.AuthInfo{Team: "t", User: "u"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestRenderEnvDocs_SubstitutesAllVariables(t *testing.T) {
	stub := &stubAPIKeyService{
		items: []service.APIKeyItem{
			{KeyMetadata: service.KeyMetadata{RawToken: "agbx_newkey"}},
		},
	}
	s := newTestServer(stub)
	raw := "env=${AGBX_ENV_NAME} pool=${AGBX_POOL_NAME} cluster=${AGBX_CLUSTER_ID} key=${AGBX_API_KEY}"
	got, err := s.renderEnvDocs(context.Background(), raw, "myenv", "cluster3", domain.AuthInfo{Team: "t", User: "alice"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// ${AGBX_POOL_NAME} renders to the env name for backward compatibility
	// with docs authored against the old per-pool docs surface.
	want := "env=myenv pool=myenv cluster=cluster3 key=agbx_newkey"
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestRenderEnvDocs_PicksFirstKeyWithRawToken(t *testing.T) {
	stub := &stubAPIKeyService{
		items: []service.APIKeyItem{
			{KeyMetadata: service.KeyMetadata{RawToken: ""}},              // legacy, skipped
			{KeyMetadata: service.KeyMetadata{RawToken: "agbx_winner"}},   // picked
			{KeyMetadata: service.KeyMetadata{RawToken: "agbx_runnerup"}}, // ignored
		},
	}
	s := newTestServer(stub)
	got, err := s.renderEnvDocs(context.Background(), "k=${AGBX_API_KEY}", "e", "c", domain.AuthInfo{Team: "t", User: "u"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "k=agbx_winner" {
		t.Fatalf("want k=agbx_winner, got %q", got)
	}
}

func TestRenderEnvDocs_NoUsableKeyReturnsAPIKeyRequired(t *testing.T) {
	stub := &stubAPIKeyService{
		items: []service.APIKeyItem{
			{KeyMetadata: service.KeyMetadata{RawToken: ""}}, // legacy only
		},
	}
	s := newTestServer(stub)
	got, err := s.renderEnvDocs(context.Background(), "k=${AGBX_API_KEY}", "e", "c", domain.AuthInfo{Team: "t", User: "u"})
	if err == nil {
		t.Fatalf("want error, got nil (rendered=%q)", got)
	}
	if err.BizCode != domain.BizErrAPIKeyRequired {
		t.Fatalf("want BizCode=%q, got %q", domain.BizErrAPIKeyRequired, err.BizCode)
	}
	if err.Code != domain.ErrCodeUnprocessableEntity {
		t.Fatalf("want 422, got %d", err.Code)
	}
}

func TestRenderEnvDocs_NoApiKeyPlaceholderSkipsLookup(t *testing.T) {
	// Stub that would fail if asked to list keys — proves the helper does not
	// query the key store when ${AGBX_API_KEY} is absent.
	stub := &stubAPIKeyService{
		listErr: domain.NewInternal("should not be called", nil),
	}
	s := newTestServer(stub)
	got, err := s.renderEnvDocs(context.Background(), "env=${AGBX_ENV_NAME}", "myenv", "c", domain.AuthInfo{Team: "t", User: "u"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "env=myenv" {
		t.Fatalf("want env=myenv, got %q", got)
	}
}

func TestRenderTemplateDocs_EmptyRaw(t *testing.T) {
	got := renderTemplateDocs("", "cluster3")
	if got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestRenderTemplateDocs_SubstitutesEnvNameAndApiKey(t *testing.T) {
	raw := "env=${AGBX_ENV_NAME} key=${AGBX_API_KEY}"
	got := renderTemplateDocs(raw, "cluster3")
	want := "env=YOUR_ENV_NAME key=YOUR_API_KEY"
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestRenderTemplateDocs_SubstitutesRealClusterID(t *testing.T) {
	raw := "cluster=${AGBX_CLUSTER_ID}"
	got := renderTemplateDocs(raw, "cluster3")
	if got != "cluster=cluster3" {
		t.Fatalf("want cluster=cluster3, got %q", got)
	}
}

func TestRenderTemplateDocs_SubstitutesAllVariables(t *testing.T) {
	raw := "env=${AGBX_ENV_NAME} pool=${AGBX_POOL_NAME} cluster=${AGBX_CLUSTER_ID} key=${AGBX_API_KEY}"
	got := renderTemplateDocs(raw, "cluster3")
	want := "env=YOUR_ENV_NAME pool=YOUR_ENV_NAME cluster=cluster3 key=YOUR_API_KEY"
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}
