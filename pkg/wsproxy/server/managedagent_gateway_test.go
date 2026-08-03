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

package server

import (
	"strings"
	"testing"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/controllers/managedagent"
)

// An agent is published under one base URL, and the workspace-fs server hangs
// off a reserved segment of it. The segment must not be able to shadow a
// gateway route: every real endpoint lives directly under the agent, so one
// that happened to collide would send genuine traffic to the wrong port.
func TestWorkspaceFSPrefixCannotShadowAGatewayRoute(t *testing.T) {
	if !strings.HasPrefix(WorkspaceFSPrefix, "/_") {
		t.Fatalf("WorkspaceFSPrefix = %q; a reserved segment must lead with an underscore",
			WorkspaceFSPrefix)
	}
	for _, route := range []string{
		"/threads", "/run", "/interactions", "/capabilities",
		"/backends", "/models", "/bot/run", "/analysis/x",
	} {
		if strings.HasPrefix(route, WorkspaceFSPrefix) {
			t.Errorf("gateway route %q is shadowed by %q", route, WorkspaceFSPrefix)
		}
	}
}

// status.endpoint is what a caller inside the cluster is told to use. It must
// name the proxy, never the Brain: the Brain authenticates nothing, so handing
// out its address lets any pod read any tenant's threads.
func TestInClusterEndpointGoesThroughTheProxy(t *testing.T) {
	got := managedagent.Endpoint("navix", "navix-extensions", 4099,
		"agentbox-dashboard-proxy.agentbox-system:9005")
	want := "http://agentbox-dashboard-proxy.agentbox-system:9005/navix"
	if got != want {
		t.Errorf("Endpoint = %q, want %q", got, want)
	}
	if strings.Contains(got, managedagent.BrainName("navix")) {
		t.Errorf("Endpoint %q names the Brain Service directly", got)
	}

	// With no proxy configured there is nothing to route through, so the raw
	// address is all that can be reported — but only then.
	if got := managedagent.Endpoint("navix", "ns", 4099, ""); !strings.Contains(got, "agentbox-brain-navix") {
		t.Errorf("fallback Endpoint = %q, want the Brain address", got)
	}
}

// Publishing is two independent switches. A chart that enables the listener
// must not thereby publish every agent it happens to host.
func TestPublishRequiresTheAgentToOptIn(t *testing.T) {
	ma := &agentsv1alpha1.ManagedAgent{}
	if managedagent.PublicURL(ma, "https://example.com/api/managed-agents") != "" {
		t.Error("an agent with no ingress block reports a public URL")
	}
	ma.Spec.Ingress = &agentsv1alpha1.ManagedAgentIngress{Enabled: false}
	if managedagent.PublicURL(ma, "https://example.com/api/managed-agents") != "" {
		t.Error("an agent that has not opted in reports a public URL")
	}
	ma.Name = "navix"
	ma.Spec.Ingress.Enabled = true
	if got := managedagent.PublicURL(ma, "https://example.com/api/managed-agents/"); got !=
		"https://example.com/api/managed-agents/navix" {
		t.Errorf("PublicURL = %q", got)
	}
}
