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
	"context"
	"fmt"
	"maps"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// Mutator accumulates the writes the autoscaler decision logic wants to
// apply this reconcile cycle. It performs no I/O until Commit is called:
//
//   - PatchStatus  schedules one or more mutations against
//     Pool.Status.AutoScaling. Multiple PatchStatus calls compose
//     (applied in registration order); Commit folds them into a single
//     SandboxPool status sub-resource patch.
//
//   - SetTargetReplicas requests a desired-replicas update. The write
//     target is Env.Spec.Clusters[i].Members[j].Spec.Replicas on the
//     owning SandboxEnv (NOT Pool.Spec.Replicas directly). The
//     existing Env reconciler's drift loop propagates the change onto
//     the live Pool, which keeps a single writer of Pool.Spec.Replicas
//     and reuses the manual UpdateMember path for free. May be called
//     at most once per Mutator; the last call wins. Requires
//     Snapshot.Env to be non-nil — Decide is responsible for the
//     guard.
//
//   - MarkPodScaleDownProtected / UnmarkPodScaleDownProtected queue
//     per-Pod annotation patches.
//
//   - EmitEvent queues a Kubernetes event for the Pool object. Events
//     are recorded after spec/status patches succeed so they reflect
//     what was actually persisted, not what was attempted.
//
// Mutator is single-threaded: it is built, populated, and committed from
// the same goroutine within one Reconcile call. There is no internal
// locking.
type Mutator struct {
	snap *Snapshot

	statusMutators []StatusMutateFunc
	targetReplicas *int32
	scaleUpAttempt *scaleUpAttempt
	podAnnOps      []podAnnotationOp
	events         []eventOp

	// scaleDownTransition is the scale-down session state change this
	// cycle wants applied to the shared ScaleDownTracker. The reconciler
	// reads it after a successful Commit and applies it; see
	// ScaleDownTransition. The zero value (ScaleDownNoTransition) means
	// the cycle touched no session state.
	scaleDownTransition ScaleDownTransition
}

// scaleUpAttempt records a scale-up intent that will be resolved at
// Commit time by running the Snapshot.Prober. Stored as a single
// op (last write wins) — Decide invokes ScaleUpAttempt at most once
// per cycle.
type scaleUpAttempt struct {
	from, target int32
}

// StatusMutateFunc mutates the autoscaling sub-status in place. The
// argument is guaranteed non-nil — Commit allocates it before invoking
// the mutators when the field was absent on the live object.
type StatusMutateFunc func(*agentsv1alpha1.PoolAutoScalingStatus)

// NewMutator returns an empty Mutator bound to snap. The Snapshot is
// retained read-only and used at Commit time to derive object keys.
func NewMutator(snap *Snapshot) *Mutator {
	return &Mutator{snap: snap}
}

// Snapshot returns the Snapshot this Mutator is bound to. Useful when a
// decision helper composes Mutator updates without needing to thread the
// Snapshot through every call.
func (m *Mutator) Snapshot() *Snapshot { return m.snap }

// PatchStatus registers fn to run against Pool.Status.AutoScaling during
// Commit. Multiple calls compose: fns run in the order they were
// registered. The fn receives a pointer to a non-nil
// PoolAutoScalingStatus — when the live object has no AutoScaling block
// yet, Commit allocates one before invoking the chain.
func (m *Mutator) PatchStatus(fn StatusMutateFunc) {
	if fn == nil {
		return
	}
	m.statusMutators = append(m.statusMutators, fn)
}

// SetTargetReplicas requests a write of n to Pool.Spec.Replicas. Calling
// SetTargetReplicas multiple times keeps the most recent value. Negative
// targets are clamped to 0 to match the CRD's Minimum=0 validation.
func (m *Mutator) SetTargetReplicas(n int32) {
	if n < 0 {
		n = 0
	}
	v := n
	m.targetReplicas = &v
}

// TargetReplicas reports the pending replicas write, if any. Exposed
// primarily so tests and downstream decision composition can inspect
// accumulated state without committing.
func (m *Mutator) TargetReplicas() (int32, bool) {
	if m.targetReplicas == nil {
		return 0, false
	}
	return *m.targetReplicas, true
}

// ScaleUpAttempt registers a scale-up intent that Commit will resolve
// by calling the Snapshot's Prober. Use this from Decide instead of
// SetTargetReplicas for autoscaler-initiated scale-ups so the probe
// is consulted and saturation state is recorded. Calling
// ScaleUpAttempt with target <= from is a no-op; later calls overwrite
// earlier ones.
//
// The resulting writes (status + spec + event) are NOT computed until
// Commit, because the probe is I/O. ScaleUpAttempt only buffers the
// intent.
func (m *Mutator) ScaleUpAttempt(from, target int32) {
	if target <= from {
		return
	}
	m.scaleUpAttempt = &scaleUpAttempt{from: from, target: target}
}

// PendingScaleUpAttempt reports the buffered intent, useful for test
// assertions on the decision logic before Commit runs the probe.
func (m *Mutator) PendingScaleUpAttempt() (from, target int32, present bool) {
	if m.scaleUpAttempt == nil {
		return 0, 0, false
	}
	return m.scaleUpAttempt.from, m.scaleUpAttempt.target, true
}

// MarkPodScaleDownProtected stamps the scale-down-protected annotation
// onto pod with the given timestamp (RFC3339 UTC). Used by the scale-down
// two-phase protection flow.
//
// Idempotent at Commit time: if the live Pod already carries the same
// timestamp the patch is skipped.
func (m *Mutator) MarkPodScaleDownProtected(pod *corev1.Pod, at time.Time) {
	if pod == nil {
		return
	}
	m.podAnnOps = append(m.podAnnOps, podAnnotationOp{
		podRef: types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name},
		set:    map[string]string{agentsv1alpha1.SandboxScaleDownProtectedAnnotationKey: at.UTC().Format(time.RFC3339)},
	})
}

// UnmarkPodScaleDownProtected removes the scale-down-protected annotation
// from pod. A JSON merge patch with a null value deletes the key.
func (m *Mutator) UnmarkPodScaleDownProtected(pod *corev1.Pod) {
	if pod == nil {
		return
	}
	m.podAnnOps = append(m.podAnnOps, podAnnotationOp{
		podRef: types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name},
		clear:  []string{agentsv1alpha1.SandboxScaleDownProtectedAnnotationKey},
	})
}

// EmitEvent queues an event for the Pool. The arguments map onto the
// k8s.io/client-go/tools/events.EventRecorder.Eventf signature:
//   - eventType matches corev1.EventTypeNormal / EventTypeWarning;
//   - action is a short verb describing what the controller did
//     (e.g. "ScaleUp", "ScaleDown"). It feeds into the event's
//     `action` field, which the newer events API surfaces alongside
//     reason for richer telemetry;
//   - reason is the more specific CamelCase code conventionally
//     prefixed with the subsystem (e.g. "AutoscalerScaleUp");
//   - format/args produce the human-readable message via fmt.Sprintf.
func (m *Mutator) EmitEvent(eventType, action, reason, format string, args ...any) {
	m.events = append(m.events, eventOp{
		eventType: eventType,
		action:    action,
		reason:    reason,
		message:   fmt.Sprintf(format, args...),
	})
}

// SetScaleDownTransition records the scale-down session state change to
// apply to the shared ScaleDownTracker after this cycle commits. Calling
// it multiple times keeps the last value. Buffering only — the tracker is
// not touched until the reconciler reads ScaleDownTransition post-Commit,
// so a session never advances ahead of a persisted decrement.
func (m *Mutator) SetScaleDownTransition(tr ScaleDownTransition) {
	m.scaleDownTransition = tr
}

// ScaleDownTransition reports the buffered session transition. The
// reconciler applies it to the ScaleDownTracker only after Commit
// succeeds.
func (m *Mutator) ScaleDownTransition() ScaleDownTransition {
	return m.scaleDownTransition
}

// HasWrites reports whether Commit would issue any K8s write. Useful for
// the reconciler's "nothing changed" fast-path logging.
func (m *Mutator) HasWrites() bool {
	return len(m.statusMutators) > 0 ||
		m.targetReplicas != nil ||
		m.scaleUpAttempt != nil ||
		len(m.podAnnOps) > 0 ||
		m.scaleDownTransition.Kind != ScaleDownNoTransition
}

// Commit applies the accumulated writes in fixed order:
//  1. Env spec patch (Env.Spec.Clusters[i].Members[j].Spec.Replicas)
//  2. Pool status patch (Pool.Status.AutoScaling)
//  3. Per-Pod annotation patches
//  4. Events
//
// Each K8s patch is wrapped in retry.RetryOnConflict so a transient
// version race re-reads and re-applies the mutation against the latest
// object. Non-conflict errors abort Commit and propagate to the caller;
// the reconciler is expected to requeue.
//
// recorder may be nil; events are then dropped silently (useful in tests
// and in code paths that have not yet wired up the event sink).
func (m *Mutator) Commit(ctx context.Context, c client.Client, recorder events.EventRecorder) error {
	if m == nil || m.snap == nil || m.snap.Pool == nil {
		return fmt.Errorf("autoscalingstate: nil Mutator or Snapshot")
	}
	if c == nil {
		return fmt.Errorf("autoscalingstate: client.Client is required")
	}
	poolKey := client.ObjectKey{Namespace: m.snap.Pool.Namespace, Name: m.snap.Pool.Name}

	// 0) Resolve scale-up attempts first — they read from the cluster
	//    (probe) and write into the same status/spec/event buffers the
	//    later phases consume.
	if m.scaleUpAttempt != nil {
		m.resolveScaleUpAttempt(ctx)
	}

	// 1) Env spec patch — Member.Spec.Replicas on the owning Env. The
	//    existing Env reconciler then propagates the change onto the
	//    live Pool, so Pool.Spec.Replicas keeps a single writer. We
	//    write Env first because a subsequent failure on the Pool
	//    status patch leaves the decision recorded on the spec side
	//    (which is the user-visible source of truth); the status
	//    timestamps can be reconstructed by the next reconcile from
	//    observed state. The reverse order would race: a successful
	//    status patch without the spec change would mark "cooled down"
	//    without anything actually happening.
	if m.targetReplicas != nil {
		if m.snap.Env == nil {
			return fmt.Errorf("autoscalingstate: SetTargetReplicas called without owning Env in Snapshot")
		}
		envKey := client.ObjectKey{Namespace: m.snap.Env.Namespace, Name: m.snap.Env.Name}
		if err := patchEnvMemberReplicasWithRetry(ctx, c, envKey, m.snap.Pool.Name, *m.targetReplicas); err != nil {
			return fmt.Errorf("patch env %s/%s member %q replicas: %w",
				m.snap.Env.Namespace, m.snap.Env.Name, m.snap.Pool.Name, err)
		}
	}

	// 2) Pool status patch — single sub-resource patch applies all mutators.
	if len(m.statusMutators) > 0 {
		if err := patchStatusWithRetry(ctx, c, poolKey, m.statusMutators); err != nil {
			return fmt.Errorf("patch status.autoscaling: %w", err)
		}
	}

	// 3) Per-Pod annotation patches — best-effort but with retry-on-conflict.
	//    A NotFound on the Pod is treated as success (the entry is gone, the
	//    operation is vacuously satisfied). Other errors abort Commit on
	//    first failure for the same reason: a partial annotation flip
	//    leaves the protection state half-applied, which the next
	//    reconcile can untangle.
	for _, op := range m.podAnnOps {
		if err := op.apply(ctx, c); err != nil {
			return fmt.Errorf("patch pod %s annotations: %w", op.podRef, err)
		}
	}

	// 4) Events — emit only after successful writes so the recorded
	//    message reflects persisted state. The newer
	//    events.EventRecorder.Eventf signature carries `regarding`,
	//    `related`, `eventtype`, `reason`, `action`, and `note`;
	//    autoscaler events have no second object so `related` is nil,
	//    and the message has already been formatted in EmitEvent so we
	//    pass it through as the literal `note`.
	if recorder != nil {
		for _, ev := range m.events {
			recorder.Eventf(m.snap.Pool, nil, ev.eventType, ev.reason, ev.action, "%s", ev.message)
		}
	}
	return nil
}

// podAnnotationOp captures either a "set annotations" or a "clear keys"
// request against one Pod. Both at once is allowed: clears apply first,
// then sets — useful when toggling the value of a single key.
type podAnnotationOp struct {
	podRef types.NamespacedName
	set    map[string]string
	clear  []string
}

func (op podAnnotationOp) apply(ctx context.Context, c client.Client) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		pod := &corev1.Pod{}
		if err := c.Get(ctx, op.podRef, pod); err != nil {
			if apierrors.IsNotFound(err) {
				// Pod gone — operations against it are vacuously satisfied.
				return nil
			}
			return err
		}
		base := pod.DeepCopy()
		if pod.Annotations == nil && (len(op.set) > 0) {
			pod.Annotations = map[string]string{}
		}
		for _, k := range op.clear {
			delete(pod.Annotations, k)
		}
		maps.Copy(pod.Annotations, op.set)
		if equality.Semantic.DeepEqual(base.Annotations, pod.Annotations) {
			return nil
		}
		return c.Patch(ctx, pod, client.MergeFrom(base))
	})
}

type eventOp struct {
	eventType string
	action    string
	reason    string
	message   string
}

// resolveScaleUpAttempt runs the cluster-admission probe queued by
// ScaleUpAttempt and translates the outcome into spec/status writes
// plus a user-visible event. It must run BEFORE the spec/status patches
// in Commit because:
//   - the probe is the source of truth for the final Accepted target,
//     which feeds m.targetReplicas;
//   - the probe outcome drives PoolAutoScalingStatus.LastScaleUpAttempt*,
//     which the success/saturation cooldowns read on the next reconcile.
//
// nil Snapshot.Prober short-circuits with PoolScaleUpAttemptEnough,
// matching the unit-test fast path where plugin admission isn't wired.
func (m *Mutator) resolveScaleUpAttempt(ctx context.Context) {
	a := m.scaleUpAttempt
	now := metav1.NewTime(m.snap.Now)

	accepted, result, errMsg := m.probeAccepted(ctx, a.from, a.target)

	// 1) Status — always update the attempt fingerprint; only stamp
	//    LastScaleUpTime when the probe actually let us grow.
	m.statusMutators = append(m.statusMutators, func(s *agentsv1alpha1.PoolAutoScalingStatus) {
		s.LastScaleUpAttemptTime = &now
		s.LastScaleUpAttemptResult = result
		if accepted > a.from {
			s.LastScaleUpTime = &now
		}
		if result == agentsv1alpha1.PoolScaleUpAttemptEnough {
			s.ScaleUpErrorMessage = ""
		} else {
			s.ScaleUpErrorMessage = errMsg
		}
	})

	// 2) Spec — only when the probe accepted at least one more replica.
	if accepted > a.from {
		v := accepted
		m.targetReplicas = &v
	}

	// 3) Event — Normal when we grew (full or partial), Warning when
	//    saturation/failure kept us at current.
	pool := m.snap.Pool
	switch {
	case accepted > a.from && result == agentsv1alpha1.PoolScaleUpAttemptEnough:
		m.events = append(m.events, eventOp{
			eventType: corev1.EventTypeNormal,
			action:    "ScaleUp",
			reason:    "AutoscalerScaleUp",
			message: fmt.Sprintf("increased %s/%s replicas from %d to %d",
				pool.Namespace, pool.Name, a.from, accepted),
		})
	case accepted > a.from:
		// Partial admission — kept in this branch separately so future
		// finer-grained reporting (PoolScaleUpAttemptJustRight) can
		// override the message without re-introducing dead branches.
		m.events = append(m.events, eventOp{
			eventType: corev1.EventTypeNormal,
			action:    "ScaleUp",
			reason:    "AutoscalerScaleUpPartial",
			message: fmt.Sprintf("partially increased %s/%s replicas from %d to %d (requested %d, plugin accepted %d): %s",
				pool.Namespace, pool.Name, a.from, accepted, a.target, accepted, errMsg),
		})
	default:
		m.events = append(m.events, eventOp{
			eventType: corev1.EventTypeWarning,
			action:    "ScaleUpAttempt",
			reason:    "AutoscalerScaleUpRejected",
			message: fmt.Sprintf("scale-up of %s/%s to %d rejected (%s): %s",
				pool.Namespace, pool.Name, a.target, result, errMsg),
		})
	}
}

// probeAccepted dispatches to the Snapshot's Prober if wired; otherwise
// trivially accepts the full target.
func (m *Mutator) probeAccepted(ctx context.Context, from, target int32) (int32, agentsv1alpha1.PoolScaleUpAttemptResult, string) {
	if m.snap.Prober == nil {
		return target, agentsv1alpha1.PoolScaleUpAttemptEnough, ""
	}
	return m.snap.Prober.Probe(ctx, m.snap.Pool, from, target)
}

// patchEnvMemberReplicasWithRetry updates the named member's Spec.Replicas
// in the owning Env's spec to target. Walks every cluster segment so the
// helper works regardless of which cluster the member lives in — the
// Pool autoscaler does not need to know the local cluster ID. A member
// not found is reported as an error so the caller (and observability) can
// distinguish "Pool reconciler ran against stale Env" from "wrote
// successfully".
//
// Re-reads the Env on every retry so conflicts caused by unrelated spec
// drift (other members, autoscaling toggles, etc.) don't blow up the
// autoscaler.
func patchEnvMemberReplicasWithRetry(ctx context.Context, c client.Client, key client.ObjectKey, poolName string, target int32) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cur := &agentsv1alpha1.SandboxEnv{}
		if err := c.Get(ctx, key, cur); err != nil {
			return err
		}
		ci, mi, ok := findMemberIndex(cur, poolName)
		if !ok {
			return fmt.Errorf("member %q not present in env %s/%s", poolName, key.Namespace, key.Name)
		}
		if cur.Spec.Clusters[ci].Members[mi].Spec.Replicas == target {
			return nil
		}
		base := cur.DeepCopy()
		cur.Spec.Clusters[ci].Members[mi].Spec.Replicas = target
		return c.Patch(ctx, cur, client.MergeFrom(base))
	})
}

// findMemberIndex returns the (clusterIdx, memberIdx) coordinates of the
// member named poolName inside env.Spec, or ok=false when no such
// member exists. Member names are unique across the Env so the first
// match is authoritative.
func findMemberIndex(env *agentsv1alpha1.SandboxEnv, poolName string) (clusterIdx, memberIdx int, ok bool) {
	for ci := range env.Spec.Clusters {
		ms := env.Spec.Clusters[ci].Members
		for mi := range ms {
			if ms[mi].Name == poolName {
				return ci, mi, true
			}
		}
	}
	return 0, 0, false
}

// patchStatusWithRetry runs every mutator against the live
// Status.AutoScaling and patches the status sub-resource. The mutators
// are applied to a fresh DeepCopy each retry so partial in-place edits
// from a failed attempt don't leak across attempts.
//
// An empty net effect (DeepEqual before/after) skips the patch entirely
// — important to keep the status from being patched every reconcile
// when nothing actually changed (e.g. cooldown-active short-circuits).
func patchStatusWithRetry(ctx context.Context, c client.Client, key client.ObjectKey, mutators []StatusMutateFunc) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cur := &agentsv1alpha1.SandboxPool{}
		if err := c.Get(ctx, key, cur); err != nil {
			return err
		}
		base := cur.DeepCopy()
		if cur.Status.AutoScaling == nil {
			cur.Status.AutoScaling = &agentsv1alpha1.PoolAutoScalingStatus{}
		}
		for _, fn := range mutators {
			fn(cur.Status.AutoScaling)
		}
		cur.Status.AutoScaling.ObservedGeneration = cur.Generation
		if equality.Semantic.DeepEqual(base.Status, cur.Status) {
			return nil
		}
		return c.Status().Patch(ctx, cur, client.MergeFrom(base))
	})
}
