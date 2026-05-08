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
	"time"

	apidomain "github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	e2bgen "github.com/scitix/agent-sandbox/pkg/e2bcompat/gen"
)

// mockSandboxService captures the CreateSandboxInput for assertion.
type mockSandboxService struct {
	service.SandboxService
	lastInput apidomain.CreateSandboxInput
}

func (m *mockSandboxService) Create(_ context.Context, input apidomain.CreateSandboxInput) (*apidomain.Sandbox, *apidomain.AppError) {
	m.lastInput = input
	return &apidomain.Sandbox{
		SandboxID: "sbx-test",
		PoolName:  input.PoolName,
		Namespace: input.Namespace,
		Status:    "Running",
	}, nil
}

// ctxWithAuth returns a context carrying AuthInfo so that authFrom() can extract it.
func ctxWithAuth(ns string) context.Context {
	return context.WithValue(context.Background(), "auth", apidomain.AuthInfo{Namespace: ns}) //nolint:staticcheck
}

func TestPostSandboxes_MetadataKeys(t *testing.T) {
	tests := []struct {
		name               string
		metadata           map[string]string
		wantImage          string
		wantStartupTimeout time.Duration
		wantMetadataKeys   []string // keys that should remain in forwarded metadata
		wantConsumedKeys   []string // keys that should NOT be in forwarded metadata
	}{
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
