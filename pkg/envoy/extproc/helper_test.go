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

package extproc

import (
	"testing"

	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
)

func TestExtractTarget_HeaderStrategy(t *testing.T) {
	hdrMap := map[string]string{
		"x-sandbox-id":   "sbx-abc",
		"x-sandbox-port": "8080",
		":path":          "/original/path",
	}
	target, err := extractTarget(hdrMap)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.SandboxID != "sbx-abc" {
		t.Fatalf("expected sandboxID sbx-abc, got %s", target.SandboxID)
	}
	if target.Port != 8080 {
		t.Fatalf("expected port 8080, got %d", target.Port)
	}
	if target.RewrittenPath != "/original/path" {
		t.Fatalf("expected path /original/path, got %s", target.RewrittenPath)
	}
}

func TestExtractTarget_HeaderStrategy_MissingPort(t *testing.T) {
	hdrMap := map[string]string{
		"x-sandbox-id": "sbx-abc",
	}
	_, err := extractTarget(hdrMap)
	if err == nil {
		t.Fatal("expected error for missing port header, got nil")
	}
}

func TestExtractTarget_HeaderStrategy_InvalidPort(t *testing.T) {
	hdrMap := map[string]string{
		"x-sandbox-id":   "sbx-abc",
		"x-sandbox-port": "notaport",
	}
	_, err := extractTarget(hdrMap)
	if err == nil {
		t.Fatal("expected error for invalid port, got nil")
	}
}

func TestExtractTarget_URLStrategy(t *testing.T) {
	tests := []struct {
		name          string
		path          string
		wantSandboxID string
		wantPort      int
		wantPath      string
		wantErr       bool
	}{
		{
			name:          "full URL with extra path",
			path:          "/v1/sandboxes/my-sandbox/8080/api/run",
			wantSandboxID: "my-sandbox",
			wantPort:      8080,
			wantPath:      "/api/run",
		},
		{
			name:          "URL with query string",
			path:          "/sandboxes/sbx-123/9090/api/v1?key=val&other=2",
			wantSandboxID: "sbx-123",
			wantPort:      9090,
			wantPath:      "/api/v1?key=val&other=2",
		},
		{
			name:          "sandbox ID and port only",
			path:          "/sandboxes/sbx-xyz/3000/",
			wantSandboxID: "sbx-xyz",
			wantPort:      3000,
			wantPath:      "/",
		},
		{
			name:     "no marker in path",
			path:     "/api/v1/something",
			wantPath: "",
		},
		{
			name:    "invalid port in URL",
			path:    "/sandboxes/sbx-abc/notaport/path",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hdrMap := map[string]string{":path": tc.path}
			target, err := extractTarget(hdrMap)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if target.SandboxID != tc.wantSandboxID {
				t.Errorf("sandboxID = %q, want %q", target.SandboxID, tc.wantSandboxID)
			}
			if target.Port != tc.wantPort {
				t.Errorf("port = %d, want %d", target.Port, tc.wantPort)
			}
			if tc.wantPath != "" && target.RewrittenPath != tc.wantPath {
				t.Errorf("rewrittenPath = %q, want %q", target.RewrittenPath, tc.wantPath)
			}
		})
	}
}

func TestExtractTarget_EmptyPath(t *testing.T) {
	hdrMap := map[string]string{}
	target, err := extractTarget(hdrMap)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.SandboxID != "" {
		t.Fatalf("expected empty sandboxID, got %s", target.SandboxID)
	}
}

func TestExtractHeaders(t *testing.T) {
	headers := &extProcPb.HttpHeaders{
		Headers: &corev3.HeaderMap{
			Headers: []*corev3.HeaderValue{
				{Key: "Content-Type", Value: "application/json"},
				{Key: "X-Sandbox-Id", RawValue: []byte("sbx-raw")},
			},
		},
	}
	hdrMap := extractHeaders(headers)
	if hdrMap["content-type"] != "application/json" {
		t.Errorf("content-type = %q, want %q", hdrMap["content-type"], "application/json")
	}
	if hdrMap["x-sandbox-id"] != "sbx-raw" {
		t.Errorf("x-sandbox-id = %q, want %q", hdrMap["x-sandbox-id"], "sbx-raw")
	}
}

func TestSandboxRoute_DestHost(t *testing.T) {
	r := &SandboxRoute{PodIP: "10.0.0.1", Port: 8080}
	got := r.DestHost()
	want := "10.0.0.1:8080"
	if got != want {
		t.Fatalf("DestHost() = %q, want %q", got, want)
	}
}
