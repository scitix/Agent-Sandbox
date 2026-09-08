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
	"testing"
	"time"
)

// A store with a clock the test owns. Every case here is about time or about
// who may act, and both are unreadable through a real clock.
func newTestStore() (*Store, *time.Time) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s := &Store{
		requests: map[string]*Request{},
		grants:   map[string]*Grant{},
	}
	s.now = func() time.Time { return now }
	return s, &now
}

var alice = Principal{Team: "t1", User: "alice", KeyID: "ns/key-a"}

func challenge(s *Store, p Principal, session, op, fp string, onceOnly bool) *Request {
	return s.Challenge(Request{
		Principal: p, SessionID: session, Operation: op,
		Fingerprint: fp, OnceOnly: onceOnly, Summary: op,
	})
}

func TestOnceApprovalAuthorisesExactlyOneCall(t *testing.T) {
	s, _ := newTestStore()
	r := challenge(s, alice, "s1", "env.create", "fp-foo", false)

	if s.Allow(alice, "s1", "env.create", "fp-foo") {
		t.Fatal("a pending request must not authorise anything")
	}
	if _, err := s.Decide(r.ID, true, ScopeOnce, "alice"); err != nil {
		t.Fatalf("decide: %v", err)
	}
	if !s.Allow(alice, "s1", "env.create", "fp-foo") {
		t.Fatal("the approved call must go through")
	}
	// The point of `once`: the retry it authorised is the only one.
	if s.Allow(alice, "s1", "env.create", "fp-foo") {
		t.Fatal("a once approval must not authorise a second call")
	}
}

func TestOnceApprovalDoesNotCoverADifferentCall(t *testing.T) {
	// Approving "create the environment called foo" must not authorise
	// "create the environment called bar" — which is the whole reason the
	// fingerprint includes the body.
	s, _ := newTestStore()
	r := challenge(s, alice, "s1", "env.create", "fp-foo", false)
	if _, err := s.Decide(r.ID, true, ScopeOnce, "alice"); err != nil {
		t.Fatalf("decide: %v", err)
	}
	if s.Allow(alice, "s1", "env.create", "fp-bar") {
		t.Fatal("a different request must not be authorised")
	}
}

func TestSessionGrantCoversTheSessionAndNothingElse(t *testing.T) {
	s, _ := newTestStore()
	r := challenge(s, alice, "s1", "sandbox.create", "fp-1", false)
	if _, err := s.Decide(r.ID, true, ScopeSession, "alice"); err != nil {
		t.Fatalf("decide: %v", err)
	}

	// Same session, a call that was never challenged: covered.
	if !s.Allow(alice, "s1", "sandbox.create", "fp-anything-else") {
		t.Fatal("the session grant must cover later calls of the same operation")
	}
	// Another session of the same person: not covered.
	if s.Allow(alice, "s2", "sandbox.create", "fp-1") {
		t.Fatal("a session grant must not leak into another session")
	}
	// Another operation in the same session: not covered.
	if s.Allow(alice, "s1", "env.delete", "fp-1") {
		t.Fatal("a session grant must not cover a different operation")
	}
	// Another person who happens to use the same session id: not covered.
	bob := Principal{Team: "t1", User: "bob", KeyID: "ns/key-b"}
	if s.Allow(bob, "s1", "sandbox.create", "fp-1") {
		t.Fatal("a session grant must not cross owners")
	}
}

func TestSessionGrantExpires(t *testing.T) {
	s, now := newTestStore()
	r := challenge(s, alice, "s1", "sandbox.create", "fp-1", false)
	if _, err := s.Decide(r.ID, true, ScopeSession, "alice"); err != nil {
		t.Fatalf("decide: %v", err)
	}
	*now = now.Add(SessionGrantTTL - time.Minute)
	if !s.Allow(alice, "s1", "sandbox.create", "fp-1") {
		t.Fatal("still inside the window")
	}
	*now = now.Add(2 * time.Minute)
	if s.Allow(alice, "s1", "sandbox.create", "fp-1") {
		t.Fatal("a session grant must not outlive its window")
	}
}

func TestKeyGrantIsBoundToTheKey(t *testing.T) {
	s, _ := newTestStore()
	r := challenge(s, alice, "", "pool.create", "fp-1", false)
	if _, err := s.Decide(r.ID, true, ScopeKey, "alice"); err != nil {
		t.Fatalf("decide: %v", err)
	}
	if !s.Allow(alice, "", "pool.create", "fp-2") {
		t.Fatal("the key grant must cover this key")
	}
	// The same person's OTHER key is a separate credential — revoking one must
	// not be undone by the other still carrying the grant.
	other := Principal{Team: "t1", User: "alice", KeyID: "ns/key-z"}
	if s.Allow(other, "", "pool.create", "fp-2") {
		t.Fatal("a key grant must not cover another key")
	}
}

func TestNoSessionHeaderCannotBecomeASessionGrant(t *testing.T) {
	// Otherwise "for this session" silently means "for everyone who also sent
	// no session id".
	s, _ := newTestStore()
	r := challenge(s, alice, "", "sandbox.create", "fp-1", false)
	if _, err := s.Decide(r.ID, true, ScopeSession, "alice"); err == nil {
		t.Fatal("a session grant without a session must be refused")
	}
}

func TestDestructiveOperationsAreOnceOnly(t *testing.T) {
	s, _ := newTestStore()
	for _, scope := range []Scope{ScopeSession, ScopeKey} {
		r := challenge(s, alice, "s1", "env.delete", "fp-"+string(scope), true)
		if _, err := s.Decide(r.ID, true, scope, "alice"); err == nil {
			t.Fatalf("scope %s must be refused for a once-only operation", scope)
		}
		// The refusal must leave the request answerable, not wedge it.
		if got, _ := s.Get(r.ID); got.Status != StatusPending {
			t.Fatalf("after a refused scope the request should still be pending, got %s", got.Status)
		}
		if _, err := s.Decide(r.ID, true, ScopeOnce, "alice"); err != nil {
			t.Fatalf("once must still be allowed: %v", err)
		}
	}
}

func TestRetryingTheSameCallReusesOneRequest(t *testing.T) {
	// A client that polls and retries would otherwise mint an approval per
	// attempt and bury the one the person is looking at.
	s, _ := newTestStore()
	a := challenge(s, alice, "s1", "env.create", "fp-foo", false)
	b := challenge(s, alice, "s1", "env.create", "fp-foo", false)
	if a.ID != b.ID {
		t.Fatalf("expected the same request back, got %s and %s", a.ID, b.ID)
	}
	pending, _ := s.List(alice)
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending request, got %d", len(pending))
	}
}

func TestDeniedAndExpiredNeverAuthorise(t *testing.T) {
	s, now := newTestStore()

	denied := challenge(s, alice, "s1", "env.create", "fp-d", false)
	if _, err := s.Decide(denied.ID, false, ScopeOnce, "alice"); err != nil {
		t.Fatalf("decide: %v", err)
	}
	if s.Allow(alice, "s1", "env.create", "fp-d") {
		t.Fatal("a denied request must not authorise")
	}

	stale := challenge(s, alice, "s1", "env.create", "fp-e", false)
	*now = now.Add(RequestTTL + time.Minute)
	if got, _ := s.Get(stale.ID); got.Status != StatusExpired {
		t.Fatalf("expected expired, got %s", got.Status)
	}
	if _, err := s.Decide(stale.ID, true, ScopeOnce, "alice"); err == nil {
		t.Fatal("an expired request must not be decidable")
	}
}

func TestApprovedOnceDoesNotSurviveItsRequestWindow(t *testing.T) {
	// The approval is a licence to retry, not a licence to come back tomorrow.
	s, now := newTestStore()
	r := challenge(s, alice, "s1", "env.create", "fp-foo", false)
	if _, err := s.Decide(r.ID, true, ScopeOnce, "alice"); err != nil {
		t.Fatalf("decide: %v", err)
	}
	*now = now.Add(RequestTTL + time.Minute)
	if s.Allow(alice, "s1", "env.create", "fp-foo") {
		t.Fatal("an approval must not authorise after its request has expired")
	}
}

func TestListAndRevoke(t *testing.T) {
	s, _ := newTestStore()
	r := challenge(s, alice, "s1", "sandbox.create", "fp-1", false)
	if _, err := s.Decide(r.ID, true, ScopeSession, "alice"); err != nil {
		t.Fatalf("decide: %v", err)
	}
	_, grants := s.List(alice)
	if len(grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(grants))
	}

	bob := Principal{Team: "t1", User: "bob"}
	if _, bobGrants := s.List(bob); len(bobGrants) != 0 {
		t.Fatal("grants must not be visible to another owner")
	}
	if s.Revoke(bob, grants[0].ID) {
		t.Fatal("another owner must not be able to revoke")
	}
	if !s.Revoke(alice, grants[0].ID) {
		t.Fatal("the owner must be able to revoke")
	}
	if s.Allow(alice, "s1", "sandbox.create", "fp-1") {
		t.Fatal("a revoked grant must stop authorising")
	}
}

func TestSweepDropsWhatHasExpired(t *testing.T) {
	s, now := newTestStore()
	challenge(s, alice, "s1", "env.create", "fp-1", false)
	*now = now.Add(RequestTTL + time.Minute)
	s.sweep()
	if len(s.requests) != 0 {
		t.Fatalf("expected the expired request to be collected, %d left", len(s.requests))
	}
}

func TestFingerprintSeparatesBodies(t *testing.T) {
	a := Fingerprint("POST", "/v1/envs", []byte(`{"name":"foo"}`))
	b := Fingerprint("POST", "/v1/envs", []byte(`{"name":"bar"}`))
	if a == b {
		t.Fatal("different bodies must fingerprint differently")
	}
	if a != Fingerprint("POST", "/v1/envs", []byte(`{"name":"foo"}`)) {
		t.Fatal("the same call must fingerprint the same")
	}
}

func TestOnlyTheNativeSurfaceIsGated(t *testing.T) {
	// The line the whole feature is drawn along. `abx` — the CLI an agent drives
	// to operate the platform — speaks the native API and nothing else, so
	// gating native is exactly gating the agent's reach. The E2B surface is a
	// third-party-compatible contract that real E2B client code is expected to
	// run against unchanged; a 428 no E2B client has heard of would break every
	// such program.
	if _, ok := Lookup("POST", "/v1/sandboxes"); !ok {
		t.Fatal("the native create should be gated")
	}
	for _, r := range []string{
		"POST /sandboxes",                    // the E2B SDK's create
		"DELETE /sandboxes/:sandboxID",       //   … and its delete
		"POST /sandboxes/:sandboxID/timeout", //   … and its timeout
		"PUT /sandboxes/:sandboxID/network",
		// The keepalive: fires on a timer for the life of every sandbox. Gating
		// it does not ask a question, it lets sandboxes die mid-run while the
		// person is reading a prompt about something else.
		"POST /sandboxes/:sandboxID/refreshes",
		"POST /sandboxes/:sandboxID/connect",
		// The platform arms a sandbox's own vault through these while starting
		// it; gating them deadlocks the start against an approval nobody can
		// grant yet.
		"POST /secrets", "POST /secrets/:secretID", "DELETE /secrets/:secretID",
	} {
		method, path, _ := splitRoute(r)
		if _, ok := Lookup(method, path); ok {
			t.Fatalf("%s is on the E2B surface and must not be gated", r)
		}
	}
}

func TestReadsAreNeverGated(t *testing.T) {
	if IsWrite("GET") || IsWrite("HEAD") || IsWrite("OPTIONS") {
		t.Fatal("reads must not count as writes")
	}
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		if !IsWrite(m) {
			t.Fatalf("%s should count as a write", m)
		}
	}
}

func splitRoute(s string) (method, path string, ok bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}
