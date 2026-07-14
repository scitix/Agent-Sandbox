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
	"io"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	syncv1 "github.com/scitix/agent-sandbox/pkg/proto/sandbox/sync/v1"
)

// federationTTL bounds how long a cluster's last-reported capacity is served to
// new subscribers. A cluster that stops reporting ages out here and on every
// Worker's own registry, so a silent cluster never keeps advertising capacity.
const federationTTL = 60 * time.Second

// federationStore is the Hub's soft-state view of every cluster's per-Env
// capacity. It is a relay cache: Workers push their local capacity, the Hub
// stores the latest value per key and replays a filtered snapshot to any newly
// connecting Worker. All values carry an absolute receive time used only for
// TTL expiry; the wire format keeps freshness relative.
type federationStore struct {
	mu    sync.RWMutex
	items map[string]fedEntry
	now   func() time.Time
}

type fedEntry struct {
	cap        *syncv1.EnvCapacity
	receivedAt time.Time
}

func newFederationStore() *federationStore {
	return &federationStore{
		items: make(map[string]fedEntry),
		now:   time.Now,
	}
}

func fedKey(clusterID, namespace, env, group string) string {
	return strings.Join([]string{clusterID, namespace, env, group}, "\x00")
}

// upsert records one cluster's capacity batch. The caller has already stamped
// each item's ClusterID with the connection's authenticated cluster ID.
func (s *federationStore) upsert(items []*syncv1.EnvCapacity) {
	if len(items) == 0 {
		return
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range items {
		s.items[fedKey(it.ClusterId, it.Namespace, it.EnvName, it.ScalingGroup)] = fedEntry{cap: it, receivedAt: now}
	}
}

// snapshot returns every non-expired capacity record, refreshing each item's
// observed_for_ms to the age measured at the Hub so a freshly connected Worker
// starts its own TTL from an accurate baseline.
func (s *federationStore) snapshot() []*syncv1.EnvCapacity {
	now := s.now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*syncv1.EnvCapacity, 0, len(s.items))
	for _, e := range s.items {
		if now.Sub(e.receivedAt) > federationTTL {
			continue
		}
		cp := proto.Clone(e.cap).(*syncv1.EnvCapacity)
		cp.ObservedForMs = now.Sub(e.receivedAt).Milliseconds()
		out = append(out, cp)
	}
	return out
}

// purgeCluster drops every record belonging to a cluster. Called when that
// cluster's sync connection closes so its capacity stops being advertised to
// new subscribers immediately rather than after the TTL.
func (s *federationStore) purgeCluster(clusterID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, e := range s.items {
		if e.cap.ClusterId == clusterID {
			delete(s.items, k)
		}
	}
}

// federationServer implements the Hub side of FederationService for one
// connected Worker.
type federationServer struct {
	syncv1.UnimplementedFederationServiceServer
	m  *SyncManager
	sc *clusterSyncConn
}

func newFederationServer(m *SyncManager, sc *clusterSyncConn) *federationServer {
	return &federationServer{m: m, sc: sc}
}

// ReportFederation consumes a Worker's capacity stream. Each batch is stamped
// with the connection's cluster ID (never trusting the client-supplied field),
// merged into the store, and fanned out to every connected Worker.
func (s *federationServer) ReportFederation(stream syncv1.FederationService_ReportFederationServer) error {
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&syncv1.ReportFederationResponse{})
		}
		if err != nil {
			return err
		}
		if len(req.Items) == 0 {
			continue
		}
		for _, it := range req.Items {
			it.ClusterId = s.sc.clusterID
		}
		s.m.fed.upsert(req.Items)
		s.m.broadcastFederation(req.Items)
	}
}

// WatchFederation sends a full snapshot on connect and then every relayed
// capacity batch. The Worker keeps one stream open for the connection's life.
func (s *federationServer) WatchFederation(_ *syncv1.WatchFederationRequest, stream syncv1.FederationService_WatchFederationServer) error {
	ctx := stream.Context()

	if snap := s.m.fed.snapshot(); len(snap) > 0 {
		if err := stream.Send(&syncv1.FederationBroadcast{Items: snap}); err != nil {
			return err
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-s.sc.fedCh:
			if !ok {
				return nil
			}
			if err := stream.Send(ev); err != nil {
				return err
			}
		}
	}
}
