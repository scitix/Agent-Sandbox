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
	"context"
	"log"
	"sort"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	syncv1 "github.com/scitix/agent-sandbox/pkg/proto/sandbox/sync/v1"
)

// The Hub is the vault's single source of truth.
//
// It has to be. A credential referenced by a sandbox is resolved on whichever
// cluster the sandbox lands on, and cross-cluster placement means that is not
// necessarily where the credential was written. Per-cluster vaults would make a
// rule work or fail depending on scheduling, which from the caller's side is
// indistinguishable from a typo.
//
// Version numbers are assigned here for the same reason: two Workers rotating
// the same entry must not both come away believing they produced version 2.

// vaultStore is the Hub's in-memory copy of every entry, keyed by
// (namespace, user, name).
//
// In memory rather than on disk because the Hub is the fan-out point, not the
// archive: a Worker holds its own Kubernetes Secret, so a Hub restart loses
// nothing that a Worker cannot re-assert. What it does mean is that the Hub
// must not answer a Watch with an authoritative empty snapshot before Workers
// have reported — see the emptySnapshot guard in WatchEntries.
type vaultStore struct {
	mu    sync.RWMutex
	items map[string]*syncv1.VaultEntry
}

func newVaultStore() *vaultStore {
	return &vaultStore{items: map[string]*syncv1.VaultEntry{}}
}

func vaultKey(namespace, user, name string) string {
	return namespace + "\x00" + user + "\x00" + name
}

// put stores an entry, assigning the next version, and returns the stored copy.
func (s *vaultStore) put(in *syncv1.VaultEntry) *syncv1.VaultEntry {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := vaultKey(in.Namespace, in.User, in.Name)
	stored := proto.Clone(in).(*syncv1.VaultEntry)
	if prev, ok := s.items[key]; ok {
		stored.Version = prev.Version + 1
		if stored.CreatedAt == nil {
			stored.CreatedAt = prev.CreatedAt
		}
	} else if stored.Version <= 0 {
		stored.Version = 1
	}
	s.items[key] = stored
	return proto.Clone(stored).(*syncv1.VaultEntry)
}

func (s *vaultStore) delete(namespace, user, name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := vaultKey(namespace, user, name)
	_, ok := s.items[key]
	delete(s.items, key)
	return ok
}

func (s *vaultStore) snapshot() []*syncv1.VaultEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*syncv1.VaultEntry, 0, len(s.items))
	for _, e := range s.items {
		out = append(out, proto.Clone(e).(*syncv1.VaultEntry))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		if out[i].User != out[j].User {
			return out[i].User < out[j].User
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (s *vaultStore) empty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items) == 0
}

// vaultServer implements the Hub side of VaultService for one connected Worker.
type vaultServer struct {
	syncv1.UnimplementedVaultServiceServer
	m  *SyncManager
	sc *clusterSyncConn
}

func newVaultServer(m *SyncManager, sc *clusterSyncConn) *vaultServer {
	return &vaultServer{m: m, sc: sc}
}

func (s *vaultServer) PutEntry(_ context.Context, req *syncv1.PutVaultEntryRequest) (*syncv1.PutVaultEntryResponse, error) {
	e := req.GetEntry()
	if e == nil || e.Namespace == "" || e.User == "" || e.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace, user and name are required")
	}
	if e.Value == "" {
		return nil, status.Error(codes.InvalidArgument, "value is required")
	}
	if e.CreatedAt == nil {
		e.CreatedAt = timestamppb.Now()
	}
	e.UpdatedAt = timestamppb.Now()

	stored := s.m.vault.put(e)
	s.m.broadcastVaultUpsert(stored)
	return &syncv1.PutVaultEntryResponse{Entry: stored}, nil
}

func (s *vaultServer) DeleteEntry(_ context.Context, req *syncv1.DeleteVaultEntryRequest) (*syncv1.DeleteVaultEntryResponse, error) {
	if req.Namespace == "" || req.User == "" || req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace, user and name are required")
	}
	// Broadcast regardless of whether the Hub knew the entry: a Worker may hold
	// one the Hub lost across a restart, and the delete has to reach it.
	s.m.vault.delete(req.Namespace, req.User, req.Name)
	s.m.broadcastVaultDelete(req.Namespace, req.User, req.Name)
	return &syncv1.DeleteVaultEntryResponse{}, nil
}

func (s *vaultServer) WatchEntries(_ *syncv1.WatchVaultRequest, stream syncv1.VaultService_WatchEntriesServer) error {
	// An empty store is ambiguous: it is either a genuinely empty vault or a
	// Hub that has just restarted and not yet been told anything. Sending it as
	// an authoritative snapshot would make every Worker delete its own
	// credentials. Send nothing and let the first real event start the stream.
	if !s.m.vault.empty() {
		snap := &syncv1.VaultEvent{Kind: &syncv1.VaultEvent_Snapshot{
			Snapshot: &syncv1.VaultSnapshot{Items: s.m.vault.snapshot()},
		}}
		if err := stream.Send(snap); err != nil {
			return err
		}
	}

	for {
		select {
		case <-s.sc.done:
			return nil
		case <-stream.Context().Done():
			return stream.Context().Err()
		case ev := <-s.sc.vaultCh:
			if err := stream.Send(ev); err != nil {
				log.Printf("syncmgr/grpc: vault stream send to %s failed: %v", s.sc.clusterID, err)
				return err
			}
		}
	}
}
