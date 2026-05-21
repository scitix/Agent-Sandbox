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
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/inplaceupdate"
	utilresource "github.com/scitix/agent-sandbox/pkg/utils/resource"
)

// SandboxBaseFromPod populates the fields of a gen.Sandbox that are common
// to both live (API) and historical (store) representations of a sandbox:
//
//   - Identity: SandboxId, Namespace, PoolName, PodName
//   - Ownership: Team, User
//   - Timing: ClaimedAt, StartedAt
//   - Content: ContainerImages (from pod.Spec), Metadata (parsed from annotation JSON)
//   - Resources: Cpu, Memory (summed from container requests/limits; zero if not set)
//
// Callers set Status and any terminal fields (TerminatedAt, FailureReason, etc.) after
// calling this function. When a running-images annotation snapshot is available (Stopping
// path), the caller may overwrite ContainerImages with that pre-release value.
func SandboxBaseFromPod(pod *corev1.Pod) gen.Sandbox {
	containerImages := make(map[string]string, len(pod.Spec.Containers))
	for _, c := range pod.Spec.Containers {
		containerImages[c.Name] = c.Image
	}

	var cpuStr, memStr string
	if cpuQ, memQ, err := utilresource.SumContainers(pod.Spec.Containers); err == nil {
		cpuStr = cpuQ.String()
		memStr = memQ.String()
	}

	sb := gen.Sandbox{
		SandboxId: pod.Labels[agentsv1alpha1.SandboxIDLabelKey],
		Namespace: pod.Namespace,
		PoolName:  pod.Labels[agentsv1alpha1.SandboxPoolLabelKey],
		PodName:   pod.Name,
		Cpu:       ptr.To(cpuStr),
		Memory:    ptr.To(memStr),
	}
	if v := pod.Spec.NodeName; v != "" {
		sb.NodeName = ptr.To(v)
	}
	if v := pod.Labels[agentsv1alpha1.LabelTeam]; v != "" {
		sb.Team = ptr.To(v)
	}
	if v := pod.Labels[agentsv1alpha1.LabelUser]; v != "" {
		sb.User = ptr.To(v)
	}
	if v := pod.Annotations[agentsv1alpha1.SandboxClaimedAtAnnotationKey]; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			sb.ClaimedAt = t
		}
	}
	if v := pod.Annotations[agentsv1alpha1.SandboxStartedAtAnnotationKey]; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			sb.StartedAt = &t
		}
	}
	if len(containerImages) > 0 {
		sb.ContainerImages = &containerImages
	}
	if id := extractContainerID(pod); id != "" {
		sb.ContainerId = ptr.To(id)
	}
	if md := parsePodMetadata(pod); len(md) > 0 {
		sb.Metadata = &md
	}
	return sb
}

// CaptureSandboxStopRecord builds a terminal gen.Sandbox for a pod that is
// transitioning through the Stopping phase. It must be called BEFORE
// MarkUpdateCompleted so that stop-metadata annotations are still present.
//
// The stop reason defaults to "Canceled" (never reached Running) or "Completed"
// when the annotation is absent, for upgrade-safety with pre-annotation pods.
//
// The running-images annotation snapshot is used for ContainerImages when
// available, falling back to pod.Spec images.
func CaptureSandboxStopRecord(pod *corev1.Pod) gen.Sandbox {
	sb := SandboxBaseFromPod(pod)

	// Override ContainerImages with the pre-release snapshot stored in the
	// running-images annotation, which reflects the image that was actually
	// executing before the in-place update rewound the pod to the idle image.
	if raw := pod.Annotations[agentsv1alpha1.SandboxRunningImagesAnnotationKey]; raw != "" {
		images := map[string]string{}
		if err := json.Unmarshal([]byte(raw), &images); err == nil && len(images) > 0 {
			sb.ContainerImages = &images
		}
	}

	// Override ContainerID with the persisted annotation snapshot when the live
	// extraction (via inplace-update-state) returned empty — this happens after
	// the in-place update clears StableContainerStatuses.
	if sb.ContainerId == nil || *sb.ContainerId == "" {
		if v := pod.Annotations[agentsv1alpha1.SandboxContainerIDAnnotationKey]; v != "" {
			sb.ContainerId = ptr.To(v)
		}
	}

	if v := pod.Annotations[agentsv1alpha1.SandboxTerminatedAtAnnotationKey]; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			sb.TerminatedAt = &t
		}
	}
	if v := pod.Annotations[agentsv1alpha1.SandboxFailureReasonAnnotationKey]; v != "" {
		sb.FailureReason = ptr.To(v)
	}
	if v := pod.Annotations[agentsv1alpha1.SandboxFailureMessageAnnotationKey]; v != "" {
		sb.FailureMessage = ptr.To(v)
	}

	// Parse exit code annotation.
	if raw := pod.Annotations[agentsv1alpha1.SandboxExitCodeAnnotationKey]; raw != "" {
		var ec int32
		if n, err := fmt.Sscanf(raw, "%d", &ec); n == 1 && err == nil {
			sb.ExitCode = &ec
		}
	}

	// Resolve stop reason, defaulting for pre-annotation pods.
	status := pod.Annotations[agentsv1alpha1.SandboxStopReasonAnnotationKey]
	if status == "" {
		if sb.StartedAt == nil {
			status = "Canceled"
		} else {
			status = "Completed"
		}
	}
	sb.Status = gen.SandboxStatus(status)

	return sb
}

// extractContainerID reads the inplace-update-state annotation and returns
// the runtime container ID of the first container captured when the sandbox
// entered Running phase. Returns "" if the annotation is absent, malformed,
// or no stable container statuses were recorded.
func extractContainerID(pod *corev1.Pod) string {
	state, err := inplaceupdate.GetInplaceUpdateState(pod)
	if err != nil || state == nil || len(state.StableContainerStatuses) == 0 {
		return ""
	}
	// Prefer the pod's first declared container to get a stable ordering.
	for _, c := range pod.Spec.Containers {
		if s, ok := state.StableContainerStatuses[c.Name]; ok && s.ContainerID != "" {
			return s.ContainerID
		}
	}
	return ""
}

// parsePodMetadata decodes the metadata annotation JSON into a map.
// Returns nil if the annotation is absent or malformed.
func parsePodMetadata(pod *corev1.Pod) map[string]string {
	raw := pod.Annotations[agentsv1alpha1.SandboxMetadataAnnotationKey]
	if raw == "" {
		return nil
	}
	m := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil
	}
	return m
}
