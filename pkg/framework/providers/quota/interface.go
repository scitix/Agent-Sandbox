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

// Info is the internal, provider-side view of a quota. It carries the public
// wire shape together with backend-only metadata (labels/annotations from the
// underlying CRD or external system) that must not leak to API callers.
//
// The service layer serializes only Info.Quota over the wire; Labels and
// Annotations stay inside the binary so that internal consumers (the
// reservation plugin, controllers, etc.) can read provider-specific hints
// (queue name, parent pool id, scheduler URL, ...) without baking those
// hints into the public schema.
type Info struct {
	// Quota is the wire-shape projection of this quota, returned to API
	// callers verbatim. Implementations are responsible for populating Id
	// and Name (Id MUST be stable across provider restarts; Name is the
	// human-readable form for the dashboard).
	Quota gen.Quota

	// Labels mirrors the source object's labels. Optional; nil when the
	// underlying backend has no notion of labels.
	Labels map[string]string

	// Annotations mirrors the source object's annotations. Optional.
	Annotations map[string]string
}

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
	// Each Info.Quota is the wire-shape value forwarded to API callers;
	// Info.Labels / Info.Annotations carry provider-internal metadata that
	// MUST NOT be serialized — they exist solely for in-process consumers
	// (reservation plugins, scheduler integration, etc.) that need to
	// derive backend-specific hints like queue names or scheduler IDs.
	ListForUser(ctx context.Context, user, team string) ([]Info, *domain.AppError)

	// DeriveShortName returns a stable human-readable short identifier for
	// a quota ID, used by the SandboxEnv Reconciler when naming member
	// SandboxPools that consume the quota (e.g. "{env-name}-{shortName}").
	//
	// Quota ID shapes are Provider-defined, so the parsing rule lives with
	// the concrete Provider — the open-source Noop returns "" and the
	// caller falls back to a deterministic hash-based suffix so naming
	// stays idempotent. Closed-source Providers can override this with a
	// catalog lookup or a tenant-specific path parser.
	DeriveShortName(quotaID string) string
}
