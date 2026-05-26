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
	"maps"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/inplaceupdate"
)

// sortPodsByPhasePriority sorts Pods by phase priority for deletion
// Priority order: idle > starting/stopping > failed > running (running is lowest priority)
func (r *SandboxPoolReconciler) sortPodsByPhasePriority(pods []corev1.Pod) []corev1.Pod {
	phasePriority := map[string]int{
		agentsv1alpha1.SandboxPhaseIdle:     0,
		agentsv1alpha1.SandboxPhaseStarting: 1,
		agentsv1alpha1.SandboxPhaseStopping: 1,
		agentsv1alpha1.SandboxPhaseFailed:   2,
		agentsv1alpha1.SandboxPhaseRunning:  3,
		"":                                  0, // Treat empty as idle
	}

	sorted := make([]corev1.Pod, len(pods))
	copy(sorted, pods)

	sort.Slice(sorted, func(i, j int) bool {
		phaseI := inplaceupdate.GetSandboxPhase(&sorted[i])
		phaseJ := inplaceupdate.GetSandboxPhase(&sorted[j])

		priorityI, okI := phasePriority[phaseI]
		if !okI {
			priorityI = 0 // Default to idle
		}

		priorityJ, okJ := phasePriority[phaseJ]
		if !okJ {
			priorityJ = 0 // Default to idle
		}

		if priorityI != priorityJ {
			return priorityI < priorityJ
		}

		// If same priority, sort by creation time (oldest first)
		return sorted[i].CreationTimestamp.Before(&sorted[j].CreationTimestamp)
	})

	return sorted
}

// labelsToExcludeFromTemplateSync lists Label keys that must NOT be copied from a
// SandboxTemplate to a SandboxPool.
//
// agentbox.io/sync-source distinguishes resources created/synced via ws-proxy
// ("global") from locally-created ones. A Pool has its own origin semantics and
// should not inherit the Template's sync-source label.
var labelsToExcludeFromTemplateSync = map[string]struct{}{
	agentsv1alpha1.LabelSyncSource: {},
}

// SyncLabelsFromTemplate merges Template Labels into Pool Labels, skipping any key
// listed in labelsToExcludeFromTemplateSync (e.g. agentbox.io/sync-source).
// Template values take precedence over any existing Pool values for the same key.
func SyncLabelsFromTemplate(dst, tmplLabels map[string]string) {
	for k, v := range tmplLabels {
		if _, excluded := labelsToExcludeFromTemplateSync[k]; !excluded {
			dst[k] = v
		}
	}
}

// annotationsToExcludeFromTemplateSync lists Template annotation keys that must not be copied
// to Pools. Docs annotations are intentionally excluded: the Pool detail API fetches them
// live from the Template at query time, so storing copies on the Pool would be redundant.
var annotationsToExcludeFromTemplateSync = map[string]struct{}{
	agentsv1alpha1.SandboxTemplateDocsAnnotationKey:     {},
	agentsv1alpha1.SandboxTemplatePoolDocsAnnotationKey: {}, //nolint:staticcheck
}

// SyncAnnotationsFromTemplate merges Template Annotations into Pool Annotations,
// skipping keys listed in annotationsToExcludeFromTemplateSync.
// Callers are responsible for overwriting system-managed keys afterwards.
func SyncAnnotationsFromTemplate(dst, tmplAnnotations map[string]string) {
	for k, v := range tmplAnnotations {
		if _, excluded := annotationsToExcludeFromTemplateSync[k]; !excluded {
			dst[k] = v
		}
	}
}

// createPod creates a new Pod for the SandboxPool
func (r *SandboxPoolReconciler) createPod(ctx context.Context, sandboxPool *agentsv1alpha1.SandboxPool) error {
	// Create a new Pod from the template
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: sandboxPool.Name + "-",
			Namespace:    sandboxPool.Namespace,
			Labels:       make(map[string]string, len(sandboxPool.Spec.Template.Labels)),
			Annotations:  make(map[string]string, len(sandboxPool.Spec.Template.Annotations)),
		},
		Spec: sandboxPool.Spec.Template.Spec,
	}
	SyncLabelsFromTemplate(pod.Labels, sandboxPool.Spec.Template.Labels)
	SyncAnnotationsFromTemplate(pod.Annotations, sandboxPool.Spec.Template.Annotations)

	// Set the owner reference
	if err := ctrl.SetControllerReference(sandboxPool, pod, r.Scheme); err != nil {
		klog.ErrorS(err, "Failed to set controller reference", "namespace", sandboxPool.Namespace, "name", sandboxPool.Name)
		return err
	}

	// Add sandbox-protection finalizer so the controller always sees a
	// DeletionTimestamp window before the pod is GC'd. This ensures sandbox
	// history records can be written even when a pod is deleted externally.
	if !containsString(pod.Finalizers, agentsv1alpha1.SandboxProtectionFinalizer) {
		pod.Finalizers = append(pod.Finalizers, agentsv1alpha1.SandboxProtectionFinalizer)
	}

	// Apply PodCreationImagePolicy: when set to IdleImage, replace the first
	// container image with spec.idleImage so the Pod enters Idle faster (the
	// lightweight idle image requires no heavy runtime layer to pull).
	// When set to PoolDefaultImage (default), the template image is preserved,
	// enabling the same-image fast path for the first sandbox assigned to this Pod.
	if sandboxPool.Spec.PodCreationImagePolicy == agentsv1alpha1.PodCreationImagePolicyIdleImage &&
		sandboxPool.Spec.IdleImage != "" &&
		len(pod.Spec.Containers) > 0 {
		pod.Spec.Containers[0].Image = sandboxPool.Spec.IdleImage
	}

	// Propagate labels and annotations from the pool spec to the pod, since the SI Scheduler expects them there
	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}
	pod.Labels[agentsv1alpha1.SandboxPoolLabelKey] = sandboxPool.Name
	pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey] = agentsv1alpha1.SandboxPhaseIdle
	maps.Copy(pod.Labels, sandboxPool.Labels)

	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	maps.Copy(pod.Annotations, sandboxPool.Annotations)

	// Create the Pod. PreCreatePod runs before the API submit, so plugin
	// mutations on `pod` are picked up by the Create call below — no
	// separate Update is needed and the updated flag is purely informational.
	if _, appErr := r.PluginManager.PreCreatePodHooks(ctx, pod, sandboxPool); appErr != nil {
		klog.ErrorS(appErr, "Plugin PreCreatePod failed, aborting pod creation", "namespace", sandboxPool.Namespace, "name", sandboxPool.Name)
		return appErr
	}
	if err := r.Create(ctx, pod); err != nil {
		klog.ErrorS(err, "Failed to create Pod", "namespace", pod.Namespace, "name", pod.Name)
		return err
	}

	klog.InfoS("Created Pod", "namespace", pod.Namespace, "name", pod.Name,
		"schedulerName", pod.Spec.SchedulerName)
	return nil
}

func filterPodsNotDeleting(pods []corev1.Pod) []corev1.Pod {
	result := make([]corev1.Pod, 0, len(pods))
	for i := range pods {
		if pods[i].DeletionTimestamp != nil {
			continue
		}
		result = append(result, pods[i])
	}
	return result
}

// defaultScaleDownProtectionWindow is the two-phase protection window applied
// to idle Pods scheduled for deletion. It gives a concurrent Sandbox.create
// request a brief opportunity to claim the Pod and cancel the scale-down.
//
// The window used to be configurable via Pool.spec.autoscaling.scaleDownPolicy
// — that knob moved to SandboxEnv when autoscaling decisions were lifted to
// the Env layer. For now the Pool unconditionally uses a sensible constant;
// if a per-Pool override is needed later, surface it via the owning Env's
// scaleDownPolicy and look it up here.
const defaultScaleDownProtectionWindow = 10 * time.Second

// scaleDownProtectionWindow returns the duration of the two-phase protection
// window applied to idle Pods being scaled down. Always returns the constant
// default since the autoscaling decision lives on SandboxEnv now.
func scaleDownProtectionWindow(pool *agentsv1alpha1.SandboxPool) time.Duration {
	_ = pool
	return defaultScaleDownProtectionWindow
}

// markScaleDownProtected stamps the pod with a scale-down-protected annotation
// containing the current RFC3339 timestamp.  This signals that the pod is a
// candidate for deletion after the protection window elapses, but can still be
// claimed by a new Create Sandbox request (which will clear the annotation).
func (r *SandboxPoolReconciler) markScaleDownProtected(ctx context.Context, pod *corev1.Pod) error {
	ts := time.Now().UTC().Format(time.RFC3339)
	patch := []byte(`{"metadata":{"annotations":{"` + agentsv1alpha1.SandboxScaleDownProtectedAnnotationKey + `":"` + ts + `"}}}`)
	return r.Patch(ctx, pod, client.RawPatch(types.MergePatchType, patch))
}

// unmarkScaleDownProtected removes the scale-down-protected annotation from a
// single pod. A JSON merge patch with a null value deletes the key in K8s
// semantics. Idempotent: re-running on a pod that no longer carries the
// annotation is a no-op patch.
func (r *SandboxPoolReconciler) unmarkScaleDownProtected(ctx context.Context, pod *corev1.Pod) error {
	patch := []byte(`{"metadata":{"annotations":{"` + agentsv1alpha1.SandboxScaleDownProtectedAnnotationKey + `":null}}}`)
	return r.Patch(ctx, pod, client.RawPatch(types.MergePatchType, patch))
}

// unmarkStaleScaleDownProtected strips SandboxScaleDownProtectedAnnotationKey
// from every Idle pod in pods that currently carries it. Returns the number of
// pods successfully cleared (for metrics). Errors on individual pods are
// logged but do not abort the sweep — the next reconcile retries stragglers.
//
// The caller decides *when* this is safe to run (e.g. autoscaling disabled, or
// no scale-down planned this cycle, or scheduler reported pending demand);
// this function makes no policy decision of its own.
func (r *SandboxPoolReconciler) unmarkStaleScaleDownProtected(ctx context.Context, pods []corev1.Pod) int {
	cleared := 0
	for i := range pods {
		p := &pods[i]
		if inplaceupdate.GetSandboxPhase(p) != agentsv1alpha1.SandboxPhaseIdle {
			continue
		}
		if p.Annotations[agentsv1alpha1.SandboxScaleDownProtectedAnnotationKey] == "" {
			continue
		}
		if err := r.unmarkScaleDownProtected(ctx, p); err != nil {
			if !errors.IsNotFound(err) {
				klog.ErrorS(err, "unmarkStaleScaleDownProtected: patch failed",
					"namespace", p.Namespace, "name", p.Name)
			}
			continue
		}
		cleared++
	}
	return cleared
}
