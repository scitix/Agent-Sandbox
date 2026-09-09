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

// Package approval holds writes made with an agent's credential until a human
// says yes.
//
// The gate is on the API, not in any one client. An agent driving `abx` from
// someone's laptop and an agent driving it from a sandbox this platform started
// are the same caller as far as this package is concerned: both arrive with a
// tenant API key, and the key is what says whether its holder may act
// unattended. Nothing here knows the assistant exists.
//
// A person clicking in the console is exempt, and that is the whole idea rather
// than an oversight: they authenticate with a session JWT, and the click IS the
// approval. Only credentials that can act while nobody is watching are gated.
package approval

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Scope is how widely one decision reaches.
type Scope string

const (
	// ScopeOnce authorises exactly the request that was refused, identified by
	// its fingerprint, and is spent the first time it is used.
	ScopeOnce Scope = "once"
	// ScopeSession authorises an operation for the rest of a session.
	ScopeSession Scope = "session"
	// ScopeKey authorises an operation for a credential until revoked. It
	// survives sessions, so it is the only scope a person can hold indefinitely
	// without noticing — hence the listing and revoke endpoints.
	ScopeKey Scope = "key"
)

// Status is where a request has got to.
type Status string

const (
	StatusPending  Status = "pending"
	StatusApproved Status = "approved"
	StatusDenied   Status = "denied"
	StatusExpired  Status = "expired"
)

const (
	// How long a refused request stays answerable. Short because the caller is
	// waiting: an approval nobody acted on should fall away rather than sit in
	// the list, and the client can always ask again.
	RequestTTL = 15 * time.Minute
	// How long a session-scoped grant lasts. Long enough to cover a working
	// session, short enough that walking away ends it.
	SessionGrantTTL = 2 * time.Hour

	gcEvery = 30 * time.Second
)

// Principal is who is asking. `KeyID` is part of the identity because a key
// grant belongs to one credential, not to everything the person owns: revoking
// a key must take its grants with it.
type Principal struct {
	Team  string `json:"team"`
	User  string `json:"user"`
	KeyID string `json:"keyId"`
}

func (p Principal) sameOwner(o Principal) bool {
	return p.Team == o.Team && p.User == o.User
}

// Request is one refused call, waiting for an answer.
type Request struct {
	ID        string    `json:"id"`
	Principal Principal `json:"principal"`
	// Empty when the caller sent no session header. Such a caller can only ever
	// be granted ScopeOnce — there is nothing to attach a longer grant to, and
	// inventing one would make "for this session" mean "forever".
	SessionID string `json:"sessionId,omitempty"`
	Operation string `json:"operation"`
	// OnceOnly refuses the wider scopes for this operation. Destructive and
	// credential-minting calls are approved one at a time or not at all: a
	// standing permission to delete is not a thing a person should be able to
	// grant by clicking the wrong button once.
	OnceOnly    bool      `json:"onceOnly"`
	Method      string    `json:"method"`
	Path        string    `json:"path"`
	Summary     string    `json:"summary"`
	Fingerprint string    `json:"-"`
	CreatedAt   time.Time `json:"createdAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
	Status      Status    `json:"status"`
	Scope       Scope     `json:"scope,omitempty"`
	DecidedBy   string    `json:"decidedBy,omitempty"`
	DecidedAt   time.Time `json:"decidedAt,omitzero"`

	// Set when a ScopeOnce approval has been spent. Kept rather than deleted so
	// a client that retries twice is told "already used" instead of being handed
	// a fresh challenge it cannot explain.
	consumed bool
}

// Grant is a standing permission.
type Grant struct {
	ID        string    `json:"id"`
	Principal Principal `json:"principal"`
	Scope     Scope     `json:"scope"`
	SessionID string    `json:"sessionId,omitempty"`
	Operation string    `json:"operation"`
	CreatedAt time.Time `json:"createdAt"`
	// Zero for ScopeKey: it ends when someone revokes it.
	ExpiresAt time.Time `json:"expiresAt,omitzero"`
	GrantedBy string    `json:"grantedBy"`
}

func (g Grant) live(now time.Time) bool {
	return g.ExpiresAt.IsZero() || now.Before(g.ExpiresAt)
}

// KeyGrantStore is where key-scoped approvals outlive this process.
//
// Only the KEY scope has one, and the asymmetry is deliberate. A once-approval
// is spent within minutes and a session grant dies with the session, so losing
// either to a restart costs one extra question. A key grant is the one a person
// gave in order to STOP being asked — losing it silently undoes the thing they
// asked for, and it goes missing exactly when it is being relied on, since a
// rollout is most disruptive during a long unattended run.
//
// Implemented by the API key store, because the key is where the permission
// belongs: a person can find it there, read who granted it, and take it back.
type KeyGrantStore interface {
	// GrantOperation records a standing approval on a key.
	GrantOperation(ctx context.Context, keyID, operation, by string) error
	// RevokeOperation withdraws one. Withdrawing an absent one is not an error.
	RevokeOperation(ctx context.Context, keyID, operation string) error
	// KeyGrants lists every standing approval held by this person's keys, so
	// the console can show and withdraw permissions this process did not itself
	// grant — which after a restart is all of them.
	KeyGrants(ctx context.Context, team, user string) ([]KeyGrant, error)
}

// KeyGrant is one standing permission as the credential records it.
type KeyGrant struct {
	KeyID     string
	Operation string
	GrantedAt time.Time
	GrantedBy string
}

// grantIDFor names a persisted grant reversibly.
//
// A persisted grant has no id of its own — it is a (key, operation) pair on a
// Secret — and this process cannot mint one that survives its own restart. So
// the id CARRIES the pair rather than pointing at a table: the console can
// revoke a permission it has only just read, on a server that has never seen it
// before. Opaque to the reader, but only by encoding, and it is not a
// capability: revoking still checks the caller owns the key.
func grantIDFor(keyID, operation string) string {
	return persistedGrantPrefix +
		base64.RawURLEncoding.EncodeToString([]byte(keyID+"\n"+operation))
}

func parseGrantID(id string) (keyID, operation string, ok bool) {
	if !strings.HasPrefix(id, persistedGrantPrefix) {
		return "", "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, persistedGrantPrefix))
	if err != nil {
		return "", "", false
	}
	k, o, found := strings.Cut(string(raw), "\n")
	if !found || k == "" || o == "" {
		return "", "", false
	}
	return k, o, true
}

// persistedGrantPrefix distinguishes a grant recorded on a key from one this
// process is holding in memory.
const persistedGrantPrefix = "grk_"

// Store is the whole state of the gate.
//
// Requests and session grants are in memory on purpose: both are short-lived,
// and losing either fails CLOSED — the next call asks again. Key grants are
// persisted through KeyGrantStore, and are also mirrored here so a decision
// takes effect on the very next call rather than after the key cache turns
// over.
type Store struct {
	mu       sync.Mutex
	requests map[string]*Request
	grants   map[string]*Grant
	keys     KeyGrantStore
	now      func() time.Time
}

// NewStore returns a store and starts its collector, which runs until done is
// closed.
//
// `keys` may be nil, in which case key-scoped grants live only as long as this
// process — which is what the tests want, and what a deployment with no key
// store could offer anyway.
func NewStore(done <-chan struct{}, keys KeyGrantStore) *Store {
	s := &Store{
		requests: map[string]*Request{},
		grants:   map[string]*Grant{},
		keys:     keys,
		now:      time.Now,
	}
	go s.collect(done)
	return s
}

func (s *Store) collect(done <-chan struct{}) {
	t := time.NewTicker(gcEvery)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			s.sweep()
		}
	}
}

func (s *Store) sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for id, r := range s.requests {
		// An expired request is dropped rather than marked: nobody is waiting on
		// it any more, and the caller's retry produces a fresh one.
		if now.After(r.ExpiresAt) {
			delete(s.requests, id)
		}
	}
	for id, g := range s.grants {
		if !g.live(now) {
			delete(s.grants, id)
		}
	}
}

// Allow reports whether this call may proceed, spending a one-time approval if
// that is what authorises it.
//
// Checked widest-first so a session or key grant is not consumed by being
// shadowed: a standing permission should keep a one-time approval in reserve
// rather than the other way round.
func (s *Store) Allow(
	p Principal,
	keyApprovals []string,
	sessionID, operation, fingerprint string,
) bool {
	// The credential's own record comes first and needs no lock: it is the one
	// answer that is true even when this process has just started and knows
	// nothing.
	if slices.Contains(keyApprovals, operation) {
		return true
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()

	for _, g := range s.grants {
		if g.Operation != operation || !g.live(now) {
			continue
		}
		switch g.Scope {
		case ScopeKey:
			if g.Principal.KeyID == p.KeyID && g.Principal.sameOwner(p) {
				return true
			}
		case ScopeSession:
			// A session grant needs a session to be in. An empty header would
			// otherwise match every grant whose session had also been empty —
			// that is, everyone else's.
			if sessionID != "" && g.SessionID == sessionID && g.Principal.sameOwner(p) {
				return true
			}
		}
	}

	for _, r := range s.requests {
		if r.Status != StatusApproved || r.consumed || r.Scope != ScopeOnce {
			continue
		}
		if r.Fingerprint != fingerprint || !r.Principal.sameOwner(p) || now.After(r.ExpiresAt) {
			continue
		}
		r.consumed = true
		return true
	}
	return false
}

// Challenge returns the request to point the caller at.
//
// An identical call that is already waiting gets the SAME request back rather
// than a new one. A client that polls and retries would otherwise mint an
// approval per attempt and bury the one the person is looking at under a pile
// of duplicates of itself.
func (s *Store) Challenge(r Request) *Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()

	for _, existing := range s.requests {
		if existing.Status == StatusPending &&
			existing.Fingerprint == r.Fingerprint &&
			existing.Principal.sameOwner(r.Principal) &&
			now.Before(existing.ExpiresAt) {
			return existing
		}
	}

	r.ID = "apr_" + randomHex(8)
	r.CreatedAt = now
	r.ExpiresAt = now.Add(RequestTTL)
	r.Status = StatusPending
	s.requests[r.ID] = &r
	return &r
}

// Get returns a request by id, reporting expiry as a status rather than as
// absence — a caller polling a request that timed out deserves to be told which
// of the two happened.
func (s *Store) Get(id string) (Request, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.requests[id]
	if !ok {
		return Request{}, false
	}
	out := *r
	if out.Status == StatusPending && s.now().After(out.ExpiresAt) {
		out.Status = StatusExpired
	}
	return out, true
}

// ErrNotDecidable explains why a decision was refused.
type ErrNotDecidable struct{ Reason string }

func (e *ErrNotDecidable) Error() string { return e.Reason }

// Decide answers a pending request, creating a grant when the answer reaches
// beyond this one call.
func (s *Store) Decide(
	ctx context.Context,
	id string,
	approve bool,
	scope Scope,
	decidedBy string,
) (Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()

	r, ok := s.requests[id]
	if !ok {
		return Request{}, &ErrNotDecidable{Reason: "approval request not found"}
	}
	if r.Status != StatusPending {
		return Request{}, &ErrNotDecidable{
			Reason: fmt.Sprintf("approval request is already %s", r.Status),
		}
	}
	if now.After(r.ExpiresAt) {
		return Request{}, &ErrNotDecidable{Reason: "approval request has expired"}
	}

	if !approve {
		r.Status = StatusDenied
		r.DecidedBy, r.DecidedAt = decidedBy, now
		return *r, nil
	}

	switch scope {
	case ScopeOnce:
	case ScopeSession:
		if r.OnceOnly {
			return Request{}, &ErrNotDecidable{
				Reason: "this operation can only be approved one call at a time",
			}
		}
		if r.SessionID == "" {
			return Request{}, &ErrNotDecidable{
				Reason: "the caller sent no session id, so there is no session to approve for",
			}
		}
	case ScopeKey:
		if r.OnceOnly {
			return Request{}, &ErrNotDecidable{
				Reason: "this operation can only be approved one call at a time",
			}
		}
		if r.Principal.KeyID == "" {
			return Request{}, &ErrNotDecidable{
				Reason: "the caller is not using an API key, so there is no key to approve for",
			}
		}
	default:
		return Request{}, &ErrNotDecidable{Reason: "unknown approval scope " + string(scope)}
	}

	r.Status = StatusApproved
	r.Scope = scope
	r.DecidedBy, r.DecidedAt = decidedBy, now

	if scope != ScopeOnce {
		g := &Grant{
			ID:        "grn_" + randomHex(8),
			Principal: r.Principal,
			Scope:     scope,
			SessionID: r.SessionID,
			Operation: r.Operation,
			CreatedAt: now,
			GrantedBy: decidedBy,
		}
		if scope == ScopeSession {
			g.ExpiresAt = now.Add(SessionGrantTTL)
		}
		if scope == ScopeKey && s.keys != nil {
			// Written BEFORE the in-memory mirror, and a failure refuses the
			// decision. Granting in memory only would look like it worked and
			// then quietly stop working on the next rollout — the person would
			// have no reason to suspect the permission they gave was never
			// written down.
			if err := s.keys.GrantOperation(
				ctx, r.Principal.KeyID, r.Operation, decidedBy,
			); err != nil {
				return Request{}, &ErrNotDecidable{
					Reason: "could not record the permission on the key: " + err.Error(),
				}
			}
		}
		s.grants[g.ID] = g
	}
	return *r, nil
}

// List returns what an owner has outstanding: requests waiting on them, and the
// standing permissions they have already given.
//
// Scoped to team+user rather than to the key, because the person deciding is
// the owner of every key they hold — and because the console shows this list to
// a human, not to a credential.
func (s *Store) List(ctx context.Context, p Principal) ([]Request, []Grant) {
	// Read the durable ones outside the lock: they come from the API server,
	// and holding the gate's mutex across a network call would stall every
	// request passing through the middleware.
	persisted := s.persistedGrants(ctx, p)

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()

	requests := []Request{}
	for _, r := range s.requests {
		if !r.Principal.sameOwner(p) || r.Status != StatusPending || now.After(r.ExpiresAt) {
			continue
		}
		requests = append(requests, *r)
	}
	// Start from what the KEYS say, because that is the durable record: after a
	// restart it is the only one, and while this process is alive it is the same
	// set the mirror below holds.
	grants := persisted
	seen := make(map[string]bool, len(grants))
	for _, g := range grants {
		seen[g.Principal.KeyID+"\n"+g.Operation] = true
	}
	for _, g := range s.grants {
		if !g.Principal.sameOwner(p) || !g.live(now) {
			continue
		}
		// A key grant already listed from the key itself — listing the mirror
		// too would show one permission twice, with two different revoke
		// buttons, one of which does half the job.
		if g.Scope == ScopeKey && seen[g.Principal.KeyID+"\n"+g.Operation] {
			continue
		}
		grants = append(grants, *g)
	}
	sort.Slice(requests, func(i, j int) bool {
		return requests[i].CreatedAt.After(requests[j].CreatedAt)
	})
	sort.Slice(grants, func(i, j int) bool {
		return grants[i].CreatedAt.After(grants[j].CreatedAt)
	})
	return requests, grants
}

// persistedGrants reads the standing permissions recorded on this person's
// keys. A read failure yields none rather than an error: the page is better
// showing the requests waiting on someone than refusing to render at all.
func (s *Store) persistedGrants(ctx context.Context, p Principal) []Grant {
	if s.keys == nil {
		return []Grant{}
	}
	rows, err := s.keys.KeyGrants(ctx, p.Team, p.User)
	if err != nil {
		return []Grant{}
	}
	out := make([]Grant, 0, len(rows))
	for _, r := range rows {
		out = append(out, Grant{
			ID:        grantIDFor(r.KeyID, r.Operation),
			Principal: Principal{KeyID: r.KeyID, Team: p.Team, User: p.User},
			Scope:     ScopeKey,
			Operation: r.Operation,
			CreatedAt: r.GrantedAt,
			GrantedBy: r.GrantedBy,
		})
	}
	return out
}

// Revoke withdraws a standing permission. Only its owner can.
func (s *Store) Revoke(ctx context.Context, p Principal, grantID string) bool {
	// A permission recorded on a key is withdrawn from the key. It may well
	// have been granted by a process that no longer exists, so there is nothing
	// in memory to look for first.
	if keyID, operation, ok := parseGrantID(grantID); ok {
		if s.keys == nil || !s.ownsKey(ctx, p, keyID) {
			return false
		}
		if err := s.keys.RevokeOperation(ctx, keyID, operation); err != nil {
			return false
		}
		// Drop the mirror too, or the permission keeps working from memory
		// until this process restarts — which is the opposite of what the
		// person just asked for, and the worse direction to fail in.
		s.mu.Lock()
		for id, g := range s.grants {
			if g.Scope == ScopeKey && g.Principal.KeyID == keyID && g.Operation == operation {
				delete(s.grants, id)
			}
		}
		s.mu.Unlock()
		return true
	}

	s.mu.Lock()
	g, ok := s.grants[grantID]
	if !ok || !g.Principal.sameOwner(p) {
		s.mu.Unlock()
		return false
	}
	delete(s.grants, grantID)
	keyID, operation, scope := g.Principal.KeyID, g.Operation, g.Scope
	s.mu.Unlock()

	if scope == ScopeKey && s.keys != nil {
		// Best effort: the in-memory grant is already gone, so the permission
		// has stopped working either way. Leaving the record behind would let
		// it come back on the next restart, so it is worth attempting, but not
		// worth reporting a failed revocation the caller cannot act on.
		_ = s.keys.RevokeOperation(ctx, keyID, operation)
	}
	return true
}

// ownsKey reports whether one of this person's keys is the one named.
//
// The grant id encodes the key, and an id is guessable by anyone who can list
// their own — so ownership is checked against the store rather than assumed
// from the fact that the caller had an id at all.
func (s *Store) ownsKey(ctx context.Context, p Principal, keyID string) bool {
	grants, err := s.keys.KeyGrants(ctx, p.Team, p.User)
	if err != nil {
		return false
	}
	for _, g := range grants {
		if g.KeyID == keyID {
			return true
		}
	}
	return false
}

// Fingerprint identifies one call closely enough that approving it cannot
// authorise a different one.
//
// The body is in it deliberately: "create an environment" is not the unit a
// person agrees to — "create the environment called foo" is. Without the body,
// a one-time approval for a harmless call would authorise any other call to the
// same route.
func Fingerprint(method, path string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(method))
	h.Write([]byte{0})
	h.Write([]byte(path))
	h.Write([]byte{0})
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand does not fail in practice, and an id that collides is
		// worse than a panic here: it would hand one caller another's approval.
		panic("approval: cannot read random bytes: " + err.Error())
	}
	return hex.EncodeToString(b)
}
