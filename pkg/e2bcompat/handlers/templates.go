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
	"sort"

	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service/federation"
	e2bdomain "github.com/scitix/agent-sandbox/pkg/e2bcompat/domain"
	e2bgen "github.com/scitix/agent-sandbox/pkg/e2bcompat/gen"
)

// An E2B "template" is an AgentBox SandboxEnv.
//
// The listing used to return SandboxPools, which did not match what the create
// endpoint accepts: templateID is resolved through the Env router, so a bare
// name is looked up as an Env. Listing pools meant the ids a caller could see
// and the ids a caller could use were two different sets — and for an agent
// choosing a template from the listing, that is a dead end it cannot reason its
// way out of.
//
// Foreign clusters' Envs are listed too, as "cluster::env". That costs nothing:
// the federation registry already receives every cluster's per-Env capacity on
// the sync stream, so the data is in local memory. Nothing here fans out.

// templateFromEnv projects a SandboxEnv onto the E2B template shape.
//
// aliases carries the member pool names as well as the Env name, so a caller
// that still knows a pool by name can find it in the listing. Sandbox creation
// accepts either.
func templateFromEnv(env *agentsv1alpha1.SandboxEnv, memberPools []string) e2bgen.Template {
	aliases := append([]string{env.Name}, memberPools...)
	sort.Strings(aliases)
	aliases = dedupe(aliases)

	updated := env.CreationTimestamp.Time
	for i := range env.Status.Conditions {
		if t := env.Status.Conditions[i].LastTransitionTime.Time; t.After(updated) {
			updated = t
		}
	}

	return e2bgen.Template{
		TemplateID:  env.Name,
		BuildID:     string(env.UID),
		BuildStatus: e2bgen.TemplateBuildStatus("ready"),
		Public:      false,
		Aliases:     aliases,
		Names:       []string{env.Name},
		EnvdVersion: e2bgen.EnvdVersion(e2bdomain.EnvdVersion),
		SpawnCount:  int64(runningReplicas(env)),
		CreatedAt:   env.CreationTimestamp.Time,
		UpdatedAt:   updated,
	}
}

// runningReplicas sums the running sandboxes across the Env's observed members.
func runningReplicas(env *agentsv1alpha1.SandboxEnv) int32 {
	var total int32
	for i := range env.Status.Clusters {
		for j := range env.Status.Clusters[i].ObservedMembers {
			total += env.Status.Clusters[i].ObservedMembers[j].RunningCount
		}
	}
	return total
}

// templateFromForeignEnv projects an Env that lives on another cluster. The id
// is the "cluster::env" form the create endpoint routes on, so an entry from
// the listing can be passed straight back.
func templateFromForeignEnv(clusterID, envName string, pools []string) e2bgen.Template {
	id := clusterID + "::" + envName
	aliases := append([]string{id, envName}, pools...)
	sort.Strings(aliases)
	aliases = dedupe(aliases)
	return e2bgen.Template{
		TemplateID:  id,
		BuildStatus: e2bgen.TemplateBuildStatus("ready"),
		Public:      false,
		Aliases:     aliases,
		Names:       []string{envName},
		EnvdVersion: e2bgen.EnvdVersion(e2bdomain.EnvdVersion),
	}
}

// memberPoolNames lists the Pool names this Env fans out to on this cluster.
func memberPoolNames(env *agentsv1alpha1.SandboxEnv) []string {
	var pools []string
	for i := range env.Spec.Clusters {
		for j := range env.Spec.Clusters[i].Members {
			if name := env.Spec.Clusters[i].Members[j].Name; name != "" {
				pools = append(pools, name)
			}
		}
	}
	return pools
}

func dedupe(in []string) []string {
	out := in[:0:0]
	var prev string
	for i, v := range in {
		if v == "" || (i > 0 && v == prev) {
			continue
		}
		out = append(out, v)
		prev = v
	}
	return out
}

// listLocalTemplates returns this cluster's Envs, with their member pool names.
func (s *Server) listLocalTemplates(ctx context.Context, namespace string) ([]e2bgen.Template, error) {
	envList := &agentsv1alpha1.SandboxEnvList{}
	if err := s.k8sClient.List(ctx, envList, client.InNamespace(namespace)); err != nil {
		return nil, err
	}

	out := make([]e2bgen.Template, 0, len(envList.Items))
	for i := range envList.Items {
		env := &envList.Items[i]
		out = append(out, templateFromEnv(env, memberPoolNames(env)))
	}
	return out, nil
}

// listForeignTemplates returns Envs on other clusters, from the capacity
// records the federation stream already delivered.
func (s *Server) listForeignTemplates(namespace string, known map[string]struct{}) []e2bgen.Template {
	if s.federation == nil {
		return nil
	}

	// cluster::env -> member pools
	pools := map[string][]string{}
	names := map[string]federation.Capacity{}
	for _, c := range s.federation.Snapshot() {
		if c.Namespace != namespace || c.ClusterID == "" || c.EnvName == "" {
			continue
		}
		key := c.ClusterID + "::" + c.EnvName
		if _, isLocal := known[c.EnvName]; isLocal && c.ClusterID == s.localClusterID {
			continue
		}
		if c.ClusterID == s.localClusterID {
			continue
		}
		names[key] = c
		if c.MemberPool != "" {
			pools[key] = append(pools[key], c.MemberPool)
		}
	}

	keys := make([]string, 0, len(names))
	for k := range names {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]e2bgen.Template, 0, len(keys))
	for _, k := range keys {
		c := names[k]
		p := pools[k]
		sort.Strings(p)
		out = append(out, templateFromForeignEnv(c.ClusterID, c.EnvName, dedupe(p)))
	}
	return out
}
