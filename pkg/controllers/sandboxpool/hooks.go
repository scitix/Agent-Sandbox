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

package sandboxpool

import (
	"context"

	corev1 "k8s.io/api/core/v1"
)

// IdleNotifier is called by the SandboxPoolReconciler to signal sandbox
// lifecycle transitions observed during pool reconciliation. Implementations
// must be non-blocking and safe to call from multiple goroutines concurrently.
//
// The typical implementation is k8sSandboxService, which routes these events
// to the poolClaimScheduler (to wake waiters) and to the ExtProc gRPC client
// (to invalidate the route cache).
type IdleNotifier interface {
	// NotifyIdleAvailable is called whenever a Pod successfully transitions to
	// the Idle phase (Stopping → Idle via MarkUpdateCompleted). Wakes any
	// waiting Create requests for the corresponding pool.
	NotifyIdleAvailable(namespace, poolName string)

	// OnSandboxReleased is called at the same Stopping → Idle transition,
	// carrying the sandbox ID that was just released. Implementations should
	// invalidate any cached mapping keyed on sandboxID (e.g. the ExtProc
	// route cache) so subsequent router queries return NotFound instead of
	// briefly hitting a stale entry.
	OnSandboxReleased(ctx context.Context, sandboxID string)
}

// SandboxReadyHook is called in a goroutine after a Starting pod successfully
// transitions to Running via MarkUpdateCompleted. Implementations must be goroutine-safe.
type SandboxReadyHook interface {
	OnSandboxReady(ctx context.Context, pod *corev1.Pod)
}

// SandboxReleaseHook is called in a goroutine after a Stopping pod completes its
// return to the pool (Stopping → Idle). Used to reset per-sandbox in-Pod state
// (e.g. the egress filter sidecar) so a reused pod does not carry the previous
// sandbox's configuration. Implementations must be goroutine-safe.
type SandboxReleaseHook interface {
	OnSandboxRelease(ctx context.Context, pod *corev1.Pod)
}
