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
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/inplaceupdate"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

var ErrSandboxNotFound = errors.New("sandbox not found")

// ReleaseSandboxPodOptions carries the stop metadata written to Pod annotations
// during Stopping so the reconciler can perform a deferred KV store write at Stopping→Idle.
type ReleaseSandboxPodOptions struct {
	StopReason     agentsv1alpha1.SandboxStopReason // "Completed" | "Released" | "Failed" | "Canceled"; defaults to "Completed"
	TerminatedAt   string                           // RFC3339; defaults to time.Now().UTC() if empty
	FailureReason  string                           // e.g. "IdleTimeout", "OOMKilled"
	FailureMessage string                           // human-readable
	ExitCode       *int32                           // container exit code for Failed sandboxes

	ExpectedCurrentSandboxPhase string

	// DisableRetry, when true, skips the retry-on-conflict loop in the
	// underlying TriggerUpdateWithOptions call. Use this for opportunistic
	// release paths (e.g. unexpected-restart detection) where retrying on a
	// stale pod view could misfire; the caller should skip and let the next
	// Reconcile re-observe before acting.
	DisableRetry bool
}

func FindClaimedPodBySandboxID(ctx context.Context, c client.Client, namespace, sandboxID string) (*corev1.Pod, error) {
	if sandboxID == "" {
		return nil, ErrSandboxNotFound
	}

	list := &corev1.PodList{}
	listOptions := []client.ListOption{client.MatchingFields{indexer.IndexFieldSandboxID: sandboxID}}
	if namespace != "" {
		listOptions = append(listOptions, client.InNamespace(namespace))
	}
	if err := c.List(ctx, list, listOptions...); err != nil {
		return nil, err
	}
	if len(list.Items) == 0 {
		return nil, ErrSandboxNotFound
	}

	pod := list.Items[0].DeepCopy()
	return pod, nil
}

func ListClaimedPods(ctx context.Context, c client.Client, namespace string) ([]corev1.Pod, error) {
	return ListClaimedPodsWithFilter(ctx, c, namespace, "", "")
}

// ListClaimedPodsWithFilter lists all claimed (non-idle) pods in the given namespace.
// When team and user are both non-empty, only pods with matching labels are returned.
func ListClaimedPodsWithFilter(ctx context.Context, c client.Client, namespace, team, user string) ([]corev1.Pod, error) {
	list := &corev1.PodList{}
	listOptions := []client.ListOption{}
	if namespace != "" {
		listOptions = append(listOptions, client.InNamespace(namespace))
	}
	if team != "" && user != "" {
		listOptions = append(listOptions, client.MatchingLabels{
			agentsv1alpha1.LabelTeam: team,
			agentsv1alpha1.LabelUser: user,
		})
	}
	if err := c.List(ctx, list, listOptions...); err != nil {
		return nil, err
	}

	claimed := make([]corev1.Pod, 0, len(list.Items))
	for i := range list.Items {
		if list.Items[i].Labels[agentsv1alpha1.SandboxIDLabelKey] == "" {
			continue
		}
		// Skip pods that are Idle but still have the sandbox-id label (cleanup pending).
		if list.Items[i].Labels[agentsv1alpha1.SandboxPhaseLabelKey] == agentsv1alpha1.SandboxPhaseIdle {
			continue
		}
		claimed = append(claimed, *list.Items[i].DeepCopy())
	}

	sort.Slice(claimed, func(i, j int) bool {
		return claimed[i].CreationTimestamp.Before(&claimed[j].CreationTimestamp)
	})

	return claimed, nil
}

func ReleaseSandboxPod(ctx context.Context, c client.Client, pod *corev1.Pod, pool *agentsv1alpha1.SandboxPool, opts ReleaseSandboxPodOptions) (*corev1.Pod, error) {
	if pod == nil {
		return nil, fmt.Errorf("pod is nil")
	}
	if pool == nil {
		return nil, fmt.Errorf("pool is nil")
	}

	// Default stop reason.
	stopReason := opts.StopReason
	if stopReason == "" {
		stopReason = agentsv1alpha1.SandboxStopReasonCompleted
	}

	// Default terminated-at.
	terminatedAt := opts.TerminatedAt
	if terminatedAt == "" {
		terminatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	// Labels to remove: managed-by + custom labels only; sandbox-id is KEPT.
	labelKeysToRemove := append([]string{
		agentsv1alpha1.ManagedByLabelKey,
	}, parseManagedKeys(pod.Annotations[agentsv1alpha1.SandboxManagedLabelKeysAnnotationKey])...)

	// Annotations to remove: operational ones only; sandbox-id, claimed-at, metadata are KEPT.
	annotationKeysToRemove := append([]string{
		agentsv1alpha1.SandboxIdleTimeoutAnnotationKey,
		agentsv1alpha1.SandboxLastActiveAnnotationKey,
		agentsv1alpha1.SandboxManagedLabelKeysAnnotationKey,
		agentsv1alpha1.SandboxManagedAnnotationKeysAnnotationKey,
	}, parseManagedKeys(pod.Annotations[agentsv1alpha1.SandboxManagedAnnotationKeysAnnotationKey])...)

	// Snapshot running container images before they get reset to idle.
	runningImages := make(map[string]string, len(pod.Spec.Containers))
	for _, c := range pod.Spec.Containers {
		runningImages[c.Name] = c.Image
	}
	runningImagesJSON, err := json.Marshal(runningImages)
	if err != nil {
		return nil, fmt.Errorf("marshal running images: %w", err)
	}

	// Snapshot container ID before the in-place update clears StableContainerStatuses.
	containerID := extractContainerID(pod)

	// Build stop metadata annotations.
	addAnnotations := map[string]string{
		agentsv1alpha1.SandboxStopReasonAnnotationKey:    string(stopReason),
		agentsv1alpha1.SandboxTerminatedAtAnnotationKey:  terminatedAt,
		agentsv1alpha1.SandboxRunningImagesAnnotationKey: string(runningImagesJSON),
	}
	if containerID != "" {
		addAnnotations[agentsv1alpha1.SandboxContainerIDAnnotationKey] = containerID
	}
	if opts.FailureReason != "" {
		addAnnotations[agentsv1alpha1.SandboxFailureReasonAnnotationKey] = opts.FailureReason
	}
	if opts.FailureMessage != "" {
		addAnnotations[agentsv1alpha1.SandboxFailureMessageAnnotationKey] = opts.FailureMessage
	}
	if opts.ExitCode != nil {
		addAnnotations[agentsv1alpha1.SandboxExitCodeAnnotationKey] = strconv.FormatInt(int64(*opts.ExitCode), 10)
	}

	idleImages := IdleContainerImages(pool)
	if podImagesMatchTarget(pod, idleImages) {
		klog.ErrorS(nil, "ReleaseSandboxPod: container images unchanged for pod, this is unexpected",
			"namespace", pod.Namespace, "name", pod.Name)
	}

	released, updateErr := inplaceupdate.TriggerUpdateWithOptions(ctx, c, pod, inplaceupdate.UpdateOptions{
		ContainerImages:             idleImages,
		RemoveLabels:                uniqueStrings(labelKeysToRemove),
		RemoveAnnotations:           uniqueStrings(annotationKeysToRemove),
		Annotations:                 addAnnotations,
		TargetPodPhase:              agentsv1alpha1.SandboxPhaseIdle,
		UpdatePodPhase:              agentsv1alpha1.SandboxPhaseStopping,
		ExpectedCurrentSandboxPhase: opts.ExpectedCurrentSandboxPhase,
		DisableRetry:                opts.DisableRetry,
	})
	if updateErr != nil {
		return nil, updateErr
	}

	return released, nil
}

func IdleContainerImages(pool *agentsv1alpha1.SandboxPool) map[string]string {
	containerImages := make(map[string]string, len(pool.Spec.Template.Spec.Containers))
	for i, container := range pool.Spec.Template.Spec.Containers {
		image := container.Image
		if i == 0 && pool.Spec.IdleImage != "" {
			image = pool.Spec.IdleImage
		}
		containerImages[container.Name] = image
	}
	return containerImages
}

func parseManagedKeys(value string) []string {
	if value == "" {
		return nil
	}

	var keys []string
	if err := json.Unmarshal([]byte(value), &keys); err != nil {
		return nil
	}
	return keys
}

func uniqueStrings(values []string) []string {
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || slices.Contains(unique, value) {
			continue
		}
		unique = append(unique, value)
	}
	return unique
}

// podImagesMatchTarget returns true if all pod container images already match
// the target images map (i.e., no image change would actually occur).
func podImagesMatchTarget(pod *corev1.Pod, targetImages map[string]string) bool {
	for _, c := range pod.Spec.Containers {
		if target, ok := targetImages[c.Name]; ok && c.Image != target {
			return false
		}
	}
	return true
}
