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
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlbuilder "sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/controllers/sandboxpool/autoscalingstate"
	"github.com/scitix/agent-sandbox/pkg/framework/plugins"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/inplaceupdate"
	pkgmetrics "github.com/scitix/agent-sandbox/pkg/metrics"
	"github.com/scitix/agent-sandbox/pkg/store"
	"github.com/scitix/agent-sandbox/pkg/utils/imageresolver"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

const (
	// FinalizerName is the finalizer name for SandboxPool
	FinalizerName = "agentbox.navix.sh/finalizer"

	// RequeueAfter is the duration to wait before requeuing
	RequeueAfter = 10 * time.Second
)

// SandboxPoolReconciler reconciles a SandboxPool object
type SandboxPoolReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	Recorder      events.EventRecorder   // for publishing Phase-transition events
	Clientset     kubernetes.Interface   // nil means Event-based diagnostics disabled
	SandboxStore  store.SandboxStore     // nil means history recording disabled
	PluginManager *plugins.PluginManager // nil means lifecycle plugins disabled
	// IdleNotifier is called whenever a Pod transitions Stopping → Idle.
	// When non-nil, it wakes the per-pool claim scheduler immediately so
	// pending Create requests can be dispatched without a poll-timer delay.
	// nil = disabled (no notification sent).
	IdleNotifier IdleNotifier
	// SandboxReadyHook is called (in a goroutine) after a Starting pod is
	// successfully marked Running via MarkUpdateCompleted. nil = disabled.
	SandboxReadyHook SandboxReadyHook
	// DigestResolver resolves image tags to content digests for in-place
	// update completion detection. nil = disabled (digest-based comparison
	// will fail and updates may not complete).
	DigestResolver imageresolver.DigestResolver

	// runningInfoPrev tracks the SandboxRunningInfo gauge label-sets that were
	// emitted in the previous reconciliation cycle, keyed by pool (ns/name).
	// On each cycle we diff against the new set: entries that disappeared (pod
	// deleted, phase changed, sandbox-id removed) are explicitly Deleted from
	// the GaugeVec. This eliminates metric leaks caused by pods vanishing
	// between cycles.
	runningInfoMu   sync.Mutex
	runningInfoPrev map[string]map[runningInfoKey]prometheus.Labels // key = "namespace/pool"

	// expectations tracks in-flight pod creation/deletion counts to prevent
	// informer-cache lag from causing oscillation. When a scale-up creates N
	// pods, N pending creations are recorded; subsequent Reconcile calls skip
	// scaling until all N Pod Add events are observed (or a 5-minute TTL fires).
	expectations *PoolExpectations

	// AutoscalingLoader is the per-Pool autoscaler's Snapshot builder.
	// nil disables the autoscaler entirely (legacy mode, unit tests
	// that only exercise pod reconciliation). When set, it must be
	// driven by the same K8s client + LastCreateTracker +
	// SchedulerLookup the rest of the process is using; see
	// cmd/sandbox/app for wire-up.
	AutoscalingLoader *autoscalingstate.Loader

	// AutoscalingEventRecorder is the events-API event recorder used
	// by the autoscaler's Mutator.Commit to emit ScaleUp / ScaleDown
	// events on the SandboxPool. May be nil; in that case the
	// autoscaler's events are dropped silently. Kept separate from
	// the existing Recorder field above because the two are wired
	// through different injection paths.
	AutoscalingEventRecorder events.EventRecorder
}

// +kubebuilder:rbac:groups=agents.navix.sh,resources=sandboxpools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agents.navix.sh,resources=sandboxpools/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=agents.navix.sh,resources=sandboxpools/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=pods/log,verbs=get
// +kubebuilder:rbac:groups=core,resources=pods/exec,verbs=create
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch;get;list
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;delete;patch;update
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *SandboxPoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	klog.V(2).InfoS("Reconciling SandboxPool", "namespace", req.Namespace, "name", req.Name)

	// Fetch the SandboxPool instance
	sandboxPool := &agentsv1alpha1.SandboxPool{}
	if err := r.Get(ctx, req.NamespacedName, sandboxPool); err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return. Created objects are automatically garbage collected.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		klog.ErrorS(err, "Failed to get SandboxPool", "namespace", req.Namespace, "name", req.Name)
		return reconcile.Result{}, err
	}

	// Handle deletion
	if !sandboxPool.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, sandboxPool)
	}

	// Ensure finalizer is present
	if !containsString(sandboxPool.Finalizers, FinalizerName) {
		sandboxPool.Finalizers = append(sandboxPool.Finalizers, FinalizerName)
		if err := r.Update(ctx, sandboxPool); err != nil {
			if errors.IsConflict(err) {
				return reconcile.Result{Requeue: true}, nil
			}
			klog.ErrorS(err, "Failed to add finalizer to SandboxPool", "namespace", req.Namespace, "name", req.Name)
			return reconcile.Result{}, err
		}
		return reconcile.Result{Requeue: true}, nil
	}

	// Run the autoscaling decision pipeline BEFORE Pod reconciliation
	// so any Spec.Replicas / Status.AutoScaling writes land before the
	// pod-execution loop reads them. The autoscaler writes to the
	// owning Env's Member.Spec.Replicas; the Env reconciler then
	// propagates the change onto this Pool's Spec.Replicas on its
	// next pass, which triggers a fresh Reconcile here and the
	// reconcilePods loop below picks up the new desired count.
	//
	// When AutoscalingLoader is nil (unit tests, deployments that
	// run the controller without an autoscaler), this is a no-op
	// and we fall straight through to pod reconciliation.
	if err := r.syncAutoscaling(ctx, sandboxPool); err != nil {
		return reconcile.Result{}, err
	}

	// Reconcile Pod replicas
	return r.reconcilePods(ctx, sandboxPool)
}

// handleDeletion handles the deletion of a SandboxPool by cleaning up associated Pods
func (r *SandboxPoolReconciler) handleDeletion(ctx context.Context, sandboxPool *agentsv1alpha1.SandboxPool) (ctrl.Result, error) {
	klog.V(2).InfoS("Handling SandboxPool deletion", "namespace", sandboxPool.Namespace, "name", sandboxPool.Name)

	if err := r.markPoolTerminating(ctx, sandboxPool); err != nil {
		if errors.IsConflict(err) {
			return reconcile.Result{Requeue: true}, nil
		}
		klog.ErrorS(err, "Failed to mark SandboxPool as terminating", "namespace", sandboxPool.Namespace, "name", sandboxPool.Name)
		return reconcile.Result{}, err
	}

	// List all Pods belonging to this SandboxPool
	pods, err := indexer.ListPodsBySandboxPool(ctx, r.Client, sandboxPool.Namespace, sandboxPool.Name)
	if err != nil {
		klog.ErrorS(err, "Failed to list Pods for SandboxPool", "namespace", sandboxPool.Namespace, "name", sandboxPool.Name)
		return reconcile.Result{}, err
	}

	// Delete remaining Pods before removing the finalizer.
	if len(pods) > 0 {
		for i := range pods {
			pod := &pods[i]
			if !pod.DeletionTimestamp.IsZero() {
				// Pod is already terminating — ensure Finalizer is removed so GC can proceed.
				if err := r.removeSandboxProtectionFinalizer(ctx, pod); err != nil && !errors.IsNotFound(err) {
					klog.ErrorS(err, "Failed to remove finalizer from terminating pod during pool cleanup",
						"namespace", pod.Namespace, "name", pod.Name)
					return reconcile.Result{}, err
				}
				continue
			}

			// Remove the sandbox-protection finalizer before deleting.
			if err := r.removeSandboxProtectionFinalizer(ctx, pod); err != nil && !errors.IsNotFound(err) {
				klog.ErrorS(err, "Failed to remove finalizer from pod during SandboxPool cleanup",
					"namespace", pod.Namespace, "name", pod.Name)
				return reconcile.Result{}, err
			}
			if err := r.Delete(ctx, pod); err != nil && !errors.IsNotFound(err) {
				klog.ErrorS(err, "Failed to delete Pod during SandboxPool cleanup", "namespace", pod.Namespace, "name", pod.Name)
				return reconcile.Result{}, err
			}
		}

		klog.V(2).InfoS("Waiting for Pods to be deleted", "namespace", sandboxPool.Namespace, "name", sandboxPool.Name, "count", len(pods))
		return reconcile.Result{RequeueAfter: RequeueAfter}, nil
	}

	// Remove the finalizer
	sandboxPool.Finalizers = removeString(sandboxPool.Finalizers, FinalizerName)
	if err := r.Update(ctx, sandboxPool); err != nil {
		if errors.IsConflict(err) {
			return reconcile.Result{Requeue: true}, nil
		}
		klog.ErrorS(err, "Failed to remove finalizer from SandboxPool", "namespace", sandboxPool.Namespace, "name", sandboxPool.Name)
		return reconcile.Result{}, err
	}

	// Clean up any remaining SandboxRunningInfo metrics for this pool.
	poolKey := sandboxPool.Namespace + "/" + sandboxPool.Name
	r.runningInfoMu.Lock()
	if prev := r.runningInfoPrev[poolKey]; prev != nil {
		for _, labels := range prev {
			pkgmetrics.SandboxRunningInfo.Delete(labels)
		}
		delete(r.runningInfoPrev, poolKey)
	}
	r.runningInfoMu.Unlock()

	// Clean up Pool replica gauges so stale metrics are not retained after deletion.
	// GaugeVec retains the last Set() value indefinitely until the process restarts,
	// so we must explicitly delete the label set here.
	poolMetricLabels := prometheus.Labels{
		"namespace": sandboxPool.Namespace,
		"pool":      sandboxPool.Name,
		"team":      sandboxPool.Labels[agentsv1alpha1.LabelTeam],
		"user":      sandboxPool.Labels[agentsv1alpha1.LabelUser],
	}
	pkgmetrics.PoolReplicasDesired.Delete(poolMetricLabels)
	pkgmetrics.PoolReplicasIdle.Delete(poolMetricLabels)
	pkgmetrics.PoolReplicasRunning.Delete(poolMetricLabels)
	pkgmetrics.PoolReplicasStarting.Delete(poolMetricLabels)
	pkgmetrics.PoolReplicasStopping.Delete(poolMetricLabels)
	pkgmetrics.PoolReplicasFailed.Delete(poolMetricLabels)

	klog.InfoS("SandboxPool deleted successfully", "namespace", sandboxPool.Namespace, "name", sandboxPool.Name)

	// Clean up in-memory expectations so the map does not grow unboundedly.
	if r.expectations != nil {
		r.expectations.DeleteExpectations(types.NamespacedName{
			Namespace: sandboxPool.Namespace,
			Name:      sandboxPool.Name,
		})
	}

	return reconcile.Result{}, nil
}

// reconcilePods reconciles the Pod replicas to match the desired count and updates status
func (r *SandboxPoolReconciler) reconcilePods(ctx context.Context, sandboxPool *agentsv1alpha1.SandboxPool) (ctrl.Result, error) { //nolint:gocyclo
	poolKey := types.NamespacedName{Namespace: sandboxPool.Namespace, Name: sandboxPool.Name}

	// List all Pods belonging to this SandboxPool
	rawPods, err := indexer.ListPodsBySandboxPool(ctx, r.Client, sandboxPool.Namespace, sandboxPool.Name)
	if err != nil {
		klog.ErrorS(err, "Failed to list Pods for SandboxPool", "namespace", sandboxPool.Namespace, "name", sandboxPool.Name)
		return reconcile.Result{}, err
	}

	// Deep-copy pods to avoid mutating the cache objects.
	pods := make([]corev1.Pod, len(rawPods))
	for i := range rawPods {
		pods[i] = *rawPods[i].DeepCopy()
	}

	// PreUpdate
	oldPool := sandboxPool.DeepCopy() // defensive copy to detect mutations
	pluginUpdated, pluginErr := r.PluginManager.PreUpdatePool(ctx, sandboxPool, pods)
	if pluginErr != nil {
		klog.ErrorS(pluginErr, "Plugin PreUpdatePool failed", "namespace", sandboxPool.Namespace, "name", sandboxPool.Name)
		return reconcile.Result{}, pluginErr
	}
	if pluginUpdated && !equality.Semantic.DeepEqual(oldPool, sandboxPool) {
		if err := r.Update(ctx, sandboxPool); err != nil {
			if errors.IsConflict(err) {
				return reconcile.Result{Requeue: true}, nil
			}
			klog.ErrorS(err, "Failed to update SandboxPool after plugin mutation", "namespace", sandboxPool.Namespace, "name", sandboxPool.Name)
			return reconcile.Result{}, err
		}
	}

	// Delete pods that Kubernetes considers permanently Failed (e.g. evicted).
	// These pods will never recover — their containers won't restart — so we
	// must remove them and let the scale-up logic create fresh replacements.
	pods, err = r.syncPodProtectionFinalizers(ctx, pods)
	if err != nil {
		return reconcile.Result{}, err
	}

	pods, err = r.syncDeletingPods(ctx, pods)
	if err != nil {
		return reconcile.Result{}, err
	}

	pods, err = r.syncFailedPods(ctx, pods)
	if err != nil {
		return reconcile.Result{}, err
	}

	pods, err = r.syncInplaceUpdatePhases(ctx, sandboxPool, pods)
	if err != nil {
		return reconcile.Result{}, err
	}

	pods, err = r.syncRestartedRunningPods(ctx, sandboxPool, pods)
	if err != nil {
		return reconcile.Result{}, err
	}

	desiredReplicas := sandboxPool.Spec.Replicas
	currentReplicas := int32(len(pods))

	// Calculate pod phase counts, pool phase, and conditions
	status := r.calculatePodStatus(sandboxPool.Namespace+"/"+sandboxPool.Name, pods, sandboxPool)
	status.Selector = fmt.Sprintf("%s=%s", agentsv1alpha1.SandboxPoolLabelKey, sandboxPool.Name)
	status.LabelSelector = &metav1.LabelSelector{
		MatchLabels: map[string]string{
			agentsv1alpha1.SandboxPoolLabelKey: sandboxPool.Name,
		},
	}
	status.PhaseSelectors = buildPhaseSelectors(sandboxPool.Name)

	// Update status if changed; emit phase-transition events when the phase changes.
	if !r.statusEquals(sandboxPool, status) {
		oldPhase := sandboxPool.Status.Phase
		sandboxPool.Status = status
		if err := r.Status().Update(ctx, sandboxPool); err != nil {
			if errors.IsConflict(err) {
				return reconcile.Result{Requeue: true}, nil
			}
			klog.ErrorS(err, "Failed to update SandboxPool status", "namespace", sandboxPool.Namespace, "name", sandboxPool.Name)
			return reconcile.Result{}, err
		}
		klog.V(2).InfoS("Updated SandboxPool status", "namespace", sandboxPool.Namespace, "name", sandboxPool.Name,
			"phase", status.Phase,
			"idle", status.IdleReplicas, "unavailableIdle", status.UnavailableIdleReplicas,
			"running", status.RunningReplicas,
			"starting", status.StartingReplicas, "stopping", status.StoppingReplicas, "failed", status.FailedReplicas)

		// Emit Kubernetes Events on phase transitions to provide an audit trail
		// visible via `kubectl describe sbp` and `kubectl get events`.
		r.emitPhaseTransitionEvent(sandboxPool, oldPhase, status.Phase, status)
	}

	// Update Pool replica gauges.
	poolMetricLabels := prometheus.Labels{
		"namespace": sandboxPool.Namespace,
		"pool":      sandboxPool.Name,
		"team":      sandboxPool.Labels[agentsv1alpha1.LabelTeam],
		"user":      sandboxPool.Labels[agentsv1alpha1.LabelUser],
	}
	pkgmetrics.PoolReplicasDesired.With(poolMetricLabels).Set(float64(sandboxPool.Spec.Replicas))
	pkgmetrics.PoolReplicasIdle.With(poolMetricLabels).Set(float64(status.IdleReplicas))
	pkgmetrics.PoolReplicasRunning.With(poolMetricLabels).Set(float64(status.RunningReplicas))
	pkgmetrics.PoolReplicasStarting.With(poolMetricLabels).Set(float64(status.StartingReplicas))
	pkgmetrics.PoolReplicasStopping.With(poolMetricLabels).Set(float64(status.StoppingReplicas))
	pkgmetrics.PoolReplicasFailed.With(poolMetricLabels).Set(float64(status.FailedReplicas))

	klog.V(2).InfoS("Reconciling Pods", "namespace", sandboxPool.Namespace, "name", sandboxPool.Name,
		"desired", desiredReplicas, "current", currentReplicas,
		"idle", status.IdleReplicas, "running", status.RunningReplicas,
		"starting", status.StartingReplicas, "stopping", status.StoppingReplicas, "failed", status.FailedReplicas)

	// ── Clear residual scale-down-protected annotations ─────────────────────
	//
	// The scheduler's ready queue (pkg/lifecycle/schedule/ready_queue.go) drops
	// any idle pod that carries SandboxScaleDownProtectedAnnotationKey, so a
	// stale annotation (e.g. from a scale-down cycle that never reached the
	// delete step) silently removes the pod from the schedulable set. When
	// no scale-down is planned this cycle (current ≤ desired) the annotation
	// has no purpose — clear it and wake the scheduler so it refreshes
	// immediately instead of waiting for its 10s pollTimer.
	//
	// When current > desired, the autoscaler already factored in reactive
	// demand (PoolScheduler.Snapshot().QueueLen / IdleReady) before lowering
	// spec.replicas, so this loop simply executes the decision.
	if currentReplicas <= desiredReplicas {
		if n := r.unmarkStaleScaleDownProtected(ctx, pods); n > 0 {
			klog.V(2).InfoS("Cleared stale scale-down-protected annotations",
				"namespace", sandboxPool.Namespace, "name", sandboxPool.Name, "count", n, "reason", "no_scale_down")
			if r.IdleNotifier != nil {
				r.IdleNotifier.NotifyIdleAvailable(sandboxPool.Namespace, sandboxPool.Name)
			}
		}
	}

	// ── Scale up ─────────────────────────────────────────────────────────────
	if currentReplicas < desiredReplicas {
		// Guard: if in-flight creations from the previous scale-up have not yet
		// landed in the informer cache, skip this cycle to avoid over-provisioning.
		if r.expectations != nil && !r.expectations.Satisfied(poolKey) {
			klog.V(4).InfoS("Skipping scale-up: waiting for pending pod creates to land in informer",
				"namespace", sandboxPool.Namespace, "name", sandboxPool.Name)
			return ctrl.Result{}, nil
		}

		delta := desiredReplicas - currentReplicas
		klog.InfoS("Scaling up Pods", "namespace", sandboxPool.Namespace, "name", sandboxPool.Name,
			"delta", delta)

		// Record the expectation BEFORE issuing r.Create calls so that any Pod
		// Add events arriving during the loop are counted from the start.
		if r.expectations != nil {
			r.expectations.ExpectCreations(poolKey, int(delta))
		}

		// Create the Pods.
		for range delta {
			if err := r.createPod(ctx, sandboxPool); err != nil {
				klog.ErrorS(err, "Failed to create Pod", "namespace", sandboxPool.Namespace, "name", sandboxPool.Name)
				return reconcile.Result{}, err
			}
		}

		// Requeue to verify Pods are created and running
		return reconcile.Result{RequeueAfter: RequeueAfter}, nil
	}

	// ── Scale down ───────────────────────────────────────────────────────────
	if currentReplicas > desiredReplicas {
		// Guard: if in-flight deletions from the previous scale-down have not yet
		// landed in the informer cache, skip this cycle to avoid over-deleting.
		if r.expectations != nil && !r.expectations.Satisfied(poolKey) {
			klog.V(4).InfoS("Skipping scale-down: waiting for pending pod deletes to land in informer",
				"namespace", sandboxPool.Namespace, "name", sandboxPool.Name)
			return ctrl.Result{}, nil
		}

		excessPods := currentReplicas - desiredReplicas
		klog.InfoS("Scaling down Pods", "namespace", sandboxPool.Namespace, "name", sandboxPool.Name,
			"excess", excessPods)

		// Compute the protection window.  When autoscaling is configured, Idle Pods
		// are marked with scale-down-protected before deletion to give ClaimIdlePod
		// a chance to reclaim them.  When autoscaling is not configured we keep the
		// original immediate-delete behaviour.
		protectionWindow := scaleDownProtectionWindow(sandboxPool)

		// Sort Pods by phase priority: idle > updating > failed > running
		// We NEVER delete running pods
		sortedPods := r.sortPodsByPhasePriority(pods)

		// Delete the first excessPods Pods (prioritizing idle pods)
		deletedCount := int32(0)
		for i := range sortedPods {
			pod := &sortedPods[i]
			if deletedCount >= excessPods {
				break
			}

			// Skip running and starting pods — we only delete idle, stopping, and failed pods.
			phase := inplaceupdate.GetSandboxPhase(pod)
			if phase == agentsv1alpha1.SandboxPhaseRunning || phase == agentsv1alpha1.SandboxPhaseStarting {
				klog.V(2).InfoS("Skipping running or starting pod for deletion", "namespace", pod.Namespace, "name", pod.Name)
				continue
			}

			// ── Two-phase protection (only when autoscaling is configured) ──
			if protectionWindow > 0 && phase == agentsv1alpha1.SandboxPhaseIdle {
				protectedAt := pod.Annotations[agentsv1alpha1.SandboxScaleDownProtectedAnnotationKey]
				if protectedAt == "" {
					// Phase A: mark the pod as a scale-down candidate and requeue.
					if markErr := r.markScaleDownProtected(ctx, pod); markErr != nil {
						klog.ErrorS(markErr, "Failed to mark pod as scale-down-protected",
							"namespace", pod.Namespace, "name", pod.Name)
						return reconcile.Result{}, markErr
					}
					klog.InfoS("Marked idle pod as scale-down candidate",
						"namespace", pod.Namespace, "name", pod.Name)
					return reconcile.Result{RequeueAfter: protectionWindow + time.Second}, nil
				}

				// Phase B: check whether the protection window has elapsed.
				markedAt, parseErr := time.Parse(time.RFC3339, protectedAt)
				if parseErr != nil || time.Since(markedAt) < protectionWindow {
					var remaining time.Duration
					if parseErr == nil {
						remaining = max(protectionWindow-time.Since(markedAt), time.Second)
					} else {
						remaining = protectionWindow
					}
					klog.V(4).InfoS("Protection window not yet elapsed, requeuing",
						"namespace", pod.Namespace, "name", pod.Name,
						"remaining", remaining.Round(time.Second))
					return reconcile.Result{RequeueAfter: remaining}, nil
				}
				// Window has elapsed — fall through to actual deletion.
				klog.V(4).InfoS("Protection window elapsed, proceeding with deletion",
					"namespace", pod.Namespace, "name", pod.Name)
			}

			// Remove the sandbox-protection finalizer before deleting so the pod can
			// be GC'd. For Idle pods this is a no-op (they have no Finalizer after
			// Stopping→Idle completion), but Stopping/Failed pods may still have it.
			if err := r.removeSandboxProtectionFinalizer(ctx, pod); err != nil && !errors.IsNotFound(err) {
				klog.ErrorS(err, "Failed to remove finalizer from scale-down pod",
					"namespace", pod.Namespace, "name", pod.Name)
				return reconcile.Result{}, err
			}
			// Delete with UID + ResourceVersion preconditions: `pods` was captured
			// at the entry of reconcilePods, so by the time we get here a fast
			// claim CAS may have mutated the pod (e.g. removed the protection
			// annotation and transitioned phase Idle→Starting). Without the
			// precondition we'd race-delete that newly-claimed pod. With it the
			// Delete is rejected as Conflict and we skip — the next reconcile
			// will re-evaluate against fresh state.
			delOpts := []client.DeleteOption{client.Preconditions{
				UID:             &pod.UID,
				ResourceVersion: &pod.ResourceVersion,
			}}
			if err := r.Delete(ctx, pod, delOpts...); err != nil {
				if errors.IsNotFound(err) {
					// Already gone — fall through.
				} else if errors.IsConflict(err) {
					klog.V(2).InfoS("Scale-down Delete skipped: pod was mutated after snapshot (likely claimed by scheduler)",
						"namespace", pod.Namespace, "name", pod.Name, "err", err)
					continue
				} else {
					klog.ErrorS(err, "Failed to delete Pod", "namespace", pod.Namespace, "name", pod.Name)
					return reconcile.Result{}, err
				}
			}
			deletedCount++
			klog.V(2).InfoS("Deleted Pod", "namespace", pod.Namespace, "name", pod.Name, "phase", phase)
		}

		if deletedCount == 0 {
			klog.InfoS("No Pods were eligible for deletion", "namespace", sandboxPool.Namespace, "name", sandboxPool.Name,
				"desired", desiredReplicas, "current", currentReplicas)
			return reconcile.Result{}, nil
		}

		// Record the actual number of deletions issued so subsequent Reconcile
		// calls wait for them to land in the informer before acting again.
		if r.expectations != nil {
			r.expectations.ExpectDeletions(poolKey, int(deletedCount))
		}

		// Requeue to verify Pods are deleted
		return reconcile.Result{RequeueAfter: RequeueAfter}, nil
	}

	return reconcile.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SandboxPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Initialize the EventRecorder if not already set (e.g. in tests).
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorder("sandboxpool-controller")
	}

	if r.expectations == nil {
		r.expectations = NewPoolExpectations()
	}

	// enqueueOwningPool derives the owning SandboxPool from the pod's label and
	// enqueues a reconcile request. Using the label is equivalent to walking
	// ownerReferences but is O(1) since createPod always stamps the label.
	enqueueOwningPool := func(obj client.Object, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
		if poolName := obj.GetLabels()[agentsv1alpha1.SandboxPoolLabelKey]; poolName != "" {
			q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
				Namespace: obj.GetNamespace(),
				Name:      poolName,
			}})
		}
	}

	builder := ctrl.NewControllerManagedBy(mgr).
		// GenerationChangedPredicate filters status-only updates (which do
		// not increment metadata.generation), preventing the reconcile
		// storm caused by Status().Patch() re-triggering itself.
		// AnnotationChangedPredicate widens the watch back to annotation
		// changes specifically so the LastSandboxCreateTimeAnnotationKey
		// patch from lifecycle/lastcreate.Tracker — the canonical
		// "demand was just observed" signal — wakes this reconciler.
		// Without it a Pool sitting at 0 replicas with no Pods produces
		// zero K8s events under load and the autoscaler would only run
		// on the 10 s RequeueAfter tick. Pool annotations change at
		// most every flushInterval (5 s) per active pool, so this
		// widening does not create a reconcile storm.
		For(&agentsv1alpha1.SandboxPool{}, ctrlbuilder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
		))).
		Named("sandboxpool").
		// Allow multiple SandboxPool objects to be reconciled concurrently.
		// Each pool is an independent unit of work; serialising them behind a
		// single goroutine means a pool with many Stopping pods (each requiring
		// an apiserver round-trip) blocks all other pools from making progress.
		WithOptions(controller.Options{MaxConcurrentReconciles: 8}).
		// Watch Pods owned by SandboxPool. The custom handler observes Pod
		// Add/Delete events to decrement the expectations counters, preventing
		// the informer-cache lag from causing scale oscillation.
		Watches(&corev1.Pod{}, handler.Funcs{
			CreateFunc: func(ctx context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				if poolName := e.Object.GetLabels()[agentsv1alpha1.SandboxPoolLabelKey]; poolName != "" {
					r.expectations.CreationObserved(types.NamespacedName{
						Namespace: e.Object.GetNamespace(),
						Name:      poolName,
					})
				}
				enqueueOwningPool(e.Object, q)
			},
			UpdateFunc: func(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				enqueueOwningPool(e.ObjectNew, q)
			},
			DeleteFunc: func(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				if poolName := e.Object.GetLabels()[agentsv1alpha1.SandboxPoolLabelKey]; poolName != "" {
					r.expectations.DeletionObserved(types.NamespacedName{
						Namespace: e.Object.GetNamespace(),
						Name:      poolName,
					})
				}
				enqueueOwningPool(e.Object, q)
			},
			GenericFunc: func(ctx context.Context, e event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				enqueueOwningPool(e.Object, q)
			},
		})

	return builder.Complete(r)
}
