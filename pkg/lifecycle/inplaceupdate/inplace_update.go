/*
Copyright 2025 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package inplaceupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/distribution/reference"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	pkgmetrics "github.com/scitix/agent-sandbox/pkg/metrics"
	"github.com/scitix/agent-sandbox/pkg/utils/imageresolver"
)

const (
	PodAnnotationInPlaceUpdateStateKey = "agentbox.navix.sh/inplace-update-state"

	InplaceUpdatePhaseStarting  = "starting"
	InplaceUpdatePhaseStopping  = "stopping"
	InplaceUpdatePhaseCompleted = "completed"
)

var (
	ErrNoContainers          = errors.New("pod has no containers")
	ErrContainerNotFound     = errors.New("container not found in pod")
	ErrUnexpectedPodPhase    = errors.New("pod phase does not match expected value")
	ErrNoIdlePodsAvailable   = errors.New("no idle sandboxes available for creating")
	ErrTargetImageIsRequired = errors.New("target image is required")
)

// InplaceUpdateState is serialized into Pod annotation to track an in-flight update.
type InplaceUpdateState struct {
	Phase string `json:"phase"`

	TargetImage string `json:"targetImage,omitempty"`

	TargetImages map[string]string `json:"targetImages,omitempty"`

	TargetPodPhase string `json:"targetPodPhase,omitempty"`

	UpdateTimestamp metav1.Time `json:"updateTimestamp,omitempty"`

	LastContainerStatuses map[string]InplaceUpdateContainerStatus `json:"lastContainerStatuses,omitempty"`

	// StableContainerStatuses is populated when the update phase transitions to
	// Completed and TargetPodPhase==Running. It records containerID+restartCount
	// so the controller can detect unexpected restarts while phase=running.
	StableContainerStatuses map[string]StableContainerStatus `json:"stableContainerStatuses,omitempty"`
}

type InplaceUpdateContainerStatus struct {
	ImageID string `json:"imageID,omitempty"`
	// ContainerID is the container ID at the moment the in-place update was
	// triggered. It is used as a secondary completion signal: even when the
	// target image has the same digest as the previous one (e.g. rolling back
	// after a failed pull of a non-existent image), a changed containerID
	// proves that the old container was replaced and the new one is running.
	ContainerID string `json:"containerID,omitempty"`
}

// StableContainerStatus records the container state after an in-place update
// completes. Used by the controller to detect unexpected restarts (e.g., OOM).
type StableContainerStatus struct {
	ContainerID  string `json:"containerID,omitempty"`
	RestartCount int32  `json:"restartCount"`
}

type UpdateOptions struct {
	ContainerImages   map[string]string
	Labels            map[string]string
	Annotations       map[string]string
	RemoveLabels      []string
	RemoveAnnotations []string
	TargetPodPhase    string

	// UpdatePodPhase is the intermediate phase label set on the Pod while the
	// in-place image update is in progress (before the new image is pulled).
	// Use PodPhaseStarting for Idle→Running transitions and PodPhaseStopping
	// for Running→Idle transitions. Defaults to PodPhaseStarting when empty.
	UpdatePodPhase string

	ExpectedCurrentSandboxPhase string

	// DisableRetry, when true, causes TriggerUpdateWithOptions to return
	// immediately on a conflict error instead of retrying. This is appropriate
	// for optimistic operations (e.g. unexpected-restart detection) where
	// retrying on stale data could misfire: the caller should skip the pod and
	// let the next Reconcile re-observe the true state before acting.
	DisableRetry bool
}

func GetInplaceUpdateState(pod *corev1.Pod) (*InplaceUpdateState, error) {
	if pod == nil || len(pod.Annotations) == 0 {
		return nil, nil
	}

	stateStr := pod.Annotations[PodAnnotationInPlaceUpdateStateKey]
	if stateStr == "" {
		return nil, nil
	}

	state := &InplaceUpdateState{}
	if err := json.Unmarshal([]byte(stateStr), state); err != nil {
		return nil, err
	}
	return state, nil
}

func GetPodInPlaceUpdateState(pod *corev1.Pod) (*InplaceUpdateState, error) {
	return GetInplaceUpdateState(pod)
}

func TriggerUpdateWithOptions(ctx context.Context, c client.Client, pod *corev1.Pod, opts UpdateOptions) (*corev1.Pod, error) {
	if pod == nil {
		return nil, fmt.Errorf("pod is nil")
	}

	key := client.ObjectKeyFromObject(pod)

	updated := &corev1.Pod{}
	attempt := func() error {
		if err := c.Get(ctx, key, updated); err != nil {
			return err
		}

		if opts.ExpectedCurrentSandboxPhase != "" && updated.Labels[agentsv1alpha1.SandboxPhaseLabelKey] != opts.ExpectedCurrentSandboxPhase {
			return ErrUnexpectedPodPhase
		}

		changed, err := applyUpdate(updated, opts)
		if err != nil {
			return err
		}
		if !changed {
			return nil
		}
		return c.Update(ctx, updated)
	}

	var err error
	if opts.DisableRetry {
		err = attempt()
	} else {
		err = retry.RetryOnConflict(retry.DefaultRetry, attempt)
	}

	result := "success"
	if err != nil {
		switch {
		case errors.Is(err, ErrUnexpectedPodPhase), apierrors.IsConflict(err):
			result = "conflict"
		default:
			result = "error"
		}
	}
	pkgmetrics.InplaceUpdateTotal.WithLabelValues(
		pod.Namespace,
		pod.Labels[agentsv1alpha1.SandboxPoolLabelKey],
		opts.TargetPodPhase,
		pod.Labels[agentsv1alpha1.LabelUser],
		pod.Labels[agentsv1alpha1.LabelTeam],
		pod.Labels[agentsv1alpha1.LabelEnv],
		result,
	).Inc()

	return updated, err
}

func IsInplaceUpdateCompleted(ctx context.Context, sandboxPool *agentsv1alpha1.SandboxPool, pod *corev1.Pod, resolver imageresolver.DigestResolver) bool {
	if resolver == nil {
		return false
	}
	state, err := GetInplaceUpdateState(pod)
	if state == nil || err != nil {
		return true
	}
	if state.Phase == InplaceUpdatePhaseCompleted {
		return true
	}

	// check if spec Image are the same as the state target images. If not, the update is not completed.
	specImages := make(map[string]string, len(pod.Spec.Containers))
	for _, c := range pod.Spec.Containers {
		specImages[c.Name] = c.Image
		if target, ok := state.TargetImages[c.Name]; ok && c.Image != target {
			return false
		}
	}

	// current status
	statusImageIDs := make(map[string]string, len(pod.Status.ContainerStatuses))
	statusImages := make(map[string]string, len(pod.Status.ContainerStatuses))
	for _, status := range pod.Status.ContainerStatuses {
		statusImageIDs[status.Name] = status.ImageID
		statusImages[status.Name] = status.Image
	}

	// compare: resolve digests and check equality
	for name := range state.LastContainerStatuses {
		currentImageID, ok := statusImageIDs[name]
		if !ok {
			// Container not yet reported in status — not completed.
			return false
		}

		// Populate cache from pod status (free — no network call).
		currentDigest, err := resolver.DigestFromStatus(statusImages[name], currentImageID)
		if err != nil {
			klog.V(4).InfoS("Failed to parse current image digest from status",
				"pod", klog.KObj(pod), "container", name,
				"image", statusImages[name], "imageID", currentImageID, "error", err)
			return false
		}

		// Resolve target digest (cache hit or registry HEAD).
		targetDigest, err := resolver.Resolve(ctx, specImages[name],
			imageresolver.WithPullSecrets(pod.Namespace, pod.Spec.ImagePullSecrets))
		if err != nil {
			klog.V(4).InfoS("Failed to resolve target image digest",
				"pod", klog.KObj(pod), "container", name,
				"targetImage", specImages[name], "error", err)
			return false
		}

		if currentDigest != targetDigest {
			// Manifest digests differ. This can happen when the same image content
			// is re-pushed (new manifest, same layers) or pushed to a different
			// registry. Fall back to comparing config digests, which are derived
			// from image content and are stable across re-pushes.
			currentConfigDigest, err := resolver.ResolveConfigDigest(ctx, currentImageID,
				imageresolver.WithPullSecrets(pod.Namespace, pod.Spec.ImagePullSecrets))
			if err != nil {
				klog.V(4).InfoS("Failed to resolve current image config digest, treating as not completed",
					"pod", klog.KObj(pod), "container", name,
					"imageID", currentImageID, "error", err)
				return false
			}
			targetConfigDigest, err := resolver.ResolveConfigDigest(ctx, specImages[name],
				imageresolver.WithPullSecrets(pod.Namespace, pod.Spec.ImagePullSecrets))
			if err != nil {
				klog.V(4).InfoS("Failed to resolve target image config digest, treating as not completed",
					"pod", klog.KObj(pod), "container", name,
					"targetImage", specImages[name], "error", err)
				return false
			}
			if currentConfigDigest != targetConfigDigest {
				return false
			}
			klog.V(4).InfoS("Manifest digests differ but config digests match, treating as completed",
				"pod", klog.KObj(pod), "container", name,
				"currentManifest", currentDigest, "targetManifest", targetDigest,
				"configDigest", currentConfigDigest)
		}
	}
	return true
}

// normalizeImage normalizes a container image reference to its canonical form
// using the distribution/reference library. This handles Docker Hub prefix
// normalization (e.g. "ubuntu:22.04" and "docker.io/library/ubuntu:22.04"
// become the same reference) and other registry-specific canonicalization.
func normalizeImage(image string) string {
	ref, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return image
	}
	return reference.FamiliarString(ref)
}

// MarkUpdateCompleted marks the in-place update as completed for the given pod.
// It returns the updated pod object as seen by the API server after the update,
// or nil if no update was performed (e.g., already completed or phase mismatch).
// Callers should replace their in-memory pod with the returned object to avoid
// operating on stale informer-cache data within the same reconcile loop.
func MarkUpdateCompleted(ctx context.Context, c client.Client, sandboxPool *agentsv1alpha1.SandboxPool, pod *corev1.Pod, resolver imageresolver.DigestResolver) (*corev1.Pod, error) {
	if pod == nil {
		return nil, fmt.Errorf("pod is nil")
	}

	key := client.ObjectKeyFromObject(pod)
	currentPhase := GetSandboxPhase(pod)
	var result *corev1.Pod
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &corev1.Pod{}
		if err := c.Get(ctx, key, current); err != nil {
			return err
		}

		state, err := GetInplaceUpdateState(current)
		if err != nil || state == nil {
			return err
		}
		if state.Phase == InplaceUpdatePhaseCompleted {
			// Already completed — nothing to do.
			result = current
			return nil
		}
		newPhase := GetSandboxPhase(current)
		if newPhase != currentPhase {
			// The Pod has already transitioned to a new phase, likely due to a concurrent update.
			// In this case we skip marking the update as completed since the desired state has already been reached.
			result = current
			return nil
		}
		if !IsInplaceUpdateCompleted(ctx, sandboxPool, current, resolver) {
			result = current
			return nil
		}

		ensureMetadataMaps(current)
		state.Phase = InplaceUpdatePhaseCompleted
		state.UpdateTimestamp = metav1.Now()
		current.Labels[agentsv1alpha1.SandboxPhaseLabelKey] = defaultTargetPodPhase(state.TargetPodPhase, current.Labels[agentsv1alpha1.SandboxPhaseLabelKey])

		// Clear LastContainerStatuses now that the update is complete.  This is
		// especially important for the fallback path in IsInplaceUpdateCompleted
		// (Stopping after a failed Starting): the ImageID never changed, so
		// keeping the snapshot around would cause future calls to loop through the
		// fallback check unnecessarily.
		state.LastContainerStatuses = nil

		// Snapshot stable container statuses when transitioning to Running so
		// the controller can detect unexpected restarts (e.g., OOM-kills) later.
		if defaultTargetPodPhase(state.TargetPodPhase, agentsv1alpha1.SandboxPhaseRunning) == agentsv1alpha1.SandboxPhaseRunning {
			state.StableContainerStatuses = buildStableContainerStatuses(current, state.TargetImages)
		} else {
			state.StableContainerStatuses = nil
		}

		encoded, err := json.Marshal(state)
		if err != nil {
			return err
		}
		current.Annotations[PodAnnotationInPlaceUpdateStateKey] = string(encoded)
		if current.Labels[agentsv1alpha1.SandboxPhaseLabelKey] == agentsv1alpha1.SandboxPhaseRunning {
			currentTime := metav1.Now().Format(time.RFC3339)
			current.Annotations[agentsv1alpha1.SandboxStartedAtAnnotationKey] = currentTime
			current.Annotations[agentsv1alpha1.SandboxLastActiveAnnotationKey] = currentTime
		}

		// When completing a Stopping→Idle transition, atomically clean up
		// sandbox identity labels and lifecycle annotations in the same
		// Update call. This eliminates the race window where a separate
		// cleanup step could delete a newly-claimed sandbox-id.
		if current.Labels[agentsv1alpha1.SandboxPhaseLabelKey] == agentsv1alpha1.SandboxPhaseIdle {
			cleanupSandboxMetadataForIdle(current)
		}

		if err := c.Update(ctx, current); err != nil {
			return err
		}
		result = current
		return nil
	})
	return result, err
}

func applyUpdate(pod *corev1.Pod, opts UpdateOptions) (bool, error) {
	ensureMetadataMaps(pod)

	containerImages := maps.Clone(opts.ContainerImages)
	changed := false

	// ── Same-image fast path ────────────────────────────────────────────
	// When ALL target container images already match the Pod's current images,
	// we skip the Starting/Stopping phase and transition directly to the
	// target phase (e.g. Idle → Running). This eliminates image pull latency
	// entirely when the Pod was created with the same image.
	if len(containerImages) > 0 && opts.UpdatePodPhase == agentsv1alpha1.SandboxPhaseStarting {
		if allImagesMatch(pod, containerImages) {
			// same-image update: should already be in Running phase, if not try next pod.
			if pod.Status.Phase != corev1.PodRunning {
				return false, fmt.Errorf("pod is in %s phase, expected %s", pod.Status.Phase, corev1.PodRunning)
			}
			klog.InfoS("Same-image update: skipping to target phase", "pod", klog.KObj(pod), "targetPhase", opts.TargetPodPhase)
			targetPhase := defaultTargetPodPhase(opts.TargetPodPhase, agentsv1alpha1.SandboxPhaseRunning)

			changed = applyMetadataChanges(pod, opts.Labels, opts.Annotations, opts.RemoveLabels, opts.RemoveAnnotations)

			if pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey] != targetPhase {
				pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey] = targetPhase
				changed = true
			}

			// Record a Completed InplaceUpdateState with StableContainerStatuses
			// so that HasUnexpectedRestart can detect crashes later.
			state := &InplaceUpdateState{
				Phase:                   InplaceUpdatePhaseCompleted,
				TargetPodPhase:          targetPhase,
				UpdateTimestamp:         metav1.Now(),
				StableContainerStatuses: buildStableContainerStatuses(pod, containerImages),
			}
			encoded, err := json.Marshal(state)
			if err != nil {
				return false, err
			}
			if pod.Annotations[PodAnnotationInPlaceUpdateStateKey] != string(encoded) {
				pod.Annotations[PodAnnotationInPlaceUpdateStateKey] = string(encoded)
				changed = true
			}

			// Set started-at and last-active timestamps for Running phase.
			if targetPhase == agentsv1alpha1.SandboxPhaseRunning {
				now := metav1.Now().Format(time.RFC3339)
				if pod.Annotations[agentsv1alpha1.SandboxStartedAtAnnotationKey] != now {
					pod.Annotations[agentsv1alpha1.SandboxStartedAtAnnotationKey] = now
					changed = true
				}
				if pod.Annotations[agentsv1alpha1.SandboxLastActiveAnnotationKey] != now {
					pod.Annotations[agentsv1alpha1.SandboxLastActiveAnnotationKey] = now
					changed = true
				}
			}

			return changed, nil
		}
	}

	// ── Normal update path ──────────────────────────────────────────────
	lastStatuses := make(map[string]InplaceUpdateContainerStatus)
	resolvedTargets := make(map[string]string)
	containerStatuses := make(map[string]string, len(pod.Status.ContainerStatuses))
	containerIDs := make(map[string]string, len(pod.Status.ContainerStatuses))
	for _, status := range pod.Status.ContainerStatuses {
		containerStatuses[status.Name] = status.ImageID
		containerIDs[status.Name] = status.ContainerID
	}

	for containerName, newImage := range containerImages {
		if newImage == "" {
			return false, fmt.Errorf("container %q: %w", containerName, ErrTargetImageIsRequired)
		}

		found := false
		for i := range pod.Spec.Containers {
			if pod.Spec.Containers[i].Name != containerName {
				continue
			}
			found = true
			if pod.Spec.Containers[i].Image == newImage {
				break
			}
			lastStatuses[containerName] = InplaceUpdateContainerStatus{
				ImageID:     containerStatuses[containerName],
				ContainerID: containerIDs[containerName],
			}
			resolvedTargets[containerName] = newImage
			pod.Spec.Containers[i].Image = newImage
			changed = true
			break
		}
		if !found {
			return false, fmt.Errorf("%w: %s", ErrContainerNotFound, containerName)
		}
	}

	changed = applyMetadataChanges(pod, opts.Labels, opts.Annotations, opts.RemoveLabels, opts.RemoveAnnotations) || changed

	if len(resolvedTargets) > 0 {
		updatePodPhase := opts.UpdatePodPhase
		if updatePodPhase == "" {
			updatePodPhase = agentsv1alpha1.SandboxPhaseStarting
		}
		state := &InplaceUpdateState{
			Phase:                 updatePodPhase,
			TargetImage:           singleTargetImage(resolvedTargets),
			TargetImages:          resolvedTargets,
			TargetPodPhase:        defaultTargetPodPhase(opts.TargetPodPhase, agentsv1alpha1.SandboxPhaseRunning),
			UpdateTimestamp:       metav1.Now(),
			LastContainerStatuses: lastStatuses,
		}
		encoded, err := json.Marshal(state)
		if err != nil {
			return false, err
		}
		if pod.Annotations[PodAnnotationInPlaceUpdateStateKey] != string(encoded) {
			pod.Annotations[PodAnnotationInPlaceUpdateStateKey] = string(encoded)
			changed = true
		}
		if pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey] != updatePodPhase {
			pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey] = updatePodPhase
			changed = true
		}
		return changed, nil
	}

	if opts.TargetPodPhase != "" && pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey] != opts.TargetPodPhase {
		pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey] = opts.TargetPodPhase
		changed = true
	}

	return changed, nil
}

// applyMetadataChanges applies labels, annotations, and their removals to the
// given Pod. It returns true if any metadata was actually modified.
func applyMetadataChanges(pod *corev1.Pod, labels, annotations map[string]string, removeLabels, removeAnnotations []string) bool {
	changed := false
	for key, value := range labels {
		if pod.Labels[key] != value {
			pod.Labels[key] = value
			changed = true
		}
	}
	for key, value := range annotations {
		if pod.Annotations[key] != value {
			pod.Annotations[key] = value
			changed = true
		}
	}
	for _, key := range removeLabels {
		if _, exists := pod.Labels[key]; exists {
			delete(pod.Labels, key)
			changed = true
		}
	}
	for _, key := range removeAnnotations {
		if _, exists := pod.Annotations[key]; exists {
			delete(pod.Annotations, key)
			changed = true
		}
	}
	return changed
}

// allImagesMatch returns true when every entry in containerImages has a
// non-empty value and the corresponding Pod container already uses the same
// image (after normalising Docker Hub prefixes).
func allImagesMatch(pod *corev1.Pod, containerImages map[string]string) bool {
	for containerName, newImage := range containerImages {
		if newImage == "" {
			return false
		}
		found := false
		for _, c := range pod.Spec.Containers {
			if c.Name == containerName {
				found = true
				if normalizeImage(c.Image) != normalizeImage(newImage) {
					return false
				}
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// buildStableContainerStatuses snapshots the current containerID and
// restartCount of the containers this update targets. The snapshot is used later
// by HasUnexpectedRestart to detect crashes (e.g. OOM-kills).
//
// Scoped to targets on purpose. Snapshotting every container makes any injected
// sidecar's restart look like the sandbox crashed, and the reaction to that is
// to recycle the Pod — so an egress proxy that got OOM-killed would take a live
// sandbox down with it, even though the proxy recovers on its own (it reloads
// its policy and credentials from the volume on start). Only the containers
// whose image this update swapped are the sandbox's own lifecycle; the rest have
// their own, and kubelet already restarts them.
//
// An empty targets map yields no snapshot, which disables the check rather than
// widening it.
func buildStableContainerStatuses(pod *corev1.Pod, targets map[string]string) map[string]StableContainerStatus {
	if len(targets) == 0 {
		return nil
	}
	stable := make(map[string]StableContainerStatus, len(targets))
	for _, cs := range pod.Status.ContainerStatuses {
		if _, ok := targets[cs.Name]; !ok {
			continue
		}
		stable[cs.Name] = StableContainerStatus{
			ContainerID:  cs.ContainerID,
			RestartCount: cs.RestartCount,
		}
	}
	return stable
}

func ensureMetadataMaps(pod *corev1.Pod) {
	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
}

func singleTargetImage(targets map[string]string) string {
	if len(targets) != 1 {
		return ""
	}
	for _, image := range targets {
		return image
	}
	return ""
}

func defaultTargetPodPhase(target, fallback string) string {
	if target != "" {
		return target
	}
	return fallback
}

// cleanupSandboxMetadataForIdle removes the sandbox-id label and all sandbox
// lifecycle annotations from a pod that is transitioning to Idle. This is
// called inside MarkUpdateCompleted so that the cleanup is atomic with the
// phase transition, preventing a race where ClaimIdlePod sets a new
// sandbox-id between the phase write and a separate cleanup step.
func cleanupSandboxMetadataForIdle(pod *corev1.Pod) {
	// Remove sandbox-id label.
	delete(pod.Labels, agentsv1alpha1.SandboxIDLabelKey)

	// Remove all sandbox lifecycle annotations.
	for _, key := range []string{
		agentsv1alpha1.SandboxIDAnnotationKey,
		agentsv1alpha1.SandboxClaimedAtAnnotationKey,
		agentsv1alpha1.SandboxStartedAtAnnotationKey,
		agentsv1alpha1.SandboxMetadataAnnotationKey,
		agentsv1alpha1.SandboxStopReasonAnnotationKey,
		agentsv1alpha1.SandboxTerminatedAtAnnotationKey,
		agentsv1alpha1.SandboxFailureReasonAnnotationKey,
		agentsv1alpha1.SandboxFailureMessageAnnotationKey,
		agentsv1alpha1.SandboxExitCodeAnnotationKey,
		agentsv1alpha1.SandboxRunningImagesAnnotationKey,
		agentsv1alpha1.SandboxContainerIDAnnotationKey,
		agentsv1alpha1.SandboxPostStartHooksAnnotationKey,
	} {
		delete(pod.Annotations, key)
	}
}

// HasUnexpectedRestart returns true if any container this update targeted has a
// different containerID than what was recorded in StableContainerStatuses. A
// containerID change indicates the container was killed and restarted by kubelet
// (e.g., OOM-kill, crash) while the pod was in Running phase.
//
// Injected sidecars are outside the snapshot (see buildStableContainerStatuses),
// so their restarts do not recycle the sandbox.
//
// Design note: we intentionally do NOT use restartCount as a signal here.
// kubelet reports containerID, restartCount, and Ready in separate status
// patches, which means the controller-runtime informer cache can reflect an
// intermediate state where imageID/Ready are already updated but restartCount
// has not yet incremented. Using only containerID avoids this race:
// containerID is assigned atomically when a new container is created, so a
// change unambiguously indicates a container replacement event. If
// stable.ContainerID is empty (cache lag prevented snapshotting), we skip the
// check to avoid a false positive.
func HasUnexpectedRestart(pod *corev1.Pod) bool {
	state, err := GetInplaceUpdateState(pod)
	if err != nil || state == nil || len(state.StableContainerStatuses) == 0 {
		return false
	}
	if state.Phase != InplaceUpdatePhaseCompleted || state.TargetPodPhase != agentsv1alpha1.SandboxPhaseRunning {
		// Only check for unexpected restarts when the update is completed and the target phase is Running.
		// During Starting→Running transitions, the containerID will naturally change when kubelet switches to the new image, so we only start monitoring for unexpected restarts after the update is marked as completed.
		return false
	}
	for _, cs := range pod.Status.ContainerStatuses {
		stable, ok := state.StableContainerStatuses[cs.Name]
		if !ok {
			continue
		}
		// Guard: if stable.ContainerID was not captured (empty string due to cache
		// lag at snapshot time), skip this container to avoid a false positive.
		if stable.ContainerID == "" {
			continue
		}
		if cs.ContainerID != stable.ContainerID {
			klog.InfoS("Detected unexpected container restart after in-place update",
				"pod", klog.KObj(pod),
				"container", cs.Name,
				"state", state,
				"newContainerID", cs.ContainerID,
			)
			return true
		}
	}
	return false
}
