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

package egressproxy

// Fixed wiring shared by the redirect installer, the proxy, and the control
// plane. These are container-internal (the sidecar owns the whole netns view),
// so constants rather than config keep init and sidecar in lockstep.
const (
	// DefaultProxyUID is the uid the sidecar runs as; the redirect exempts it so
	// the proxy's own upstream connections are not looped back.
	DefaultProxyUID = 1337

	DefaultHTTPPort  = 15001
	DefaultTLSPort   = 15002
	DefaultOtherPort = 15003

	// DefaultHealthPort serves /healthz. Separate from the three data-plane
	// ports on purpose: a probe aimed at those arrives indistinguishable from a
	// redirected sandbox connection, so it would be policy-evaluated, logged as
	// a denial on every interval, and — with private ranges allowed — dialed
	// straight back into this listener.
	DefaultHealthPort = 15004

	// DefaultPolicyPath is the emptyDir file the control plane writes (exec) and
	// the proxy reads (fsnotify). Mounted only in the sidecar.
	DefaultPolicyPath = "/var/run/egress/policy.json"

	// DefaultSecretsPath is the injection config, written 0600 next to the
	// policy. It holds live credential material and the sandbox's CA key, so it
	// lives on the same sidecar-only tmpfs and is removed on release.
	DefaultSecretsPath = "/var/run/egress/secrets.json"
)
