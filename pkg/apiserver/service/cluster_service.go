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
	"sort"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
)

// ClusterService exposes the routing-visible cluster list to API consumers.
// It wraps a cluster.Store and tags the local cluster so SDK/CLI callers can
// distinguish the native home of a request from remote worker clusters.
type ClusterService interface {
	// List returns the cluster catalog sorted by ID. When store or localID are
	// empty the result is a single-entry list containing the local cluster, or an
	// empty slice when neither is configured. The list is a defensive copy; the
	// caller may mutate it freely.
	List(ctx context.Context) ([]gen.ClusterSummary, *domain.AppError)
}

type clusterService struct {
	store          *cluster.Store
	localClusterID string
}

// NewClusterService returns a ClusterService backed by the given store and
// local cluster ID. store may be nil (common in single-cluster deployments);
// in that case List reports only the local cluster if localID is set.
func NewClusterService(store *cluster.Store, localClusterID string) ClusterService {
	return &clusterService{store: store, localClusterID: localClusterID}
}

func (s *clusterService) List(_ context.Context) ([]gen.ClusterSummary, *domain.AppError) {
	var entries []cluster.ClusterEntry
	if s.store != nil {
		entries = s.store.All()
	}

	byID := make(map[string]gen.ClusterSummary, len(entries)+1)
	for _, e := range entries {
		entry := gen.ClusterSummary{
			Id:    e.ID,
			Local: e.ID == s.localClusterID,
		}
		if e.Name != "" {
			n := e.Name
			entry.Name = &n
		}
		byID[e.ID] = entry
	}

	// Always surface the local cluster, even if the config-map-backed store
	// does not list it (e.g. single-cluster deployments, or the local entry
	// was never written to the shared ConfigMap).
	if s.localClusterID != "" {
		if existing, ok := byID[s.localClusterID]; ok {
			existing.Local = true
			byID[s.localClusterID] = existing
		} else {
			n := s.localClusterID
			byID[s.localClusterID] = gen.ClusterSummary{
				Id:    s.localClusterID,
				Name:  &n,
				Local: true,
			}
		}
	}

	out := make([]gen.ClusterSummary, 0, len(byID))
	for _, v := range byID {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Id < out[j].Id })
	return out, nil
}
