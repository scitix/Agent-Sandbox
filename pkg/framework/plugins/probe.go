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

package plugins

import (
	"context"

	corev1 "k8s.io/api/core/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
)

// ProbeResultKind enumerates outcomes from ProbeAcceptedReplicas. It tracks
// the *reason* the search terminated, in addition to the numeric Accepted
// value, so the autoscaler can decide whether to consider the candidate
// member saturated, surface an InvalidSpec condition, or simply retry later.
type ProbeResultKind int

const (
	// ProbeOK — the full candidate replicas value was admitted; no binary
	// search was needed. Accepted == candidate.
	ProbeOK ProbeResultKind = iota
	// ProbeInsufficientResources — at least one probe in the search range
	// returned PluginErrKindInsufficientResources. Accepted is the largest
	// value that admitted (>= current; <= candidate). When Accepted > current
	// the autoscaler may still apply a partial scale-up.
	ProbeInsufficientResources
	// ProbeInternalError — a probe returned PluginErrKindInternal. The
	// search aborts immediately; Accepted is the best admitted value seen
	// so far (commonly current). The autoscaler should NOT mark the member
	// as saturated — the next reconcile will retry.
	ProbeInternalError
	// ProbeInvalidSpec — a probe returned PluginErrKindInvalidSpec. The
	// search aborts; the autoscaler should park the pool with an explicit
	// Condition rather than retrying with smaller targets.
	ProbeInvalidSpec
)

// ProbeResult is the outcome of ProbeAcceptedReplicas.
//
// Kind describes which branch terminated the search; Accepted is the
// largest replicas value the plugin admitted in [current, candidate]
// (always >= current); Err is the most recent non-OK *AppError, useful
// for surfacing diagnostic info on Env.Status.ObservedMember.
type ProbeResult struct {
	Kind     ProbeResultKind
	Accepted int32
	Err      *domain.AppError
}

// ProbeAcceptedReplicas finds the largest replicas count in [current, candidate]
// that PluginManager.PreUpdatePool admits.
//
// Algorithm:
//   - Probe `candidate` first. If accepted, return ProbeOK{Accepted: candidate}
//     (the common case — scheduler / quota has full headroom).
//   - On InsufficientResources, binary-search the [current+1, candidate-1]
//     range. The largest mid that admits becomes Accepted.
//   - On Internal or InvalidSpec at any probe, abort immediately and return
//     the corresponding kind. Internal errors must not look like saturation
//     (transient infra problem); InvalidSpec must surface a Condition.
//
// PreUpdatePool MUST be side-effect-free / idempotent — the existing plugin
// framework treats it as admission only, but plugin authors should be aware
// that ProbeAcceptedReplicas may call it O(log(candidate-current)) times.
//
// pm may be nil (no plugins registered) — in that case every probe trivially
// succeeds and the function returns ProbeOK{Accepted: candidate} without
// allocating.
func ProbeAcceptedReplicas(
	ctx context.Context,
	pm *PluginManager,
	pool *agentsv1alpha1.SandboxPool,
	pods []corev1.Pod,
	current, candidate int32,
) ProbeResult {
	if candidate <= current {
		return ProbeResult{Kind: ProbeOK, Accepted: current}
	}
	// Optimistic first probe — usually the scheduler has full headroom.
	if kind, err := probeAt(ctx, pm, pool, pods, candidate); kind == ProbeOK {
		return ProbeResult{Kind: ProbeOK, Accepted: candidate}
	} else if kind == ProbeInternalError || kind == ProbeInvalidSpec {
		return ProbeResult{Kind: kind, Accepted: current, Err: err}
	}

	// candidate was rejected as InsufficientResources — binary-search down.
	accepted := current
	var lastErr *domain.AppError
	lo, hi := current+1, candidate-1
	for lo <= hi {
		mid := lo + (hi-lo)/2
		kind, err := probeAt(ctx, pm, pool, pods, mid)
		switch kind {
		case ProbeOK:
			accepted = mid
			lo = mid + 1
		case ProbeInsufficientResources:
			lastErr = err
			hi = mid - 1
		default:
			// Internal / InvalidSpec interrupts the search; surface
			// the error and return whatever we have so far.
			return ProbeResult{Kind: kind, Accepted: accepted, Err: err}
		}
	}
	return ProbeResult{Kind: ProbeInsufficientResources, Accepted: accepted, Err: lastErr}
}

// probeAt runs a single PreUpdatePool admission probe against a clone of pool
// with spec.replicas set to target. Returns (ProbeOK, nil) on admit, otherwise
// the classified kind plus the raw error for diagnostic surfacing.
func probeAt(
	ctx context.Context,
	pm *PluginManager,
	pool *agentsv1alpha1.SandboxPool,
	pods []corev1.Pod,
	target int32,
) (ProbeResultKind, *domain.AppError) {
	if pm == nil {
		return ProbeOK, nil
	}
	clone := pool.DeepCopy()
	clone.Spec.Replicas = target
	_, err := pm.PreUpdatePool(ctx, clone, pods)
	if err == nil {
		return ProbeOK, nil
	}
	switch KindFromAppError(err) {
	case PluginErrKindInsufficientResources:
		return ProbeInsufficientResources, err
	case PluginErrKindInvalidSpec:
		return ProbeInvalidSpec, err
	default:
		return ProbeInternalError, err
	}
}
