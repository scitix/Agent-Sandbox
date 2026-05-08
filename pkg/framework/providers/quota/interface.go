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

package quota

import (
	"context"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
)

// QuotaInfo is re-exported from the domain package to keep callers decoupled
// from the service layer. When the Scitix implementation is extracted this
// remains the stable type.
type QuotaInfo = domain.QuotaInfo

// Provider exposes read-only access to quota information.
//
// A nil Provider is not valid; callers should always hold at least a Noop.
// Use Enabled() to decide whether to expose quota-related UI.
type Provider interface {
	// Enabled reports whether the provider can answer queries. False means
	// the quota feature is unavailable (no backend configured, CRD missing,
	// etc.) — callers should skip quota checks gracefully.
	Enabled() bool

	// ListForUser returns all quotas visible to the (user, team) pair.
	// Returns (nil, nil) when the provider is disabled or the user has no
	// visible quotas; returns an AppError only on transient/system failures.
	ListForUser(ctx context.Context, user, team string) ([]QuotaInfo, *domain.AppError)
}
