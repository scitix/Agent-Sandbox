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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/apiserver/router/middleware"
	"github.com/scitix/agent-sandbox/pkg/controllers/managedagent"
	"github.com/scitix/agent-sandbox/pkg/wsproxy/config"
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

// The console reaches an agent through the internal API, which means the two
// things that make that safe have to hold: tenant scoping, and not depending on
// the agent being published to the internet.
//
// Driven through a REAL server rather than a synthetic gin context, because the
// proxy streams: it needs a ResponseWriter that a recorder does not implement, and
// a test that skips that is testing a different code path from production.
func consoleProxyServer(t *testing.T, api *ManagedAgentAPI, user string) *httptest.Server {
	t.Helper()
	api.Gateway = NewManagedAgentGateway(api.Client, api.Namespace)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/internal", func(c *gin.Context) {
		c.Set(middleware.AuthContextKey, domain.AuthInfo{User: user})
	})
	api.RegisterManagedAgentRoutes(g)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func seedAgent(t *testing.T, api *ManagedAgentAPI, name, owner string) {
	t.Helper()
	agent := &agentsv1alpha1.ManagedAgent{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: api.Namespace},
		Spec: agentsv1alpha1.ManagedAgentSpec{
			// No spec.ingress at all: deliberately NOT published.
			Owner: &agentsv1alpha1.ManagedAgentOwner{User: owner},
		},
	}
	if err := api.Client.Create(context.Background(), agent); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
}

func TestConsoleProxyReachesTheBrainWithoutPublishing(t *testing.T) {
	api := newManagedAgentAPI(t, nil)
	seedAgent(t, api, "alpha", "alice")
	srv := consoleProxyServer(t, api, "alice")

	// The Brain's Service does not resolve in a unit test, so the proxy answers
	// with its own unreachable error. That is the right assertion anyway: it
	// proves the request was FORWARDED rather than refused, which is what
	// distinguishes "the console may talk to this agent" from "it may not".
	res, err := srv.Client().Get(srv.URL + "/internal/managedagents/alpha/proxy/threads")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(res.Body)

	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (forwarded then unreachable); body: %s",
			res.StatusCode, body)
	}
	if !strings.Contains(string(body), "agent is unreachable") {
		t.Errorf("body = %s, want the proxy's unreachable error", body)
	}
}

// Another tenant's agent is absent, not forbidden — the same rule every other
// route here follows, so the proxy cannot become the one way to discover which
// agents someone else owns.
func TestConsoleProxyHidesAnotherTenantsAgent(t *testing.T) {
	api := newManagedAgentAPI(t, nil)
	seedAgent(t, api, "beta", "alice")
	srv := consoleProxyServer(t, api, "bob")

	res, err := srv.Client().Get(srv.URL + "/internal/managedagents/beta/proxy/threads")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for another tenant's agent", res.StatusCode)
	}
}

// The browser must not be able to name which end user it is. The gateway behind
// this takes the caller's word for it by default, so if the request's own value
// survived, any console user could read another's conversations by editing a query
// string. Asserted on the wire, against a stub Brain, because the pin is only worth
// anything if it is what actually arrives.
func TestConsoleProxyPinsTheEndUserAgainstTheRequest(t *testing.T) {
	var got http.Header
	brain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer brain.Close()

	api := newManagedAgentAPI(t, nil)
	seedAgent(t, api, "gamma", "alice")
	srv := consoleProxyServer(t, api, "alice")
	// Point the proxy at the stub instead of a Service that does not resolve.
	api.Gateway.targetHost = strings.TrimPrefix(brain.URL, "http://")

	req, err := http.NewRequest(http.MethodGet,
		srv.URL+"/internal/managedagents/gamma/proxy/threads?userKey=bob", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	// Both channels the gateway would otherwise read, plus a forged pin.
	req.Header.Set("X-Agentbox-User", "carol")
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	if v := got.Get("X-Agentbox-User"); v != "alice" {
		t.Errorf("pinned user = %q, want the authenticated caller %q", v, "alice")
	}
}

// ─── Platform defaults ────────────────────────────────────────────────────────

func getDefaults(t *testing.T, api *ManagedAgentAPI) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/internal/managedagents/_defaults", nil)
	c.Params = gin.Params{{Key: "name", Value: defaultsName}}
	api.get(c)
	return rec.Code, rec.Body.String()
}

// The console needs the platform's answers to render a form the caller does not
// have to fill in — but never the credential itself, which has no reason to leave
// the namespace it is mounted in.
func TestDefaultsReportTheSupplyWithoutItsCredential(t *testing.T) {
	api := newManagedAgentAPI(t, nil)
	https := true
	api.Defaults = PlatformDefaults{
		BrainImage: agentsv1alpha1.ManagedAgentImage{Repository: "reg/brain", Tag: "v1"},
		Hands: &agentsv1alpha1.ManagedAgentHands{
			External: &agentsv1alpha1.HandsExternal{
				APIURL:  "https://console.example.com/api/e2b",
				Domain:  "console.example.com/api/data",
				HTTPS:   &https,
				EnvName: "navix",
				Image:   "reg/e2b-sandbox:v1",
				CredentialsRef: &agentsv1alpha1.SecretKeySelector{
					Name: "platform-sandbox", Key: "E2B_API_KEY",
				},
			},
		},
		ModelProvider: config.ModelProviderDefaults{
			BaseURL: "https://api.example.com/model-api",
			Models:  []agentsv1alpha1.ManagedAgentModel{{ID: "claude-sonnet-5"}},
		},
	}

	code, body := getDefaults(t, api)
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", code, body)
	}
	for _, want := range []string{
		`"repository":"reg/brain"`, `"envName":"navix"`,
		`"image":"reg/e2b-sandbox:v1"`, `"credentialConfigured":true`,
		`"baseURL":"https://api.example.com/model-api"`, `"claude-sonnet-5"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("response is missing %s: %s", want, body)
		}
	}
	// Not "no key was set in this test" — the Secret's name and key are the shape
	// of a credential and have no business in a response a browser reads.
	for _, leaked := range []string{"platform-sandbox", "E2B_API_KEY", "credentialsRef"} {
		if strings.Contains(body, leaked) {
			t.Errorf("response leaked %q: %s", leaked, body)
		}
	}
}

// A deployment that publishes nothing answers with an empty object rather than an
// error: "no defaults" is a valid configuration, and the console renders the full
// form for it.
func TestDefaultsAreEmptyWhenNothingIsConfigured(t *testing.T) {
	api := newManagedAgentAPI(t, nil)
	code, body := getDefaults(t, api)
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", code, body)
	}
	if strings.TrimSpace(body) != "{}" {
		t.Errorf("body = %s, want {}", body)
	}
}

// A default whose credential is missing must be visibly incomplete: sandboxes
// created against it fail to start, and the control plane looks healthy while it
// happens.
func TestDefaultsReportAMissingCredential(t *testing.T) {
	api := newManagedAgentAPI(t, nil)
	api.Defaults = PlatformDefaults{
		Hands: &agentsv1alpha1.ManagedAgentHands{
			External: &agentsv1alpha1.HandsExternal{
				APIURL: "https://console.example.com/api/e2b", EnvName: "navix",
			},
		},
	}
	_, body := getDefaults(t, api)
	if !strings.Contains(body, `"credentialConfigured":false`) {
		t.Errorf("body = %s, want credentialConfigured false", body)
	}
}

// The reserved name cannot shadow an agent, because no agent may be called it: an
// underscore is invalid in the DNS-1123 label every agent name has to be.
func TestDefaultsNameIsNotAValidAgentName(t *testing.T) {
	if errs := validation.IsDNS1123Label(defaultsName); len(errs) == 0 {
		t.Fatalf("%q is a legal agent name, so it can hide one", defaultsName)
	}
}
