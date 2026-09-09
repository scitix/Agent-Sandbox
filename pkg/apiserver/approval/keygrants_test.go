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
	"errors"
	"testing"
	"time"
)

// fakeKeys stands in for the credentials, holding what a Secret would.
type fakeKeys struct {
	// ops[keyID][operation] = who granted it
	ops    map[string]map[string]string
	owner  map[string]string // keyID -> "team/user"
	failOn string            // an operation whose write fails
}

// newFakeKeys holds one key, owned by the test's own principal.
func newFakeKeys() *fakeKeys {
	return &fakeKeys{
		ops:   map[string]map[string]string{},
		owner: map[string]string{testKeyID: testTeam + "/" + testUser},
	}
}

func (f *fakeKeys) GrantOperation(_ context.Context, keyID, operation, by string) error {
	if operation == f.failOn {
		return errors.New("secret is read-only")
	}
	if f.ops[keyID] == nil {
		f.ops[keyID] = map[string]string{}
	}
	f.ops[keyID][operation] = by
	return nil
}

func (f *fakeKeys) RevokeOperation(_ context.Context, keyID, operation string) error {
	delete(f.ops[keyID], operation)
	return nil
}

func (f *fakeKeys) KeyGrants(_ context.Context, team, user string) ([]KeyGrant, error) {
	out := []KeyGrant{}
	for keyID, ops := range f.ops {
		if f.owner[keyID] != team+"/"+user {
			continue
		}
		for op, by := range ops {
			out = append(out, KeyGrant{
				KeyID: keyID, Operation: op,
				GrantedAt: time.Now().UTC(), GrantedBy: by,
			})
		}
	}
	return out, nil
}

const (
	testKeyID = "agentbox-system/agentbox-apikey-abc"
	testTeam  = "acme"
	testUser  = "bob"
)

func testPrincipal() Principal {
	return Principal{KeyID: testKeyID, Team: testTeam, User: testUser}
}

// ask raises a pending request for `operation` and returns its id.
func ask(t *testing.T, s *Store, operation string, onceOnly bool) string {
	t.Helper()
	r := s.Challenge(Request{
		Principal: testPrincipal(),
		Operation: operation,
		OnceOnly:  onceOnly,
		Method:    "POST",
		Path:      "/v1/envs",
		Summary:   "Create an environment",
	})
	if r == nil {
		t.Fatal("no request was raised")
	}
	return r.ID
}

// The reason key grants are persisted at all: a permission given so that
// someone would STOP being asked must not be quietly undone by a rollout, which
// is most likely to happen during the long unattended run that needed it.
func TestAKeyGrantSurvivesARestart(t *testing.T) {
	ctx := context.Background()
	keys := newFakeKeys()

	before := NewStore(nil, keys)
	if _, err := before.Decide(ctx, ask(t, before, "env.create", false), true, ScopeKey, "bob@example.com"); err != nil {
		t.Fatalf("decide: %v", err)
	}
	if !before.Allow(testPrincipal(), nil, "", "env.create", "fp") {
		t.Fatal("the grant did not take effect immediately")
	}

	// A new process: everything the old one held in memory is gone. What the
	// caller presents is what its credential records.
	after := NewStore(nil, keys)
	recorded := opsOf(t, keys)
	if !after.Allow(testPrincipal(), recorded, "", "env.create", "fp") {
		t.Fatal("the grant did not survive the restart")
	}
	// And it is still only that one operation.
	if after.Allow(testPrincipal(), recorded, "", "env.delete", "fp") {
		t.Fatal("a grant for one operation authorised another")
	}
}

func opsOf(t *testing.T, keys *fakeKeys) []string {
	t.Helper()
	rows, err := keys.KeyGrants(context.Background(), testTeam, testUser)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	ops := make([]string, 0, len(rows))
	for _, r := range rows {
		ops = append(ops, r.Operation)
	}
	return ops
}

// A decision that could not be written down must not report success. Granting
// in memory alone looks identical to the person pressing the button and then
// stops working on the next rollout, with nothing to explain why.
func TestADecisionThatCannotBeRecordedIsRefused(t *testing.T) {
	ctx := context.Background()
	keys := newFakeKeys()
	keys.failOn = "env.create"
	s := NewStore(nil, keys)

	if _, err := s.Decide(ctx, ask(t, s, "env.create", false), true, ScopeKey, "bob@example.com"); err == nil {
		t.Fatal("a decision that could not be written down was reported as made")
	}
	if s.Allow(testPrincipal(), nil, "", "env.create", "fp") {
		t.Fatal("a refused decision still authorised the call")
	}
}

// Revoking has to reach the record, or the permission comes back on the next
// restart — the failure direction that matters, since the person revoking
// believes it is gone.
func TestRevokingReachesTheRecordAndTheMirror(t *testing.T) {
	ctx := context.Background()
	keys := newFakeKeys()
	s := NewStore(nil, keys)
	if _, err := s.Decide(ctx, ask(t, s, "env.create", false), true, ScopeKey, "bob@example.com"); err != nil {
		t.Fatalf("decide: %v", err)
	}

	_, grants := s.List(ctx, testPrincipal())
	if len(grants) != 1 {
		t.Fatalf("expected one standing permission, got %d", len(grants))
	}
	if !s.Revoke(ctx, testPrincipal(), grants[0].ID) {
		t.Fatal("revoke was refused")
	}
	if s.Allow(testPrincipal(), opsOf(t, keys), "", "env.create", "fp") {
		t.Fatal("the permission still works after being revoked")
	}
	if len(keys.ops[testKeyID]) != 0 {
		t.Fatal("the record was left behind; it would come back on restart")
	}
}

// The console lists what the KEYS say, not what this process happens to
// remember — otherwise a restart empties the page while the permissions are
// still in force, and there is no way to take one back.
func TestListShowsGrantsThisProcessNeverMade(t *testing.T) {
	ctx := context.Background()
	keys := newFakeKeys()
	if err := keys.GrantOperation(ctx, testKeyID, "pool.create", "someone@example.com"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	fresh := NewStore(nil, keys)
	_, grants := fresh.List(ctx, testPrincipal())
	if len(grants) != 1 || grants[0].Operation != "pool.create" {
		t.Fatalf("expected the recorded permission, got %+v", grants)
	}
	if grants[0].GrantedBy != "someone@example.com" {
		t.Fatalf("lost who granted it: %+v", grants[0])
	}
	if !fresh.Revoke(ctx, testPrincipal(), grants[0].ID) {
		t.Fatal("could not revoke a permission this process did not grant")
	}
}

// One permission, listed once. Both the record and the mirror describe the same
// grant, and showing it twice would offer two revoke buttons, one of which only
// does half the job.
func TestAKeyGrantIsListedOnce(t *testing.T) {
	ctx := context.Background()
	keys := newFakeKeys()
	s := NewStore(nil, keys)
	if _, err := s.Decide(ctx, ask(t, s, "env.create", false), true, ScopeKey, "bob@example.com"); err != nil {
		t.Fatalf("decide: %v", err)
	}
	if _, grants := s.List(ctx, testPrincipal()); len(grants) != 1 {
		t.Fatalf("expected one row, got %d", len(grants))
	}
}

// A grant id encodes the key it belongs to, so it is guessable by anyone who
// can read their own. Ownership is therefore checked, not inferred from the
// fact that the caller had an id at all.
func TestOneUserCannotRevokeAnotherUsersGrant(t *testing.T) {
	ctx := context.Background()
	keys := newFakeKeys()
	s := NewStore(nil, keys)
	if _, err := s.Decide(ctx, ask(t, s, "env.create", false), true, ScopeKey, "bob@example.com"); err != nil {
		t.Fatalf("decide: %v", err)
	}
	_, grants := s.List(ctx, testPrincipal())

	stranger := Principal{KeyID: "other/key", Team: "acme", User: "eve"}
	if s.Revoke(ctx, stranger, grants[0].ID) {
		t.Fatal("someone else revoked this person's permission")
	}
	if len(keys.ops[testKeyID]) != 1 {
		t.Fatal("the record was changed by someone who does not own it")
	}
}

// Destructive operations are never granted standing permission, whichever
// storage is behind it.
func TestAOnceOnlyOperationIsNeverRecorded(t *testing.T) {
	ctx := context.Background()
	keys := newFakeKeys()
	s := NewStore(nil, keys)
	if _, err := s.Decide(ctx, ask(t, s, "env.delete", true), true, ScopeKey, "bob@example.com"); err == nil {
		t.Fatal("a once-only operation was granted standing permission")
	}
	if len(keys.ops[testKeyID]) != 0 {
		t.Fatal("a once-only operation was written to the credential")
	}
}
