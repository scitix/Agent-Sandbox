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
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/inplaceupdate"
)

// PodDiagnosticEvent is a single Warning event associated with a Pod.
type PodDiagnosticEvent struct {
	// Reason is the event reason, e.g. "Failed", "BackOff", "ErrImagePull".
	Reason string
	// Message is the human-readable event message.
	Message string
	// LastTimestamp is the RFC3339 time of the most recent occurrence.
	LastTimestamp string
	// Count is the total number of times this event fired.
	Count int32
}

// PodStatusDetail holds diagnostic information for a single pod, derived on-demand
// from the Pod's current YAML state and (for Get) from its Kubernetes Events.
type PodStatusDetail struct {
	// PodName is the name of the pod.
	PodName string
	// Phase is the agentbox pod phase label value (starting, failed, …).
	Phase string
	// Reason is a machine-readable cause extracted from the Pod spec, e.g.
	// "Pulling", "ImagePullBackOff", "ErrImagePull", "CrashLoopBackOff", "OOMKilled".
	Reason string
	// Message is a human-readable description derived from the Pod spec.
	Message string
	// Events contains Warning events fetched from the Kubernetes API (only
	// populated by Get, never by List).
	Events []PodDiagnosticEvent
}

// BuildSandboxStatusDetailFromPod derives diagnostic information for a Starting or
// Failed pod purely from its in-memory Pod object — no API calls are made.
// Returns nil when the pod has no diagnosable state.
func BuildSandboxStatusDetailFromPod(pod *corev1.Pod) *PodStatusDetail {
	phase := inplaceupdate.GetSandboxPhase(pod)
	if phase != agentsv1alpha1.SandboxPhaseStarting && phase != agentsv1alpha1.SandboxPhaseFailed {
		return nil
	}

	var detail *agentsv1alpha1.SandboxStatusDetail
	if phase == agentsv1alpha1.SandboxPhaseFailed {
		detail = buildFailedDetailFromPod(pod)
	} else {
		detail = buildStartingDetailFromPod(pod)
	}
	if detail == nil {
		return nil
	}
	return &PodStatusDetail{
		PodName: pod.Name,
		Phase:   phase,
		Reason:  detail.Reason,
		Message: detail.Message,
	}
}

// BuildPodStatusDetailWithEvents is like BuildPodStatusDetailFromPod but also
// fetches Warning events for the pod from the Kubernetes API and attaches them.
// clientset may be nil, in which case Events will be empty.
func BuildPodStatusDetailWithEvents(ctx context.Context, clientset kubernetes.Interface, pod *corev1.Pod) *PodStatusDetail {
	d := BuildSandboxStatusDetailFromPod(pod)
	if d == nil {
		return nil
	}
	if clientset != nil {
		d.Events = fetchPodWarningEvents(ctx, clientset, pod)
	}
	return d
}

// buildStartingDetailFromPod inspects a Starting pod's container statuses and
// returns a SandboxStatusDetail derived solely from the pod's current YAML.
func buildStartingDetailFromPod(pod *corev1.Pod) *agentsv1alpha1.SandboxStatusDetail {
	now := time.Now().UTC().Format(time.RFC3339)

	targetImage := ""
	for _, c := range pod.Spec.Containers {
		targetImage = c.Image
		break
	}

	reason := ""
	message := ""

	if len(pod.Status.ContainerStatuses) == 0 {
		reason = "Pulling"
		message = fmt.Sprintf("Pulling image: %s", targetImage)
	} else {
		for _, cs := range pod.Status.ContainerStatuses {
			if w := cs.State.Waiting; w != nil && w.Reason != "" {
				reason = w.Reason
				message = w.Message
				break
			}
		}
		if reason == "" {
			// Container statuses exist but no waiting state — still initialising
			reason = "Pulling"
			message = fmt.Sprintf("Pulling image: %s", targetImage)
		}
	}

	// Pod-level failure overrides container-level
	if pod.Status.Phase == corev1.PodFailed {
		reason = "PodFailed"
		message = pod.Status.Message
	}

	if reason == "" {
		return nil
	}
	return &agentsv1alpha1.SandboxStatusDetail{
		Reason:          reason,
		Message:         message,
		LastUpdatedTime: now,
	}
}

// buildFailedDetailFromPod inspects a Failed pod and returns a SandboxStatusDetail
// derived solely from the pod's current YAML.
func buildFailedDetailFromPod(pod *corev1.Pod) *agentsv1alpha1.SandboxStatusDetail {
	now := time.Now().UTC().Format(time.RFC3339)

	reason := ""
	message := ""

	// Eviction / pod-level failure
	if pod.Status.Reason != "" {
		reason = pod.Status.Reason
		message = pod.Status.Message
	}

	// Container termination info (overrides pod-level if more specific)
	for _, cs := range pod.Status.ContainerStatuses {
		t := cs.LastTerminationState.Terminated
		if t == nil {
			t = cs.State.Terminated
		}
		if t == nil {
			continue
		}
		reason = t.Reason
		if t.Message != "" {
			message = t.Message
		} else {
			message = fmt.Sprintf("Exit code: %d", t.ExitCode)
		}
		break
	}

	if pod.Status.Phase == corev1.PodFailed && reason == "" {
		reason = "PodFailed"
		message = pod.Status.Message
	}

	if reason == "" {
		return nil
	}
	return &agentsv1alpha1.SandboxStatusDetail{
		Reason:          reason,
		Message:         message,
		LastUpdatedTime: now,
	}
}

// relevantWarningReasons is the set of Kubernetes event reasons considered
// diagnostic for sandbox pods.
var relevantWarningReasons = map[string]bool{
	"Failed":       true,
	"BackOff":      true,
	"ErrImagePull": true,
	"FailedMount":  true,
}

// fetchPodWarningEvents queries the Kubernetes API for Warning events related to
// the given pod and returns them sorted by most-recent first.
func fetchPodWarningEvents(ctx context.Context, clientset kubernetes.Interface, pod *corev1.Pod) []PodDiagnosticEvent {
	events, err := clientset.CoreV1().Events(pod.Namespace).List(ctx, metav1.ListOptions{
		FieldSelector: "involvedObject.name=" + pod.Name,
	})
	if err != nil {
		return nil
	}

	var result []PodDiagnosticEvent
	for _, evt := range events.Items {
		if evt.Type != corev1.EventTypeWarning {
			continue
		}
		if !relevantWarningReasons[evt.Reason] {
			continue
		}
		t := evt.LastTimestamp
		if t.IsZero() {
			t = metav1.NewTime(evt.EventTime.Time)
		}
		result = append(result, PodDiagnosticEvent{
			Reason:        evt.Reason,
			Message:       evt.Message,
			LastTimestamp: t.UTC().Format(time.RFC3339),
			Count:         evt.Count,
		})
	}

	// Sort most-recent first
	sort.Slice(result, func(i, j int) bool {
		return result[i].LastTimestamp > result[j].LastTimestamp
	})
	return result
}
