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
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
)

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
	//
	// Implementations are responsible for populating the wire-shape fields
	// of each gen.Quota, including the required `Label` field (typically
	// derived from QuotaUrl, falling back to Name).
	ListForUser(ctx context.Context, user, team string) ([]gen.Quota, *domain.AppError)

	// DeriveShortName returns a stable human-readable short identifier for
	// a quota URL, used by the SandboxEnv Reconciler when naming member
	// SandboxPools that consume the quota (e.g. "{env-name}-{shortName}").
	//
	// Examples (open-source default, see DeriveDefaultShortName):
	//   "zxli.ai-lab.math.exclusive"     → "exclusive"
	//   "upgrader.autoupg.test.ondemand" → "ondemand"
	//
	// Returns an empty string when no usable identifier can be derived;
	// callers fall back to a deterministic hash-based suffix so naming
	// stays idempotent.
	DeriveShortName(quotaURL string) string
}
