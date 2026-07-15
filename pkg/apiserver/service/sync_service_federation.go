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
	"errors"
	"io"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/scitix/agent-sandbox/pkg/apiserver/service/federation"
	syncv1 "github.com/scitix/agent-sandbox/pkg/proto/sandbox/sync/v1"
)

const defaultFederationReportInterval = 5 * time.Second

// SetFederation enables cross-cluster capacity federation on this sync service.
// registry is the local soft-state store consumed by the router; source
// produces this cluster's capacity to advertise. Both must be non-nil for the
// report/watch goroutines to start on connect. Call before the first connect.
func (s *syncServiceImpl) SetFederation(registry *federation.Registry, source federation.CapacitySource, localClusterID string, reportInterval time.Duration) {
	if reportInterval <= 0 {
		reportInterval = defaultFederationReportInterval
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fedRegistry = registry
	s.fedSource = source
	s.localClusterID = localClusterID
	s.fedReportInterval = reportInterval
}

// runWatchFederation consumes the Hub's FederationBroadcast stream, folding
// every batch into the local registry and refreshing the observability
// metrics. A cluster without a federation-aware Hub returns Unimplemented; the
// goroutine logs once and exits without retrying on this connection.
func (s *syncServiceImpl) runWatchFederation(ctx context.Context, connID uint64) {
	s.mu.RLock()
	fc, reg := s.fedClient, s.fedRegistry
	s.mu.RUnlock()
	if fc == nil || reg == nil {
		return
	}
	stream, err := fc.WatchFederation(ctx, &syncv1.WatchFederationRequest{})
	if err != nil {
		s.log.Error(err, "WatchFederation subscribe failed", "connID", connID)
		return
	}
	for {
		ev, err := stream.Recv()
		if err != nil {
			if status.Code(err) == codes.Unimplemented {
				s.log.Info("WatchFederation not implemented by Hub; cross-cluster federation disabled for this connection", "connID", connID)
				return
			}
			if !errors.Is(err, io.EOF) && status.Code(err) != codes.Canceled {
				s.log.Error(err, "WatchFederation recv error", "connID", connID)
			}
			return
		}
		s.applyFederationBroadcast(reg, ev)
	}
}

func (s *syncServiceImpl) applyFederationBroadcast(reg *federation.Registry, ev *syncv1.FederationBroadcast) {
	if ev == nil || len(ev.Items) == 0 {
		return
	}
	now := time.Now()
	batch := make([]federation.Capacity, 0, len(ev.Items))
	for _, it := range ev.Items {
		batch = append(batch, federation.Capacity{
			ClusterID:    it.ClusterId,
			Namespace:    it.Namespace,
			EnvName:      it.EnvName,
			MemberPool:   it.MemberPool,
			ScalingGroup: it.ScalingGroup,
			Idle:         it.Idle,
			Running:      it.Running,
			Pending:      it.Pending,
			Desired:      it.Desired,
			Capacity:     it.Capacity,
			SaturatedFor: time.Duration(it.SaturatedFor) * time.Second,
			ObservedAt:   now.Add(-time.Duration(it.ObservedForMs) * time.Millisecond),
		})
	}
	reg.Upsert(batch)
	federation.PublishMetrics(reg.Snapshot())
	s.log.Info("federation: applied capacity batch", "items", len(batch))
}

// runReportFederation advertises this cluster's capacity to the Hub on a ticker
// (full resync each tick; the Hub keeps the latest value per key, so a dropped
// batch self-heals on the next tick).
func (s *syncServiceImpl) runReportFederation(ctx context.Context, connID uint64) {
	s.mu.RLock()
	fc, src := s.fedClient, s.fedSource
	interval := s.fedReportInterval
	s.mu.RUnlock()
	if fc == nil || src == nil {
		return
	}
	if interval <= 0 {
		interval = defaultFederationReportInterval
	}
	stream, err := fc.ReportFederation(ctx)
	if err != nil {
		if status.Code(err) != codes.Unimplemented {
			s.log.Error(err, "ReportFederation open failed", "connID", connID)
		}
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_, _ = stream.CloseAndRecv()
			return
		case <-ticker.C:
			caps, err := src.Collect(ctx)
			if err != nil {
				s.log.Error(err, "ReportFederation collect failed", "connID", connID)
				continue
			}
			if err := stream.Send(toFederationProto(caps)); err != nil {
				if status.Code(err) != codes.Canceled {
					s.log.Error(err, "ReportFederation send failed", "connID", connID)
				}
				return
			}
		}
	}
}

func toFederationProto(caps []federation.Capacity) *syncv1.ReportFederationRequest {
	now := time.Now()
	items := make([]*syncv1.EnvCapacity, 0, len(caps))
	for _, c := range caps {
		ageMs := max(now.Sub(c.ObservedAt).Milliseconds(), 0)
		items = append(items, &syncv1.EnvCapacity{
			ClusterId:     c.ClusterID,
			Namespace:     c.Namespace,
			EnvName:       c.EnvName,
			MemberPool:    c.MemberPool,
			ScalingGroup:  c.ScalingGroup,
			Idle:          c.Idle,
			Running:       c.Running,
			Pending:       c.Pending,
			Desired:       c.Desired,
			Capacity:      c.Capacity,
			SaturatedFor:  int32(c.SaturatedFor.Seconds()),
			ObservedForMs: ageMs,
		})
	}
	return &syncv1.ReportFederationRequest{Items: items}
}
