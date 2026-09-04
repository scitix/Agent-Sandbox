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

package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

func newTestVault(t *testing.T) (VaultService, client.Client) {
	t.Helper()
	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("fake client: %v", err)
	}
	c := cb.Build()
	return NewVaultService(c), c
}

const (
	vNS  = "t-team-alice"
	vUsr = "alice"
)

func TestVault_CreateGetListDelete(t *testing.T) {
	v, _ := newTestVault(t)
	ctx := context.Background()

	item, appErr := v.Create(ctx, vNS, vUsr, VaultCreateInput{
		Name: "openai-api-key", Value: "sk-real", Metadata: map[string]string{"team": "infra"},
	})
	if appErr != nil {
		t.Fatalf("create: %v", appErr)
	}
	if item.Version != 1 {
		t.Fatalf("a new entry starts at version 1, got %d", item.Version)
	}
	if item.SecretID() != "sec_openai-api-key" {
		t.Fatalf("unexpected secretID %q", item.SecretID())
	}

	got, appErr := v.Get(ctx, vNS, vUsr, "openai-api-key")
	if appErr != nil {
		t.Fatalf("get by name: %v", appErr)
	}
	if !reflect.DeepEqual(got.Metadata, map[string]string{"team": "infra"}) {
		t.Fatalf("metadata not round-tripped: %+v", got.Metadata)
	}

	// The ID and the name address the same entry, so a caller can hand back
	// whatever the API returned without a lookup table.
	if _, appErr := v.Get(ctx, vNS, vUsr, "sec_openai-api-key"); appErr != nil {
		t.Fatalf("get by secretID: %v", appErr)
	}

	items, appErr := v.List(ctx, vNS, vUsr)
	if appErr != nil {
		t.Fatalf("list: %v", appErr)
	}
	if len(items) != 1 || items[0].Name != "openai-api-key" {
		t.Fatalf("unexpected list: %+v", items)
	}

	if appErr := v.Delete(ctx, vNS, vUsr, "openai-api-key"); appErr != nil {
		t.Fatalf("delete: %v", appErr)
	}
	if _, appErr := v.Get(ctx, vNS, vUsr, "openai-api-key"); appErr == nil {
		t.Fatal("expected the entry to be gone")
	}
}

// The vault is created on first write, so an untouched user is an empty list
// rather than an error or a stray empty object.
func TestVault_ListBeforeAnyWrite_IsEmptyNotAnError(t *testing.T) {
	v, c := newTestVault(t)
	items, appErr := v.List(context.Background(), vNS, vUsr)
	if appErr != nil {
		t.Fatalf("list: %v", appErr)
	}
	if len(items) != 0 {
		t.Fatalf("expected no entries, got %+v", items)
	}
	secret := &corev1.Secret{}
	err := c.Get(context.Background(),
		client.ObjectKey{Namespace: vNS, Name: VaultSecretName(vUsr)}, secret)
	if err == nil {
		t.Fatal("listing must not create the vault Secret")
	}
}

func TestVault_UpdateBumpsVersionAndKeepsCreatedAt(t *testing.T) {
	v, _ := newTestVault(t)
	ctx := context.Background()

	created, _ := v.Create(ctx, vNS, vUsr, VaultCreateInput{Name: "tok", Value: "v1"})
	updated, appErr := v.Update(ctx, vNS, vUsr, "tok", VaultUpdateInput{Value: "v2"})
	if appErr != nil {
		t.Fatalf("update: %v", appErr)
	}
	if updated.Version != 2 {
		t.Fatalf("expected version 2, got %d", updated.Version)
	}
	if !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("createdAt must survive a rotation: %s vs %s", updated.CreatedAt, created.CreatedAt)
	}
}

func TestVault_CreateDuplicate_Conflicts(t *testing.T) {
	v, _ := newTestVault(t)
	ctx := context.Background()
	if _, appErr := v.Create(ctx, vNS, vUsr, VaultCreateInput{Name: "tok", Value: "v1"}); appErr != nil {
		t.Fatalf("create: %v", appErr)
	}
	_, appErr := v.Create(ctx, vNS, vUsr, VaultCreateInput{Name: "tok", Value: "v2"})
	if appErr == nil || appErr.Code != domain.ErrCodeConflict {
		t.Fatalf("expected 409, got %v", appErr)
	}
}

func TestVault_UpdateMissing_NotFound(t *testing.T) {
	v, _ := newTestVault(t)
	_, appErr := v.Update(context.Background(), vNS, vUsr, "nope", VaultUpdateInput{Value: "x"})
	if appErr == nil || appErr.Code != domain.ErrCodeNotFound {
		t.Fatalf("expected 404, got %v", appErr)
	}
}

// Two users sharing a namespace — the `default` case — must not see or
// overwrite each other. This is the whole reason the scope is (ns, user).
func TestVault_UsersInTheSameNamespaceAreIsolated(t *testing.T) {
	v, _ := newTestVault(t)
	ctx := context.Background()

	if _, appErr := v.Create(ctx, "default", "alice", VaultCreateInput{Name: "openai", Value: "alice-key"}); appErr != nil {
		t.Fatalf("alice create: %v", appErr)
	}
	if _, appErr := v.Create(ctx, "default", "bob", VaultCreateInput{Name: "openai", Value: "bob-key"}); appErr != nil {
		t.Fatalf("bob must be able to use the same name: %v", appErr)
	}

	aliceItems, _ := v.List(ctx, "default", "alice")
	bobItems, _ := v.List(ctx, "default", "bob")
	if len(aliceItems) != 1 || len(bobItems) != 1 {
		t.Fatalf("each user should see exactly their own entry: %d / %d", len(aliceItems), len(bobItems))
	}

	// And they resolve to different backing objects.
	aliceRefs, _ := v.ResolveRefs(ctx, "default", "alice", []string{"openai"})
	bobRefs, _ := v.ResolveRefs(ctx, "default", "bob", []string{"openai"})
	if aliceRefs["openai"].Name == bobRefs["openai"].Name {
		t.Fatal("two users must not share a backing Secret")
	}

	if appErr := v.Delete(ctx, "default", "alice", "openai"); appErr != nil {
		t.Fatalf("alice delete: %v", appErr)
	}
	if _, appErr := v.Get(ctx, "default", "bob", "openai"); appErr != nil {
		t.Fatalf("alice's delete must not touch bob's entry: %v", appErr)
	}
}

func TestVaultSecretName_DistinctForSluggingCollisions(t *testing.T) {
	// "a.b" and "a-b" slug to the same string; the hash is what keeps them apart.
	if VaultSecretName("a.b") == VaultSecretName("a-b") {
		t.Fatal("users whose slugs collide must still get distinct Secrets")
	}
	// And the name is a legal Kubernetes object name.
	for _, u := range []string{"a.b", "Alice@Example.com", strings.Repeat("x", 200), "", "---"} {
		got := VaultSecretName(u)
		if len(got) > 253 {
			t.Errorf("%q: name too long: %d", u, len(got))
		}
		if strings.HasPrefix(got, "-") || strings.HasSuffix(got, "-") {
			t.Errorf("%q: name has a leading/trailing dash: %q", u, got)
		}
	}
}

func TestCanonicalVaultName(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "openai-api-key", want: "openai-api-key"},
		{in: "sec_openai-api-key", want: "openai-api-key"},
		// E2B lower-cases before storage, and returns the canonical form.
		{in: "OpenAI-Key", want: "openai-key"},
		// E2B allows "_"; tightening that would break code that works upstream.
		{in: "my_key", want: "my_key"},
		// "." is not allowed by E2B, which is what lets it separate elsewhere.
		{in: "my.key", wantErr: true},
		{in: "my key", wantErr: true},
		{in: "with{brace}", wantErr: true},
		{in: "", wantErr: true},
		{in: strings.Repeat("x", 129), wantErr: true},
	} {
		got, appErr := canonicalVaultName(tc.in)
		if tc.wantErr {
			if appErr == nil {
				t.Errorf("%q: expected rejection, got %q", tc.in, got)
			}
			continue
		}
		if appErr != nil {
			t.Errorf("%q: unexpected error %v", tc.in, appErr)
			continue
		}
		if got != tc.want {
			t.Errorf("%q: got %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestVault_ValueLimits(t *testing.T) {
	v, _ := newTestVault(t)
	ctx := context.Background()

	if _, appErr := v.Create(ctx, vNS, vUsr, VaultCreateInput{Name: "empty", Value: ""}); appErr == nil {
		t.Fatal("an empty value would surface upstream as an opaque 401; reject it here")
	}
	big := strings.Repeat("x", maxVaultValueBytes+1)
	if _, appErr := v.Create(ctx, vNS, vUsr, VaultCreateInput{Name: "big", Value: big}); appErr == nil {
		t.Fatal("expected an oversized value to be rejected")
	}
}

func TestVault_MetadataLimits(t *testing.T) {
	v, _ := newTestVault(t)
	md := map[string]string{}
	for i := range maxVaultMetaEntries + 1 {
		md[string(rune('a'+i%26))+strings.Repeat("k", i)] = "v"
	}
	if _, appErr := v.Create(context.Background(), vNS, vUsr,
		VaultCreateInput{Name: "tok", Value: "v", Metadata: md}); appErr == nil {
		t.Fatal("expected too many metadata entries to be rejected")
	}
}

// --------------------------------------------------------------------------
// ResolveRefs
// --------------------------------------------------------------------------

func TestVault_ResolveRefs_PointsAtTheBackingSecret(t *testing.T) {
	v, _ := newTestVault(t)
	ctx := context.Background()
	if _, appErr := v.Create(ctx, vNS, vUsr, VaultCreateInput{Name: "openai", Value: "sk"}); appErr != nil {
		t.Fatalf("create: %v", appErr)
	}

	refs, appErr := v.ResolveRefs(ctx, vNS, vUsr, []string{"openai"})
	if appErr != nil {
		t.Fatalf("resolve: %v", appErr)
	}
	ref := refs["openai"]
	if ref.Name != VaultSecretName(vUsr) || ref.Key != "openai" {
		t.Fatalf("unexpected ref %+v", ref)
	}
}

// An unresolvable reference has to fail the create. Resolving what it can would
// hand back a sandbox that reaches its upstream with a decoy and 401s there,
// far from the actual mistake.
func TestVault_ResolveRefs_UnknownName_ListsWhatExists(t *testing.T) {
	v, _ := newTestVault(t)
	ctx := context.Background()
	if _, appErr := v.Create(ctx, vNS, vUsr, VaultCreateInput{Name: "openai", Value: "sk"}); appErr != nil {
		t.Fatalf("create: %v", appErr)
	}

	_, appErr := v.ResolveRefs(ctx, vNS, vUsr, []string{"openai", "anthropic"})
	if appErr == nil {
		t.Fatal("expected a rejection")
	}
	if appErr.Code != domain.ErrCodeBadRequest {
		t.Fatalf("expected 400, got %d", appErr.Code)
	}
	if !strings.Contains(appErr.Message, "anthropic") {
		t.Fatalf("message must name the missing secret: %q", appErr.Message)
	}
	// Naming the alternatives is what makes this correctable by a caller that
	// cannot list the vault.
	if !strings.Contains(appErr.Message, "openai") {
		t.Fatalf("message must list what does exist: %q", appErr.Message)
	}
}

func TestVault_ResolveRefs_EmptyVault_SaysSo(t *testing.T) {
	v, _ := newTestVault(t)
	_, appErr := v.ResolveRefs(context.Background(), vNS, vUsr, []string{"openai"})
	if appErr == nil {
		t.Fatal("expected a rejection")
	}
	if !strings.Contains(appErr.Message, "no secrets yet") {
		t.Fatalf("unexpected message: %q", appErr.Message)
	}
}

// The read surface has no field that could carry a value — the guarantee is
// structural, not a matter of remembering. This pins that.
func TestVaultItem_HasNoValueField(t *testing.T) {
	typ := reflect.TypeFor[VaultItem]()
	for i := range typ.NumField() {
		if strings.EqualFold(typ.Field(i).Name, "value") {
			t.Fatal("VaultItem must never gain a value field: it is the read shape")
		}
	}
}

// --------------------------------------------------------------------------
// Replication
// --------------------------------------------------------------------------

// fakeReplicator stands in for the Hub.
type fakeReplicator struct {
	puts    []VaultReplicatedEntry
	deletes []string
	version int64
	err     error
}

func (f *fakeReplicator) RequestVaultPut(_ context.Context, e VaultReplicatedEntry) (*VaultReplicatedEntry, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.puts = append(f.puts, e)
	f.version++
	out := e
	out.Version = f.version
	return &out, nil
}

func (f *fakeReplicator) RequestVaultDelete(_ context.Context, ns, user, name string) error {
	if f.err != nil {
		return f.err
	}
	f.deletes = append(f.deletes, ns+"/"+user+"/"+name)
	return nil
}

// With a Hub wired, the version comes back from it — a locally invented one
// would let two clusters both claim to have produced v2.
func TestVault_WriteGoesThroughTheHub(t *testing.T) {
	v, _ := newTestVault(t)
	rep := &fakeReplicator{version: 41}
	v.(*k8sVaultService).SetReplicator(rep)

	item, appErr := v.Create(context.Background(), vNS, vUsr, VaultCreateInput{Name: "tok", Value: "v1"})
	if appErr != nil {
		t.Fatalf("create: %v", appErr)
	}
	if len(rep.puts) != 1 {
		t.Fatalf("expected the write to be forwarded, got %d", len(rep.puts))
	}
	if item.Version != 42 {
		t.Fatalf("version must come from the hub, got %d", item.Version)
	}

	// It is also applied locally straight away: the caller's next request may
	// create a sandbox referencing it, and losing that race would fail a create
	// for a secret that demonstrably exists.
	if _, appErr := v.Get(context.Background(), vNS, vUsr, "tok"); appErr != nil {
		t.Fatalf("entry should be readable immediately: %v", appErr)
	}
}

func TestVault_HubUnreachable_WriteFailsLoudly(t *testing.T) {
	v, _ := newTestVault(t)
	v.(*k8sVaultService).SetReplicator(&fakeReplicator{err: errors.New("not connected")})

	_, appErr := v.Create(context.Background(), vNS, vUsr, VaultCreateInput{Name: "tok", Value: "v1"})
	if appErr == nil {
		t.Fatal("a write that did not reach the hub must not report success")
	}
	if appErr.Code != domain.ErrCodeServiceUnavailable {
		t.Fatalf("expected 503, got %d", appErr.Code)
	}
	// And nothing may be left behind locally, or this cluster would resolve a
	// credential no other cluster has.
	if _, getErr := v.Get(context.Background(), vNS, vUsr, "tok"); getErr == nil {
		t.Fatal("a failed replicated write must not be stored locally")
	}
}

func TestVault_ApplyReplicatedEntry(t *testing.T) {
	v, _ := newTestVault(t)
	sink := v.(*k8sVaultService)
	ctx := context.Background()

	if err := sink.ApplyVaultEntry(ctx, VaultReplicatedEntry{
		Namespace: vNS, User: vUsr, Name: "remote", Value: "sk", Version: 7,
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	item, appErr := v.Get(ctx, vNS, vUsr, "remote")
	if appErr != nil {
		t.Fatalf("get: %v", appErr)
	}
	if item.Version != 7 {
		t.Fatalf("replicated version not kept: %d", item.Version)
	}
}

// Streams can reorder. An older version arriving late must not move the entry
// backwards to a credential that has already been rotated away.
func TestVault_ApplyOlderVersion_IsIgnored(t *testing.T) {
	v, _ := newTestVault(t)
	sink := v.(*k8sVaultService)
	ctx := context.Background()

	if err := sink.ApplyVaultEntry(ctx, VaultReplicatedEntry{
		Namespace: vNS, User: vUsr, Name: "tok", Value: "new", Version: 5,
	}); err != nil {
		t.Fatalf("apply v5: %v", err)
	}
	if err := sink.ApplyVaultEntry(ctx, VaultReplicatedEntry{
		Namespace: vNS, User: vUsr, Name: "tok", Value: "old", Version: 2,
	}); err != nil {
		t.Fatalf("apply v2: %v", err)
	}
	item, _ := v.Get(ctx, vNS, vUsr, "tok")
	if item.Version != 5 {
		t.Fatalf("a stale event must not roll the entry back, got v%d", item.Version)
	}
}

func TestVault_ReconcileDropsEntriesTheHubNoLongerHas(t *testing.T) {
	v, _ := newTestVault(t)
	sink := v.(*k8sVaultService)
	ctx := context.Background()

	for _, n := range []string{"keep", "drop"} {
		if err := sink.ApplyVaultEntry(ctx, VaultReplicatedEntry{
			Namespace: vNS, User: vUsr, Name: n, Value: "v", Version: 1,
		}); err != nil {
			t.Fatalf("apply %s: %v", n, err)
		}
	}

	if err := sink.ReconcileVault(ctx, []VaultReplicatedEntry{
		{Namespace: vNS, User: vUsr, Name: "keep", Value: "v", Version: 1},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if _, appErr := v.Get(ctx, vNS, vUsr, "keep"); appErr != nil {
		t.Fatalf("kept entry disappeared: %v", appErr)
	}
	// A delete that landed while this cluster was disconnected only takes
	// effect here through reconciliation.
	if _, appErr := v.Get(ctx, vNS, vUsr, "drop"); appErr == nil {
		t.Fatal("entry absent from the snapshot should have been dropped")
	}
}

// A snapshot only describes the scopes it mentions. Deleting entries in a
// namespace the snapshot says nothing about would destroy credentials on
// evidence that does not exist.
func TestVault_ReconcileLeavesUnmentionedScopesAlone(t *testing.T) {
	v, _ := newTestVault(t)
	sink := v.(*k8sVaultService)
	ctx := context.Background()

	if err := sink.ApplyVaultEntry(ctx, VaultReplicatedEntry{
		Namespace: "other-ns", User: "carol", Name: "tok", Value: "v", Version: 1,
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if err := sink.ReconcileVault(ctx, []VaultReplicatedEntry{
		{Namespace: vNS, User: vUsr, Name: "keep", Value: "v", Version: 1},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if _, appErr := v.Get(ctx, "other-ns", "carol", "tok"); appErr != nil {
		t.Fatalf("an unmentioned scope must be left alone: %v", appErr)
	}
}

// A replicated write echoes back from the Hub and is applied to the same Secret
// by the watch handler, so the object a request read a moment ago is routinely
// stale by the time it writes. Before the conflict retry that surfaced as a 409
// on a delete that had in fact succeeded.
func TestVault_ConcurrentApplyDoesNotFailTheRequest(t *testing.T) {
	v, _ := newTestVault(t)
	sink := v.(*k8sVaultService)
	ctx := context.Background()

	if _, appErr := v.Create(ctx, vNS, vUsr, VaultCreateInput{Name: "tok", Value: "v1"}); appErr != nil {
		t.Fatalf("create: %v", appErr)
	}

	// Another writer touches the same Secret between this request's read and
	// its write, exactly as the replication sink does.
	if err := sink.ApplyVaultEntry(ctx, VaultReplicatedEntry{
		Namespace: vNS, User: vUsr, Name: "other", Value: "v", Version: 1,
	}); err != nil {
		t.Fatalf("concurrent apply: %v", err)
	}

	if appErr := v.Delete(ctx, vNS, vUsr, "tok"); appErr != nil {
		t.Fatalf("delete must not fail because another writer touched the vault: %v", appErr)
	}

	items, _ := v.List(ctx, vNS, vUsr)
	if len(items) != 1 || items[0].Name != "other" {
		t.Fatalf("expected only the concurrently-added entry to remain, got %+v", items)
	}
}

// Two writes to different entries of the same user must both land; a
// last-write-wins persist would drop one.
func TestVault_InterleavedWritesBothSurvive(t *testing.T) {
	v, _ := newTestVault(t)
	ctx := context.Background()

	for _, name := range []string{"a", "b", "c"} {
		if _, appErr := v.Create(ctx, vNS, vUsr, VaultCreateInput{Name: name, Value: "v"}); appErr != nil {
			t.Fatalf("create %s: %v", name, appErr)
		}
	}
	items, _ := v.List(ctx, vNS, vUsr)
	if len(items) != 3 {
		t.Fatalf("expected 3 entries, got %d: %+v", len(items), items)
	}
}
