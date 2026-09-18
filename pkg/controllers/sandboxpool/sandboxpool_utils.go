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
	"fmt"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"

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

// resolveLastActive returns the best available last-active time for pod, as a
// maximum over every source rather than a priority list.
//
// The last-active annotation is written by the gateway replicas and is the only
// source that reflects traffic; started-at and CreationTimestamp are floors for
// a sandbox that has not been used yet. Taking the maximum matters: a priority
// chain that prefers one source outright lets a stale value from that source
// pull the answer *backwards*, which shows up as releasing a sandbox that is in
// use. A maximum can only ever make the answer more conservative.
func resolveLastActive(pod *corev1.Pod) time.Time {
	best := pod.CreationTimestamp.Time

	if t, err := parseRFC3339Annotation(pod, agentsv1alpha1.SandboxStartedAtAnnotationKey); err == nil && t.After(best) {
		best = t
	}
	if t, err := parseRFC3339Annotation(pod, agentsv1alpha1.SandboxLastActiveAnnotationKey); err == nil && t.After(best) {
		best = t
	}
	return best
}

// parseRFC3339Annotation reads a timestamp annotation. A missing annotation is
// reported as an error so callers can skip it with the same branch as a
// malformed one.
func parseRFC3339Annotation(pod *corev1.Pod, key string) (time.Time, error) {
	raw := pod.Annotations[key]
	if raw == "" {
		return time.Time{}, fmt.Errorf("annotation %s is absent", key)
	}
	return time.Parse(time.RFC3339, raw)
}
