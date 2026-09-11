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

	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
)

// An agent credential gets key metadata and no key material.
//
// Minting a credential is forbidden for an agent precisely so it cannot issue
// itself one without the agent restriction. Returning an existing key's
// plaintext token would have made that forbidding decorative: it would not need
// to create a key, only to list one.
func TestAPIKeyItemsToGen_WithholdsMaterialFromAgents(t *testing.T) {
	items := []service.APIKeyItem{{
		ShortName: "agentbox-apikey-1",
		KeyMetadata: service.KeyMetadata{
			User:     "u",
			Team:     "t",
			Role:     "tenant",
			RawToken: "agbx_should_not_leak",
		},
	}}

	agent := apiKeyItemsToGen(items, false)
	if len(agent) != 1 {
		t.Fatalf("expected one item, got %d", len(agent))
	}
	if agent[0].RawToken != nil {
		t.Fatalf("an agent credential was handed key material: %q", *agent[0].RawToken)
	}
	// The metadata is still there — an agent can say which keys exist and which
	// are gated, which is the useful half and carries nothing secret.
	if agent[0].KeyId != "agentbox-apikey-1" {
		t.Fatalf("metadata should survive, got %+v", agent[0])
	}

	person := apiKeyItemsToGen(items, true)
	if person[0].RawToken == nil || *person[0].RawToken != "agbx_should_not_leak" {
		t.Fatalf("a person recovering their own key should still get it, got %+v", person[0])
	}
}
