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
	"testing"
	"time"

	syncv1 "github.com/scitix/agent-sandbox/pkg/proto/sandbox/sync/v1"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
)

// Everything a key carries has to survive the trip to a Worker, because the
// Hub is where keys are made and the Worker is where almost everything reads
// them.
//
// This test exists because one field did not. `requireApproval` — the whole of
// what "agent mode" means — was set on the Hub and dropped by this function, so
// every key issued from the console arrived unmarked and its writes were never
// gated. Nothing failed; the feature was simply absent for the path people
// actually use. A field-by-field assertion is the only thing that catches that
// class of omission, so add to this list whenever KeyMetadata grows.
func TestEveryKeyFieldSurvivesTheBroadcast(t *testing.T) {
	issued := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	expires := issued.Add(24 * time.Hour)

	meta := apikey.KeyMetadata{
		KeyID:           "agentbox-system/agentbox-apikey-abcd1234",
		TokenHash:       "abcd1234deadbeef",
		Namespace:       "ns1",
		Role:            apikey.RoleTenant,
		User:            "alice",
		Team:            "team1",
		QuotaURL:        "https://quota.example.com/q1",
		Description:     "an agent's key",
		RawToken:        "agbx_secret",
		IssuedAt:        issued,
		ExpiresAt:       expires,
		RequireApproval: true,
	}

	p := metaToProto(meta)

	checks := []struct {
		field string
		got   any
		want  any
	}{
		{"token_hash", p.TokenHash, meta.TokenHash},
		{"namespace", p.Namespace, meta.Namespace},
		{"role", p.Role, meta.Role},
		{"user", p.User, meta.User},
		{"team", p.Team, meta.Team},
		{"quota_url", p.QuotaUrl, meta.QuotaURL},
		{"description", p.Description, meta.Description},
		{"raw_token", p.RawToken, meta.RawToken},
		// The one that was missing. A Worker that does not receive it runs the
		// gate against a key it believes is unrestricted.
		{"require_approval", p.RequireApproval, meta.RequireApproval},
		{"secret_name", p.SecretName, "agentbox-apikey-abcd1234"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.field, c.got, c.want)
		}
	}
	if p.IssuedAt == nil || !p.IssuedAt.AsTime().Equal(issued) {
		t.Errorf("issued_at: got %v", p.IssuedAt)
	}
	if p.ExpiresAt == nil || !p.ExpiresAt.AsTime().Equal(expires) {
		t.Errorf("expires_at: got %v", p.ExpiresAt)
	}
}

func TestAnUnmarkedKeyStaysUnmarked(t *testing.T) {
	// The default has to survive too: every key issued before the mode existed
	// is unrestricted, and a broadcast that invented a flag would start gating
	// credentials nobody chose to gate.
	p := metaToProto(apikey.KeyMetadata{User: "u", Team: "t"})
	if p.RequireApproval {
		t.Fatal("an unmarked key must not arrive marked")
	}
}

func TestTheConsoleAddressRidesTheClusterSnapshot(t *testing.T) {
	// A Worker is never configured with it; this conversion is how it learns.
	out := clusterConfigToProto(cluster.ClusterConfig{
		Clusters:       []cluster.ClusterEntry{{ID: "c1"}},
		ConsoleBaseURL: "https://console.example.com/agentbox",
	})
	if out.ConsoleBaseUrl != "https://console.example.com/agentbox" {
		t.Fatalf("got %q", out.ConsoleBaseUrl)
	}
}

// Agent mode has to survive the CREATE, not just the broadcast.
//
// The broadcast direction was right from the start; the create was not. Every
// field a Worker sends when issuing a key was mapped onto the request except
// this one, so on any deployment with a Hub — which is every multi-cluster
// one — a key asked for as `agent` was minted unrestricted, and the approval
// gate it was supposed to run under simply never engaged. Nothing failed: the
// key worked, and it worked with more authority than it was asked for.
func TestAgentModeSurvivesKeyCreation(t *testing.T) {
	req := &syncv1.CreateKeyRequest{
		User:            "u",
		Team:            "t",
		Description:     "an agent's key",
		RequireApproval: true,
	}

	// The mapping the Hub performs before handing the metadata to its store.
	meta := apikey.KeyMetadata{
		User:            req.User,
		Team:            req.Team,
		Role:            apikey.RoleTenant,
		Description:     req.Description,
		RequireApproval: req.RequireApproval,
	}
	if !meta.RequireApproval {
		t.Fatal("a key asked for as agent was about to be minted unrestricted")
	}

	// And it has to come back down the broadcast, because the gate that reads
	// it runs on the Worker.
	if !metaToProto(meta).RequireApproval {
		t.Fatal("the flag stopped at the Hub; the gate would never see it")
	}
}

// The log-store shard flag has to ride the snapshot too.
//
// Only the Hub is configured with the list of sharded clusters, so this
// conversion is the Worker's only way to learn it. It did not carry the flag,
// which meant a Worker serving a finished sandbox's logs queried whichever
// store the endpoint defaulted to. On a sharded cluster that is the store its
// namespaces are not in, and the service answers an unmatched query with 200
// and no rows — so the Worker reported "no logs" for a sandbox whose logs were
// sitting in the other store the whole time.
func TestTheLogShardFlagRidesTheClusterSnapshot(t *testing.T) {
	out := clusterConfigToProto(cluster.ClusterConfig{
		Clusters: []cluster.ClusterEntry{{
			ID: "c1",
			Logs: &cluster.LogsConfig{
				Filters:      map[string]string{"cluster": "prod-foo"},
				SplitProject: true,
			},
		}},
	})
	if out.Clusters[0].Logs == nil {
		t.Fatal("the logs scope was dropped entirely")
	}
	if !out.Clusters[0].Logs.SplitProject {
		t.Fatal("splitProject stopped at the Hub; the Worker would query the wrong store")
	}
	if got := out.Clusters[0].Logs.Filters["cluster"]; got != "prod-foo" {
		t.Fatalf("filters: got %q", got)
	}
}

// A cluster that is sharded but declares no filters still has something to say.
//
// The guard used to be `len(Filters) > 0`, which discarded the whole logs
// block for such a cluster — taking the shard flag with it, silently.
func TestShardedWithoutFiltersStillCarriesTheScope(t *testing.T) {
	out := clusterConfigToProto(cluster.ClusterConfig{
		Clusters: []cluster.ClusterEntry{{
			ID:   "c1",
			Logs: &cluster.LogsConfig{SplitProject: true},
		}},
	})
	if out.Clusters[0].Logs == nil || !out.Clusters[0].Logs.SplitProject {
		t.Fatal("a sharded cluster with no filters lost its shard flag")
	}
}
