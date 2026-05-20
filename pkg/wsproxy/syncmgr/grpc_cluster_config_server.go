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
	syncv1 "github.com/scitix/agent-sandbox/pkg/proto/sandbox/sync/v1"
)

// clusterConfigServer streams the current ClusterConfig snapshot and every
// subsequent change to one connected Worker. ClusterConfig is broadcast by
// the Manager when clusters.yaml reloads (see BroadcastClusterConfig).
type clusterConfigServer struct {
	syncv1.UnimplementedClusterConfigServiceServer
	m  *SyncManager
	sc *clusterSyncConn
}

func newClusterConfigServer(m *SyncManager, sc *clusterSyncConn) *clusterConfigServer {
	return &clusterConfigServer{m: m, sc: sc}
}

func (s *clusterConfigServer) WatchClusterConfig(_ *syncv1.WatchClusterConfigRequest, stream syncv1.ClusterConfigService_WatchClusterConfigServer) error {
	ctx := stream.Context()

	// Initial snapshot. Worker treats empty snapshot as no-op (does not erase
	// existing ConfigMap), matching the v1 semantics in protocol/sync.go.
	snap := s.m.currentSnapshot()
	if !isEmptySnapshot(snap) {
		if err := stream.Send(&syncv1.ClusterConfigEvent{Snapshot: clusterConfigToProto(snap)}); err != nil {
			return err
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-s.sc.cfgCh:
			if !ok {
				return nil
			}
			if err := stream.Send(ev); err != nil {
				return err
			}
		}
	}
}
