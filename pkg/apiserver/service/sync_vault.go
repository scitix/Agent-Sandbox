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
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	syncv1 "github.com/scitix/agent-sandbox/pkg/proto/sandbox/sync/v1"
)

// Worker side of vault replication.
//
// Writes are forwarded to the Hub and come back on the watch stream, so a
// cluster never has a locally-invented version of an entry: the Hub assigns the
// version and every cluster, including the one that made the request, learns
// the result the same way.

// VaultSink applies replicated vault entries to local storage.
type VaultSink interface {
	ApplyVaultEntry(ctx context.Context, e VaultReplicatedEntry) error
	DeleteVaultEntry(ctx context.Context, namespace, user, name string) error
	// ReconcileVault drops local entries absent from an authoritative snapshot.
	// Without it, a delete that happened while this cluster was disconnected
	// would survive forever here.
	ReconcileVault(ctx context.Context, keep []VaultReplicatedEntry) error
}

// VaultReplicatedEntry is one entry as it travels between clusters.
type VaultReplicatedEntry struct {
	Namespace string
	User      string
	Name      string
	Value     string
	Version   int64
	Metadata  map[string]string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SetVaultSink wires local vault storage into the sync loop.
func (s *syncServiceImpl) SetVaultSink(sink VaultSink) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vaultSink = sink
}

// RequestVaultPut forwards a create/rotate to the Hub and returns the stored
// entry, version included.
func (s *syncServiceImpl) RequestVaultPut(ctx context.Context, e VaultReplicatedEntry) (*VaultReplicatedEntry, error) {
	s.mu.RLock()
	vc, conn := s.vaultClient, s.conn
	s.mu.RUnlock()
	if conn == nil || vc == nil {
		return nil, ErrSyncNotConnected
	}

	resp, err := vc.PutEntry(ctx, &syncv1.PutVaultEntryRequest{Entry: vaultEntryToProto(e)})
	if err != nil {
		return nil, err
	}
	out := vaultEntryFromProto(resp.GetEntry())
	return &out, nil
}

// RequestVaultDelete forwards a delete to the Hub.
func (s *syncServiceImpl) RequestVaultDelete(ctx context.Context, namespace, user, name string) error {
	s.mu.RLock()
	vc, conn := s.vaultClient, s.conn
	s.mu.RUnlock()
	if conn == nil || vc == nil {
		return ErrSyncNotConnected
	}
	_, err := vc.DeleteEntry(ctx, &syncv1.DeleteVaultEntryRequest{
		Namespace: namespace, User: user, Name: name,
	})
	return err
}

func (s *syncServiceImpl) runWatchVault(ctx context.Context, connID uint64) {
	s.mu.RLock()
	vc := s.vaultClient
	s.mu.RUnlock()
	if vc == nil {
		return
	}
	stream, err := vc.WatchEntries(ctx, &syncv1.WatchVaultRequest{})
	if err != nil {
		s.log.Error(err, "WatchEntries subscribe failed", "connID", connID)
		return
	}
	for {
		ev, err := stream.Recv()
		if err != nil {
			if !errors.Is(err, io.EOF) && status.Code(err) != codes.Canceled {
				s.log.Error(err, "WatchEntries recv error", "connID", connID)
			}
			return
		}
		s.dispatchVaultEvent(ctx, ev)
	}
}

func (s *syncServiceImpl) dispatchVaultEvent(ctx context.Context, ev *syncv1.VaultEvent) {
	s.mu.RLock()
	sink := s.vaultSink
	s.mu.RUnlock()
	if sink == nil {
		return
	}

	switch k := ev.Kind.(type) {
	case *syncv1.VaultEvent_Snapshot:
		items := make([]VaultReplicatedEntry, 0, len(k.Snapshot.Items))
		for _, item := range k.Snapshot.Items {
			e := vaultEntryFromProto(item)
			items = append(items, e)
			if err := sink.ApplyVaultEntry(ctx, e); err != nil {
				s.log.Error(err, "vault snapshot apply error", "namespace", e.Namespace, "name", e.Name)
			}
		}
		// Applied first, then reconciled: an entry that is both in the snapshot
		// and locally present must never spend a moment deleted.
		if err := sink.ReconcileVault(ctx, items); err != nil {
			s.log.Error(err, "vault snapshot reconcile error")
		}
	case *syncv1.VaultEvent_Upsert:
		e := vaultEntryFromProto(k.Upsert)
		if err := sink.ApplyVaultEntry(ctx, e); err != nil {
			s.log.Error(err, "vault upsert apply error", "namespace", e.Namespace, "name", e.Name)
		}
	case *syncv1.VaultEvent_Delete:
		d := k.Delete
		if err := sink.DeleteVaultEntry(ctx, d.Namespace, d.User, d.Name); err != nil {
			s.log.Error(err, "vault delete apply error", "namespace", d.Namespace, "name", d.Name)
		}
	}
}

func vaultEntryToProto(e VaultReplicatedEntry) *syncv1.VaultEntry {
	md := ""
	if len(e.Metadata) > 0 {
		if raw, err := json.Marshal(e.Metadata); err == nil {
			md = string(raw)
		}
	}
	out := &syncv1.VaultEntry{
		Namespace:    e.Namespace,
		User:         e.User,
		Name:         e.Name,
		Value:        e.Value,
		Version:      e.Version,
		MetadataJson: md,
	}
	if !e.CreatedAt.IsZero() {
		out.CreatedAt = timestamppb.New(e.CreatedAt)
	}
	if !e.UpdatedAt.IsZero() {
		out.UpdatedAt = timestamppb.New(e.UpdatedAt)
	}
	return out
}

func vaultEntryFromProto(in *syncv1.VaultEntry) VaultReplicatedEntry {
	if in == nil {
		return VaultReplicatedEntry{}
	}
	out := VaultReplicatedEntry{
		Namespace: in.Namespace,
		User:      in.User,
		Name:      in.Name,
		Value:     in.Value,
		Version:   in.Version,
	}
	if in.MetadataJson != "" {
		md := map[string]string{}
		if err := json.Unmarshal([]byte(in.MetadataJson), &md); err == nil {
			out.Metadata = md
		}
	}
	if in.CreatedAt != nil {
		out.CreatedAt = in.CreatedAt.AsTime()
	}
	if in.UpdatedAt != nil {
		out.UpdatedAt = in.UpdatedAt.AsTime()
	}
	return out
}
