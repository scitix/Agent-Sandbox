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
	"github.com/prometheus/client_golang/prometheus/collectors"
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

	// WSSyncEventsTotal counts proto events emitted to Worker streams,
	// partitioned by cluster and kind (key_upsert / key_delete /
	// template_upsert / template_delete / cluster_config).
	WSSyncEventsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agentbox_wsproxy_sync_events_total",
		Help: "Total number of proto sync events emitted to Worker streams.",
	}, []string{"cluster", "kind"})

	// WSSyncEventsDroppedTotal counts events that could not be enqueued onto
	// a cluster's broadcast channel because the buffer was full. A non-zero
	// value indicates the Worker is slow to consume; investigate.
	WSSyncEventsDroppedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agentbox_wsproxy_sync_events_dropped_total",
		Help: "Total number of sync events dropped because the per-cluster broadcast buffer was full.",
	}, []string{"cluster", "kind"})

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
	metricsRegistry.MustRegister(collectors.NewGoCollector())
	metricsRegistry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	// Register wsproxy metrics.
	metricsRegistry.MustRegister(
		WSSyncConnectionsActive,
		WSSyncReconnectsTotal,
		WSSyncDisconnectsTotal,
		WSSyncEventsTotal,
		WSSyncEventsDroppedTotal,
		WSSyncPingFailuresTotal,
	)
}

// MetricsHandler returns an http.Handler for the /metrics endpoint.
func MetricsHandler() http.Handler {
	return promhttp.HandlerFor(metricsRegistry, promhttp.HandlerOpts{})
}
