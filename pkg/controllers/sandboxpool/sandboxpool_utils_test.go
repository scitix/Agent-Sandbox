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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// ── parseDurationSecondsAnnotation ────────────────────────────────────────────

func TestParseDurationSecondsAnnotation_Valid(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				agentsv1alpha1.SandboxStartupTimeoutAnnotationKey: "300",
			},
		},
	}
	got := parseDurationSecondsAnnotation(pod, agentsv1alpha1.SandboxStartupTimeoutAnnotationKey)
	if got != 5*time.Minute {
		t.Fatalf("expected 5m, got %v", got)
	}
}

func TestParseDurationSecondsAnnotation_Missing(t *testing.T) {
	pod := &corev1.Pod{}
	got := parseDurationSecondsAnnotation(pod, agentsv1alpha1.SandboxStartupTimeoutAnnotationKey)
	if got != 0 {
		t.Fatalf("expected 0 for missing annotation, got %v", got)
	}
}

func TestParseDurationSecondsAnnotation_Invalid(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				agentsv1alpha1.SandboxStartupTimeoutAnnotationKey: "not-a-number",
			},
		},
	}
	got := parseDurationSecondsAnnotation(pod, agentsv1alpha1.SandboxStartupTimeoutAnnotationKey)
	if got != 0 {
		t.Fatalf("expected 0 for invalid annotation, got %v", got)
	}
}

func TestParseDurationSecondsAnnotation_Zero(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				agentsv1alpha1.SandboxStartupTimeoutAnnotationKey: "0",
			},
		},
	}
	// Zero or negative values should return 0 (treated as "not set").
	got := parseDurationSecondsAnnotation(pod, agentsv1alpha1.SandboxStartupTimeoutAnnotationKey)
	if got != 0 {
		t.Fatalf("expected 0 for zero annotation, got %v", got)
	}
}

func TestParseDurationSecondsAnnotation_Negative(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				agentsv1alpha1.SandboxStartupTimeoutAnnotationKey: "-60",
			},
		},
	}
	got := parseDurationSecondsAnnotation(pod, agentsv1alpha1.SandboxStartupTimeoutAnnotationKey)
	if got != 0 {
		t.Fatalf("expected 0 for negative annotation, got %v", got)
	}
}

// ── resolveStartupTimeout ─────────────────────────────────────────────────────

func makePoolWithStartupTimeout(d time.Duration) *agentsv1alpha1.SandboxPool {
	return &agentsv1alpha1.SandboxPool{
		Spec: agentsv1alpha1.SandboxPoolSpec{
			DefaultStartupTimeout: &metav1.Duration{Duration: d},
		},
	}
}

func TestResolveStartupTimeout_PodAnnotationOnly(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				agentsv1alpha1.SandboxStartupTimeoutAnnotationKey: "60",
			},
		},
	}
	got := resolveStartupTimeout(pod, nil)
	if got != 60*time.Second {
		t.Fatalf("expected 60s, got %v", got)
	}
}

func TestResolveStartupTimeout_PoolSpecOnly(t *testing.T) {
	pod := &corev1.Pod{}
	pool := makePoolWithStartupTimeout(5 * time.Minute)
	got := resolveStartupTimeout(pod, pool)
	if got != 5*time.Minute {
		t.Fatalf("expected 5m, got %v", got)
	}
}

func TestResolveStartupTimeout_AnnotationTakesPriority(t *testing.T) {
	// Pod annotation = 60s, Pool = 300s → should use annotation (60s)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				agentsv1alpha1.SandboxStartupTimeoutAnnotationKey: "60",
			},
		},
	}
	pool := makePoolWithStartupTimeout(5 * time.Minute)
	got := resolveStartupTimeout(pod, pool)
	if got != 60*time.Second {
		t.Fatalf("expected 60s (from annotation), got %v", got)
	}
}

func TestResolveStartupTimeout_NeitherSet(t *testing.T) {
	pod := &corev1.Pod{}
	got := resolveStartupTimeout(pod, nil)
	if got != 0 {
		t.Fatalf("expected 0, got %v", got)
	}
}

func TestResolveStartupTimeout_NilPool(t *testing.T) {
	// No annotation, nil pool — expect 0.
	pod := &corev1.Pod{}
	got := resolveStartupTimeout(pod, nil)
	if got != 0 {
		t.Fatalf("expected 0 for nil pool and no annotation, got %v", got)
	}
}

func TestResolveStartupTimeout_InvalidAnnotationFallsBackToPool(t *testing.T) {
	// Invalid annotation value → should fall back to Pool spec.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				agentsv1alpha1.SandboxStartupTimeoutAnnotationKey: "not-a-number",
			},
		},
	}
	pool := makePoolWithStartupTimeout(2 * time.Minute)
	got := resolveStartupTimeout(pod, pool)
	if got != 2*time.Minute {
		t.Fatalf("expected 2m (pool fallback), got %v", got)
	}
}
