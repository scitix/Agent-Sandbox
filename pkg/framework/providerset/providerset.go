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

// Package providerset bundles every framework Provider into a single value
// passed to Plugin Factories. Plugins take what they need; the bundle stays
// small and out-of-tree extensions don't have to know about the host's
// construction order.
//
// The package lives in its own sub-tree (NOT inside pkg/framework) so that
// the core framework package — which Providers themselves import for
// framework.Handle — does not in turn depend on every Provider type. This
// breaks the import cycle that would otherwise appear if `framework.Handle`
// exposed Provider accessors directly.
//
// Add a new Provider here in three places:
//
//  1. Add a field to Set.
//  2. Return its Noop from Empty.
//  3. Plumb the host's instance into Set wherever buildPlugins is called.
package providerset

import (
	"github.com/scitix/agent-sandbox/pkg/framework/providers/instancetype"
	"github.com/scitix/agent-sandbox/pkg/framework/providers/quota"
)

// Set is the read-only bundle of Providers a Plugin can consume at
// construction time. The host populates this once during bootstrap and
// passes the same value to every Plugin Factory.
//
// Plugins must NOT retain a pointer to fields they don't need — Providers
// are interfaces, not goroutine-safe in any specific way beyond the
// implementation guarantees.
type Set struct {
	// Quota exposes quota information (read-only). Always non-nil — Noop is
	// returned when no quota backend is configured.
	Quota quota.Provider

	// InstanceType exposes the InstanceType catalog (read-only). Always
	// non-nil — Noop is returned when no catalog backend is configured.
	InstanceType instancetype.Provider
}

// Empty returns a Set populated entirely with Noop Providers. Useful in
// tests that construct a Plugin without exercising any Provider, and as the
// fallback when a Provider field is left nil at registration time.
func Empty() Set {
	return Set{
		Quota:        quota.NewNoop(),
		InstanceType: instancetype.NewNoop(),
	}
}

// Normalize replaces any nil field in s with the Noop variant. Callers
// should run this before handing the Set to a Plugin Factory so plugins can
// safely dereference any field without a nil check.
func (s Set) Normalize() Set {
	if s.Quota == nil {
		s.Quota = quota.NewNoop()
	}
	if s.InstanceType == nil {
		s.InstanceType = instancetype.NewNoop()
	}
	return s
}
