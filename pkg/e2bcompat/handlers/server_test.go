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
	"maps"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	apidomain "github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	e2bgen "github.com/scitix/agent-sandbox/pkg/e2bcompat/gen"
)

// mockSandboxService captures the CreateSandboxInput for assertion and serves a
// canned list for the List path.
type mockSandboxService struct {
	service.SandboxService
	lastInput service.CreateSandboxInput
	listItems []gen.Sandbox
}

func (m *mockSandboxService) Create(_ context.Context, input service.CreateSandboxInput) (*gen.Sandbox, *apidomain.AppError) {
	m.lastInput = input
	return &gen.Sandbox{
		SandboxId: "sbx-test",
		PoolName:  input.PoolName,
		Namespace: input.Namespace,
		Status:    "Running",
	}, nil
}

func (m *mockSandboxService) List(_ context.Context, _ service.SandboxListFilter) (*service.ListSandboxesResult, *apidomain.AppError) {
	return &service.ListSandboxesResult{Items: m.listItems, Total: len(m.listItems)}, nil
}

// ctxWithAuth returns a context carrying AuthInfo so that authFrom() can extract it.
func ctxWithAuth(ns string) context.Context {
	return context.WithValue(context.Background(), "auth", apidomain.AuthInfo{Namespace: ns}) //nolint:staticcheck
}

// ginCtxWithAuth builds a *gin.Context (which doubles as a context.Context under
// oapi-codegen's strict server) backed by a recorder so tests can assert both the
// handler result and any response headers it sets (e.g. x-next-token).
func ginCtxWithAuth() (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	gc, _ := gin.CreateTestContext(rec)
	gc.Set("auth", apidomain.AuthInfo{Namespace: "test-ns"})
	return gc, rec
}

func mkSandbox(id string, metadata map[string]string) gen.Sandbox {
	sb := gen.Sandbox{
		SandboxId: id,
		PoolName:  "my-pool",
		Namespace: "test-ns",
		Status:    "Running",
	}
	if metadata != nil {
		m := make(map[string]string, len(metadata))
		maps.Copy(m, metadata)
		sb.Metadata = &m
	}
	return sb
}

func TestGetV2Sandboxes_MetadataFilter(t *testing.T) {
	mock := &mockSandboxService{listItems: []gen.Sandbox{
		mkSandbox("a", map[string]string{"app": "prod"}),
		mkSandbox("b", map[string]string{"app": "staging"}),
		mkSandbox("c", map[string]string{"app": "prod", "team": "x"}),
	}}
	srv := &Server{sandbox: mock}

	filter := "app=prod"
	gc, _ := ginCtxWithAuth()
	resp, err := srv.GetV2Sandboxes(gc, e2bgen.GetV2SandboxesRequestObject{
		Params: e2bgen.GetV2SandboxesParams{Metadata: &filter},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	page, ok := resp.(e2bgen.GetV2Sandboxes200JSONResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", resp)
	}
	if len(page) != 2 {
		t.Fatalf("want 2 sandboxes matching app=prod, got %d", len(page))
	}
	for _, sb := range page {
		if sb.SandboxID != "a" && sb.SandboxID != "c" {
			t.Errorf("unexpected sandbox %q in filtered result", sb.SandboxID)
		}
	}
}

func TestGetV2Sandboxes_StateFilter(t *testing.T) {
	mock := &mockSandboxService{listItems: []gen.Sandbox{
		mkSandbox("a", nil),
		mkSandbox("b", nil),
	}}
	srv := &Server{sandbox: mock}

	// All sandboxes report "running"; filtering to paused yields none.
	paused := []e2bgen.SandboxState{e2bgen.Paused}
	gc, _ := ginCtxWithAuth()
	resp, err := srv.GetV2Sandboxes(gc, e2bgen.GetV2SandboxesRequestObject{
		Params: e2bgen.GetV2SandboxesParams{State: &paused},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if page := resp.(e2bgen.GetV2Sandboxes200JSONResponse); len(page) != 0 {
		t.Fatalf("want 0 paused sandboxes, got %d", len(page))
	}

	running := []e2bgen.SandboxState{e2bgen.Running}
	gc2, _ := ginCtxWithAuth()
	resp2, _ := srv.GetV2Sandboxes(gc2, e2bgen.GetV2SandboxesRequestObject{
		Params: e2bgen.GetV2SandboxesParams{State: &running},
	})
	if page := resp2.(e2bgen.GetV2Sandboxes200JSONResponse); len(page) != 2 {
		t.Fatalf("want 2 running sandboxes, got %d", len(page))
	}
}

func TestGetV2Sandboxes_Pagination(t *testing.T) {
	items := make([]gen.Sandbox, 5)
	for i := range items {
		items[i] = mkSandbox(string(rune('a'+i)), nil)
	}
	mock := &mockSandboxService{listItems: items}
	srv := &Server{sandbox: mock}

	limit := int32(2)

	// First page: 2 items + a next-token header pointing at the third item.
	gc, rec := ginCtxWithAuth()
	resp, err := srv.GetV2Sandboxes(gc, e2bgen.GetV2SandboxesRequestObject{
		Params: e2bgen.GetV2SandboxesParams{Limit: &limit},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	page := resp.(e2bgen.GetV2Sandboxes200JSONResponse)
	if len(page) != 2 || page[0].SandboxID != "a" || page[1].SandboxID != "b" {
		t.Fatalf("page 1 unexpected: %+v", page)
	}
	token := rec.Header().Get("x-next-token")
	if token == "" {
		t.Fatal("expected x-next-token header on non-final page")
	}

	// Second page: next 2 items.
	gc2, _ := ginCtxWithAuth()
	resp2, _ := srv.GetV2Sandboxes(gc2, e2bgen.GetV2SandboxesRequestObject{
		Params: e2bgen.GetV2SandboxesParams{Limit: &limit, NextToken: &token},
	})
	page2 := resp2.(e2bgen.GetV2Sandboxes200JSONResponse)
	if len(page2) != 2 || page2[0].SandboxID != "c" || page2[1].SandboxID != "d" {
		t.Fatalf("page 2 unexpected: %+v", page2)
	}

	// Final page: 1 item, no next-token header.
	token2 := decodeNextToken(&token)
	nextTok := encodeNextToken(token2 + 2)
	gc3, rec3 := ginCtxWithAuth()
	resp3, _ := srv.GetV2Sandboxes(gc3, e2bgen.GetV2SandboxesRequestObject{
		Params: e2bgen.GetV2SandboxesParams{Limit: &limit, NextToken: &nextTok},
	})
	page3 := resp3.(e2bgen.GetV2Sandboxes200JSONResponse)
	if len(page3) != 1 || page3[0].SandboxID != "e" {
		t.Fatalf("page 3 unexpected: %+v", page3)
	}
	if rec3.Header().Get("x-next-token") != "" {
		t.Fatal("did not expect x-next-token header on final page")
	}
}

func TestPostSandboxes_MetadataKeys(t *testing.T) {
	tests := []struct {
		name               string
		metadata           map[string]string
		wantImage          string
		wantStartupTimeout time.Duration
		wantScalingGroup   string
		wantMetadataKeys   []string // keys that should remain in forwarded metadata
		wantConsumedKeys   []string // keys that should NOT be in forwarded metadata
	}{
		{
			name:             "scaling group from metadata",
			metadata:         map[string]string{"agentbox.scitix.ai/scaling-group": "1c2Gi"},
			wantScalingGroup: "1c2Gi",
			wantConsumedKeys: []string{"agentbox.scitix.ai/scaling-group"},
		},
		{
			name:               "scaling group alongside user metadata and other reserved keys",
			metadata:           map[string]string{"agentbox.scitix.ai/scaling-group": "2c4Gi", "agentbox.scitix.ai/startup-timeout": "90", "env": "staging"},
			wantStartupTimeout: 90 * time.Second,
			wantScalingGroup:   "2c4Gi",
			wantMetadataKeys:   []string{"env"},
			wantConsumedKeys:   []string{"agentbox.scitix.ai/scaling-group", "agentbox.scitix.ai/startup-timeout"},
		},
		{
			name:               "startup timeout from metadata",
			metadata:           map[string]string{"agentbox.scitix.ai/startup-timeout": "300"},
			wantStartupTimeout: 300 * time.Second,
			wantConsumedKeys:   []string{"agentbox.scitix.ai/startup-timeout"},
		},
		{
			name:               "image and startup timeout together",
			metadata:           map[string]string{"agentbox.scitix.ai/image": "python:3.11", "agentbox.scitix.ai/startup-timeout": "60"},
			wantImage:          "python:3.11",
			wantStartupTimeout: 60 * time.Second,
			wantConsumedKeys:   []string{"agentbox.scitix.ai/image", "agentbox.scitix.ai/startup-timeout"},
		},
		{
			name:               "invalid startup timeout ignored",
			metadata:           map[string]string{"agentbox.scitix.ai/startup-timeout": "abc"},
			wantStartupTimeout: 0,
			wantConsumedKeys:   []string{"agentbox.scitix.ai/startup-timeout"},
		},
		{
			name:               "zero startup timeout ignored",
			metadata:           map[string]string{"agentbox.scitix.ai/startup-timeout": "0"},
			wantStartupTimeout: 0,
			wantConsumedKeys:   []string{"agentbox.scitix.ai/startup-timeout"},
		},
		{
			name:               "negative startup timeout ignored",
			metadata:           map[string]string{"agentbox.scitix.ai/startup-timeout": "-10"},
			wantStartupTimeout: 0,
			wantConsumedKeys:   []string{"agentbox.scitix.ai/startup-timeout"},
		},
		{
			name:               "user metadata preserved alongside consumed keys",
			metadata:           map[string]string{"agentbox.scitix.ai/startup-timeout": "120", "env": "staging"},
			wantStartupTimeout: 120 * time.Second,
			wantMetadataKeys:   []string{"env"},
			wantConsumedKeys:   []string{"agentbox.scitix.ai/startup-timeout"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockSandboxService{}
			srv := &Server{sandbox: mock}

			meta := e2bgen.SandboxMetadata(tc.metadata)
			req := e2bgen.PostSandboxesRequestObject{
				Body: &e2bgen.NewSandbox{
					TemplateID: "my-pool",
					Metadata:   &meta,
				},
			}

			// Inject auth context (handler reads namespace from it)
			ctx := ctxWithAuth("test-ns")

			_, err := srv.PostSandboxes(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if mock.lastInput.Image != tc.wantImage {
				t.Errorf("Image = %q, want %q", mock.lastInput.Image, tc.wantImage)
			}
			if mock.lastInput.StartupTimeout != tc.wantStartupTimeout {
				t.Errorf("StartupTimeout = %v, want %v", mock.lastInput.StartupTimeout, tc.wantStartupTimeout)
			}
			if mock.lastInput.RequestedScalingGroup != tc.wantScalingGroup {
				t.Errorf("RequestedScalingGroup = %q, want %q", mock.lastInput.RequestedScalingGroup, tc.wantScalingGroup)
			}

			for _, key := range tc.wantMetadataKeys {
				if _, ok := mock.lastInput.Metadata[key]; !ok {
					t.Errorf("expected metadata key %q to be preserved", key)
				}
			}
			for _, key := range tc.wantConsumedKeys {
				if _, ok := mock.lastInput.Metadata[key]; ok {
					t.Errorf("expected metadata key %q to be consumed (removed), but it still exists", key)
				}
			}
		})
	}
}
