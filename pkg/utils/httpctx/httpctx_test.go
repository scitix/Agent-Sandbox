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

package httpctx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
)

// serveAndDisconnect runs handler behind a real listener, sends one request
// and drops the connection after 100ms, reporting whether the handler saw the
// client leave. A real socket matters: a test that cancels a context by hand
// cannot reproduce the bug, because it never goes through gin's Done().
func serveAndDisconnect(t *testing.T, wrap func(context.Context) context.Context) (sawCancel bool, auth domain.AuthInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New() // ContextWithFallback off, as in the real servers
	type result struct {
		cancelled bool
		auth      domain.AuthInfo
	}
	out := make(chan result, 1)
	r.POST("/create", func(c *gin.Context) {
		c.Set("auth", domain.AuthInfo{Namespace: "ns-a", User: "alice"})
		var ctx context.Context = c // what the strict server passes
		ctx = wrap(ctx)
		select {
		case <-ctx.Done():
			out <- result{cancelled: true, auth: AuthFrom(ctx)}
		case <-time.After(3 * time.Second):
			out <- result{cancelled: false, auth: AuthFrom(ctx)}
		}
	})
	srv := httptest.NewServer(r)
	defer srv.Close()

	cctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(cctx, http.MethodPost, srv.URL+"/create", nil)
	if resp, err := http.DefaultClient.Do(req); err == nil {
		_ = resp.Body.Close()
	}
	res := <-out
	return res.cancelled, res.auth
}

// The defect being guarded: the bare *gin.Context never reports a client
// that left. If gin ever changes that default this test will say so, and
// WithClientCancel can be revisited.
func TestGinContext_DoesNotSeeClientDisconnect(t *testing.T) {
	if cancelled, _ := serveAndDisconnect(t, func(c context.Context) context.Context { return c }); cancelled {
		t.Fatal("bare *gin.Context reported the disconnect; gin's default changed")
	}
}

func TestWithClientCancel_SeesClientDisconnect(t *testing.T) {
	cancelled, auth := serveAndDisconnect(t, WithClientCancel)
	if !cancelled {
		t.Fatal("handler kept waiting after the client disconnected")
	}
	// Values must still come from the gin context: the create path reads
	// the caller's identity from it after the wait.
	if auth.Namespace != "ns-a" || auth.User != "alice" {
		t.Fatalf("auth lost through the wrapper: %+v", auth)
	}
}

func TestWithClientCancel_KeepsGinContextReachable(t *testing.T) {
	gc, _ := gin.CreateTestContext(httptest.NewRecorder())
	gc.Request = httptest.NewRequest(http.MethodPost, "/create", nil)
	if got := GinFromCtx(WithClientCancel(gc)); got != gc {
		t.Fatal("GinFromCtx must still find the *gin.Context (cross-cluster forwarding uses it)")
	}
}

func TestWithClientCancel_PassesThroughNonGinContext(t *testing.T) {
	ctx := context.Background()
	if WithClientCancel(ctx) != ctx {
		t.Fatal("a context without gin must be returned unchanged")
	}
}
