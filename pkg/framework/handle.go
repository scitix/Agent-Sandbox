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

// Package framework defines the host/extension contract shared by every
// extension point in AgentBox (SandboxPool lifecycle Plugins, quota Providers,
// and future Providers such as billing or telemetry).
//
// It is intentionally small:
//
//   - Handle bundles the shared runtime dependencies the host passes to an
//     extension at construction time (controller-runtime client + cache, log).
//   - Args is the extension-specific parameter object. Each Factory declares
//     the concrete *MyArgs struct it expects and asserts it inside the body;
//     passing nil is legal for extensions that need no parameters.
//
// Concrete Factory signatures live next to each extension point (e.g.
// pkg/plugins.Factory for SandboxPool plugins, pkg/plugins/quota.Factory
// for quota Providers) because their return types differ. Registry
// implementations likewise live in each extension point so they can be
// strongly typed.
package framework

import (
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Handle is the stable contract between the host and any extension.
//
// The host implements Handle once during bootstrap and passes the same value
// to every Factory. Extensions take only what they need.
//
// Keep the surface minimal. Adding a method is an ABI break for every
// out-of-tree extension, so prefer passing domain-specific dependencies via
// Args rather than widening Handle itself.
type Handle interface {
	// Client returns a controller-runtime client backed by the manager's
	// informer cache for reads and the API server for writes.
	Client() client.Client

	// Cache returns the manager's informer cache. Extensions register their
	// own informers here (e.g. a ConfigMap hot-reload watch inside a Plugin's
	// Start hook).
	Cache() cache.Cache

	// Log is the logger extensions should derive children from; the host
	// injects contextual fields (edition, cluster, etc.) so extension logs
	// correlate with host logs without extra wiring.
	Log() logr.Logger
}
