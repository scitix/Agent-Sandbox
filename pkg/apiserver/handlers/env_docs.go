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
	"strconv"
	"strings"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
)

// Placeholders a template's docs markdown may use. Env-scoped rendering
// (GetSandboxEnv) substitutes real values; the Template detail page has no env
// context and substitutes readable hints for the env-dependent ones.
const (
	docsVarEnvName      = "${AGBX_ENV_NAME}"
	docsVarPoolName     = "${AGBX_POOL_NAME}"
	docsVarClusterID    = "${AGBX_CLUSTER_ID}"
	docsVarAPIKey       = "${AGBX_API_KEY}"
	docsVarNativeURL    = "${AGBX_NATIVE_URL}"
	docsVarE2BURL       = "${AGBX_E2B_URL}"
	docsVarDataURL      = "${AGBX_DATA_URL}"
	docsVarDataDomain   = "${AGBX_DATA_DOMAIN}"
	docsVarHost         = "${AGBX_HOST}"
	docsVarInnerIP      = "${AGBX_INNER_IP}"
	docsVarHTTPS        = "${AGBX_HTTPS}"
	docsVarRegistryHost = "${AGBX_REGISTRY_HOST}"
)

// docsPoolNameFallbackSuffix is appended to the Env name to synthesise a
// ${AGBX_POOL_NAME} value when the Env has no materialised member Pool in the
// local cluster yet. Member Pools are always named "<env>-<something>", so
// "<env>-pool-name" reads as the shape of a real name and makes it obvious the
// snippet needs a concrete pool once one exists.
const docsPoolNameFallbackSuffix = "-pool-name"

// docsVars carries the substitutions for one docs rendering pass. Fields left
// empty are not substituted at all — the placeholder survives verbatim in the
// output, which is the honest answer for a value this deployment does not know
// (an unconfigured gateway, a host with no in-cluster alias, a cluster with no
// registry). Rendering a guess there would hand users a URL that resolves
// nowhere.
type docsVars struct {
	envName   string
	poolName  string
	clusterID string
	apiKey    string

	nativeURL    string
	e2bURL       string
	dataURL      string
	dataDomain   string
	host         string
	innerIP      string
	https        string
	registryHost string
}

// clusterDocsVars builds the docs variables that describe the serving cluster:
// its ID plus every endpoint the cluster config knows about. Safe when
// cross-cluster routing or the cluster catalog is unconfigured — the affected
// variables stay empty and their placeholders are left in the output.
func (s *Server) clusterDocsVars() docsVars {
	clusterID := s.forwarder.LocalClusterID()
	if s.cluster == nil {
		return docsVars{clusterID: clusterID}
	}
	return clusterDocsVars(clusterID, s.cluster.Endpoints(clusterID))
}

// clusterDocsVars fills the cluster-config-derived fields from one cluster's
// gateway configuration. These are facts about the serving cluster, so both the
// env-scoped and the Template-scoped renderer resolve them for real.
func clusterDocsVars(clusterID string, ep service.ClusterEndpoints) docsVars {
	v := docsVars{
		clusterID:    clusterID,
		nativeURL:    ep.NativeURL,
		e2bURL:       ep.E2BURL,
		dataURL:      ep.DataURL,
		dataDomain:   ep.DataDomain,
		host:         ep.Host,
		innerIP:      ep.InnerIP,
		registryHost: ep.RegistryHost,
	}
	// HTTPS is only meaningful once a data URL was parsed; without one there is
	// nothing to report and the placeholder stays put.
	if ep.DataURL != "" {
		v.https = strconv.FormatBool(ep.HTTPS)
	}
	return v
}

// apply substitutes every non-empty variable into raw, leaving placeholders for
// unknown values untouched.
func (v docsVars) apply(raw string) string {
	for _, sub := range []struct{ placeholder, value string }{
		{docsVarEnvName, v.envName},
		{docsVarPoolName, v.poolName},
		{docsVarClusterID, v.clusterID},
		{docsVarAPIKey, v.apiKey},
		{docsVarNativeURL, v.nativeURL},
		{docsVarE2BURL, v.e2bURL},
		{docsVarDataURL, v.dataURL},
		{docsVarDataDomain, v.dataDomain},
		{docsVarHost, v.host},
		{docsVarInnerIP, v.innerIP},
		{docsVarHTTPS, v.https},
		{docsVarRegistryHost, v.registryHost},
	} {
		if sub.value == "" {
			continue
		}
		raw = strings.ReplaceAll(raw, sub.placeholder, sub.value)
	}
	return raw
}

// renderEnvDocs substitutes the docs placeholders in the raw env docs markdown
// with real values. The caller supplies the env/pool/cluster context in vars;
// the API key is resolved here from the acting user's API keys: the first entry
// whose RawToken is non-empty wins (legacy keys with only a hash stored are
// skipped because users cannot run the rendered snippets without the plaintext
// token).
//
// When raw is empty, returns ("", nil) — nothing to render.
// When raw contains ${AGBX_API_KEY} but no usable key is found, returns
// ("", APIKeyRequired AppError) so the caller can surface it to the user.
func (s *Server) renderEnvDocs(ctx context.Context, raw string, vars docsVars, auth domain.AuthInfo) (string, *domain.AppError) {
	if raw == "" {
		return "", nil
	}

	if strings.Contains(raw, docsVarAPIKey) {
		keys, appErr := s.apikey.ListByTeamAndUser(ctx, auth.Team, auth.User)
		if appErr != nil {
			return "", appErr
		}
		for _, k := range keys {
			if k.RawToken != "" {
				vars.apiKey = k.RawToken
				break
			}
		}
		if vars.apiKey == "" {
			return "", domain.NewAPIKeyRequired(
				"no API key with a recoverable token found for this user; please create a new API key on the API Keys page to view the env docs",
			)
		}
	}

	return vars.apply(raw), nil
}

// docsPoolName resolves the value ${AGBX_POOL_NAME} renders to for an Env: the
// first member Pool that actually exists in the local cluster, read off the
// Env's local status segment (the Reconciler marks a member Missing when its
// Pool CR is absent, so a non-Missing member is a live Pool). Member order
// follows spec order, so the answer is stable across calls.
//
// Only local members are considered: the same docs block renders
// ${AGBX_CLUSTER_ID} as the local cluster ID, and pairing that with a foreign
// cluster's pool would produce a reference that resolves nowhere.
//
// Falls back to "<envName><docsPoolNameFallbackSuffix>" when the Env has no
// live local member — e.g. a bare Env shell whose members have not been added
// yet, or one whose status has not been observed for the first time.
func docsPoolName(env *gen.SandboxEnv, localClusterID string) string {
	if env == nil {
		return ""
	}
	if env.Status != nil && env.Status.Clusters != nil {
		for _, c := range *env.Status.Clusters {
			isLocal := c.ClusterID == localClusterID || (c.IsLocal != nil && *c.IsLocal)
			if !isLocal || c.ObservedMembers == nil {
				continue
			}
			for _, m := range *c.ObservedMembers {
				if m.Name == "" || (m.State != nil && *m.State == gen.Missing) {
					continue
				}
				return m.Name
			}
		}
	}
	return env.Name + docsPoolNameFallbackSuffix
}

// renderTemplateDocs substitutes the docs placeholders with a preview of what
// the rendered snippets will look like. The Template detail page is not scoped
// to an env, so the env-dependent placeholders (env name, pool name, API key)
// become readable stand-ins; everything derived from the serving cluster's
// gateway config is substituted for real, since it does not depend on the env.
func renderTemplateDocs(raw string, vars docsVars) string {
	if raw == "" {
		return ""
	}
	vars.envName = "YOUR_ENV_NAME"
	vars.poolName = "YOUR_POOL_NAME"
	vars.apiKey = "YOUR_API_KEY"
	return vars.apply(raw)
}
