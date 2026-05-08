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

package syncmgr

import (
	"encoding/json"
	"log"

	"github.com/scitix/agent-sandbox/pkg/api/protocol"
	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
)

// sendClusterConfigSnapshot serialises the full cluster config (clusters +
// host aliases) and sends it as a cluster_config_snapshot frame to the given
// connection. Called once per new WS connection so Workers pick up the full
// state without waiting for the next periodic broadcast.
func (m *SyncManager) sendClusterConfigSnapshot(sc *clusterSyncConn) error {
	snapshot := m.currentSnapshot()
	if isEmptySnapshot(snapshot) {
		return nil
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	return sc.send(protocol.Frame{
		Type:           protocol.FrameClusterConfigSnapshot,
		ConfigSnapshot: raw,
	})
}

// BroadcastClusterConfig serialises the full cluster config and broadcasts
// it as a cluster_config_sync frame to every connected Worker. Call this
// after reloading the Manager's cluster config file so Workers pick up
// changes (new Gateway fields, host-alias updates, ...) without restarting.
// A no-op when the snapshot is empty (does not clear Worker config).
func (m *SyncManager) BroadcastClusterConfig() {
	snapshot := m.currentSnapshot()
	if isEmptySnapshot(snapshot) {
		log.Printf("syncManager: BroadcastClusterConfig: snapshot is empty, skipping broadcast")
		return
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		log.Printf("syncManager: BroadcastClusterConfig: marshal error: %v", err)
		return
	}
	m.broadcast(protocol.Frame{
		Type:           protocol.FrameClusterConfigSync,
		ConfigSnapshot: raw,
	})
	log.Printf("syncManager: broadcast cluster_config_sync to all workers (clusters=%d, hostAliases=%d)",
		len(snapshot.Clusters), len(snapshot.HostAliases))
}

// ExportClusterConfigSnapshot is a test helper that returns the current
// serialised snapshot as a json.RawMessage. Exported so tests in the _test
// package can verify the encoding without a live WebSocket.
func (m *SyncManager) ExportClusterConfigSnapshot() json.RawMessage {
	raw, _ := json.Marshal(m.currentSnapshot())
	return raw
}

// currentSnapshot assembles the wire-level ClusterConfig from the in-memory
// Store. Returns the zero value when nothing is configured.
func (m *SyncManager) currentSnapshot() cluster.ClusterConfig {
	return cluster.ClusterConfig{
		Clusters:    m.clusters.All(),
		HostAliases: m.clusters.HostAliases(),
	}
}

func isEmptySnapshot(cfg cluster.ClusterConfig) bool {
	return len(cfg.Clusters) == 0 && len(cfg.HostAliases) == 0
}
