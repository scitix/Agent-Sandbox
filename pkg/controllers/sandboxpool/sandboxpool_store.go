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
	"slices"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	pkgmetrics "github.com/scitix/agent-sandbox/pkg/metrics"
)

// emitSandboxStopMetrics records SandboxDeleteTotal, SandboxStartingDuration,
// and SandboxRunningDuration for a sandbox that has terminated.
func emitSandboxStopMetrics(pod *corev1.Pod, stopReason, claimedAt, startedAt, terminatedAt string) {
	labels := prometheus.Labels{
		"namespace":   pod.Namespace,
		"pool":        pod.Labels[agentsv1alpha1.SandboxPoolLabelKey],
		"team":        pod.Labels[agentsv1alpha1.LabelTeam],
		"user":        pod.Labels[agentsv1alpha1.LabelUser],
		"sandbox_env": pod.Labels[agentsv1alpha1.LabelEnv],
		"stop_reason": stopReason,
	}
	pkgmetrics.SandboxDeleteTotal.With(labels).Inc()

	if claimedAt != "" {
		// For normal completions: claimedAt → startedAt, outcome=success.
		// For Canceled (never reached Running, startedAt empty): claimedAt → terminatedAt, outcome=canceled.
		endTs := startedAt
		startingOutcome := "success"
		if endTs == "" {
			endTs = terminatedAt
			startingOutcome = "canceled"
		}
		if endTs != "" {
			if claimedTs, err := time.Parse(time.RFC3339, claimedAt); err == nil {
				if endT, err := time.Parse(time.RFC3339, endTs); err == nil {
					pkgmetrics.SandboxStartingDuration.With(prometheus.Labels{
						"namespace":   pod.Namespace,
						"pool":        pod.Labels[agentsv1alpha1.SandboxPoolLabelKey],
						"team":        pod.Labels[agentsv1alpha1.LabelTeam],
						"user":        pod.Labels[agentsv1alpha1.LabelUser],
						"sandbox_env": pod.Labels[agentsv1alpha1.LabelEnv],
						"outcome":     startingOutcome,
					}).Observe(endT.Sub(claimedTs).Seconds())
				}
			}
		}
	}

	if startedAt == "" || terminatedAt == "" {
		return
	}
	startedTs, err := time.Parse(time.RFC3339, startedAt)
	if err != nil {
		klog.V(4).InfoS("emitSandboxStopMetrics: failed to parse startedAt", "startedAt", startedAt, "err", err)
		return
	}
	terminatedTs, err := time.Parse(time.RFC3339, terminatedAt)
	if err != nil {
		klog.V(4).InfoS("emitSandboxStopMetrics: failed to parse terminatedAt", "terminatedAt", terminatedAt, "err", err)
		return
	}
	pkgmetrics.SandboxRunningDuration.With(labels).Observe(terminatedTs.Sub(startedTs).Seconds())
}

// setRecycledAtNow stamps the recycled-at field of a sandbox record with the
// current UTC wall-clock time. Convenience wrapper around the *time.Time
// pointer required by gen.Sandbox.
func setRecycledAtNow(sb *gen.Sandbox) {
	t := time.Now().UTC()
	sb.RecycledAt = &t
}

// formatRFC3339Ptr renders a *time.Time as an RFC3339 string, or empty when nil.
func formatRFC3339Ptr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

// sandboxRecordFromPod constructs a terminal gen.Sandbox from a pod's labels/annotations.
// CPU and Memory are computed by SandboxBaseFromPod from container resource requests/limits.
func sandboxRecordFromPod(pod *corev1.Pod, status, terminatedAt, failureReason string, exitCode *int32, failureMessage string) gen.Sandbox {
	sb := SandboxBaseFromPod(pod)
	sb.Status = gen.SandboxStatus(status)
	if terminatedAt != "" {
		if t, err := time.Parse(time.RFC3339, terminatedAt); err == nil {
			sb.TerminatedAt = &t
		}
	}
	if failureReason != "" {
		sb.FailureReason = ptr.To(failureReason)
	}
	sb.ExitCode = exitCode
	if failureMessage != "" {
		sb.FailureMessage = ptr.To(failureMessage)
	}
	return sb
}

// containsString checks if a string exists in a slice
func containsString(slice []string, s string) bool {
	return slices.Contains(slice, s)
}

// removeString removes a string from a slice
func removeString(slice []string, s string) []string {
	var result []string
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return result
}

// removeSandboxProtectionFinalizer removes the sandbox-protection finalizer from a pod
// via a Merge patch. It is idempotent: if the finalizer is absent, it is a no-op.
func (r *SandboxPoolReconciler) removeSandboxProtectionFinalizer(ctx context.Context, pod *corev1.Pod) error {
	return removeSandboxProtectionFinalizer(ctx, r.Client, pod)
}

func removeSandboxProtectionFinalizer(ctx context.Context, c client.Client, pod *corev1.Pod) error {
	if !containsString(pod.Finalizers, agentsv1alpha1.SandboxProtectionFinalizer) {
		return nil
	}
	patch := client.MergeFrom(pod.DeepCopy())
	pod.Finalizers = removeString(pod.Finalizers, agentsv1alpha1.SandboxProtectionFinalizer)
	return c.Patch(ctx, pod, patch)
}
