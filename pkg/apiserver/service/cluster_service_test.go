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

package service

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
)

// storeWithDemo mirrors the shape of a real agentbox-clusters-config entry:
// gateway URLs behind a public host, one region-local registry, and a host alias
// pinning that host to an in-cluster IP.
func storeWithDemo() *cluster.Store {
	s := cluster.NewStore()
	s.ApplyConfig(cluster.ClusterConfig{
		Clusters: []cluster.ClusterEntry{{
			ID:   "demo",
			Name: "Demo",
			Gateway: &cluster.GatewayConfig{
				NativeURL: "https://gw.example.com/agent-sandbox/api/native",
				E2BURL:    "https://gw.example.com/agent-sandbox/api/e2b",
				DataURL:   "https://gw.example.com/agent-sandbox/api/data",
			},
			Registries: []cluster.RegistryEntry{
				{Host: "registry.example.com"},
				{Host: "registry-gar.example.com", Type: "gar"},
			},
		}},
		HostAliases: []corev1.HostAlias{
			{IP: "10.0.0.1", Hostnames: []string{"gw.example.com"}},
		},
	})
	return s
}

func TestClusterServiceEndpoints(t *testing.T) {
	got := NewClusterService(storeWithDemo(), "demo").Endpoints("demo")

	want := ClusterEndpoints{
		NativeURL:    "https://gw.example.com/agent-sandbox/api/native",
		E2BURL:       "https://gw.example.com/agent-sandbox/api/e2b",
		DataURL:      "https://gw.example.com/agent-sandbox/api/data",
		DataDomain:   "gw.example.com/agent-sandbox/api/data",
		Host:         "gw.example.com",
		InnerIP:      "10.0.0.1",
		HTTPS:        true,
		RegistryHost: "registry.example.com",
	}
	if got != want {
		t.Errorf("Endpoints() = %+v\nwant %+v", got, want)
	}
}

func TestClusterServiceEndpoints_MissingPieces(t *testing.T) {
	// No gateway: only the registry is knowable.
	noGateway := cluster.NewStore()
	noGateway.Set([]cluster.ClusterEntry{{
		ID:         "bare",
		Registries: []cluster.RegistryEntry{{Host: "registry-local.example.com"}},
	}})
	if got := NewClusterService(noGateway, "bare").Endpoints("bare"); got.RegistryHost != "registry-local.example.com" ||
		got.DataURL != "" || got.Host != "" || got.InnerIP != "" {
		t.Errorf("no-gateway Endpoints() = %+v", got)
	}

	// Gateway without a host alias: everything but InnerIP resolves, over plain HTTP.
	noAlias := cluster.NewStore()
	noAlias.Set([]cluster.ClusterEntry{{
		ID:      "plain",
		Gateway: &cluster.GatewayConfig{DataURL: "http://gw.internal:8080/api/data"},
	}})
	got := NewClusterService(noAlias, "plain").Endpoints("plain")
	if got.Host != "gw.internal" || got.DataDomain != "gw.internal:8080/api/data" || got.HTTPS || got.InnerIP != "" {
		t.Errorf("no-alias Endpoints() = %+v", got)
	}

	// Unknown cluster / nil store / empty ID are all empty, never a panic.
	for name, svc := range map[string]ClusterService{
		"nil store":   NewClusterService(nil, "demo"),
		"known store": NewClusterService(storeWithDemo(), "demo"),
	} {
		t.Run(name, func(t *testing.T) {
			if got := svc.Endpoints("nope"); got != (ClusterEndpoints{}) {
				t.Errorf("Endpoints(unknown) = %+v, want zero value", got)
			}
			if got := svc.Endpoints(""); got != (ClusterEndpoints{}) {
				t.Errorf("Endpoints(\"\") = %+v, want zero value", got)
			}
		})
	}
}
