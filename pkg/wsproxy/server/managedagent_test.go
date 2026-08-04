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
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/controllers/managedagent"
)

// fakeHands records what the API asked the worker for, and can fail on demand.
type fakeHands struct {
	calls   int
	cluster string
	env     json.RawMessage
	members []json.RawMessage
	name    string
	err     error
}

func (f *fakeHands) EnsureEnv(context.Context, string, managedagent.DerivedEnv) (bool, string, error) {
	return true, "", nil
}

func (f *fakeHands) CreateEnv(
	_ context.Context, clusterID string, env json.RawMessage, members []json.RawMessage,
) (string, error) {
	f.calls++
	f.cluster, f.env, f.members = clusterID, env, members
	if f.err != nil {
		return "", f.err
	}
	return f.name, nil
}

func newManagedAgentAPI(t *testing.T, hands managedagent.HandsProvisioner) *ManagedAgentAPI {
	t.Helper()
	sch := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(sch); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	if err := agentsv1alpha1.AddToScheme(sch); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	return &ManagedAgentAPI{
		Client:    fake.NewClientBuilder().WithScheme(sch).Build(),
		Scheme:    sch,
		Namespace: "agentbox-system",
		Hands:     hands,
	}
}

func postCreate(t *testing.T, api *ManagedAgentAPI, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/internal/managedagents", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	api.create(c)
	return rec
}

const minimalAgentSpec = `"spec":{"image":{"repository":"repo/brain"},` +
	`"runtime":{"default":"claude-code"},"hands":{}}`

// The env is created first and the agent is pointed at it, so a console can set
// up an agent and its sandbox supply in one request.
func TestCreateWithSandboxEnvCreatesEnvFirstAndReferencesIt(t *testing.T) {
	hands := &fakeHands{name: "navix-hands"}
	api := newManagedAgentAPI(t, hands)

	rec := postCreate(t, api, `{"name":"navix",`+minimalAgentSpec+`,
		"sandboxEnv":{"clusterID":"gw-a","env":{"name":"navix-hands","templateRef":{"name":"tpl"}},
		"members":[{"instanceType":"1c2gi"}]}}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if hands.calls != 1 || hands.cluster != "gw-a" {
		t.Fatalf("CreateEnv called %d time(s) for cluster %q", hands.calls, hands.cluster)
	}
	// The worker's own request body must arrive unchanged — the console offers the
	// whole env API through it, so anything this layer re-serialised would drift.
	if !strings.Contains(string(hands.env), `"templateRef"`) {
		t.Errorf("env body was not forwarded verbatim: %s", hands.env)
	}
	if len(hands.members) != 1 {
		t.Errorf("members forwarded = %d, want 1", len(hands.members))
	}

	ma := &agentsv1alpha1.ManagedAgent{}
	if err := api.Client.Get(context.Background(),
		client.ObjectKey{Namespace: "agentbox-system", Name: "navix"}, ma); err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if ma.Spec.Hands.EnvRef == nil || ma.Spec.Hands.EnvRef.Name != "navix-hands" {
		t.Fatalf("hands.envRef = %+v, want the env just created", ma.Spec.Hands.EnvRef)
	}
	if ma.Spec.Hands.EnvRef.ClusterID != "gw-a" {
		t.Errorf("hands.envRef.clusterID = %q", ma.Spec.Hands.EnvRef.ClusterID)
	}
	// The agent stores no credential or rule data: the env is the only writer of
	// that, and a copy here would be a second source of truth in etcd.
	if raw, _ := json.Marshal(ma.Spec); strings.Contains(string(raw), "secretInjection") {
		t.Errorf("agent spec carries injection config: %s", raw)
	}
}

// A failed env leaves no agent behind. An agent whose env creation failed would
// still be published and would fail every tool call instead.
func TestCreateWithSandboxEnvFailureLeavesNoAgent(t *testing.T) {
	hands := &fakeHands{err: errors.New("worker rejected the template")}
	api := newManagedAgentAPI(t, hands)

	rec := postCreate(t, api, `{"name":"navix",`+minimalAgentSpec+`,
		"sandboxEnv":{"clusterID":"gw-a","env":{"name":"navix-hands"}}}`)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "worker rejected the template") {
		t.Errorf("the worker's reason must reach the caller: %s", rec.Body.String())
	}
	list := &agentsv1alpha1.ManagedAgentList{}
	if err := api.Client.List(context.Background(), list); err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Items) != 0 {
		t.Errorf("agent was created anyway: %d item(s)", len(list.Items))
	}
}

// Without a provisioner there is no way to reach a worker's env API. Saying so
// beats creating an agent whose sandbox supply does not exist.
func TestCreateWithSandboxEnvNeedsAProvisioner(t *testing.T) {
	api := newManagedAgentAPI(t, nil)

	rec := postCreate(t, api, `{"name":"navix",`+minimalAgentSpec+`,
		"sandboxEnv":{"env":{"name":"navix-hands"}}}`)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "hands.envRef") {
		t.Errorf("the error should point at the alternative: %s", rec.Body.String())
	}
}

// A create without the field keeps working untouched.
func TestCreateWithoutSandboxEnvDoesNotTouchTheProvisioner(t *testing.T) {
	hands := &fakeHands{name: "unused"}
	api := newManagedAgentAPI(t, hands)

	rec := postCreate(t, api, `{"name":"navix",`+minimalAgentSpec+`}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if hands.calls != 0 {
		t.Errorf("CreateEnv called %d time(s) without a sandboxEnv", hands.calls)
	}
}

// Update refuses the field instead of ignoring it: a console that thought it had
// just rotated an injected credential here would be wrong, and silently.
func TestUpdateRejectsSandboxEnv(t *testing.T) {
	hands := &fakeHands{}
	api := newManagedAgentAPI(t, hands)
	ma := &agentsv1alpha1.ManagedAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "navix", Namespace: "agentbox-system"},
	}
	if err := api.Client.Create(context.Background(), ma); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Params = gin.Params{{Key: "name", Value: "navix"}}
	c.Request = httptest.NewRequest(http.MethodPut, "/internal/managedagents/navix",
		strings.NewReader(`{`+minimalAgentSpec+`,"sandboxEnv":{"env":{"name":"x"}}}`))
	c.Request.Header.Set("Content-Type", "application/json")
	api.update(c)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if hands.calls != 0 {
		t.Errorf("CreateEnv must not run on update (called %d time(s))", hands.calls)
	}
}
