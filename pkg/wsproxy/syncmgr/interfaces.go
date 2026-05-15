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

package syncmgr

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
)

// KeyStore is the subset of apikey.SecretKeyStore methods used by syncmgr.
type KeyStore interface {
	List(ctx context.Context) ([]apikey.KeyMetadata, error)
	ListByTeamAndUser(ctx context.Context, team, user string) ([]apikey.KeyMetadata, error)
	CountUserKeys(ctx context.Context, namespace, user string) (int, error)
	Create(ctx context.Context, meta apikey.KeyMetadata) (rawToken, keyID string, err error)
	CreateFromHash(ctx context.Context, meta apikey.KeyMetadata, tokenHash, hashPrefix string) error
	Validate(ctx context.Context, rawToken string) (*apikey.KeyMetadata, error)
	Get(ctx context.Context, keyID string) (*apikey.KeyMetadata, error)
	Delete(ctx context.Context, keyID string) error
}

// Ensure *apikey.SecretKeyStore satisfies KeyStore at compile time.
var _ KeyStore = (*apikey.SecretKeyStore)(nil)

// Ensure controller-runtime client.Client satisfies client.Client at compile time.
var _ client.Client = (client.Client)(nil)
