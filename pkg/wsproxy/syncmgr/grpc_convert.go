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
	"maps"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	syncv1 "github.com/scitix/agent-sandbox/pkg/proto/sandbox/sync/v1"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
)

// metaToProto converts an apikey.KeyMetadata into the proto representation
// used in WatchKeys events and CreateKey responses.
func metaToProto(meta apikey.KeyMetadata) *syncv1.APIKeyMetadata {
	out := &syncv1.APIKeyMetadata{
		TokenHash:   meta.TokenHash,
		Namespace:   meta.Namespace,
		Role:        meta.Role,
		User:        meta.User,
		Team:        meta.Team,
		QuotaUrl:    meta.QuotaURL,
		Description: meta.Description,
		RawToken:    meta.RawToken,
	}
	if !meta.IssuedAt.IsZero() {
		out.IssuedAt = timestamppb.New(meta.IssuedAt.UTC())
	}
	if !meta.ExpiresAt.IsZero() {
		out.ExpiresAt = timestamppb.New(meta.ExpiresAt.UTC())
	}
	// Derive HashPrefix + SecretName from the fully qualified KeyID.
	shortName := meta.KeyID
	if i := strings.LastIndex(meta.KeyID, "/"); i >= 0 {
		shortName = meta.KeyID[i+1:]
	}
	out.SecretName = shortName
	const prefix = "agentbox-apikey-"
	if strings.HasPrefix(shortName, prefix) {
		out.HashPrefix = shortName[len(prefix):]
	}
	return out
}

// protoToTime returns the time.Time for a proto Timestamp, or zero if nil.
func protoToTime(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}

// clusterConfigToProto converts an in-memory cluster.ClusterConfig into the
// proto representation used in WatchClusterConfig events.
func clusterConfigToProto(cfg cluster.ClusterConfig) *syncv1.ClusterConfig {
	out := &syncv1.ClusterConfig{
		Clusters:    make([]*syncv1.ClusterEntry, 0, len(cfg.Clusters)),
		HostAliases: make([]*syncv1.HostAlias, 0, len(cfg.HostAliases)),
	}
	for _, c := range cfg.Clusters {
		entry := &syncv1.ClusterEntry{
			Id:       c.ID,
			Name:     c.Name,
			Url:      c.URL,
			Selector: c.Selector,
			Headers:  c.Headers,
			Visible:  c.Visible,
		}
		if c.Gateway != nil {
			entry.Gateway = &syncv1.GatewayConfig{
				NativeUrl:     c.Gateway.NativeURL,
				E2BUrl:        c.Gateway.E2BURL,
				DataUrl:       c.Gateway.DataURL,
				Headers:       c.Gateway.Headers,
				NativeHeaders: c.Gateway.NativeHeaders,
				E2BHeaders:    c.Gateway.E2BHeaders,
				DataHeaders:   c.Gateway.DataHeaders,
			}
		}
		for _, r := range c.Registries {
			entry.Registries = append(entry.Registries, &syncv1.RegistryEntry{
				Host: r.Host,
				Type: r.Type,
			})
		}
		if c.Logs != nil && len(c.Logs.Filters) > 0 {
			entry.Logs = &syncv1.LogsConfig{Filters: maps.Clone(c.Logs.Filters)}
		}
		out.Clusters = append(out.Clusters, entry)
	}
	for _, ha := range cfg.HostAliases {
		out.HostAliases = append(out.HostAliases, &syncv1.HostAlias{
			Ip:        ha.IP,
			Hostnames: append([]string(nil), ha.Hostnames...),
		})
	}
	return out
}
