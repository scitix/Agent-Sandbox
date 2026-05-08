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

package syncmgr_test

import (
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
	"github.com/scitix/agent-sandbox/pkg/wsproxy/syncmgr"
)

func TestClusterConfigSnapshot_RoundTrip(t *testing.T) {
	entries := []cluster.ClusterEntry{
		{
			ID:   "cluster-a",
			Name: "Cluster A",
			URL:  "https://cluster-a.example.com",
			Gateway: &cluster.GatewayConfig{
				NativeURL: "https://native.cluster-a.internal",
				DataURL:   "https://data.cluster-a.internal",
				Headers:   map[string]string{"X-Auth": "token"},
			},
		},
		{
			ID:   "cluster-b",
			Name: "Cluster B",
			URL:  "https://cluster-b.example.com",
		},
	}

	store := cluster.NewStore()
	store.ApplyConfig(cluster.ClusterConfig{
		Clusters: entries,
		HostAliases: []corev1.HostAlias{
			{IP: "10.0.0.1", Hostnames: []string{"cluster-a.example.com"}},
		},
	})

	sm := syncmgr.New(store, "sync-token", "manager-token", syncmgr.Deps{})

	raw := sm.ExportClusterConfigSnapshot()
	if len(raw) == 0 {
		t.Fatal("expected non-empty snapshot")
	}

	var cfg cluster.ClusterConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if len(cfg.Clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(cfg.Clusters))
	}
	if len(cfg.HostAliases) != 1 || cfg.HostAliases[0].IP != "10.0.0.1" {
		t.Errorf("unexpected hostAliases: %+v", cfg.HostAliases)
	}
}

func TestBroadcastClusterConfig_EmptyStoreIsNoOp(t *testing.T) {
	store := cluster.NewStore() // empty
	sm := syncmgr.New(store, "token", "mgr", syncmgr.Deps{})

	// Should not panic or broadcast anything (no connections anyway).
	sm.BroadcastClusterConfig()
}

func TestBroadcastClusterConfig_WithGateway(t *testing.T) {
	entries := []cluster.ClusterEntry{
		{
			ID:  "cluster-a",
			URL: "https://a.example.com",
			Gateway: &cluster.GatewayConfig{
				NativeURL: "https://native.a.example.com",
				DataURL:   "https://data.a.example.com",
			},
		},
	}

	store := cluster.NewStore()
	store.ApplyConfig(cluster.ClusterConfig{Clusters: entries})

	sm := syncmgr.New(store, "token", "mgr", syncmgr.Deps{})

	// BroadcastClusterConfig with no connections should not error.
	sm.BroadcastClusterConfig()

	raw := sm.ExportClusterConfigSnapshot()
	var cfg cluster.ClusterConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(cfg.Clusters) != 1 {
		t.Fatalf("want 1 cluster, got %d", len(cfg.Clusters))
	}
	if cfg.Clusters[0].Gateway == nil {
		t.Fatal("gateway is nil after round-trip")
	}
	if cfg.Clusters[0].Gateway.NativeURL != "https://native.a.example.com" {
		t.Errorf("NativeURL = %q", cfg.Clusters[0].Gateway.NativeURL)
	}
}
