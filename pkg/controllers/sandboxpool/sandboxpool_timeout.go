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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/activity"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/inplaceupdate"
	"github.com/scitix/agent-sandbox/pkg/store"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

// IdleTimeoutReconciler is a background runnable that periodically:
//  1. Lists Running pods that carry an idle-timeout annotation.
//  2. Resolves each one's last activity from its own last-active annotation.
//  3. Releases the ones that have been idle past their timeout plus the
//     gateway's refresh slack.
//
// The annotation is the meeting point: every gateway replica observes the
// requests that land on it and writes what it saw (see ActivityFlusher in
// pkg/envoy/extproc), so the annotation is the union of all replicas' views.
// This used to be a gRPC poll of the gateway, which cannot work with more than
// one replica — one connection pins to one pod, so the controller saw 1/N of
// the traffic and released sandboxes that were in use.
//
// The slack comes from pkg/activity: the annotation always lags the true last
// activity by up to the gateway's refresh interval, so the comparison has to
// add that bound back or the lag itself becomes a false-positive release.
type IdleTimeoutReconciler struct {
	client        client.Client
	sandboxStore  store.SandboxStore
	checkInterval time.Duration
}

// NewIdleTimeoutReconciler creates a new IdleTimeoutReconciler.
func NewIdleTimeoutReconciler(c client.Client, s store.SandboxStore, interval time.Duration) *IdleTimeoutReconciler {
	return &IdleTimeoutReconciler{
		client:        c,
		sandboxStore:  s,
		checkInterval: interval,
	}
}

// Start implements manager.Runnable. It performs an initial check immediately,
// then ticks at checkInterval. Runs only on the leader when leader election is enabled.
func (r *IdleTimeoutReconciler) Start(ctx context.Context) error {
	klog.InfoS("IdleTimeoutReconciler: starting", "checkInterval", r.checkInterval)

	// Run-then-wait loop: each check must finish before the next interval begins,
	// preventing overlapping reconciliations.
	for {
		// r.checkAndReleasePendingSandboxes(ctx)
		r.checkAndReleaseIdleSandboxes(ctx)

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(r.checkInterval):
		}
	}
}

// checkAndReleaseIdleSandboxes is the main reconcile body for one tick.
func (r *IdleTimeoutReconciler) checkAndReleaseIdleSandboxes(ctx context.Context) {
	// Step 1: List Running pods that have an idle-timeout annotation.
	podList := &corev1.PodList{}
	if listErr := r.client.List(ctx, podList,
		client.MatchingFields{indexer.IndexFieldSandboxPhase: agentsv1alpha1.SandboxPhaseRunning},
	); listErr != nil {
		klog.ErrorS(listErr, "IdleTimeoutReconciler: failed to list Running pods")
		return
	}

	now := time.Now().UTC()

	for i := range podList.Items {
		pod := &podList.Items[i]

		// Only process pods with an idle-timeout annotation.
		timeoutStr := pod.Annotations[agentsv1alpha1.SandboxIdleTimeoutAnnotationKey]
		if timeoutStr == "" {
			continue
		}
		idleTimeout := parseDurationSecondsAnnotation(pod, agentsv1alpha1.SandboxIdleTimeoutAnnotationKey)
		if idleTimeout <= 0 {
			continue
		}

		sandboxID := pod.Labels[agentsv1alpha1.SandboxIDLabelKey]

		// Step 2: Resolve last-active time from the Pod itself — the union of
		// what every gateway replica observed.
		lastActive := resolveLastActive(pod)

		// Step 3: Decide whether to release. The buffer absorbs the gateway's
		// refresh lag; without it the lag itself would look like idleness.
		if !activity.IsIdle(now, lastActive, idleTimeout) {
			continue
		}

		klog.InfoS("IdleTimeoutReconciler: releasing idle sandbox",
			"namespace", pod.Namespace, "pod", pod.Name, "sandboxID", sandboxID,
			"idleDuration", now.Sub(lastActive).Round(time.Second),
			"idleTimeout", idleTimeout,
			"reclaimBuffer", activity.ReclaimBuffer(idleTimeout).Round(time.Second))

		if releaseErr := r.releaseSandbox(ctx, pod); releaseErr != nil {
			klog.ErrorS(releaseErr, "IdleTimeoutReconciler: failed to release pod",
				"namespace", pod.Namespace, "pod", pod.Name)
		}
	}
}

// TODO: enable this check after implementing startup timeouts in the e2b sdk.
func (r *IdleTimeoutReconciler) CheckAndReleasePendingSandboxes(ctx context.Context) {
	podList := &corev1.PodList{}
	if err := r.client.List(ctx, podList,
		client.MatchingFields{indexer.IndexFieldSandboxPhase: agentsv1alpha1.SandboxPhaseStarting},
	); err != nil {
		klog.ErrorS(err, "IdleTimeoutReconciler: failed to list Starting pods")
		return
	}

	now := time.Now().UTC()
	pools := make(map[client.ObjectKey]*agentsv1alpha1.SandboxPool)

	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.DeletionTimestamp != nil {
			continue
		}

		poolName := pod.Labels[agentsv1alpha1.SandboxPoolLabelKey]
		if poolName == "" {
			continue
		}

		key := client.ObjectKey{Namespace: pod.Namespace, Name: poolName}
		pool, ok := pools[key]
		if !ok {
			pool = &agentsv1alpha1.SandboxPool{}
			if err := r.client.Get(ctx, key, pool); err != nil {
				klog.V(4).ErrorS(err, "IdleTimeoutReconciler: failed to get pool for Starting pod cleanup",
					"namespace", pod.Namespace, "pod", pod.Name, "pool", poolName)
				pools[key] = nil
				continue
			}
			pools[key] = pool
		}

		if pool == nil {
			continue
		}

		// Resolve effective startup timeout: pod annotation takes priority over pool spec.
		timeout := resolveStartupTimeout(pod, pool)
		if timeout <= 0 {
			continue
		}

		phaseDuration, ok, err := inplaceupdate.GetPodPhaseDuration(pod, agentsv1alpha1.SandboxPhaseStarting, now)
		if err != nil {
			klog.ErrorS(err, "IdleTimeoutReconciler: failed to resolve Starting phase duration",
				"namespace", pod.Namespace, "pod", pod.Name)
			continue
		}
		if !ok || phaseDuration <= timeout {
			continue
		}

		klog.InfoS("IdleTimeoutReconciler: releasing timed out Starting pod",
			"namespace", pod.Namespace,
			"pod", pod.Name,
			"pool", poolName,
			"phaseDuration", phaseDuration.Round(time.Second),
			"startupTimeout", timeout)

		failureMessage := fmt.Sprintf("Sandbox startup phase duration %s exceeded timeout of %v", phaseDuration.Round(time.Second), timeout)
		if _, err := ReleaseSandboxPod(ctx, r.client, pod, pool, ReleaseSandboxPodOptions{
			StopReason:                  agentsv1alpha1.SandboxStopReasonReleased,
			TerminatedAt:                now.Format(time.RFC3339),
			FailureReason:               "StartupTimeout",
			FailureMessage:              failureMessage,
			ExpectedCurrentSandboxPhase: agentsv1alpha1.SandboxPhaseStarting,
		}); err != nil {
			klog.ErrorS(err, "IdleTimeoutReconciler: failed to release timed out Starting pod",
				"namespace", pod.Namespace, "pod", pod.Name)
		}
	}
}

// releaseSandbox loads the owning SandboxPool and calls ReleaseSandboxPod with stop metadata.
// The store write is deferred to syncInplaceUpdatePhases when Stopping→Idle completes.
func (r *IdleTimeoutReconciler) releaseSandbox(ctx context.Context, pod *corev1.Pod) error {
	poolName := pod.Labels[agentsv1alpha1.SandboxPoolLabelKey]
	if poolName == "" {
		return fmt.Errorf("pod %s/%s has no sandbox pool label", pod.Namespace, pod.Name)
	}

	pool := &agentsv1alpha1.SandboxPool{}
	if err := r.client.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: poolName}, pool); err != nil {
		// Pool may have been deleted; skip release — pool deletion handler will clean up.
		return fmt.Errorf("get pool %s/%s: %w", pod.Namespace, poolName, err)
	}

	// Build failure message BEFORE release since last-active/idle-timeout annotations will be removed.
	lastActiveStr := pod.Annotations[agentsv1alpha1.SandboxLastActiveAnnotationKey]
	idleTimeoutStr := pod.Annotations[agentsv1alpha1.SandboxIdleTimeoutAnnotationKey]
	failureMessage := fmt.Sprintf("Sandbox last active at %s exceeded idle timeout of %s", lastActiveStr, idleTimeoutStr)

	if _, err := ReleaseSandboxPod(ctx, r.client, pod, pool, ReleaseSandboxPodOptions{
		StopReason:                  agentsv1alpha1.SandboxStopReasonReleased,
		TerminatedAt:                time.Now().UTC().Format(time.RFC3339),
		FailureReason:               "IdleTimeout",
		FailureMessage:              failureMessage,
		ExpectedCurrentSandboxPhase: agentsv1alpha1.SandboxPhaseRunning,
	}); err != nil {
		return fmt.Errorf("ReleaseSandboxPod: %w", err)
	}

	return nil
}
