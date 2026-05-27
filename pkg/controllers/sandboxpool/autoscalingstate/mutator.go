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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
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
//     status sub-resource patch.
//
//   - SetTargetReplicas requests an update to Pool.Spec.Replicas. May be
//     called at most once per Mutator; the last call wins. A no-op is
//     elided at Commit time when target == current.
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
	podAnnOps      []podAnnotationOp
	events         []eventOp
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

// EmitEvent queues an event for the Pool. eventType matches corev1
// EventTypeNormal / EventTypeWarning; reason is a short CamelCase code;
// format/args produce the human-readable message via fmt.Sprintf.
func (m *Mutator) EmitEvent(eventType, reason, format string, args ...any) {
	m.events = append(m.events, eventOp{
		eventType: eventType,
		reason:    reason,
		message:   fmt.Sprintf(format, args...),
	})
}

// HasWrites reports whether Commit would issue any K8s write. Useful for
// the reconciler's "nothing changed" fast-path logging.
func (m *Mutator) HasWrites() bool {
	return len(m.statusMutators) > 0 || m.targetReplicas != nil || len(m.podAnnOps) > 0
}

// Commit applies the accumulated writes in fixed order:
//  1. Spec patch (Pool.Spec.Replicas)
//  2. Status patch (Pool.Status.AutoScaling)
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
func (m *Mutator) Commit(ctx context.Context, c client.Client, recorder record.EventRecorder) error {
	if m == nil || m.snap == nil || m.snap.Pool == nil {
		return fmt.Errorf("autoscalingstate: nil Mutator or Snapshot")
	}
	if c == nil {
		return fmt.Errorf("autoscalingstate: client.Client is required")
	}
	key := client.ObjectKey{Namespace: m.snap.Pool.Namespace, Name: m.snap.Pool.Name}

	// 1) Spec patch — write first so a subsequent failure on Status does
	//    not strand the decision; the next reconcile re-derives status
	//    from observed state.
	if m.targetReplicas != nil {
		if err := patchSpecReplicasWithRetry(ctx, c, key, *m.targetReplicas); err != nil {
			return fmt.Errorf("patch spec.replicas: %w", err)
		}
	}

	// 2) Status patch — single sub-resource patch applies all mutators.
	if len(m.statusMutators) > 0 {
		if err := patchStatusWithRetry(ctx, c, key, m.statusMutators); err != nil {
			return fmt.Errorf("patch status.autoscaling: %w", err)
		}
	}

	// 3) Per-Pod annotation patches — best-effort but with retry-on-conflict.
	//    A NotFound on the Pod is logged via the returned error; the caller
	//    decides whether to requeue. We bail on first failure for the same
	//    reason — partial annotation flips leave the protection state
	//    half-applied which the next reconcile can untangle.
	for _, op := range m.podAnnOps {
		if err := op.apply(ctx, c); err != nil {
			return fmt.Errorf("patch pod %s annotations: %w", op.podRef, err)
		}
	}

	// 4) Events — emit only after successful writes so the recorded
	//    message reflects persisted state.
	if recorder != nil {
		for _, ev := range m.events {
			recorder.Event(m.snap.Pool, ev.eventType, ev.reason, ev.message)
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
	reason    string
	message   string
}

// patchSpecReplicasWithRetry updates Pool.Spec.Replicas to target. Re-reads
// the Pool on every retry so conflicts caused by drift in other spec fields
// don't blow up the autoscaler.
func patchSpecReplicasWithRetry(ctx context.Context, c client.Client, key client.ObjectKey, target int32) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cur := &agentsv1alpha1.SandboxPool{}
		if err := c.Get(ctx, key, cur); err != nil {
			return err
		}
		if cur.Spec.Replicas == target {
			return nil
		}
		base := cur.DeepCopy()
		cur.Spec.Replicas = target
		return c.Patch(ctx, cur, client.MergeFrom(base))
	})
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
