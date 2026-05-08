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
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"

	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
)

func TestHandleCrossClusterRequest_HappyPath(t *testing.T) {
	store := cluster.NewStore()
	store.Set([]cluster.ClusterEntry{
		{
			ID: "cluster-b",
			Gateway: &cluster.GatewayConfig{
				DataURL: "https://gw.internal.example.com/clusters/cluster-b/data",
				Headers: map[string]string{"X-GW-Auth": "secret123"},
			},
		},
	})

	s := &Server{
		clusterStore:   store,
		localClusterID: "cluster-a",
	}

	hdrMap := map[string]string{
		":path":      "/v1/sandboxes/cluster-b.abc-123/8080/exec",
		":authority": "original-host",
	}
	target := RouteTarget{
		SandboxID:     "cluster-b.abc-123",
		Port:          8080,
		RewrittenPath: "/exec",
	}

	resp := s.handleCrossClusterRequest(hdrMap, target, "cluster-b")

	// Should be a RequestHeaders response (not ImmediateResponse).
	rh, ok := resp.Response.(*extProcPb.ProcessingResponse_RequestHeaders)
	if !ok {
		t.Fatalf("expected RequestHeaders response, got %T", resp.Response)
	}

	common := rh.RequestHeaders.Response
	if !common.ClearRouteCache {
		t.Error("expected ClearRouteCache=true")
	}

	headers := make(map[string]string)
	for _, h := range common.HeaderMutation.SetHeaders {
		headers[h.Header.Key] = string(h.Header.RawValue)
	}

	// Verify :authority is rewritten to the gateway host.
	if got := headers[":authority"]; got != "gw.internal.example.com" {
		t.Errorf("expected :authority=gw.internal.example.com, got %q", got)
	}

	// Verify :path is prepended with the data plane prefix.
	expectedPath := "/clusters/cluster-b/data/v1/sandboxes/cluster-b.abc-123/8080/exec"
	if got := headers[":path"]; got != expectedPath {
		t.Errorf("expected :path=%s, got %q", expectedPath, got)
	}

	// Verify the cross-cluster header is set.
	if got := headers[crossClusterHeader]; got != "true" {
		t.Errorf("expected %s=true, got %q", crossClusterHeader, got)
	}

	// Verify gateway auth header is injected.
	if got := headers["X-GW-Auth"]; got != "secret123" {
		t.Errorf("expected X-GW-Auth=secret123, got %q", got)
	}
}

func TestHandleCrossClusterRequest_UnknownCluster(t *testing.T) {
	store := cluster.NewStore()

	s := &Server{
		clusterStore:   store,
		localClusterID: "cluster-a",
	}

	hdrMap := map[string]string{":path": "/something"}
	target := RouteTarget{SandboxID: "unknown.abc-123", Port: 8080}

	resp := s.handleCrossClusterRequest(hdrMap, target, "unknown")

	// Should be an ImmediateResponse with BadGateway.
	ir, ok := resp.Response.(*extProcPb.ProcessingResponse_ImmediateResponse)
	if !ok {
		t.Fatalf("expected ImmediateResponse, got %T", resp.Response)
	}
	if ir.ImmediateResponse.Status.Code != typev3.StatusCode_BadGateway {
		t.Errorf("expected BadGateway, got %v", ir.ImmediateResponse.Status.Code)
	}
}

func TestHandleCrossClusterRequest_NoGateway(t *testing.T) {
	store := cluster.NewStore()
	store.Set([]cluster.ClusterEntry{
		{ID: "cluster-c"}, // no Gateway
	})

	s := &Server{
		clusterStore:   store,
		localClusterID: "cluster-a",
	}

	hdrMap := map[string]string{":path": "/something"}
	target := RouteTarget{SandboxID: "cluster-c.abc-123", Port: 8080}

	resp := s.handleCrossClusterRequest(hdrMap, target, "cluster-c")

	ir, ok := resp.Response.(*extProcPb.ProcessingResponse_ImmediateResponse)
	if !ok {
		t.Fatalf("expected ImmediateResponse, got %T", resp.Response)
	}
	if ir.ImmediateResponse.Status.Code != typev3.StatusCode_BadGateway {
		t.Errorf("expected BadGateway, got %v", ir.ImmediateResponse.Status.Code)
	}
}

func TestCrossClusterDetection_InHandleRequestHeaders(t *testing.T) {
	// Test that the cross-cluster detection in handleRequestHeaders correctly
	// strips local cluster prefix from sandbox ID.
	store := cluster.NewStore()
	store.Set([]cluster.ClusterEntry{
		{ID: "cluster-a"}, // local cluster, no gateway needed
	})

	// We can't fully test handleRequestHeaders without a real router, but we
	// can verify that SplitSandboxID logic works correctly by testing the
	// helper directly.
	cID, rawID := cluster.SplitSandboxID("cluster-a.550e8400-e29b-41d4-a716-446655440000")
	if cID != "cluster-a" {
		t.Errorf("expected cID=cluster-a, got %q", cID)
	}
	if rawID != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("expected rawID=550e8400-e29b-41d4-a716-446655440000, got %q", rawID)
	}

	// No prefix case.
	cID, rawID = cluster.SplitSandboxID("550e8400-e29b-41d4-a716-446655440000")
	if cID != "" {
		t.Errorf("expected empty cID, got %q", cID)
	}
	if rawID != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("expected rawID unchanged, got %q", rawID)
	}
}

// TestHandleCrossClusterRequest_MergedDataHeaders verifies that both common
// Headers and DataHeaders are injected, and DataHeaders wins on key conflict.
func TestHandleCrossClusterRequest_MergedDataHeaders(t *testing.T) {
	store := cluster.NewStore()
	store.Set([]cluster.ClusterEntry{
		{
			ID: "cluster-b",
			Gateway: &cluster.GatewayConfig{
				DataURL:     "https://gw.internal.example.com/clusters/cluster-b/data",
				Headers:     map[string]string{"X-GW-Auth": "common", "X-Shared": "yes"},
				DataHeaders: map[string]string{"X-GW-Auth": "data-override", "X-Data-Only": "d"},
				// NativeHeaders must NOT leak into a data-plane request.
				NativeHeaders: map[string]string{"X-Native-Only": "n"},
			},
		},
	})

	s := &Server{
		clusterStore:   store,
		localClusterID: "cluster-a",
	}

	hdrMap := map[string]string{":path": "/foo", ":authority": "orig"}
	resp := s.handleCrossClusterRequest(hdrMap, RouteTarget{SandboxID: "cluster-b.abc"}, "cluster-b")

	rh, ok := resp.Response.(*extProcPb.ProcessingResponse_RequestHeaders)
	if !ok {
		t.Fatalf("expected RequestHeaders, got %T", resp.Response)
	}
	headers := make(map[string]string)
	for _, h := range rh.RequestHeaders.Response.HeaderMutation.SetHeaders {
		headers[h.Header.Key] = string(h.Header.RawValue)
	}

	if headers["X-GW-Auth"] != "data-override" {
		t.Errorf("X-GW-Auth = %q, want data-override (per-plane must win)", headers["X-GW-Auth"])
	}
	if headers["X-Shared"] != "yes" {
		t.Errorf("X-Shared = %q, want yes", headers["X-Shared"])
	}
	if headers["X-Data-Only"] != "d" {
		t.Errorf("X-Data-Only = %q, want d", headers["X-Data-Only"])
	}
	if _, exists := headers["X-Native-Only"]; exists {
		t.Error("X-Native-Only leaked into a data-plane request")
	}
}
