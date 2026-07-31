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
	"net/url"
	"sort"
	"strings"

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

	// Endpoints returns the client-facing addresses configured for one cluster.
	// Every field is best-effort: an unconfigured gateway, a data URL that does
	// not parse, a host with no matching host alias, or a cluster with no
	// registries each leave the corresponding field empty rather than failing.
	// Callers render what they got and omit the rest.
	Endpoints(clusterID string) ClusterEndpoints
}

// ClusterEndpoints is the client-facing view of one cluster's gateway config:
// where to point an SDK, and how to shortcut the public path from inside the
// cluster. Used to render the ${AGBX_*_URL} / ${AGBX_HOST} / ${AGBX_INNER_IP} /
// ${AGBX_REGISTRY_HOST} placeholders in template documentation.
type ClusterEndpoints struct {
	// NativeURL, E2BURL, DataURL are the gateway URLs verbatim, scheme included
	// (e.g. "https://gw.example.com/agent-sandbox/api/e2b").
	NativeURL string
	E2BURL    string
	DataURL   string
	// DataDomain is DataURL without its scheme ("gw.example.com/agent-sandbox/api/data").
	// The E2B SDK's E2B_DOMAIN expects exactly this form.
	DataDomain string
	// Host is the bare hostname shared by the gateway URLs, taken from DataURL
	// ("gw.example.com"). It is what an /etc/hosts entry or a hostAliases
	// override has to name.
	Host string
	// InnerIP is the in-cluster address Host resolves to per the host-alias set,
	// empty when no alias covers it. Pointing Host at it keeps traffic inside
	// the cluster instead of going out over the public gateway.
	InnerIP string
	// HTTPS reports whether DataURL uses https, so docs can render the SDK's
	// E2B_HTTPS switch instead of hardcoding it.
	HTTPS bool
	// RegistryHost is the cluster's first configured registry host — the region
	// -local registry sandbox images are rewritten to.
	RegistryHost string
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

func (s *clusterService) Endpoints(clusterID string) ClusterEndpoints {
	var out ClusterEndpoints
	if s.store == nil || clusterID == "" {
		return out
	}
	entry, ok := s.store.Get(clusterID)
	if !ok {
		return out
	}
	if len(entry.Registries) > 0 {
		out.RegistryHost = entry.Registries[0].Host
	}
	if entry.Gateway == nil {
		return out
	}
	out.NativeURL = entry.Gateway.NativeURL
	out.E2BURL = entry.Gateway.E2BURL
	out.DataURL = entry.Gateway.DataURL

	// The data URL carries both the host clients must reach and the host+path
	// form the E2B SDK wants; derive the rest from it.
	u, err := url.Parse(out.DataURL)
	if err != nil || u.Host == "" {
		return out
	}
	out.Host = u.Hostname()
	out.HTTPS = u.Scheme == "https"
	out.DataDomain = strings.TrimPrefix(strings.TrimPrefix(out.DataURL, u.Scheme+"://"), "//")
	if ip, found := s.store.HostAliasIP(out.Host); found {
		out.InnerIP = ip
	}
	return out
}
