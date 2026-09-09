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

package approval

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// A router with the gate in front of a handler that echoes the body it
// received. The echo is the point of several cases below: the gate reads the
// body to fingerprint it, and a handler that then finds an empty body is the
// failure this would otherwise ship with.
func newGatedRouter(s *Store, gatedCaller bool) (*gin.Engine, *[]string) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	var seen []string

	identity := func(*gin.Context) Identity {
		return Identity{Gated: gatedCaller, Principal: alice}
	}
	mw := New(s, identity, func(id string) string {
		return "https://console.example.com/approvals/" + id
	})

	handler := func(c *gin.Context) {
		b, _ := io.ReadAll(c.Request.Body)
		seen = append(seen, string(b))
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}

	r.POST("/v1/envs", mw, handler)
	r.GET("/v1/envs", mw, handler)
	r.DELETE("/v1/envs/:name", mw, handler)
	r.POST("/v1/unlisted", mw, handler)
	// The E2B surface, mounted with no /v1 prefix exactly as the real one is.
	r.POST("/sandboxes", mw, handler)
	return r, &seen
}

func post(r *gin.Engine, method, path, body, session string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if session != "" {
		req.Header.Set(SessionHeader, session)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestGatedWriteIsRefusedWithSomethingActionable(t *testing.T) {
	s, _ := newTestStore()
	r, _ := newGatedRouter(s, true)

	w := post(r, "POST", "/v1/envs", `{"name":"foo"}`, "s1")
	if w.Code != StatusApprovalRequired {
		t.Fatalf("expected %d, got %d: %s", StatusApprovalRequired, w.Code, w.Body)
	}

	var body struct {
		ErrorCode string `json:"errorCode"`
		Detail    Detail `json:"detail"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.ErrorCode != string(BizErrApprovalRequired) {
		t.Fatalf("errorCode = %q", body.ErrorCode)
	}
	// Everything a client needs to act without knowing the catalogue.
	if body.Detail.ApprovalID == "" || body.Detail.PollURL == "" || body.Detail.URL == "" {
		t.Fatalf("detail is missing an actionable field: %+v", body.Detail)
	}
	if body.Detail.Operation != "env.create" {
		t.Fatalf("operation = %q", body.Detail.Operation)
	}
}

func TestTheHandlerStillSeesItsBody(t *testing.T) {
	// The gate consumes the body to fingerprint it. If it does not put it back,
	// every gated create reaches its handler as an empty document — and does so
	// only once approval is granted, which is the worst possible time to find
	// out.
	s, _ := newTestStore()
	r, seen := newGatedRouter(s, true)

	post(r, "POST", "/v1/envs", `{"name":"foo"}`, "s1")
	pending, _ := s.List(context.Background(), alice)
	if len(pending) != 1 {
		t.Fatalf("expected a pending request, got %d", len(pending))
	}
	if _, err := s.Decide(context.Background(), pending[0].ID, true, ScopeOnce, "alice"); err != nil {
		t.Fatalf("decide: %v", err)
	}

	w := post(r, "POST", "/v1/envs", `{"name":"foo"}`, "s1")
	if w.Code != http.StatusOK {
		t.Fatalf("expected the approved retry to pass, got %d: %s", w.Code, w.Body)
	}
	if len(*seen) != 1 || (*seen)[0] != `{"name":"foo"}` {
		t.Fatalf("handler saw %#v", *seen)
	}
}

func TestUngatedCallerPassesStraightThrough(t *testing.T) {
	// A console session. The click IS the approval.
	s, _ := newTestStore()
	r, seen := newGatedRouter(s, false)
	if w := post(r, "POST", "/v1/envs", `{"name":"foo"}`, ""); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(*seen) != 1 {
		t.Fatalf("handler should have run, saw %#v", *seen)
	}
	if pending, _ := s.List(context.Background(), alice); len(pending) != 0 {
		t.Fatal("an ungated caller must not create approval requests")
	}
}

func TestReadsAndUnlistedRoutesPassThrough(t *testing.T) {
	s, _ := newTestStore()
	r, _ := newGatedRouter(s, true)

	if w := post(r, "GET", "/v1/envs", "", "s1"); w.Code != http.StatusOK {
		t.Fatalf("a read must not be gated, got %d", w.Code)
	}
	// A write on a route that is not in the catalogue: ungated by construction,
	// so that adding an endpoint does not silently start prompting people.
	if w := post(r, "POST", "/v1/unlisted", `{}`, "s1"); w.Code != http.StatusOK {
		t.Fatalf("an unlisted write must not be gated, got %d", w.Code)
	}
	// The E2B create, with a gated caller: still ungated. Real E2B client code
	// runs against this surface and has never heard of a 428.
	if w := post(r, "POST", "/sandboxes", `{"templateID":"x"}`, "s1"); w.Code != http.StatusOK {
		t.Fatalf("the E2B surface must not be gated, got %d", w.Code)
	}
}

func TestTheSummaryNamesWhatIsBeingActedOn(t *testing.T) {
	// "Delete an environment" is not a question anyone can answer.
	s, _ := newTestStore()
	r, _ := newGatedRouter(s, true)

	w := post(r, "DELETE", "/v1/envs/prod", "", "s1")
	if w.Code != StatusApprovalRequired {
		t.Fatalf("expected a challenge, got %d", w.Code)
	}
	var body struct {
		Detail Detail `json:"detail"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(body.Detail.Summary, "prod") {
		t.Fatalf("summary should name the target, got %q", body.Detail.Summary)
	}
	// And it must tell the client that "for this session" is not on offer here.
	if !body.Detail.OnceOnly {
		t.Fatal("a delete must be reported as once-only")
	}
}

func TestSessionGrantLetsLaterCallsThrough(t *testing.T) {
	s, _ := newTestStore()
	r, _ := newGatedRouter(s, true)

	post(r, "POST", "/v1/envs", `{"name":"a"}`, "s1")
	pending, _ := s.List(context.Background(), alice)
	if _, err := s.Decide(context.Background(), pending[0].ID, true, ScopeSession, "alice"); err != nil {
		t.Fatalf("decide: %v", err)
	}

	// A DIFFERENT body: covered, because the grant is about the operation.
	if w := post(r, "POST", "/v1/envs", `{"name":"b"}`, "s1"); w.Code != http.StatusOK {
		t.Fatalf("expected the session grant to cover this, got %d", w.Code)
	}
	// A delete in the same session: still asked, because deletes are once-only.
	if w := post(r, "DELETE", "/v1/envs/a", "", "s1"); w.Code != StatusApprovalRequired {
		t.Fatalf("a delete must still be challenged, got %d", w.Code)
	}
}
