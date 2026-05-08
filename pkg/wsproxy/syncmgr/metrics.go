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

// Package syncmgr implements the WSProxy sync manager that maintains persistent
// WebSocket connections to every Worker cluster and pushes API key, SandboxTemplate,
// and ClusterConfig updates.
package syncmgr

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ── Prometheus metrics ────────────────────────────────────────────────────────

var (
	// WSSyncConnectionsActive tracks the number of currently active sync
	// WebSocket connections (one per Worker cluster).
	WSSyncConnectionsActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "agentbox_wsproxy_sync_connections_active",
		Help: "Number of active WebSocket sync connections to Worker clusters.",
	})

	// WSSyncReconnectsTotal counts the total number of successful (re)connections
	// to Worker clusters, partitioned by cluster ID.
	WSSyncReconnectsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agentbox_wsproxy_sync_reconnects_total",
		Help: "Total number of sync WebSocket (re)connections to Worker clusters.",
	}, []string{"cluster"})

	// WSSyncDisconnectsTotal counts the total number of disconnections,
	// partitioned by cluster ID.
	WSSyncDisconnectsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agentbox_wsproxy_sync_disconnects_total",
		Help: "Total number of sync WebSocket disconnections from Worker clusters.",
	}, []string{"cluster"})

	// WSSyncFramesTotal counts sync protocol frames sent/received,
	// partitioned by cluster and direction (tx/rx).
	WSSyncFramesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agentbox_wsproxy_sync_frames_total",
		Help: "Total number of sync protocol frames sent (tx) or received (rx).",
	}, []string{"cluster", "direction"})

	// WSSyncPingFailuresTotal counts Ping write failures (connection likely dead).
	WSSyncPingFailuresTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agentbox_wsproxy_sync_ping_failures_total",
		Help: "Total number of WebSocket Ping failures to Worker clusters.",
	}, []string{"cluster"})
)

// metricsRegistry is a dedicated Prometheus registry for wsproxy.
// Using a dedicated registry avoids polluting the default registry
// and gives clean /metrics output.
var metricsRegistry = prometheus.NewRegistry()

func init() {
	// Register Go runtime and process collectors.
	metricsRegistry.MustRegister(prometheus.NewGoCollector())
	metricsRegistry.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))

	// Register wsproxy metrics.
	metricsRegistry.MustRegister(
		WSSyncConnectionsActive,
		WSSyncReconnectsTotal,
		WSSyncDisconnectsTotal,
		WSSyncFramesTotal,
		WSSyncPingFailuresTotal,
	)
}

// MetricsHandler returns an http.Handler for the /metrics endpoint.
func MetricsHandler() http.Handler {
	return promhttp.HandlerFor(metricsRegistry, promhttp.HandlerOpts{})
}
