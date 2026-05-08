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

package domain

// QuotaInfo is the domain model for a single ScitixQuota.
type QuotaInfo struct {
	// Name is the Kubernetes object name of the ScitixQuota.
	Name string
	// QuotaURL is the hierarchical path label (quota.scitix.ai/url), e.g. "alice.bob.carol.ted".
	// This value is written as a label on SandboxPool objects.
	QuotaURL string
	// Queue is the scheduling queue type, e.g. "exclusive", "ondemand", "idle".
	Queue string
	// Team is the team this quota belongs to.
	Team string
	// User is the user this quota belongs to.
	User string
	// PoolID is the resource-pool-id label value (string form of the integer ID).
	PoolID string
	// PoolName is the human-friendly name of the resource pool.
	PoolName string
	// Resources is the total hard quota limits (spec.resources).
	Resources map[string]string
	// Used is the currently consumed amount (status.used).
	Used map[string]string
	// Reserved is the reserved but not yet consumed amount (status.reserved).
	Reserved map[string]string
	// Free is the currently free amount from status.statistics.free (may be nil).
	Free map[string]string
}
