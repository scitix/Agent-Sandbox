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

package handlers

import (
	"context"
	"testing"

	"k8s.io/utils/ptr"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
)

// stubAPIKeyService implements just the ListByTeamAndUser method used by
// renderEnvDocs. Other methods panic so any unexpected call is caught.
type stubAPIKeyService struct {
	items   []service.APIKeyItem
	listErr *domain.AppError
}

var _ service.APIKeyService = (*stubAPIKeyService)(nil)

func (s *stubAPIKeyService) ListByTeamAndUser(context.Context, string, string) ([]service.APIKeyItem, *domain.AppError) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.items, nil
}

func (s *stubAPIKeyService) Create(context.Context, service.CreateAPIKeyInput) (*service.APIKeyResult, *domain.AppError) {
	panic("not implemented")
}
func (s *stubAPIKeyService) List(context.Context) ([]service.APIKeyItem, *domain.AppError) {
	panic("not implemented")
}
func (s *stubAPIKeyService) Get(context.Context, string) (*service.APIKeyItem, *domain.AppError) {
	panic("not implemented")
}
func (s *stubAPIKeyService) Delete(context.Context, string) *domain.AppError {
	panic("not implemented")
}
func (s *stubAPIKeyService) Promote(context.Context, string) *domain.AppError {
	panic("not implemented")
}

func newTestServer(stub *stubAPIKeyService) *Server {
	return &Server{apikey: stub}
}

func TestRenderEnvDocs_EmptyRaw(t *testing.T) {
	s := newTestServer(&stubAPIKeyService{})
	got, err := s.renderEnvDocs(context.Background(), "", docsVars{envName: "e", poolName: "e-pool", clusterID: "c"}, domain.AuthInfo{Team: "t", User: "u"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestRenderEnvDocs_SubstitutesAllVariables(t *testing.T) {
	stub := &stubAPIKeyService{
		items: []service.APIKeyItem{
			{KeyMetadata: service.KeyMetadata{RawToken: "agbx_newkey"}},
		},
	}
	s := newTestServer(stub)
	raw := "env=${AGBX_ENV_NAME} pool=${AGBX_POOL_NAME} cluster=${AGBX_CLUSTER_ID} key=${AGBX_API_KEY}"
	got, err := s.renderEnvDocs(context.Background(), raw,
		docsVars{envName: "myenv", poolName: "myenv-1c2gi-ondemand", clusterID: "cluster3"},
		domain.AuthInfo{Team: "t", User: "alice"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "env=myenv pool=myenv-1c2gi-ondemand cluster=cluster3 key=agbx_newkey"
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestRenderEnvDocs_PicksFirstKeyWithRawToken(t *testing.T) {
	stub := &stubAPIKeyService{
		items: []service.APIKeyItem{
			{KeyMetadata: service.KeyMetadata{RawToken: ""}},              // legacy, skipped
			{KeyMetadata: service.KeyMetadata{RawToken: "agbx_winner"}},   // picked
			{KeyMetadata: service.KeyMetadata{RawToken: "agbx_runnerup"}}, // ignored
		},
	}
	s := newTestServer(stub)
	got, err := s.renderEnvDocs(context.Background(), "k=${AGBX_API_KEY}", docsVars{envName: "e"}, domain.AuthInfo{Team: "t", User: "u"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "k=agbx_winner" {
		t.Fatalf("want k=agbx_winner, got %q", got)
	}
}

func TestRenderEnvDocs_NoUsableKeyReturnsAPIKeyRequired(t *testing.T) {
	stub := &stubAPIKeyService{
		items: []service.APIKeyItem{
			{KeyMetadata: service.KeyMetadata{RawToken: ""}}, // legacy only
		},
	}
	s := newTestServer(stub)
	got, err := s.renderEnvDocs(context.Background(), "k=${AGBX_API_KEY}", docsVars{envName: "e"}, domain.AuthInfo{Team: "t", User: "u"})
	if err == nil {
		t.Fatalf("want error, got nil (rendered=%q)", got)
	}
	if err.BizCode != domain.BizErrAPIKeyRequired {
		t.Fatalf("want BizCode=%q, got %q", domain.BizErrAPIKeyRequired, err.BizCode)
	}
	if err.Code != domain.ErrCodeUnprocessableEntity {
		t.Fatalf("want 422, got %d", err.Code)
	}
}

func TestRenderEnvDocs_NoApiKeyPlaceholderSkipsLookup(t *testing.T) {
	// Stub that would fail if asked to list keys — proves the helper does not
	// query the key store when ${AGBX_API_KEY} is absent.
	stub := &stubAPIKeyService{
		listErr: domain.NewInternal("should not be called", nil),
	}
	s := newTestServer(stub)
	got, err := s.renderEnvDocs(context.Background(), "env=${AGBX_ENV_NAME}", docsVars{envName: "myenv"}, domain.AuthInfo{Team: "t", User: "u"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "env=myenv" {
		t.Fatalf("want env=myenv, got %q", got)
	}
}

// envWithObservedMembers builds a gen.SandboxEnv named "slime" carrying one
// status cluster segment with the supplied observed members, in order.
func envWithObservedMembers(clusterID string, members ...gen.EnvObservedMember) *gen.SandboxEnv {
	return &gen.SandboxEnv{
		Name: "slime",
		Status: &gen.SandboxEnvStatus{
			Clusters: &[]gen.EnvClusterStatus{{
				ClusterID:       clusterID,
				ObservedMembers: &members,
			}},
		},
	}
}

func TestDocsPoolName_FirstLiveLocalMember(t *testing.T) {
	env := envWithObservedMembers("cluster3",
		gen.EnvObservedMember{Name: "slime-gone", State: ptr.To(gen.Missing)},
		gen.EnvObservedMember{Name: "slime-1c2gi-ondemand", State: ptr.To(gen.Active)},
		gen.EnvObservedMember{Name: "slime-2c4gi-spot", State: ptr.To(gen.Active)},
	)
	if got := docsPoolName(env, "cluster3"); got != "slime-1c2gi-ondemand" {
		t.Fatalf("want slime-1c2gi-ondemand, got %q", got)
	}
}

func TestDocsPoolName_InconsistentMemberStillCounts(t *testing.T) {
	// Inconsistent means the Pool exists but lags the Template revision — it is
	// still a usable target for a create.
	env := envWithObservedMembers("cluster3",
		gen.EnvObservedMember{Name: "slime-1c2gi-ondemand", State: ptr.To(gen.Inconsistent)},
	)
	if got := docsPoolName(env, "cluster3"); got != "slime-1c2gi-ondemand" {
		t.Fatalf("want slime-1c2gi-ondemand, got %q", got)
	}
}

func TestDocsPoolName_SkipsForeignClusterSegment(t *testing.T) {
	env := envWithObservedMembers("other-cluster",
		gen.EnvObservedMember{Name: "slime-in-other-cluster", State: ptr.To(gen.Active)},
	)
	if got := docsPoolName(env, "cluster3"); got != "slime-pool-name" {
		t.Fatalf("want slime-pool-name, got %q", got)
	}
}

func TestDocsPoolName_FallsBackWhenNoLiveMember(t *testing.T) {
	cases := map[string]*gen.SandboxEnv{
		"no status":  {Name: "slime"},
		"no members": envWithObservedMembers("cluster3"),
		"all missing": envWithObservedMembers("cluster3",
			gen.EnvObservedMember{Name: "slime-gone", State: ptr.To(gen.Missing)},
		),
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			if got := docsPoolName(env, "cluster3"); got != "slime-pool-name" {
				t.Fatalf("want slime-pool-name, got %q", got)
			}
		})
	}
}

func TestRenderTemplateDocs_EmptyRaw(t *testing.T) {
	got := renderTemplateDocs("", docsVars{clusterID: "cluster3"})
	if got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestRenderTemplateDocs_SubstitutesEnvNameAndApiKey(t *testing.T) {
	raw := "env=${AGBX_ENV_NAME} key=${AGBX_API_KEY}"
	got := renderTemplateDocs(raw, docsVars{clusterID: "cluster3"})
	want := "env=YOUR_ENV_NAME key=YOUR_API_KEY"
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestRenderTemplateDocs_SubstitutesRealClusterID(t *testing.T) {
	raw := "cluster=${AGBX_CLUSTER_ID}"
	got := renderTemplateDocs(raw, docsVars{clusterID: "cluster3"})
	if got != "cluster=cluster3" {
		t.Fatalf("want cluster=cluster3, got %q", got)
	}
}

func TestRenderTemplateDocs_SubstitutesAllVariables(t *testing.T) {
	raw := "env=${AGBX_ENV_NAME} pool=${AGBX_POOL_NAME} cluster=${AGBX_CLUSTER_ID} key=${AGBX_API_KEY}"
	got := renderTemplateDocs(raw, docsVars{clusterID: "cluster3"})
	want := "env=YOUR_ENV_NAME pool=YOUR_POOL_NAME cluster=cluster3 key=YOUR_API_KEY"
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

// demoEndpoints mirrors a configured cluster: public gateway URLs, an
// in-cluster alias for the gateway host, one region-local registry.
func demoEndpoints() service.ClusterEndpoints {
	return service.ClusterEndpoints{
		NativeURL:    "https://gw.example.com/agent-sandbox/api/native",
		E2BURL:       "https://gw.example.com/agent-sandbox/api/e2b",
		DataURL:      "https://gw.example.com/agent-sandbox/api/data",
		DataDomain:   "gw.example.com/agent-sandbox/api/data",
		Host:         "gw.example.com",
		InnerIP:      "10.0.0.1",
		HTTPS:        true,
		RegistryHost: "registry.example.com",
	}
}

const clusterVarsRaw = "native=${AGBX_NATIVE_URL}\ne2b=${AGBX_E2B_URL}\ndata=${AGBX_DATA_URL}\n" +
	"domain=${AGBX_DATA_DOMAIN}\nhost=${AGBX_HOST}\nip=${AGBX_INNER_IP}\n" +
	"https=${AGBX_HTTPS}\nregistry=${AGBX_REGISTRY_HOST}"

func TestClusterDocsVars_SubstitutesEndpoints(t *testing.T) {
	got := clusterDocsVars("demo", demoEndpoints()).apply(clusterVarsRaw)
	want := "native=https://gw.example.com/agent-sandbox/api/native\n" +
		"e2b=https://gw.example.com/agent-sandbox/api/e2b\n" +
		"data=https://gw.example.com/agent-sandbox/api/data\n" +
		"domain=gw.example.com/agent-sandbox/api/data\n" +
		"host=gw.example.com\nip=10.0.0.1\nhttps=true\nregistry=registry.example.com"
	if got != want {
		t.Fatalf("want:\n%s\ngot:\n%s", want, got)
	}
}

func TestClusterDocsVars_UnknownValuesKeepPlaceholders(t *testing.T) {
	// Nothing configured: every cluster-derived placeholder must survive
	// verbatim rather than render as an empty string or a guess, so users can
	// tell "not configured" from "configured as blank".
	got := clusterDocsVars("demo", service.ClusterEndpoints{}).apply(clusterVarsRaw)
	if got != clusterVarsRaw {
		t.Fatalf("placeholders should be untouched, got:\n%s", got)
	}

	// Data URL present but no host alias: only ${AGBX_INNER_IP} stays a placeholder.
	partial := clusterDocsVars("demo", service.ClusterEndpoints{
		DataURL:    "http://gw.internal/agent-sandbox/api/data",
		DataDomain: "gw.internal/agent-sandbox/api/data",
		Host:       "gw.internal",
	}).apply("ip=${AGBX_INNER_IP} host=${AGBX_HOST} https=${AGBX_HTTPS}")
	if partial != "ip=${AGBX_INNER_IP} host=gw.internal https=false" {
		t.Fatalf("unexpected partial render: %s", partial)
	}
}

func TestRenderEnvDocs_SubstitutesClusterEndpoints(t *testing.T) {
	s := newTestServer(&stubAPIKeyService{})
	vars := clusterDocsVars("demo", demoEndpoints())
	vars.envName = "slime"
	vars.poolName = "slime-1c16gi-10-ondemand"

	got, err := s.renderEnvDocs(context.Background(),
		"E2B_DOMAIN=${AGBX_DATA_DOMAIN} E2B_API_URL=${AGBX_E2B_URL} pool=${AGBX_POOL_NAME}",
		vars, domain.AuthInfo{Team: "t", User: "u"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "E2B_DOMAIN=gw.example.com/agent-sandbox/api/data " +
		"E2B_API_URL=https://gw.example.com/agent-sandbox/api/e2b pool=slime-1c16gi-10-ondemand"
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestRenderTemplateDocs_KeepsRealClusterEndpoints(t *testing.T) {
	// The Template page has no env, but the gateway addresses are cluster facts —
	// they must render for real, not as hints.
	got := renderTemplateDocs("env=${AGBX_ENV_NAME} data=${AGBX_DATA_URL} ip=${AGBX_INNER_IP}",
		clusterDocsVars("demo", demoEndpoints()))
	want := "env=YOUR_ENV_NAME data=https://gw.example.com/agent-sandbox/api/data ip=10.0.0.1"
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}
