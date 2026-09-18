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

package activity

import (
	"testing"
	"time"
)

func TestRefreshIntervalIsBoundedByTheIdleTimeout(t *testing.T) {
	cases := []struct {
		name        string
		idleTimeout time.Duration
		want        time.Duration
	}{
		// At or above 6m the base interval wins and the timeout is irrelevant.
		{"30m", 30 * time.Minute, BaseRefreshInterval},
		{"6m is exactly the crossover", 6 * time.Minute, BaseRefreshInterval},
		// Below 6m the timeout is the binding constraint: T/3. Note this
		// includes the E2B SDKs' own 5-minute default.
		{"5m (E2B SDK default)", 5 * time.Minute, 100 * time.Second},
		{"5m59s", 5*time.Minute + 59*time.Second, 119*time.Second + 666666666*time.Nanosecond},
		{"2m", 2 * time.Minute, 40 * time.Second},
		{"60s", time.Minute, 20 * time.Second},
		{"3s", 3 * time.Second, time.Second},
		// Pathological: the floor keeps the flusher off a busy loop. It is the
		// one case where the interval may exceed the timeout itself — harmless,
		// because the buffer the controller adds is derived from the same
		// function, so eviction stays correct; it just no longer honours a
		// sub-second timeout literally.
		{"1s", time.Second, MinRefreshInterval},
		{"1ms", time.Millisecond, MinRefreshInterval},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RefreshInterval(tc.idleTimeout)
			if got != tc.want {
				t.Errorf("RefreshInterval(%v) = %v, want %v", tc.idleTimeout, got, tc.want)
			}
			if got <= 0 {
				t.Fatalf("RefreshInterval(%v) = %v, must be positive", tc.idleTimeout, got)
			}
			// The interval must stay under the timeout it is refreshing, except
			// where the floor kicks in.
			if got > tc.idleTimeout && got != MinRefreshInterval {
				t.Errorf("RefreshInterval(%v) = %v exceeds the timeout itself", tc.idleTimeout, got)
			}
		})
	}
}

// The whole point of the interval is that it stays smaller than the idle
// timeout, otherwise the annotation goes stale while the sandbox is in use and
// the controller releases it. Check the property across the range rather than
// at hand-picked points.
func TestRefreshIntervalStaysUnderTheIdleTimeout(t *testing.T) {
	for d := time.Second; d <= 2*time.Hour; d += time.Second {
		interval := RefreshInterval(d)
		if interval > d {
			t.Fatalf("idleTimeout=%v gives interval=%v, which is larger", d, interval)
		}
		// And the bound the controller relies on has to hold for every draw.
		for _, id := range []string{"a", "sandbox-1", "0195f0b2-5c3d-7a41-9c6e-1e6f2a8b4d10"} {
			if got := RefreshIntervalFor(id, d); got > MaxRefreshInterval(d) {
				t.Fatalf("id=%s idleTimeout=%v: RefreshIntervalFor=%v > MaxRefreshInterval=%v",
					id, d, got, MaxRefreshInterval(d))
			}
		}
	}
}

func TestJitterFactorIsStableAndOneSided(t *testing.T) {
	ids := []string{
		"0195f0b2-5c3d-7a41-9c6e-1e6f2a8b4d10",
		"0195f0b2-5c3d-7a41-9c6e-1e6f2a8b4d11",
		"a", "b", "c", "sandbox-with-a-longer-name",
	}
	seen := map[string]float64{}
	for _, id := range ids {
		f := JitterFactor(id)
		if f < 1 || f >= 1+RefreshJitter {
			t.Errorf("JitterFactor(%q) = %v, want [1, %v)", id, f, 1+RefreshJitter)
		}
		// Stable: re-deriving must give the same value. Re-rolling per tick
		// would keep postponing the deadline and the sandbox could starve.
		if again := JitterFactor(id); again != f {
			t.Errorf("JitterFactor(%q) is not stable: %v then %v", id, f, again)
		}
		seen[id] = f
	}

	// Different sandboxes should not all land on the same offset, or the jitter
	// buys nothing.
	distinct := map[float64]bool{}
	for _, f := range seen {
		distinct[f] = true
	}
	if len(distinct) < 2 {
		t.Errorf("all jitter factors identical (%v); the jitter is not spreading anything", seen)
	}
}

func TestMaxRefreshIntervalIncludesJitterAndTick(t *testing.T) {
	const T = 30 * time.Minute
	base := RefreshInterval(T)
	max := MaxRefreshInterval(T)

	if max <= base {
		t.Fatalf("MaxRefreshInterval(%v) = %v, must exceed the base %v", T, max, base)
	}
	// The worst draw any sandbox can produce is base*(1+jitter), plus one tick
	// for a refresh that becomes due just after the flusher wakes up.
	if want := time.Duration(float64(base)*(1+RefreshJitter)) + FlushTick; max != want {
		t.Errorf("MaxRefreshInterval(%v) = %v, want %v", T, max, want)
	}
}

// The safety property the package exists for: a sandbox that is still being
// used is never released, for any idle timeout and any jitter draw.
func TestActiveSandboxIsNeverIdle(t *testing.T) {
	now := time.Now()
	for _, timeout := range []time.Duration{
		time.Second, 30 * time.Second, time.Minute, 5 * time.Minute, 30 * time.Minute, 24 * time.Hour,
	} {
		// The annotation understates the truth by at most MaxRefreshInterval.
		for _, id := range []string{"a", "b", "0195f0b2-5c3d-7a41-9c6e-1e6f2a8b4d10"} {
			annotation := now.Add(-MaxRefreshInterval(timeout))
			if IsIdle(now, annotation, timeout) {
				t.Errorf("idleTimeout=%v id=%s: a sandbox used right now reads as idle "+
					"(annotation lagged by the full bound)", timeout, id)
			}
		}
	}
}

func TestIdleOnlyAfterTheTimeoutPlusBuffer(t *testing.T) {
	now := time.Now()
	const timeout = 5 * time.Minute
	buffer := ReclaimBuffer(timeout)

	cases := []struct {
		name       string
		lastActive time.Time
		want       bool
	}{
		{"just used", now, false},
		{"idle for the timeout alone", now.Add(-timeout), false},
		{"idle for timeout+buffer, exactly", now.Add(-(timeout + buffer)), false},
		{"idle past timeout+buffer", now.Add(-(timeout + buffer + time.Second)), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsIdle(now, tc.lastActive, timeout); got != tc.want {
				t.Errorf("IsIdle(%v) = %v, want %v", tc.lastActive, got, tc.want)
			}
		})
	}
}

func TestNoTimeoutNeverIdle(t *testing.T) {
	now := time.Now()
	if IsIdle(now, now.Add(-100*time.Hour), 0) {
		t.Error("a sandbox with no idle timeout must never be released by this rule")
	}
	if IsIdle(now, now.Add(-100*time.Hour), -time.Minute) {
		t.Error("a negative idle timeout must never be released by this rule")
	}
}
