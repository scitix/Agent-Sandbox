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

package managedagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

func autoAgent() *agentsv1alpha1.ManagedAgent {
	return &agentsv1alpha1.ManagedAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "navix", Namespace: "agents"},
		Spec: agentsv1alpha1.ManagedAgentSpec{
			Hands: agentsv1alpha1.ManagedAgentHands{
				Auto: &agentsv1alpha1.HandsAutoSpec{
					ClusterID:   "gw-a",
					TemplateRef: "navix-tmpl",
					Image:       "registry.example.com/sandbox:v1",
					InstanceTypes: []agentsv1alpha1.HandsInstanceType{
						{Name: "1c2gi", Replicas: 2, Default: true},
						{Name: "2c4gi", Replicas: 1},
					},
				},
			},
		},
	}
}

// The worker caps env names at 24 characters and derives each pool name by
// appending the instance type. A name that only fits before the suffix is
// rejected by the remote on every reconcile, with nothing local to look at.
func TestHandsEnvNameFitsTheRemoteLimit(t *testing.T) {
	for _, tc := range []struct{ agent, want string }{
		{"navix", "navix-hands"},
		{"a-very-long-agent-name-indeed", "a-very-long-agent-hands"},
	} {
		got := HandsEnvName(tc.agent)
		if got != tc.want {
			t.Errorf("HandsEnvName(%q) = %q, want %q", tc.agent, got, tc.want)
		}
		if len(got) > 24 {
			t.Errorf("HandsEnvName(%q) = %q is %d chars, over the remote's 24", tc.agent, got, len(got))
		}
	}
}

func TestDeriveEnvRejectsIncompleteSpecs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*agentsv1alpha1.HandsAutoSpec)
	}{
		{"no template", func(a *agentsv1alpha1.HandsAutoSpec) { a.TemplateRef = "" }},
		{"no instance types", func(a *agentsv1alpha1.HandsAutoSpec) { a.InstanceTypes = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ma := autoAgent()
			tc.mutate(ma.Spec.Hands.Auto)
			if _, err := DeriveEnv(ma); err == nil {
				t.Error("DeriveEnv accepted a spec that cannot produce a usable env")
			}
		})
	}
}

// fakeWorker is a worker cluster's native API, just enough of it to drive the
// provisioner: an env store, a member store, and a record of what was called.
type fakeWorker struct {
	envs        map[string]bool
	members     map[string][]string
	ready       bool
	inlineSized bool
	calls       []string
}

func newFakeWorker() *fakeWorker {
	return &fakeWorker{envs: map[string]bool{}, members: map[string][]string{}}
}

func (f *fakeWorker) handler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("AGENTBOX-API-KEY"); got != "admin-key" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"missing api key"}`))
			return
		}
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		path := strings.TrimPrefix(r.URL.Path, "/v1/envs")

		switch {
		case r.Method == http.MethodPost && path == "":
			var body struct {
				Name string `json:"name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			// A real worker refuses to re-create an env that exists; EnsureEnv
			// tolerates that (a concurrent reconcile is the desired state either
			// way) while CreateEnv must report it.
			if f.envs[body.Name] {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"error":"env already exists"}`))
				return
			}
			f.envs[body.Name] = true
			w.WriteHeader(http.StatusCreated)

		case r.Method == http.MethodGet && strings.HasSuffix(path, "/sandboxpools"):
			name := strings.TrimSuffix(strings.TrimPrefix(path, "/"), "/sandboxpools")
			items := []map[string]string{}
			for _, it := range f.members[name] {
				// A real worker echoes back whichever field sized the member.
				key := "instanceType"
				if f.inlineSized {
					key = "scalingGroup"
				}
				items = append(items, map[string]string{key: it, "name": name + "-" + it})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": items})

		case r.Method == http.MethodPost && strings.HasSuffix(path, "/sandboxpools"):
			name := strings.TrimSuffix(strings.TrimPrefix(path, "/"), "/sandboxpools")
			var body struct {
				InstanceType string `json:"instanceType"`
				ScalingGroup string `json:"scalingGroup"`
				Inline       any    `json:"inlineResources"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if (body.InstanceType == "") == (body.ScalingGroup == "") {
				t.Errorf("member request set neither or both of instanceType/scalingGroup: %+v", body)
			}
			if body.ScalingGroup != "" && body.Inline == nil {
				t.Error("inline-sized member sent no inlineResources; the worker would reject it")
			}
			id := body.InstanceType
			if id == "" {
				id = body.ScalingGroup
			}
			f.members[name] = append(f.members[name], id)
			w.WriteHeader(http.StatusCreated)

		case r.Method == http.MethodGet && path == "":
			items := []map[string]any{}
			for name := range f.envs {
				items = append(items, map[string]any{"name": name, "ready": f.ready})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": items})

		case r.Method == http.MethodGet:
			// The real worker renders env docs here and needs a recoverable
			// per-user key, which an admin key is not. Anything that reaches
			// this path is a bug in the provisioner.
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"errorCode":"API_KEY_REQUIRED"}`))

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func newTestProvisioner(t *testing.T, f *fakeWorker) *RESTHandsProvisioner {
	t.Helper()
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	return NewRESTHandsProvisioner(func(id string) (ClusterEndpoint, bool) {
		if id != "gw-a" {
			return ClusterEndpoint{}, false
		}
		return ClusterEndpoint{BaseURL: srv.URL}, true
	}, "admin-key")
}

// Provisioning converges over reconciles rather than in one shot: the env is
// created first, then its members, then it reports ready. Each call has to be
// safe to repeat, because the reconciler makes it on every event.
func TestEnsureEnvConvergesAndIsIdempotent(t *testing.T) {
	f := newFakeWorker()
	p := newTestProvisioner(t, f)
	spec, err := DeriveEnv(autoAgent())
	if err != nil {
		t.Fatalf("DeriveEnv: %v", err)
	}
	ctx := context.Background()

	// Pass 1: env does not exist yet.
	ready, detail, err := p.EnsureEnv(ctx, "gw-a", spec)
	if err != nil {
		t.Fatalf("pass 1: %v", err)
	}
	if ready {
		t.Error("pass 1 reported ready before any member pool existed")
	}
	if !strings.Contains(detail, "navix-hands") {
		t.Errorf("pass 1 detail = %q, want it to name the env", detail)
	}

	// Pass 2: env exists, members do not.
	if _, _, err = p.EnsureEnv(ctx, "gw-a", spec); err != nil {
		t.Fatalf("pass 2: %v", err)
	}
	if got := f.members["navix-hands"]; len(got) != 2 {
		t.Fatalf("member pools = %v, want one per instance type", got)
	}

	// Pass 3: everything exists but the remote has not finished rolling out.
	ready, _, err = p.EnsureEnv(ctx, "gw-a", spec)
	if err != nil {
		t.Fatalf("pass 3: %v", err)
	}
	if ready {
		t.Error("reported ready while the remote reports 0 ready members")
	}
	if got := f.members["navix-hands"]; len(got) != 2 {
		t.Errorf("a repeat call duplicated member pools: %v", got)
	}

	// Pass 4: remote is serving.
	f.ready = true
	ready, detail, err = p.EnsureEnv(ctx, "gw-a", spec)
	if err != nil {
		t.Fatalf("pass 4: %v", err)
	}
	if !ready {
		t.Errorf("not ready although every member is: %q", detail)
	}

	for _, c := range f.calls {
		if strings.HasPrefix(c, "DELETE") {
			t.Fatalf("provisioner issued %q; a derived env is never deleted from here", c)
		}
	}
}

// An unregistered cluster is a configuration error on the agent, not a
// transient remote failure. Saying so by name beats a connection error against
// an empty address.
func TestEnsureEnvRejectsUnknownCluster(t *testing.T) {
	p := newTestProvisioner(t, newFakeWorker())
	spec, _ := DeriveEnv(autoAgent())
	_, _, err := p.EnsureEnv(context.Background(), "nope", spec)
	if err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Errorf("error = %v, want it to say the cluster is not registered", err)
	}
}

// The remote's own message is the only description of what went wrong; a bare
// status code sends the reader to the wrong cluster's logs.
func TestEnsureEnvSurfacesTheRemoteError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"template \"navix-tmpl\" not found"}`))
	}))
	defer srv.Close()
	p := NewRESTHandsProvisioner(func(string) (ClusterEndpoint, bool) {
		return ClusterEndpoint{BaseURL: srv.URL}, true
	}, "admin-key")
	spec, _ := DeriveEnv(autoAgent())
	_, _, err := p.EnsureEnv(context.Background(), "gw-a", spec)
	if err == nil || !strings.Contains(err.Error(), "navix-tmpl") {
		t.Errorf("error = %v, want the remote's own text", err)
	}
}

// A cluster without an instance-type catalog sizes members inline and echoes
// them back as scalingGroup. Matching only instanceType would make every
// existing member look absent, so each reconcile would re-add it — the pool
// count would climb until the worker refused.
func TestEnsureEnvDedupesInlineSizedMembers(t *testing.T) {
	f := newFakeWorker()
	f.inlineSized = true
	p := newTestProvisioner(t, f)

	ma := autoAgent()
	for i := range ma.Spec.Hands.Auto.InstanceTypes {
		ma.Spec.Hands.Auto.InstanceTypes[i].Resources = &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
		}
	}
	spec, err := DeriveEnv(ma)
	if err != nil {
		t.Fatalf("DeriveEnv: %v", err)
	}

	ctx := context.Background()
	for pass := 1; pass <= 4; pass++ {
		if _, _, err := p.EnsureEnv(ctx, "gw-a", spec); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
	}
	if got := f.members["navix-hands"]; len(got) != 2 {
		t.Errorf("member pools after 4 reconciles = %v, want exactly one per instance type", got)
	}
}

// CreateEnv forwards the caller's body as-is. The console offers the worker's
// whole env API through it (credential injection included), so anything this
// layer re-serialised would be a second schema to keep in step.
func TestCreateEnvForwardsTheBodyVerbatimAndAddsMembers(t *testing.T) {
	f := newFakeWorker()
	p := newTestProvisioner(t, f)

	name, err := p.CreateEnv(context.Background(), "gw-a",
		json.RawMessage(`{"name":"navix-hands","templateRef":{"name":"tpl"},`+
			`"overrides":{"networkPolicy":{"secretInjection":{"credentials":[{"name":"navix"}]}}}}`),
		[]json.RawMessage{json.RawMessage(`{"instanceType":"1c2gi"}`)})
	if err != nil {
		t.Fatalf("CreateEnv: %v", err)
	}
	if name != "navix-hands" {
		t.Errorf("name = %q, want navix-hands", name)
	}
	if !f.envs["navix-hands"] {
		t.Error("env was not created")
	}
	if got := f.members["navix-hands"]; len(got) != 1 {
		t.Errorf("members = %v, want one", got)
	}
}

// Unlike EnsureEnv, which is a reconcile and treats an existing env as the
// desired state, this is a one-shot console request: adopting somebody else's env
// would attach an agent to a sandbox supply configured for another purpose.
func TestCreateEnvReportsAConflictInsteadOfAdopting(t *testing.T) {
	f := newFakeWorker()
	f.envs["taken"] = true
	p := newTestProvisioner(t, f)

	if _, err := p.CreateEnv(context.Background(), "gw-a",
		json.RawMessage(`{"name":"taken","templateRef":{"name":"tpl"}}`), nil); err == nil {
		t.Fatal("expected an existing env to be refused")
	}
}

// A body with no name cannot address the member endpoint, and would leave the
// agent pointing at "".
func TestCreateEnvRequiresAName(t *testing.T) {
	p := newTestProvisioner(t, newFakeWorker())
	if _, err := p.CreateEnv(context.Background(), "gw-a", json.RawMessage(`{}`), nil); err == nil {
		t.Fatal("expected a nameless env request to be refused")
	}
}
