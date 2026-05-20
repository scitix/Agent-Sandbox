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

	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
)

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

// ExportClusterConfigSnapshot is a test helper that returns the current
// snapshot as a json.RawMessage. Exported so unit tests can verify the
// snapshot composition without standing up a WebSocket session.
func (m *SyncManager) ExportClusterConfigSnapshot() json.RawMessage {
	raw, _ := json.Marshal(m.currentSnapshot())
	return raw
}
