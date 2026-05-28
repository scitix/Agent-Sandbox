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
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/inplaceupdate"
	pkgmetrics "github.com/scitix/agent-sandbox/pkg/metrics"
)

func (r *SandboxPoolReconciler) syncPodProtectionFinalizers(ctx context.Context, pods []corev1.Pod) ([]corev1.Pod, error) {
	for i := range pods {
		pod := &pods[i]
		if pod.DeletionTimestamp != nil {
			continue
		}
		if containsString(pod.Finalizers, agentsv1alpha1.SandboxProtectionFinalizer) {
			continue
		}

		// Keep the protection finalizer attached for the full pod lifetime so
		// an Idle -> Starting/Running transition never passes through an
		// unprotected window after Stopping -> Idle cleanup.
		patch := client.MergeFrom(pod.DeepCopy())
		pod.Finalizers = append(pod.Finalizers, agentsv1alpha1.SandboxProtectionFinalizer)
		if err := r.Patch(ctx, pod, patch); err != nil {
			klog.ErrorS(err, "Failed to backfill sandbox-protection finalizer on existing pod",
				"namespace", pod.Namespace, "name", pod.Name)
			return pods, err
		}
		// pods[i].Finalizers already updated in-memory above.
	}
	return pods, nil
}

// syncDeletingPods handles pods that have a DeletionTimestamp set, writing terminal
// store records and removing the sandbox-protection finalizer so GC can proceed.
func (r *SandboxPoolReconciler) syncDeletingPods(ctx context.Context, pods []corev1.Pod) ([]corev1.Pod, error) {
	for i := range pods {
		pod := &pods[i]
		if pod.DeletionTimestamp == nil {
			continue
		}
		if !containsString(pod.Finalizers, agentsv1alpha1.SandboxProtectionFinalizer) {
			continue
		}

		switch inplaceupdate.GetSandboxPhase(pod) {
		case agentsv1alpha1.SandboxPhaseRunning:
			klog.V(2).InfoS("Running pod is terminating, recording as failed",
				"namespace", pod.Namespace, "name", pod.Name)

			if sandboxID := pod.Labels[agentsv1alpha1.SandboxIDLabelKey]; sandboxID != "" {
				terminatedAt := pod.DeletionTimestamp.UTC().Format(time.RFC3339)
				failureReason := pod.Status.Reason
				if failureReason == "" {
					failureReason = "Evicted"
				}
				failureMessage := pod.Status.Message
				if failureMessage == "" {
					failureMessage = "Pod was deleted or evicted"
				}

				if r.SandboxStore != nil {
					record := sandboxRecordFromPod(pod, "Failed", terminatedAt, failureReason, nil, failureMessage)
					setRecycledAtNow(&record)
					if err := r.SandboxStore.Save(record); err != nil {
						klog.ErrorS(err, "Failed to write Failed record for terminating pod",
							"namespace", pod.Namespace, "name", pod.Name)
					}
				}

				emitSandboxStopMetrics(pod, string(agentsv1alpha1.SandboxStopReasonFailed),
					pod.Annotations[agentsv1alpha1.SandboxClaimedAtAnnotationKey],
					pod.Annotations[agentsv1alpha1.SandboxStartedAtAnnotationKey], terminatedAt)
			}

		case agentsv1alpha1.SandboxPhaseStopping:
			klog.V(2).InfoS("Stopping pod is terminating, recording terminal sandbox state",
				"namespace", pod.Namespace, "name", pod.Name)

			if sandboxID := pod.Labels[agentsv1alpha1.SandboxIDLabelKey]; sandboxID != "" {
				record := CaptureSandboxStopRecord(pod)
				if record.TerminatedAt == nil {
					t := pod.DeletionTimestamp.UTC()
					record.TerminatedAt = &t
				}

				if r.SandboxStore != nil {
					setRecycledAtNow(&record)
					if err := r.SandboxStore.Save(record); err != nil {
						klog.ErrorS(err, "Failed to write terminal record for terminating stopping pod",
							"namespace", pod.Namespace, "name", pod.Name)
					}
				}

				emitSandboxStopMetrics(pod, string(record.Status),
					record.ClaimedAt.Format(time.RFC3339),
					formatRFC3339Ptr(record.StartedAt),
					formatRFC3339Ptr(record.TerminatedAt))
			}
		}

		if err := removeSandboxProtectionFinalizer(ctx, r.Client, pod); err != nil && !errors.IsNotFound(err) {
			return pods, err
		}
		// pod.Finalizers updated in-memory by removeSandboxProtectionFinalizer (MergeFrom patch updates the object)
	}
	return pods, nil
}

// syncFailedPods detects pods that Kubernetes considers permanently Failed
// (e.g. evicted due to node memory pressure) and deletes them so the pool
// controller can create fresh replacements. Without this, evicted pods whose
// sandbox-phase label is "stopping" (or any other active phase) would remain
// stuck forever: the in-place update can never complete on a dead pod, and no
// other reconcile path handles this scenario.
func (r *SandboxPoolReconciler) syncFailedPods(ctx context.Context, pods []corev1.Pod) ([]corev1.Pod, error) {
	result := pods[:0:len(pods)]
	for i := range pods {
		pod := &pods[i]

		// Only handle pods that Kubernetes considers permanently Failed.
		if pod.Status.Phase != corev1.PodFailed {
			result = append(result, pods[i])
			continue
		}

		// Skip pods already being deleted.
		if pod.DeletionTimestamp != nil {
			result = append(result, pods[i])
			continue
		}

		sandboxPhase := pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey]
		klog.InfoS("Detected Failed/Evicted pod, deleting for replacement",
			"namespace", pod.Namespace, "name", pod.Name,
			"sandboxPhase", sandboxPhase,
			"reason", pod.Status.Reason, "message", pod.Status.Message)

		// Write terminal store record and emit metrics if the pod had an active sandbox session.
		if sandboxID := pod.Labels[agentsv1alpha1.SandboxIDLabelKey]; sandboxID != "" {
			terminatedAt := time.Now().UTC().Format(time.RFC3339)
			// Prefer the terminated-at annotation if present (set by ReleaseSandboxPod).
			if t := pod.Annotations[agentsv1alpha1.SandboxTerminatedAtAnnotationKey]; t != "" {
				terminatedAt = t
			}
			failureReason := pod.Status.Reason // e.g. "Evicted"
			if failureReason == "" {
				failureReason = "PodFailed"
			}

			// Use stop-reason from annotation when available (e.g. pod was already in
			// Stopping when evicted), otherwise default to "Failed".
			stopReason := pod.Annotations[agentsv1alpha1.SandboxStopReasonAnnotationKey]
			if stopReason == "" {
				stopReason = string(agentsv1alpha1.SandboxStopReasonFailed)
			}

			if r.SandboxStore != nil {
				record := sandboxRecordFromPod(pod, string(agentsv1alpha1.SandboxStopReasonFailed), terminatedAt, failureReason, nil, pod.Status.Message)
				setRecycledAtNow(&record)
				if err := r.SandboxStore.Save(record); err != nil {
					klog.ErrorS(err, "Failed to write store record for failed pod",
						"namespace", pod.Namespace, "name", pod.Name)
				}
			}

			emitSandboxStopMetrics(pod, stopReason,
				pod.Annotations[agentsv1alpha1.SandboxClaimedAtAnnotationKey],
				pod.Annotations[agentsv1alpha1.SandboxStartedAtAnnotationKey], terminatedAt)
		}

		// Delete the pod so the pool scale-up logic creates a fresh replacement.
		// Remove the sandbox-protection finalizer first so the pod can actually be GC'd.
		if err := r.removeSandboxProtectionFinalizer(ctx, pod); err != nil && !errors.IsNotFound(err) {
			klog.ErrorS(err, "Failed to remove finalizer from evicted/failed pod",
				"namespace", pod.Namespace, "name", pod.Name)
			return pods, err
		}
		if err := r.Delete(ctx, pod); err != nil {
			if !errors.IsNotFound(err) {
				klog.ErrorS(err, "Failed to delete evicted/failed pod",
					"namespace", pod.Namespace, "name", pod.Name)
				return pods, err
			}
		}
		// Pod deleted: do not append to result, effectively removing it from the slice.
	}
	return result, nil
}

func (r *SandboxPoolReconciler) syncInplaceUpdatePhases(ctx context.Context, sandboxPool *agentsv1alpha1.SandboxPool, pods []corev1.Pod) ([]corev1.Pod, error) {
	// Separate pods into those that need phase-completion work and those that don't.
	// Stopping and Starting pods that have completed their in-place update are
	// processed concurrently to avoid serialising N apiserver round-trips on the
	// reconcile goroutine.  All other pods pass through unchanged.

	type result struct {
		idx     int
		updated *corev1.Pod
	}

	var (
		mu      sync.Mutex
		results []result
		// notifyPools collects (namespace, poolName) pairs that need an idle
		// notification after all concurrent work completes, so the scheduler
		// refresh happens once the API server writes have been issued.
		notifyPools []notifyEntry
	)

	eg, egCtx := errgroup.WithContext(ctx)

	for i := range pods {
		pod := &pods[i]
		if pod.DeletionTimestamp != nil {
			continue
		}

		phase := inplaceupdate.GetSandboxPhase(pod)
		if phase != agentsv1alpha1.SandboxPhaseStarting && phase != agentsv1alpha1.SandboxPhaseStopping {
			continue
		}

		state, err := inplaceupdate.GetInplaceUpdateState(pod)
		if err != nil {
			klog.ErrorS(err, "Failed to parse inplace update state", "namespace", pod.Namespace, "name", pod.Name)
			continue
		}
		if state == nil || !inplaceupdate.IsInplaceUpdateCompleted(ctx, sandboxPool, pod, r.DigestResolver) {
			continue
		}

		// Capture loop variables for the goroutine.
		idx := i
		podSnap := pod.DeepCopy()
		phaseCopy := phase

		eg.Go(func() error {
			switch phaseCopy {
			case agentsv1alpha1.SandboxPhaseStopping:
				// Capture stop metadata BEFORE MarkUpdateCompleted so we read the
				// annotations written by ReleaseSandboxPod before cleanup erases them.
				record := CaptureSandboxStopRecord(podSnap)
				updated, err := inplaceupdate.MarkUpdateCompleted(egCtx, r.Client, sandboxPool, podSnap, r.DigestResolver)
				if err != nil {
					klog.ErrorS(err, "Failed to mark inplace update complete", "namespace", podSnap.Namespace, "name", podSnap.Name)
					return err
				}

				recycledAt := time.Now().UTC()

				if record.TerminatedAt != nil {
					pkgmetrics.SandboxRecycleDuration.With(prometheus.Labels{
						"namespace":   podSnap.Namespace,
						"pool":        podSnap.Labels[agentsv1alpha1.SandboxPoolLabelKey],
						"team":        podSnap.Labels[agentsv1alpha1.LabelTeam],
						"user":        podSnap.Labels[agentsv1alpha1.LabelUser],
						"sandbox_env": podSnap.Labels[agentsv1alpha1.LabelEnv],
					}).Observe(recycledAt.Sub(*record.TerminatedAt).Seconds())
				}

				if r.SandboxStore != nil && record.SandboxId != "" {
					record.RecycledAt = &recycledAt
					if err := r.SandboxStore.Save(record); err != nil {
						klog.ErrorS(err, "writeDeferredStoreRecord: failed to save sandbox record",
							"namespace", podSnap.Namespace, "name", podSnap.Name, "sandboxID", record.SandboxId)
					}
					emitSandboxStopMetrics(podSnap, string(record.Status),
						record.ClaimedAt.Format(time.RFC3339),
						formatRFC3339Ptr(record.StartedAt),
						formatRFC3339Ptr(record.TerminatedAt))
				}

				mu.Lock()
				if updated != nil {
					results = append(results, result{idx: idx, updated: updated})
				}
				// Collect idle notification: fire AFTER the errgroup so that the
				// informer cache has a chance to reflect the apiserver write before
				// refreshReady() runs. OnSandboxReleased is time-insensitive (cache
				// eviction), so it is also deferred to the same batch.
				notifyPools = append(notifyPools, notifyEntry{
					namespace:  podSnap.Namespace,
					poolName:   podSnap.Labels[agentsv1alpha1.SandboxPoolLabelKey],
					sandboxID:  record.SandboxId,
					notifyIdle: r.IdleNotifier != nil,
				})
				mu.Unlock()

			case agentsv1alpha1.SandboxPhaseStarting:
				updated, err := inplaceupdate.MarkUpdateCompleted(egCtx, r.Client, sandboxPool, podSnap, r.DigestResolver)
				if err != nil {
					klog.ErrorS(err, "Failed to mark inplace update complete", "namespace", podSnap.Namespace, "name", podSnap.Name)
					return err
				}
				if updated != nil {
					mu.Lock()
					results = append(results, result{idx: idx, updated: updated})
					mu.Unlock()
					if r.SandboxReadyHook != nil {
						snap := updated.DeepCopy()
						go r.SandboxReadyHook.OnSandboxReady(context.Background(), snap)
					}
				}
			default:
				klog.ErrorS(nil, "Unhandled sandbox phase", "namespace", podSnap.Namespace, "name", podSnap.Name, "phase", phaseCopy)
			}
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return pods, err
	}

	// Apply in-memory updates back to the pods slice.
	for _, res := range results {
		pods[res.idx] = *res.updated
	}

	// Notify the claim scheduler and evict route-cache entries. We do this AFTER
	// all MarkUpdateCompleted writes have completed and after the errgroup is
	// done. The informer watch pipeline typically delivers the update event within
	// ~50-100 ms; firing here (rather than inside the goroutine) gives the maximum
	// head-start before refreshReady() runs its List against the informer cache.
	if r.IdleNotifier != nil {
		// Deduplicate by pool so a burst of N Stopping→Idle transitions only
		// fires one NotifyIdleAvailable per pool instead of N.
		notified := make(map[string]struct{}, len(notifyPools))
		for _, e := range notifyPools {
			key := e.namespace + "/" + e.poolName
			if _, seen := notified[key]; !seen && e.notifyIdle {
				r.IdleNotifier.NotifyIdleAvailable(e.namespace, e.poolName)
				notified[key] = struct{}{}
			}
			if e.sandboxID != "" {
				r.IdleNotifier.OnSandboxReleased(ctx, e.sandboxID)
			}
		}
	}

	return pods, nil
}

type notifyEntry struct {
	namespace  string
	poolName   string
	sandboxID  string
	notifyIdle bool
}

func (r *SandboxPoolReconciler) syncRestartedRunningPods(
	ctx context.Context,
	sandboxPool *agentsv1alpha1.SandboxPool,
	pods []corev1.Pod,
) ([]corev1.Pod, error) { //nolint:unparam
	for i := range pods {
		pod := &pods[i]
		if pod.DeletionTimestamp != nil {
			continue
		}
		if inplaceupdate.GetSandboxPhase(pod) != agentsv1alpha1.SandboxPhaseRunning {
			continue
		}

		// Case 2: Container restarted unexpectedly (OOM, crash, etc.)
		if !inplaceupdate.HasUnexpectedRestart(pod) {
			continue
		}

		klog.InfoS("Detected unexpected restart in running pod, triggering stopping",
			"namespace", pod.Namespace, "name", pod.Name)

		// Determine failure reason from LastTerminationState. The terminated
		// record's FinishedAt is the kubelet-reported time the old container
		// exited — we intentionally do NOT use it as the sandbox terminatedAt,
		// because it can lag behind (or drift from) the controller's own clock,
		// and the timestamp users care about is when we detected the failure and
		// stopped accepting work, not the precise container-exit moment.
		failureReason := "UnexpectedRestart"
		var exitCode *int32
		var failureMessage string
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.LastTerminationState.Terminated != nil {
				t := cs.LastTerminationState.Terminated
				failureReason = t.Reason
				ec := t.ExitCode
				exitCode = &ec
				failureMessage = t.Message
				break
			}
		}

		detectedAt := time.Now().UTC()

		// Recycle the pod back to Idle; stop metadata is written to pod annotations
		// so the reconciler can perform a deferred store write at Stopping→Idle.
		// DisableRetry: on conflict we skip this pod and let the next Reconcile
		// re-observe the pod's true state before acting, avoiding misfires.
		updated, err := ReleaseSandboxPod(ctx, r.Client, pod, sandboxPool, ReleaseSandboxPodOptions{
			StopReason:                  agentsv1alpha1.SandboxStopReasonFailed,
			TerminatedAt:                detectedAt.Format(time.RFC3339),
			FailureReason:               failureReason,
			FailureMessage:              failureMessage,
			ExitCode:                    exitCode,
			ExpectedCurrentSandboxPhase: agentsv1alpha1.SandboxPhaseRunning,
			DisableRetry:                true,
		})
		if err != nil {
			if errors.IsConflict(err) {
				klog.V(3).InfoS("Conflict releasing restarted pod, skipping until next reconcile",
					"namespace", pod.Namespace, "name", pod.Name)
				continue
			}
			klog.ErrorS(err, "Failed to release restarted pod", "namespace", pod.Namespace, "name", pod.Name)
			continue
		}
		if updated != nil {
			pods[i] = *updated
		}
	}
	return pods, nil
}
