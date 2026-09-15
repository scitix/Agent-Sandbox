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
	}, func(page string) string {
		return "https://console.example.com/" + page
	})

	handler := func(c *gin.Context) {
		b, _ := io.ReadAll(c.Request.Body)
		seen = append(seen, string(b))
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}

	r.POST("/v1/envs", mw, handler)
	r.GET("/v1/envs", mw, handler)
	r.PUT("/v1/envs/:name", mw, handler)
	r.DELETE("/v1/envs/:name", mw, handler)
	r.POST("/v1/unlisted", mw, handler)
	r.POST("/v1/api-keys", mw, handler)
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

// Two calls that act on two different objects are two questions.
//
// They used to be one: the fingerprint was taken over the route TEMPLATE, and a
// DELETE carries no body, so every delete from one credential produced the same
// fingerprint. The person was then shown whichever env asked first, and their
// answer released whatever the next retry happened to name.
func TestDifferentObjectsAreDifferentRequests(t *testing.T) {
	s, _ := newTestStore()
	r, _ := newGatedRouter(s, true)

	first := post(r, "DELETE", "/v1/envs/alpha", "", "s1")
	second := post(r, "DELETE", "/v1/envs/beta", "", "s1")

	if first.Code != StatusApprovalRequired || second.Code != StatusApprovalRequired {
		t.Fatalf("both deletes must be challenged, got %d and %d", first.Code, second.Code)
	}
	pending, _ := s.List(context.Background(), alice)
	if len(pending) != 2 {
		t.Fatalf("expected one request per object, got %d", len(pending))
	}
	paths := map[string]bool{}
	for _, p := range pending {
		paths[p.Path] = true
	}
	if !paths["/v1/envs/alpha"] || !paths["/v1/envs/beta"] {
		t.Fatalf("each request must name its own object, got %v", paths)
	}

	// And the same object twice is still one question: a client that retries
	// while it waits must not bury the request a person is looking at.
	again := post(r, "DELETE", "/v1/envs/alpha", "", "s1")
	var body struct {
		Detail Detail `json:"detail"`
	}
	if err := json.Unmarshal(again.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, p := range pending {
		if p.Path == "/v1/envs/alpha" && p.ID != body.Detail.ApprovalID {
			t.Fatalf("retrying the same call minted a second approval: %s vs %s", p.ID, body.Detail.ApprovalID)
		}
	}
}

// Updating an env is a write an agent credential must ask about. The catalogue
// pinned it to PATCH, a route the spec has not had for as long as the gate has
// existed, so this is the case that shipped as an open door.
func TestUpdatingAnEnvIsChallenged(t *testing.T) {
	s, _ := newTestStore()
	r, _ := newGatedRouter(s, true)

	w := post(r, "PUT", "/v1/envs/demo", `{"overrides":{"defaultIdleTimeout":"30m"}}`, "s1")
	if w.Code != StatusApprovalRequired {
		t.Fatalf("want %d for an env update, got %d: %s", StatusApprovalRequired, w.Code, w.Body)
	}
	var body struct {
		Detail Detail `json:"detail"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Detail.Operation != "env.update" {
		t.Fatalf("operation = %q", body.Detail.Operation)
	}
	// The path is the concrete one, so the approval names what it is about.
	if !strings.Contains(body.Detail.Summary, "demo") {
		t.Fatalf("summary should name the env, got %q", body.Detail.Summary)
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

// Issuing a credential is refused outright — no approval is created, because
// there is nothing here a person should be able to wave through from a prompt.
//
// An approval card reading "create an API key" does not say what is actually
// being decided, which is whether this agent may hold a credential without the
// restriction it is running under. So the answer is a link to the page where a
// person does it as themselves.
func TestMintingACredentialIsForbiddenRatherThanGated(t *testing.T) {
	s, _ := newTestStore()
	r, seen := newGatedRouter(s, true)

	w := post(r, http.MethodPost, "/v1/api-keys", `{"description":"x"}`, "")

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		ErrorCode string `json:"errorCode"`
		Detail    struct {
			URL        string `json:"url"`
			ApprovalID string `json:"approvalId"`
		} `json:"detail"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.ErrorCode != string(BizErrForbiddenForAgent) {
		t.Fatalf("want FORBIDDEN_FOR_AGENT so a client stops polling, got %q", body.ErrorCode)
	}
	if body.Detail.ApprovalID != "" {
		t.Fatalf("no approval should exist to wait for, got %q", body.Detail.ApprovalID)
	}
	if body.Detail.URL != "https://console.example.com/api-keys" {
		t.Fatalf("want the api-keys page, got %q", body.Detail.URL)
	}
	if len(*seen) != 0 {
		t.Fatalf("the handler must not have run, saw %v", *seen)
	}
	// And nothing is queued: a person opening the approvals page should not
	// find a request they cannot meaningfully answer.
	if pending, _ := s.List(context.Background(), alice); len(pending) != 0 {
		t.Fatalf("no approval should have been created, got %d", len(pending))
	}
}
