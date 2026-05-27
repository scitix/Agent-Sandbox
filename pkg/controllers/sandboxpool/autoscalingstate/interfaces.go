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

package autoscalingstate

import (
	"time"

	"github.com/scitix/agent-sandbox/pkg/lifecycle/schedule"
)

// SchedulerLookup is the read-only view onto the apiserver's per-Pool
// scheduler registry that the autoscaler needs. The production
// implementation is the same map maintained by k8sSandboxService; tests
// inject a fake. nil is allowed and is treated as "no scheduler running"
// for every Pool (useful in unit tests that don't exercise reactive
// demand signals).
type SchedulerLookup interface {
	// GetScheduler returns the running PoolScheduler for the named pool
	// or nil when none has been created yet.
	GetScheduler(namespace, poolName string) *schedule.PoolScheduler
}

// LastCreateTracker exposes the in-process "most recent Create" timestamp
// for a Pool. Production wires this to lifecycle/lastcreate.Tracker;
// tests inject a fake. nil is treated as "tracker not wired" — the
// Snapshot loader falls back to the LastSandboxCreateTimeAnnotationKey
// on the Pool object.
type LastCreateTracker interface {
	// Get returns the most recent Create timestamp the tracker has seen
	// for the given pool, and whether any timestamp was recorded.
	Get(namespace, poolName string) (time.Time, bool)
}

// Clock is the wall-clock abstraction used by the Snapshot and Mutator.
// Tests inject a fixed-time clock so timestamp comparisons (cooldown,
// quiet window) are deterministic.
type Clock interface {
	Now() time.Time
}

// realClock implements Clock by delegating to time.Now().
type realClock struct{}

// Now returns the current wall-clock time.
func (realClock) Now() time.Time { return time.Now() }

// SystemClock returns a Clock backed by time.Now(). Convenience for
// production wiring.
func SystemClock() Clock { return realClock{} }
