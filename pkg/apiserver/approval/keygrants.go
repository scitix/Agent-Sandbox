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

	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
)

// keyBackend is the slice of the API key store this needs.
//
// Narrow on purpose: the gate reads and writes one field of a credential, and
// naming exactly that makes it obvious it cannot mint, delete or read one.
type keyBackend interface {
	GrantOperation(ctx context.Context, keyID, operation, by string) error
	RevokeOperation(ctx context.Context, keyID, operation string) error
	ListByTeamAndUser(ctx context.Context, team, user string) ([]apikey.KeyMetadata, error)
}

type keyGrantStore struct{ keys keyBackend }

// NewKeyGrantStore persists key-scoped approvals on the credentials themselves.
//
// The credential is where a standing permission belongs. It is the durable
// identity of an agent's work, so the permission outlives this process, follows
// the key rather than a conversation, and appears somewhere a person can find
// it and take it back — none of which is true of a map in memory.
func NewKeyGrantStore(keys keyBackend) KeyGrantStore {
	return &keyGrantStore{keys: keys}
}

func (s *keyGrantStore) GrantOperation(ctx context.Context, keyID, operation, by string) error {
	return s.keys.GrantOperation(ctx, keyID, operation, by)
}

func (s *keyGrantStore) RevokeOperation(ctx context.Context, keyID, operation string) error {
	return s.keys.RevokeOperation(ctx, keyID, operation)
}

func (s *keyGrantStore) KeyGrants(ctx context.Context, team, user string) ([]KeyGrant, error) {
	metas, err := s.keys.ListByTeamAndUser(ctx, team, user)
	if err != nil {
		return nil, err
	}
	out := []KeyGrant{}
	for _, m := range metas {
		for op, a := range m.Approvals {
			out = append(out, KeyGrant{
				KeyID:     m.KeyID,
				Operation: op,
				GrantedAt: a.GrantedAt,
				GrantedBy: a.GrantedBy,
			})
		}
	}
	return out, nil
}
