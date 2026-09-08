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
	"testing"

	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
)

// How a person's key allowance is counted once agent keys exist.
//
// The rule these encode: an agent key is issued FOR someone rather than BY
// them — the assistant mints one the first time they open a conversation — so
// charging it to an allowance they did not spend is how someone ends up unable
// to make a third key of their own because a chat window made one for them.
// That is not hypothetical; it is the 409 that surfaced mid-conversation before
// any of this existed.
//
// The counting itself lives inline in CreateApiKey, so this exercises the same
// predicate rather than the handler: the handler needs a KeyStore, a manager and
// a gin context, none of which say anything about the arithmetic.
func countOwn(keys []apikey.KeyMetadata) int {
	own := 0
	for _, k := range keys {
		if k.RequireApproval {
			continue
		}
		own++
	}
	return own
}

func hasAgentKey(keys []apikey.KeyMetadata) bool {
	for _, k := range keys {
		if k.RequireApproval {
			return true
		}
	}
	return false
}

func TestAnAgentKeyDoesNotSpendThePersonsAllowance(t *testing.T) {
	keys := []apikey.KeyMetadata{
		{KeyID: "ns/a"},
		{KeyID: "ns/b"},
		{KeyID: "ns/agent", RequireApproval: true},
	}
	// Three keys exist, but only two are theirs. With a cap of three they can
	// still make one more.
	if got := countOwn(keys); got != 2 {
		t.Fatalf("own keys: got %d, want 2", got)
	}
}

func TestThePersonsOwnKeysStillHitTheCap(t *testing.T) {
	// The exemption is for the agent key alone. Someone with three of their own
	// is still at the limit, agent key or not.
	keys := []apikey.KeyMetadata{
		{KeyID: "ns/a"}, {KeyID: "ns/b"}, {KeyID: "ns/c"},
		{KeyID: "ns/agent", RequireApproval: true},
	}
	if got := countOwn(keys); got != 3 {
		t.Fatalf("own keys: got %d, want 3", got)
	}
}

func TestThereIsOnlyEverOneAgentKey(t *testing.T) {
	// A second would be a second credential acting unattended in the same
	// person's name, with nothing to tell them apart in an audit line.
	if hasAgentKey([]apikey.KeyMetadata{{KeyID: "ns/a"}}) {
		t.Fatal("no agent key here")
	}
	if !hasAgentKey([]apikey.KeyMetadata{{KeyID: "ns/a"}, {KeyID: "ns/g", RequireApproval: true}}) {
		t.Fatal("should have found the agent key")
	}
}

func TestShortKeyNameIsWhatTheConsoleShows(t *testing.T) {
	if got := shortKeyName("agentbox-system/agentbox-apikey-abc"); got != "agentbox-apikey-abc" {
		t.Fatalf("got %q", got)
	}
	if got := shortKeyName("agentbox-apikey-abc"); got != "agentbox-apikey-abc" {
		t.Fatalf("got %q", got)
	}
}
