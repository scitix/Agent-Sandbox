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
// renderPoolDocs. Other methods panic so any unexpected call is caught.
type stubAPIKeyService struct {
	items   []domain.APIKeyItem
	listErr *domain.AppError
}

var _ service.APIKeyService = (*stubAPIKeyService)(nil)

func (s *stubAPIKeyService) ListByTeamAndUser(context.Context, string, string) (*domain.ListAPIKeysResult, *domain.AppError) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return &domain.ListAPIKeysResult{Items: s.items}, nil
}

func (s *stubAPIKeyService) Create(context.Context, domain.CreateAPIKeyInput) (*domain.APIKeyResult, *domain.AppError) {
	panic("not implemented")
}
func (s *stubAPIKeyService) List(context.Context) (*domain.ListAPIKeysResult, *domain.AppError) {
	panic("not implemented")
}
func (s *stubAPIKeyService) Get(context.Context, string) (*domain.APIKeyItem, *domain.AppError) {
	panic("not implemented")
}
func (s *stubAPIKeyService) Delete(context.Context, domain.DeleteAPIKeyInput) *domain.AppError {
	panic("not implemented")
}
func (s *stubAPIKeyService) Promote(context.Context, string) *domain.AppError {
	panic("not implemented")
}

func newTestServer(stub *stubAPIKeyService) *Server {
	return &Server{apikey: stub}
}

func TestRenderPoolDocs_EmptyRaw(t *testing.T) {
	s := newTestServer(&stubAPIKeyService{})
	got, err := s.renderPoolDocs(context.Background(), "", "p", "c", domain.AuthInfo{Team: "t", User: "u"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestRenderPoolDocs_SubstitutesAllVariables(t *testing.T) {
	stub := &stubAPIKeyService{
		items: []domain.APIKeyItem{
			{KeyMetadata: domain.KeyMetadata{RawToken: "agbx_newkey"}},
		},
	}
	s := newTestServer(stub)
	raw := "pool=${AGBX_POOL_NAME} cluster=${AGBX_CLUSTER_ID} key=${AGBX_API_KEY}"
	got, err := s.renderPoolDocs(context.Background(), raw, "mypool", "cluster3", domain.AuthInfo{Team: "t", User: "alice"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "pool=mypool cluster=cluster3 key=agbx_newkey"
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestRenderPoolDocs_PicksFirstKeyWithRawToken(t *testing.T) {
	stub := &stubAPIKeyService{
		items: []domain.APIKeyItem{
			{KeyMetadata: domain.KeyMetadata{RawToken: ""}},              // legacy, skipped
			{KeyMetadata: domain.KeyMetadata{RawToken: "agbx_winner"}},   // picked
			{KeyMetadata: domain.KeyMetadata{RawToken: "agbx_runnerup"}}, // ignored
		},
	}
	s := newTestServer(stub)
	got, err := s.renderPoolDocs(context.Background(), "k=${AGBX_API_KEY}", "p", "c", domain.AuthInfo{Team: "t", User: "u"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "k=agbx_winner" {
		t.Fatalf("want k=agbx_winner, got %q", got)
	}
}

func TestRenderPoolDocs_NoUsableKeyReturnsAPIKeyRequired(t *testing.T) {
	stub := &stubAPIKeyService{
		items: []domain.APIKeyItem{
			{KeyMetadata: domain.KeyMetadata{RawToken: ""}}, // legacy only
		},
	}
	s := newTestServer(stub)
	got, err := s.renderPoolDocs(context.Background(), "k=${AGBX_API_KEY}", "p", "c", domain.AuthInfo{Team: "t", User: "u"})
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

func TestRenderPoolDocs_NoApiKeyPlaceholderSkipsLookup(t *testing.T) {
	// Stub that would fail if asked to list keys — proves the helper does not
	// query the key store when ${AGBX_API_KEY} is absent.
	stub := &stubAPIKeyService{
		listErr: domain.NewInternal("should not be called", nil),
	}
	s := newTestServer(stub)
	got, err := s.renderPoolDocs(context.Background(), "pool=${AGBX_POOL_NAME}", "mypool", "c", domain.AuthInfo{Team: "t", User: "u"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "pool=mypool" {
		t.Fatalf("want pool=mypool, got %q", got)
	}
}

func TestRenderTemplateDocs_EmptyRaw(t *testing.T) {
	got := renderTemplateDocs("", "cluster3")
	if got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestRenderTemplateDocs_SubstitutesPoolNameAndApiKey(t *testing.T) {
	raw := "pool=${AGBX_POOL_NAME} key=${AGBX_API_KEY}"
	got := renderTemplateDocs(raw, "cluster3")
	want := "pool=YOUR_POOL_NAME key=YOUR_API_KEY"
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
	raw := "pool=${AGBX_POOL_NAME} cluster=${AGBX_CLUSTER_ID} key=${AGBX_API_KEY}"
	got := renderTemplateDocs(raw, "cluster3")
	want := "pool=YOUR_POOL_NAME cluster=cluster3 key=YOUR_API_KEY"
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}
