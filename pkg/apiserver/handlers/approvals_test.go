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

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
)

// Who may answer an approval.
//
// The one case that matters is the first: without it the gate is a formality,
// because the agent that was refused holds the credential that would approve
// it. The rest are here because an earlier version of this check said "JWT
// only", and that locked the console's own API-key logins out of the page built
// for them.
func TestOnlyAnUnattendedCredentialIsRefused(t *testing.T) {
	cases := []struct {
		name    string
		auth    domain.AuthInfo
		refused bool
	}{
		{
			name:    "the agent's own key, which is the whole point",
			auth:    domain.AuthInfo{AuthMethod: "apikey", Unattended: true},
			refused: true,
		},
		{
			name: "a console session (OIDC): the proxy forwards the JWT",
			auth: domain.AuthInfo{AuthMethod: "jwt"},
		},
		{
			// The dashboard's cluster proxy injects the caller's own API key for
			// an API-key login rather than forwarding a JWT. That person is at a
			// browser, and their key is not marked unattended.
			name: "a console session that signed in with an API key",
			auth: domain.AuthInfo{AuthMethod: "apikey"},
		},
		{
			name: "an ordinary key from a second terminal",
			auth: domain.AuthInfo{AuthMethod: "apikey", KeyID: "ns/other"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := requireHuman(tc.auth)
			if tc.refused && err == nil {
				t.Fatal("expected the decision to be refused")
			}
			if !tc.refused && err != nil {
				t.Fatalf("expected it to be allowed, got %v", err)
			}
		})
	}
}

func TestADecisionIsAttributedToWhoeverMadeIt(t *testing.T) {
	// The email when there is one, because that is what a person recognises in
	// the audit line; the team/user pair otherwise, which every credential has.
	if got := decidedBy(domain.AuthInfo{Email: "a@b.c", Team: "t", User: "u"}); got != "a@b.c" {
		t.Fatalf("got %q", got)
	}
	if got := decidedBy(domain.AuthInfo{Team: "t", User: "u"}); got != "t/u" {
		t.Fatalf("got %q", got)
	}
}
