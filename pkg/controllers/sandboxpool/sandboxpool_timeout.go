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
	"github.com/scitix/agent-sandbox/pkg/lifecycle/inplaceupdate"
	"github.com/scitix/agent-sandbox/pkg/store"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

// LastActiveSource retrieves per-sandbox last-activity timestamps. Implemented
// by the ExtProc gRPC client (production) and test doubles. Kept narrow so
// this package stays decoupled from the full ExtProc RPC surface.
type LastActiveSource interface {
	GetLastActive(ctx context.Context) (map[string]time.Time, error)
}

// IdleTimeoutReconciler is a background runnable that periodically:
//  1. Polls the ExtProc control-plane API for per-sandbox last-active timestamps.
//  2. Patches pod last-active annotations so ExtProc can recover state after restarts.
//  3. Releases Running pods whose idle duration exceeds their idle-timeout annotation.
//
// If the source is unreachable, the check is skipped entirely to avoid
// false-positive releases during ExtProc rolling updates.
type IdleTimeoutReconciler struct {
	client        client.Client
	sandboxStore  store.SandboxStore
	checkInterval time.Duration
	lastActive    LastActiveSource
}

// NewIdleTimeoutReconciler creates a new IdleTimeoutReconciler. If lastActive
// is nil, the reconciler logs a warning and idles (no releases issued).
func NewIdleTimeoutReconciler(c client.Client, s store.SandboxStore, interval time.Duration, lastActive LastActiveSource) *IdleTimeoutReconciler {
	return &IdleTimeoutReconciler{
		client:        c,
		sandboxStore:  s,
		checkInterval: interval,
		lastActive:    lastActive,
	}
}

// Start implements manager.Runnable. It performs an initial check immediately,
// then ticks at checkInterval. Runs only on the leader when leader election is enabled.
func (r *IdleTimeoutReconciler) Start(ctx context.Context) error {
	if r.lastActive == nil {
		klog.InfoS("IdleTimeoutReconciler: lastActive source is nil, idle timeout enforcement disabled")
		<-ctx.Done()
		return nil
	}

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

// fetchLastActiveFromExtProc calls the ExtProc control-plane API and returns
// a map of sandboxID → last-active time. Returns an error when the source is
// unreachable, so callers can skip the check.
func (r *IdleTimeoutReconciler) fetchLastActiveFromExtProc(ctx context.Context) (map[string]time.Time, error) {
	return r.lastActive.GetLastActive(ctx)
}

// checkAndReleaseIdleSandboxes is the main reconcile body for one tick.
func (r *IdleTimeoutReconciler) checkAndReleaseIdleSandboxes(ctx context.Context) {

	// Step 1: Poll ExtProc. Skip entirely if unreachable.
	extprocMap, err := r.fetchLastActiveFromExtProc(ctx)
	if err != nil {
		klog.ErrorS(err, "IdleTimeoutReconciler: ExtProc unreachable, skipping check")
		return
	}

	// Step 2: List Running pods that have an idle-timeout annotation.
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

		// Step 3: Resolve last-active time.
		// Priority: ExtProc in-memory value > last-active annotation > started-at annotation > CreationTimestamp
		lastActive := resolveLastActive(pod, extprocMap)

		// Step 4: Decide whether to release.
		idleDuration := now.Sub(lastActive)
		if idleDuration > idleTimeout {
			klog.InfoS("IdleTimeoutReconciler: releasing idle sandbox",
				"namespace", pod.Namespace, "pod", pod.Name, "sandboxID", sandboxID,
				"idleDuration", idleDuration.Round(time.Second), "idleTimeout", idleTimeout)

			if releaseErr := r.releaseSandbox(ctx, pod); releaseErr != nil {
				klog.ErrorS(releaseErr, "IdleTimeoutReconciler: failed to release pod",
					"namespace", pod.Namespace, "pod", pod.Name)
			}
			continue
		}

		// Step 5: Not timed out — patch the last-active annotation with the ExtProc value
		// so it can recover state after a restart. Skip this when releasing to avoid a
		// redundant write that releasePod will immediately overwrite.
		if sandboxID != "" {
			if extprocTs, ok := extprocMap[sandboxID]; ok {
				patchLastActiveAnnotation(ctx, r.client, pod, extprocTs)
			}
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
