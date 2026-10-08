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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	apidomain "github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	e2bgen "github.com/scitix/agent-sandbox/pkg/e2bcompat/gen"
)

// newV2Router mounts the generated routes over srv, as the real server does,
// so a test exercises routing and body binding rather than calling a handler
// method the router might never reach. The missing route is exactly how the
// v2 SDKs failed: a gin 404 before any handler ran.
func newV2Router(srv *Server) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	e2bgen.RegisterHandlersWithOptions(r, e2bgen.NewStrictHandler(srv, nil), e2bgen.GinServerOptions{
		Middlewares: []e2bgen.MiddlewareFunc{func(c *gin.Context) {
			c.Set("auth", apidomain.AuthInfo{Namespace: "test-ns"})
		}},
	})
	return r
}

func postJSON(t *testing.T, r http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestPostV2Sandboxes_RoutedAndCreates(t *testing.T) {
	mock := &mockSandboxService{}
	r := newV2Router(&Server{sandbox: mock})

	timeout := int32(600)
	rec := postJSON(t, r, "/v2/sandboxes", e2bgen.NewSandboxV2{
		TemplateID: "my-env//docker.io/library/ubuntu:24.04",
		Timeout:    &timeout,
		Metadata:   &e2bgen.SandboxMetadata{"app": "x"},
		EnvVars:    &e2bgen.EnvVars{"FOO": "bar"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /v2/sandboxes = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var sb e2bgen.Sandbox
	if err := json.Unmarshal(rec.Body.Bytes(), &sb); err != nil {
		t.Fatalf("response is not an E2B Sandbox: %v", err)
	}
	if sb.SandboxID != "sbx-test" {
		t.Errorf("sandboxID = %q, want sbx-test", sb.SandboxID)
	}

	// Every field the v1 path consumes has to arrive through the v2 projection.
	in := mock.lastInput
	if in.PoolName != "my-env" || in.Image != "docker.io/library/ubuntu:24.04" {
		t.Errorf("template not parsed through v2: pool=%q image=%q", in.PoolName, in.Image)
	}
	if in.IdleTimeout != 600*time.Second {
		t.Errorf("timeout = %v, want 10m", in.IdleTimeout)
	}
	if in.Metadata["app"] != "x" {
		t.Errorf("metadata dropped: %v", in.Metadata)
	}
	if len(in.PostStartHooks) != 1 {
		t.Errorf("envVars did not become an init hook: %+v", in.PostStartHooks)
	}
}

// The v1 refusals have to hold on v2 too: the adapter shares the validation,
// and its status mapping must carry the 400 rather than collapsing it to 500.
func TestPostV2Sandboxes_KeepsV1Refusals(t *testing.T) {
	r := newV2Router(&Server{sandbox: &mockSandboxService{}})

	autoPause := true
	for name, body := range map[string]e2bgen.NewSandboxV2{
		"missing template": {},
		"autoPause":        {TemplateID: "my-env", AutoPause: &autoPause},
	} {
		rec := postJSON(t, r, "/v2/sandboxes", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", name, rec.Code, rec.Body.String())
		}
	}
}

func TestPostV2SandboxesConnect_RoutedAndAppliesTimeout(t *testing.T) {
	sb := mkSandbox("sbx-1", nil)
	svc := &mockConnectService{sandbox: &sb}
	r := newV2Router(&Server{sandbox: svc})

	timeout := int32(120)
	rec := postJSON(t, r, "/v2/sandboxes/sbx-1/connect", e2bgen.ConnectSandboxV2{Timeout: &timeout})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /v2/sandboxes/{id}/connect = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if svc.timeoutHit != 1 || svc.timeoutSet != 120*time.Second {
		t.Errorf("timeout applied %d time(s) as %v; want once as 2m", svc.timeoutHit, svc.timeoutSet)
	}
}

// v2 makes the timeout optional. Omitting it must leave the deadline alone,
// the same as v1 with no positive timeout.
func TestPostV2SandboxesConnect_OmittedTimeoutLeavesDeadlineAlone(t *testing.T) {
	sb := mkSandbox("sbx-2", nil)
	svc := &mockConnectService{sandbox: &sb}
	r := newV2Router(&Server{sandbox: svc})

	rec := postJSON(t, r, "/v2/sandboxes/sbx-2/connect", map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if svc.timeoutHit != 0 {
		t.Errorf("SetTimeout called %d time(s) without a timeout in the body", svc.timeoutHit)
	}
}

func TestPostV2SandboxesConnect_MissingSandboxIs404(t *testing.T) {
	svc := &mockConnectService{liveErr: apidomain.NewNotFound(`sandbox "sbx-gone" not found`)}
	r := newV2Router(&Server{sandbox: svc})

	rec := postJSON(t, r, "/v2/sandboxes/sbx-gone/connect", map[string]any{})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 so the SDK raises sandbox-not-found: %s", rec.Code, rec.Body.String())
	}
}

// Cross-cluster forwarding replays the request path on the target cluster, so
// while the v1 handler runs it must see the v1 path (served by every release),
// and the caller's path must be back once it returns.
func TestAsV1Path_RewritesForTheCallAndRestores(t *testing.T) {
	for in, want := range map[string]string{
		"/v2/sandboxes":                       "/sandboxes",
		"/v2/sandboxes/sbx-1/connect":         "/sandboxes/sbx-1/connect",
		"/agent-sandbox/api/e2b/v2/sandboxes": "/agent-sandbox/api/e2b/sandboxes",
	} {
		gc, _ := gin.CreateTestContext(httptest.NewRecorder())
		gc.Request = httptest.NewRequest(http.MethodPost, in+"?x=1", nil)

		var seen string
		asV1Path(gc, func() { seen = gc.Request.URL.RequestURI() })

		if seen != want+"?x=1" {
			t.Errorf("%s: handler saw %q, want %q", in, seen, want+"?x=1")
		}
		if got := gc.Request.URL.Path; got != in {
			t.Errorf("%s: path not restored, got %q", in, got)
		}
	}
}

// blockingCreateService waits in Create until its context ends, as the real
// one does while a claim or arming is pending, and reports whether that end
// was a cancellation.
type blockingCreateService struct {
	mockSandboxService
	sawCancel chan bool
}

func (b *blockingCreateService) Create(ctx context.Context, _ service.CreateSandboxInput) (*gen.Sandbox, *apidomain.AppError) {
	select {
	case <-ctx.Done():
		b.sawCancel <- true
		return nil, apidomain.NewInternal("request canceled by client", ctx.Err())
	case <-time.After(3 * time.Second):
		b.sawCancel <- false
		return &gen.Sandbox{SandboxId: "sbx-late"}, nil
	}
}

// A caller that gives up on create must reach the service as a cancellation.
// Without it the claim runs to completion for nobody: the sandbox is armed and
// held, and its ID never reaches anyone who could use or delete it.
func TestPostV2Sandboxes_ClientDisconnectCancelsCreate(t *testing.T) {
	svc := &blockingCreateService{sawCancel: make(chan bool, 1)}
	srv := httptest.NewServer(newV2Router(&Server{sandbox: svc}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	b, _ := json.Marshal(e2bgen.NewSandboxV2{TemplateID: "my-env"})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/v2/sandboxes", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if resp, err := http.DefaultClient.Do(req); err == nil {
		_ = resp.Body.Close()
	}

	if !<-svc.sawCancel {
		t.Fatal("create kept running after the client disconnected")
	}
}
