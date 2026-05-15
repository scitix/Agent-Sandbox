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
	"testing"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
)

// ---------------------------------------------------------------------------
// sandboxToGen tests — endpoints conversion
// ---------------------------------------------------------------------------

func TestSandboxToGen_EndpointsWithLogDir(t *testing.T) {
	logDir := "/tmp/envd.log"
	sb := &domain.Sandbox{
		SandboxID: "sb-123",
		Namespace: "default",
		PoolName:  "test-pool",
		PodName:   "pod-abc",
		Status:    "Running",
		ClaimedAt: "2026-01-01T00:00:00Z",
		Endpoints: map[string]domain.SandboxEndpoint{
			"envd": {URL: "http://gw/sandboxes/sb-123/49983", LogDir: logDir},
		},
	}

	result := sandboxToGen(sb)

	if result.Endpoints == nil {
		t.Fatal("Endpoints is nil")
	}
	ep, ok := (*result.Endpoints)["envd"]
	if !ok {
		t.Fatal("envd endpoint not found")
	}
	if ep.Url != "http://gw/sandboxes/sb-123/49983" {
		t.Errorf("URL: want http://gw/sandboxes/sb-123/49983, got %s", ep.Url)
	}
	if ep.LogDir == nil || *ep.LogDir != "/tmp/envd.log" {
		t.Errorf("LogDir: want /tmp/envd.log, got %v", ep.LogDir)
	}
}

func TestSandboxToGen_EndpointsWithoutLogDir(t *testing.T) {
	sb := &domain.Sandbox{
		SandboxID: "sb-456",
		Namespace: "default",
		PoolName:  "test-pool",
		PodName:   "pod-def",
		Status:    "Running",
		ClaimedAt: "2026-01-01T00:00:00Z",
		Endpoints: map[string]domain.SandboxEndpoint{
			"swerex": {URL: "http://gw/sandboxes/sb-456/8080"},
		},
	}

	result := sandboxToGen(sb)

	if result.Endpoints == nil {
		t.Fatal("Endpoints is nil")
	}
	ep, ok := (*result.Endpoints)["swerex"]
	if !ok {
		t.Fatal("swerex endpoint not found")
	}
	if ep.Url != "http://gw/sandboxes/sb-456/8080" {
		t.Errorf("URL: want http://gw/sandboxes/sb-456/8080, got %s", ep.Url)
	}
	if ep.LogDir != nil {
		t.Errorf("LogDir: want nil for empty logDir, got %v", *ep.LogDir)
	}
}

func TestSandboxToGen_NoEndpoints(t *testing.T) {
	sb := &domain.Sandbox{
		SandboxID: "sb-789",
		Namespace: "default",
		PoolName:  "test-pool",
		PodName:   "pod-ghi",
		Status:    "Starting",
		ClaimedAt: "2026-01-01T00:00:00Z",
	}

	result := sandboxToGen(sb)
	if result.Endpoints != nil {
		t.Errorf("Endpoints should be nil when no endpoints configured, got %v", result.Endpoints)
	}
}
