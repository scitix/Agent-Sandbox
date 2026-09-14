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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/store"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
	"github.com/scitix/agent-sandbox/pkg/utils/logclient"
)

// A sandbox whose Pod is gone is the only case these paths serve, so every test
// here starts from "no live Pod" and varies what is available behind it.
func centralLogsService(t *testing.T, rec *store.SandboxStore) *k8sSandboxService {
	t.Helper()
	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("fake client builder: %v", err)
	}
	s := &k8sSandboxService{client: cb.Build()}
	if rec != nil {
		s.store = *rec
	}
	return s
}

func historyWith(t *testing.T, sb gen.Sandbox) store.SandboxStore {
	t.Helper()
	st, err := store.NewSandboxStore(time.Hour)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := st.Save(sb); err != nil {
		t.Fatalf("save: %v", err)
	}
	return st
}

// Without a log service the sandbox really is unanswerable — but the reason has
// to be in the message, because it is a deployment gap and not a bad request.
func TestGetCentralLogs_NoServiceConfigured(t *testing.T) {
	s := centralLogsService(t, nil)
	_, appErr := s.getCentralLogs(context.Background(), "t-a", "sbx-1", "", 100)
	if appErr == nil {
		t.Fatal("expected an error for a sandbox with no pod and no log service")
	}
	if appErr.Code != domain.ErrCodeNotFound {
		t.Fatalf("expected NotFound, got %v", appErr.Code)
	}
	if !strings.Contains(appErr.Message, "no central log service configured") {
		t.Fatalf("the message must name the deployment gap, got %q", appErr.Message)
	}
}

// The whole point of the scope string: an empty body and a mis-scoped query are
// the same 200 over the wire.
func TestGetCentralLogs_EmptyResultCarriesTheScopeItAsked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "")
	}))
	defer srv.Close()

	hist := historyWith(t, gen.Sandbox{
		SandboxId: "sbx-1", Namespace: "t-a", PodName: "pod-1",
		ClaimedAt: time.Unix(1000, 0), TerminatedAt: ptr.To(time.Unix(2000, 0)),
	})
	s := centralLogsService(t, &hist)
	s.SetCentralLogs(
		logclient.New(logclient.Config{URL: srv.URL, Token: "tok", Project: "default"}),
		func() CentralLogScope { return CentralLogScope{} },
	)

	res, appErr := s.getCentralLogs(context.Background(), "t-a", "sbx-1", "", 100)
	if appErr != nil {
		t.Fatalf("an empty result is not an error: %v", appErr)
	}
	if res.Source != gen.Central {
		t.Fatalf("expected source=central, got %q", res.Source)
	}
	if res.Scope == nil {
		t.Fatal("an empty result must say what was asked")
	}
	for _, want := range []string{"project=default", "pod_name=pod-1", "declares no log filters"} {
		if !strings.Contains(*res.Scope, want) {
			t.Errorf("scope %q is missing %q", *res.Scope, want)
		}
	}
}

// A cluster that shards its log store by namespace must have the store chosen
// per query; picking the wrong one returns 200 and nothing.
func TestGetCentralLogs_SplitProjectPicksTheStoreByNamespace(t *testing.T) {
	for _, tc := range []struct{ namespace, wantProject string }{
		{"t-team-a", logclient.TenantProject},
		{"agentbox-system", logclient.PlatformProject},
	} {
		t.Run(tc.namespace, func(t *testing.T) {
			var gotProject string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotProject = r.URL.Query().Get("project")
				_, _ = io.WriteString(w, `{"_timestamp":"2026-09-04T10:00:00Z","container_name":"sandbox","log":"hello","pod_name":"pod-1"}`+"\n")
			}))
			defer srv.Close()

			hist := historyWith(t, gen.Sandbox{
				SandboxId: "sbx-1", Namespace: tc.namespace, PodName: "pod-1",
				ClaimedAt: time.Unix(1000, 0), TerminatedAt: ptr.To(time.Unix(2000, 0)),
			})
			s := centralLogsService(t, &hist)
			s.SetCentralLogs(
				logclient.New(logclient.Config{URL: srv.URL + "?project=default", Token: "tok"}),
				func() CentralLogScope {
					return CentralLogScope{Filters: map[string]string{"cluster": "prod-foo"}, SplitProject: true}
				},
			)

			res, appErr := s.getCentralLogs(context.Background(), tc.namespace, "sbx-1", "", 100)
			if appErr != nil {
				t.Fatalf("query: %v", appErr)
			}
			if gotProject != tc.wantProject {
				t.Fatalf("namespace %q must be read from %q, got %q", tc.namespace, tc.wantProject, gotProject)
			}
			if len(res.Entries) != 1 || res.Entries[0].Log != "hello" {
				t.Fatalf("unexpected entries: %+v", res.Entries)
			}
		})
	}
}

// An unsharded cluster keeps whatever the endpoint itself declares — the shape
// every cluster had before sharding existed.
func TestGetCentralLogs_UnshardedClusterLeavesTheEndpointProject(t *testing.T) {
	var gotProject string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotProject = r.URL.Query().Get("project")
		_, _ = io.WriteString(w, "")
	}))
	defer srv.Close()

	hist := historyWith(t, gen.Sandbox{
		SandboxId: "sbx-1", Namespace: "agentbox-system", PodName: "pod-1",
		ClaimedAt: time.Unix(1000, 0),
	})
	s := centralLogsService(t, &hist)
	s.SetCentralLogs(
		logclient.New(logclient.Config{URL: srv.URL + "?project=default", Token: "tok"}),
		func() CentralLogScope {
			return CentralLogScope{Filters: map[string]string{"cluster": "prod-foo"}}
		},
	)

	if _, appErr := s.getCentralLogs(context.Background(), "agentbox-system", "sbx-1", "", 100); appErr != nil {
		t.Fatalf("query: %v", appErr)
	}
	if gotProject != "default" {
		t.Fatalf("expected the endpoint's own project to survive, got %q", gotProject)
	}
}

// The wire always carries the resolved value so the console never has to know
// which way an unset field leans. The write half (envdFromGen, in
// pkg/apiserver/handlers) maps it back; changing one without the other is what
// "stores fine, reads back empty" looks like.
func TestEnvdToGen_AlwaysReportsTheEffectiveValue(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   *agentsv1alpha1.EnvdSpec
		want bool
	}{
		{"unset means on", nil, true},
		{"empty spec means on", &agentsv1alpha1.EnvdSpec{}, true},
		{"explicit on", &agentsv1alpha1.EnvdSpec{Verbose: ptr.To(true)}, true},
		{"explicit off", &agentsv1alpha1.EnvdSpec{Verbose: ptr.To(false)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := envdToGen(tc.in)
			if got == nil || got.Verbose == nil {
				t.Fatalf("the wire shape must always carry an explicit value, got %+v", got)
			}
			if *got.Verbose != tc.want {
				t.Fatalf("expected verbose=%v, got %v", tc.want, *got.Verbose)
			}
		})
	}
}

// A sandbox Pod runs the egress proxy alongside the sandbox, and the proxy
// logs a line per connection it evaluates. Reading every container by default
// buried what the sandbox itself printed, so the default is the sandbox's own
// container — on both the live path and this one.
func TestGetCentralLogs_DefaultsToTheSandboxContainer(t *testing.T) {
	var gotContainer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Filters map[string]struct {
				Op    string `json:"op"`
				Value string `json:"value"`
			} `json:"filters"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotContainer = body.Filters["container_name"].Value
		_, _ = io.WriteString(w, "")
	}))
	defer srv.Close()

	for _, tc := range []struct{ name, ask, want string }{
		{"unspecified reads the sandbox container", "", SandboxContainerName},
		{"an explicit container is honoured", "egress-proxy", "egress-proxy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hist := historyWith(t, gen.Sandbox{
				SandboxId: "sbx-1", Namespace: "t-a", PodName: "pod-1",
				ClaimedAt: time.Unix(1000, 0), TerminatedAt: ptr.To(time.Unix(2000, 0)),
			})
			s := centralLogsService(t, &hist)
			s.SetCentralLogs(
				logclient.New(logclient.Config{URL: srv.URL, Token: "tok"}),
				func() CentralLogScope { return CentralLogScope{} },
			)
			if _, appErr := s.getCentralLogs(context.Background(), "t-a", "sbx-1", tc.ask, 100); appErr != nil {
				t.Fatalf("query: %v", appErr)
			}
			if gotContainer != tc.want {
				t.Fatalf("expected container_name=%q, got %q", tc.want, gotContainer)
			}
		})
	}
}

// The Pod is gone, so the container list has to come from the record — and it
// has to be offered, or nobody can reach the sidecar's log for the very
// sandbox whose failure they are investigating.
func TestRecordedContainerNames(t *testing.T) {
	cases := []struct {
		name string
		in   *map[string]string
		want []string
	}{
		{"no record falls back to the sandbox container", nil, []string{"sandbox"}},
		{"empty record likewise", &map[string]string{}, []string{"sandbox"}},
		{
			"the sandbox container is listed first",
			&map[string]string{"egress-proxy": "i", "sandbox": "i"},
			[]string{"sandbox", "egress-proxy"},
		},
		{
			"a pod without one keeps what it has",
			&map[string]string{"worker": "i", "helper": "i"},
			[]string{"helper", "worker"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := recordedContainerNames(&gen.Sandbox{ContainerImages: tc.in})
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// Native sidecars live in initContainers[] with restartPolicy: Always, and the
// kubelet serves their logs like any other container. Leaving them out of the
// selectable set would make the egress proxy unreadable on exactly the
// clusters that run it that way.
func TestPodContainerSelection(t *testing.T) {
	pod := &corev1.Pod{Spec: corev1.PodSpec{
		Containers: []corev1.Container{{Name: "sandbox"}},
		InitContainers: []corev1.Container{
			{Name: "envd-injector"},
			{Name: "egress-proxy", RestartPolicy: ptr.To(corev1.ContainerRestartPolicyAlways)},
		},
	}}
	if names := containerNames(pod); len(names) != 3 || names[0] != "sandbox" {
		t.Fatalf("every container must be selectable, sandbox first: %v", names)
	}
	def := defaultLogContainers(pod)
	if len(def) != 1 || def[0].Name != "sandbox" {
		t.Fatalf("the default must be the sandbox container alone, got %+v", def)
	}

	// A Template is free to name its container something else; falling back to
	// every container there would reintroduce the noise this avoids.
	odd := &corev1.Pod{Spec: corev1.PodSpec{
		Containers: []corev1.Container{{Name: "main"}, {Name: "helper"}},
	}}
	if d := defaultLogContainers(odd); len(d) != 1 || d[0].Name != "main" {
		t.Fatalf("expected the first container as the fallback, got %+v", d)
	}
}
