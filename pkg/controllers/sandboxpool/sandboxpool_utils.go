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
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// parseDurationSecondsAnnotation reads an annotation value that encodes a duration
// as an integer number of seconds (e.g. "300" → 5 minutes). Returns 0 if the
// annotation is absent or its value cannot be parsed as a positive integer.
func parseDurationSecondsAnnotation(pod *corev1.Pod, key string) time.Duration {
	v := pod.Annotations[key]
	if v == "" {
		return 0
	}
	secs, err := strconv.ParseInt(v, 10, 64)
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

// resolveStartupTimeout returns the effective startup timeout for a Starting pod.
// Priority:
//  1. Pod annotation (agentbox.navix.sh/startup-timeout) — set at claim time from the
//     resolved request/pool timeout. This survives pool spec changes and controller restarts.
//  2. Pool spec DefaultStartupTimeout — pool-level fallback for pods that were claimed before
//     the annotation was introduced.
//  3. 0 — no startup timeout configured; the pod should not be cleaned up by the reconciler.
func resolveStartupTimeout(pod *corev1.Pod, pool *agentsv1alpha1.SandboxPool) time.Duration {
	if d := parseDurationSecondsAnnotation(pod, agentsv1alpha1.SandboxStartupTimeoutAnnotationKey); d > 0 {
		return d
	}
	if pool != nil && pool.Spec.DefaultStartupTimeout != nil && pool.Spec.DefaultStartupTimeout.Duration > 0 {
		return pool.Spec.DefaultStartupTimeout.Duration
	}
	return 0
}

// resolveLastActive returns the best available last-active time for pod.
// Priority: ExtProc map > last-active annotation > started-at annotation > CreationTimestamp.
func resolveLastActive(pod *corev1.Pod, extprocMap map[string]time.Time) time.Time {
	sandboxID := pod.Labels[agentsv1alpha1.SandboxIDLabelKey]

	if ts, ok := extprocMap[sandboxID]; ok && !ts.IsZero() {
		return ts
	}
	if v := pod.Annotations[agentsv1alpha1.SandboxLastActiveAnnotationKey]; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
	}
	if v := pod.Annotations[agentsv1alpha1.SandboxStartedAtAnnotationKey]; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
	}
	return pod.CreationTimestamp.Time
}

// isPendingScaleUpAnnotationFresh returns true when the pool has a
// PoolScaleUpPendingAnnotationKey annotation that is younger than maxAge
// (defaulting to 2 minutes when cooldown is zero). The Env autoscaler now
// owns the scale-up decision, but the Pool reconciler still consults this
// annotation to suppress scale-down when pending demand is signalled.
func isPendingScaleUpAnnotationFresh(pool *agentsv1alpha1.SandboxPool, cooldown time.Duration) bool {
	ts, ok := pool.Annotations[agentsv1alpha1.PoolScaleUpPendingAnnotationKey]
	if !ok || ts == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return false
	}
	maxAge := max(cooldown*2, 2*time.Minute)
	return time.Since(t) <= maxAge
}

// patchLastActiveAnnotation patches the last-active annotation on the pod
// if the new timestamp is strictly newer than the existing one.
func patchLastActiveAnnotation(ctx context.Context, c client.Client, pod *corev1.Pod, ts time.Time) {
	// Only write if the new value is newer than what's already on the pod.
	if existing := pod.Annotations[agentsv1alpha1.SandboxLastActiveAnnotationKey]; existing != "" {
		if t, err := time.Parse(time.RFC3339, existing); err == nil && !ts.After(t) {
			return
		}
	}

	tsStr := ts.UTC().Format(time.RFC3339)
	patch := fmt.Appendf(nil,
		`{"metadata":{"annotations":{%q:%q}}}`,
		agentsv1alpha1.SandboxLastActiveAnnotationKey, tsStr,
	)
	if err := c.Patch(ctx, pod, client.RawPatch(types.MergePatchType, patch)); err != nil {
		klog.V(4).ErrorS(err, "sandboxpool: failed to patch last-active annotation",
			"namespace", pod.Namespace, "pod", pod.Name)
	}
}
