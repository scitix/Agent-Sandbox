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

// Package activity holds the timing contract that two independent components
// have to agree on for idle-timeout eviction to be correct:
//
//   - the gateway (every replica) observes traffic and refreshes the
//     `agentbox.navix.sh/last-active` annotation on the sandbox's Pod;
//   - the controller reads that annotation and decides when a sandbox has been
//     idle long enough to be released.
//
// They are separate processes that can be rolled independently, so the numbers
// live here once rather than being duplicated as magic constants on both sides.
//
// # Why the buffer exists
//
// The gateway cannot write the annotation on every request, so the annotation
// always *understates* how recently the sandbox was used: it lags the true last
// activity by at most [MaxRefreshInterval]. A reader that reclaimed on
// `now - lastActive > idleTimeout` would therefore release sandboxes that were
// used seconds ago. Adding the same bound as a buffer cancels the lag out:
//
//	reclaim ⟺ now - lastActive > idleTimeout + MaxRefreshInterval(idleTimeout)
//
// which, given `lastActive ≥ trueLastActive - MaxRefreshInterval`, implies
// `now - trueLastActive > idleTimeout`. No sandbox in active use is ever
// released, whatever the gateway's write cadence does in between.
package activity

import (
	"hash/fnv"
	"time"
)

const (
	// BaseRefreshInterval is the gateway's default refresh cadence (δ): how long
	// an annotation may go un-refreshed while the sandbox keeps being used.
	//
	// Chosen against traffic volume rather than latency: the steady-state write
	// rate is (sandboxes with a timeout in active use) / δ, and each write is a
	// Pod PATCH that every Pod informer in the cluster sees. Two minutes keeps
	// that rate in the tens per second even for thousands of busy sandboxes.
	BaseRefreshInterval = 120 * time.Second

	// RefreshJitter is the maximum one-sided jitter applied to
	// [BaseRefreshInterval], as a fraction. It exists to keep a batch of
	// sandboxes that became active at the same instant from refreshing in
	// lockstep — a burst the API server pays for directly.
	//
	// One-sided on purpose: shortening δ for some sandboxes would make the
	// worst-case lag unbounded in the other direction, and the worst case is
	// what the buffer has to cover.
	RefreshJitter = 0.25

	// RefreshRatio bounds δ from above by a fraction of the sandbox's own idle
	// timeout: δ ≤ idleTimeout / RefreshRatio.
	//
	// The API accepts any positive idle timeout (the E2B SDKs happen to default
	// to 5 minutes, but `set_timeout(60)` is legal), and a fixed δ would exceed
	// a short timeout outright — the annotation would go stale between
	// refreshes and the controller would release a sandbox that is in use. A
	// third leaves room for the jitter and the flush tick on top.
	RefreshRatio = 3

	// FlushTick is how often the gateway's flusher wakes up. It is part of the
	// lag bound because a refresh that becomes due just after a tick waits for
	// the next one.
	FlushTick = 5 * time.Second

	// MinRefreshInterval is a floor on δ, so that a pathologically small idle
	// timeout cannot turn the flusher into a busy loop.
	MinRefreshInterval = time.Second
)

// RefreshInterval is δ for a sandbox with this idle timeout, before jitter:
// the longest the gateway may leave the annotation alone while the sandbox
// keeps being used.
func RefreshInterval(idleTimeout time.Duration) time.Duration {
	interval := BaseRefreshInterval
	if third := idleTimeout / RefreshRatio; third < interval {
		interval = third
	}
	if interval < MinRefreshInterval {
		interval = MinRefreshInterval
	}
	return interval
}

// JitterFactor returns the stable multiplier in [1, 1+RefreshJitter) for a
// sandbox, derived from its ID.
//
// Stable, not re-rolled: a fresh random factor each tick would keep pushing the
// deadline out and the sandbox could go un-refreshed indefinitely. Deriving it
// from the ID instead gives every sandbox its own constant offset, so the same
// population still spreads evenly across the interval.
func JitterFactor(sandboxID string) float64 {
	h := fnv.New64a()
	// Hash.Write never returns an error for fnv.
	_, _ = h.Write([]byte(sandboxID))
	return 1 + RefreshJitter*float64(h.Sum64()%1000)/1000.0
}

// RefreshIntervalFor is δ for one specific sandbox, jitter included. This is
// what the gateway waits between two refreshes of that sandbox.
func RefreshIntervalFor(sandboxID string, idleTimeout time.Duration) time.Duration {
	base := RefreshInterval(idleTimeout)
	return time.Duration(float64(base) * JitterFactor(sandboxID))
}

// MaxRefreshInterval is the largest lag the annotation can have behind the true
// last activity for any sandbox with this idle timeout: the maximum δ any
// sandbox can draw, plus one flush tick.
func MaxRefreshInterval(idleTimeout time.Duration) time.Duration {
	base := RefreshInterval(idleTimeout)
	return time.Duration(float64(base)*(1+RefreshJitter)) + FlushTick
}

// ReclaimBuffer is what the controller adds to a sandbox's idle timeout before
// comparing against the annotation. See the package comment for the derivation.
func ReclaimBuffer(idleTimeout time.Duration) time.Duration {
	return MaxRefreshInterval(idleTimeout)
}

// IsIdle reports whether a sandbox whose last activity was lastActive should be
// released at now, given its idle timeout. Both the buffer above and the
// comparison itself live here so the two sides cannot drift apart.
func IsIdle(now, lastActive time.Time, idleTimeout time.Duration) bool {
	if idleTimeout <= 0 {
		return false
	}
	return now.Sub(lastActive) > idleTimeout+ReclaimBuffer(idleTimeout)
}
