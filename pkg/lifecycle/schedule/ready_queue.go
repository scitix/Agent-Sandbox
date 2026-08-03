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

package schedule

import (
	"context"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// readyQueue is a FIFO of idle pods awaiting dispatch, with built-in dedup and
// scale-down filtering.
//
// Design notes:
//
//   - Items are stored as NamespacedName (not full Pod objects). This keeps
//     the queue lightweight and ensures each dispatch receives the freshest Pod
//     snapshot from the informer cache, reducing CAS resource-version conflicts.
//
//   - The queue order is the order in which pods were first observed as Idle by
//     refreshReady. Because notifyIdle fires on every Stopping→Idle transition,
//     the earliest-recycled pods enter the queue earliest and are dispatched
//     earliest. This is equivalent to the old AgePlugin's "prefer older idle"
//     semantics, obtained for free from FIFO discipline — no scorer required.
//
//   - Name-keyed dedup set prevents duplicate entries when refreshReady is
//     called repeatedly (the informer cache still reports the same pods on
//     subsequent List calls until a pod transitions out of Idle).
//
//   - Pods carrying SandboxScaleDownProtectedAnnotationKey are not admitted at
//     all: the autoscaler has already selected them for deletion, and racing
//     a PATCH (remove annotation) against the autoscaler's DELETE produces
//     rare but surprising outcomes. Skipping them is simpler and correct —
//     if idle supply is short the scheduler falls back to scale-up.
//
//   - popUnreservedAndReserve couples queue pop with an informer-cache Get and
//     reservation write. Pods that have been deleted or have transitioned out of
//     Idle since they were admitted are silently discarded; the next entry is
//     tried automatically. This means scale-down deletions are detected at
//     dispatch time without any additional eviction sweep.
type readyQueue struct {
	mu    sync.Mutex
	items []types.NamespacedName
	set   map[types.NamespacedName]struct{}
}

func newReadyQueue() *readyQueue {
	return &readyQueue{
		set: make(map[types.NamespacedName]struct{}),
	}
}

// appendFiltered appends pods that are not already in the queue, not reserved,
// and not scale-down-protected. Returns the number of pods admitted and the
// number skipped due to scale-down-protection (for metrics).
func (q *readyQueue) appendFiltered(pods []corev1.Pod, reserved *reservations) (admitted, skippedProtected int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i := range pods {
		p := &pods[i]
		if p.Annotations[agentsv1alpha1.SandboxScaleDownProtectedAnnotationKey] != "" {
			skippedProtected++
			continue
		}
		nn := types.NamespacedName{Namespace: p.Namespace, Name: p.Name}
		if _, dup := q.set[nn]; dup {
			continue
		}
		if reserved != nil && reserved.isReserved(p.Name) {
			continue
		}
		q.items = append(q.items, nn)
		q.set[nn] = struct{}{}
		admitted++
	}
	return admitted, skippedProtected
}

// popOutcome counts the entries a single pop walked past, for metrics.
type popOutcome struct {
	// Discarded counts entries dropped because the Pod vanished from the
	// informer cache or had left Idle. Normal churn.
	Discarded int
	// MissingSidecar counts entries rejected because the caller requires
	// egress enforcement but the Pod predates its Pool's SandboxNetworkPolicy
	// and therefore carries no filter sidecar. Never normal — see
	// agentsv1alpha1.PodHasEgressProxy.
	MissingSidecar int
}

// popUnreservedAndReserve removes the first queued pod whose name is not
// currently reserved, fetches the fresh Pod object from the informer cache,
// and atomically records a reservation for it. Returns the live Pod on success.
//
// Pods that are reserved, deleted, or no longer Idle are silently discarded and
// the next entry is tried. When requireEgressSidecar is set, Pods without the
// egress filter sidecar are rejected the same way: the claim would otherwise
// land on a Pod with no iptables redirect and no proxy, i.e. no enforcement at
// all, while the API reported success. Parking the request until a rolled Pod
// appears is the fail-closed choice. If no dispatchable pod exists, ok=false.
func (q *readyQueue) popUnreservedAndReserve(ctx context.Context, c client.Client, reserved *reservations, requireEgressSidecar bool) (pod corev1.Pod, ok bool, outcome popOutcome) {
	for {
		q.mu.Lock()
		if len(q.items) == 0 {
			q.mu.Unlock()
			return corev1.Pod{}, false, outcome
		}
		nn := q.items[0]
		q.items = q.items[1:]
		delete(q.set, nn)
		if reserved != nil && reserved.isReserved(nn.Name) {
			// Reserved by a prior dispatch still within its TTL window; drop
			// silently (this is expected normal flow, not an anomaly).
			q.mu.Unlock()
			continue
		}
		q.mu.Unlock()

		// Fetch the latest pod snapshot from the informer cache. This is a
		// local map lookup — no API server round-trip.
		var p corev1.Pod
		if err := c.Get(ctx, nn, &p); err != nil {
			// Pod is no longer in the cache (deleted during scale-down or
			// otherwise). Discard and try the next entry.
			outcome.Discarded++
			continue
		}
		if p.Labels[agentsv1alpha1.SandboxPhaseLabelKey] != string(agentsv1alpha1.SandboxPhaseIdle) {
			// Phase has changed since the pod was admitted to the queue
			// (e.g. another actor already claimed it). Discard.
			outcome.Discarded++
			continue
		}
		if requireEgressSidecar && !agentsv1alpha1.PodHasEgressProxy(&p) {
			// Pod predates its Pool's SandboxNetworkPolicy: it has neither the
			// redirect nor the proxy, so nothing would enforce the policy. Drop
			// it and keep looking; the Pool rollout is already rebuilding these.
			outcome.MissingSidecar++
			continue
		}
		if reserved != nil {
			reserved.reserve(p.Name)
		}
		return p, true, outcome
	}
}

// len returns the current queue depth.
func (q *readyQueue) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// contains reports whether a pod with the given NamespacedName is currently in
// the queue. Intended for tests / diagnostics.
func (q *readyQueue) contains(nn types.NamespacedName) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	_, ok := q.set[nn]
	return ok
}
