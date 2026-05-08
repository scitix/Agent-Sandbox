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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/inplaceupdate"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

func TestReleaseSandboxPod(t *testing.T) {
	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "default"},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			Replicas: 1,
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "pause:3.10",
				Template: &corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{
					{Name: "sandbox", Image: "myapp:v1"},
				}}},
			},
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-1",
			Namespace: "default",
			Labels: map[string]string{
				agentsv1alpha1.SandboxPoolLabelKey:  pool.Name,
				agentsv1alpha1.SandboxIDLabelKey:    "sandbox-1",
				agentsv1alpha1.ManagedByLabelKey:    agentsv1alpha1.ManagedBySandboxAPIServer,
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseRunning,
				"custom-label":                      "custom",
			},
			Annotations: map[string]string{
				agentsv1alpha1.SandboxManagedLabelKeysAnnotationKey:      `["custom-label"]`,
				agentsv1alpha1.SandboxManagedAnnotationKeysAnnotationKey: `["custom-annotation"]`,
				agentsv1alpha1.SandboxIDAnnotationKey:                    "sandbox-1",
				agentsv1alpha1.SandboxMetadataAnnotationKey:              `{"suite":"unit"}`,
				"custom-annotation":                                      "custom",
				// inplace-update state with a stable container ID.
				inplaceupdate.PodAnnotationInPlaceUpdateStateKey: `{"phase":"completed","targetPodPhase":"running","stableContainerStatuses":{"sandbox":{"containerID":"containerd://abc123","restartCount":0}}}`,
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "sandbox", Image: "pause:3.9"}}},
	}

	cli := newTestClientBuilder(t).WithObjects(pool, pod).Build()
	released, err := ReleaseSandboxPod(context.Background(), cli, pod, pool, ReleaseSandboxPodOptions{})
	if err != nil {
		t.Fatalf("release sandbox pod: %v", err)
	}
	if released.Spec.Containers[0].Image != "pause:3.10" {
		t.Fatalf("expected idle image, got %s", released.Spec.Containers[0].Image)
	}

	updated := &corev1.Pod{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, updated); err != nil {
		t.Fatalf("get updated pod: %v", err)
	}
	// sandbox-id LABEL must be KEPT during Stopping (so FindClaimedPodBySandboxID still works).
	if updated.Labels[agentsv1alpha1.SandboxIDLabelKey] == "" {
		t.Fatalf("expected sandbox-id label to be kept during Stopping, got %#v", updated.Labels)
	}
	// managed-by label must be removed.
	if updated.Labels[agentsv1alpha1.ManagedByLabelKey] != "" {
		t.Fatalf("expected managed-by label removed, got %#v", updated.Labels)
	}
	// custom labels must be removed.
	if updated.Labels["custom-label"] != "" {
		t.Fatalf("expected custom-label removed, got %#v", updated.Labels)
	}
	// sandbox-metadata annotation must be KEPT during Stopping.
	if updated.Annotations[agentsv1alpha1.SandboxMetadataAnnotationKey] == "" {
		t.Fatalf("expected sandbox-metadata annotation to be kept during Stopping, got %#v", updated.Annotations)
	}
	// custom annotation must be removed.
	if updated.Annotations["custom-annotation"] != "" {
		t.Fatalf("expected custom-annotation removed, got %#v", updated.Annotations)
	}
	// stop-reason annotation must be set.
	if updated.Annotations[agentsv1alpha1.SandboxStopReasonAnnotationKey] != "Completed" {
		t.Fatalf("expected stop-reason=Completed, got %q", updated.Annotations[agentsv1alpha1.SandboxStopReasonAnnotationKey])
	}
	// running-images annotation must be set.
	if updated.Annotations[agentsv1alpha1.SandboxRunningImagesAnnotationKey] == "" {
		t.Fatalf("expected running-images annotation to be set, got %#v", updated.Annotations)
	}
	// container-id annotation must be set when inplace-update-state had a stable container ID.
	if updated.Annotations[agentsv1alpha1.SandboxContainerIDAnnotationKey] != "containerd://abc123" {
		t.Fatalf("expected container-id annotation containerd://abc123, got %q", updated.Annotations[agentsv1alpha1.SandboxContainerIDAnnotationKey])
	}
	if updated.Labels[agentsv1alpha1.SandboxPhaseLabelKey] != agentsv1alpha1.SandboxPhaseStopping {
		t.Fatalf("expected stopping phase while reverting image, got %s", updated.Labels[agentsv1alpha1.SandboxPhaseLabelKey])
	}
}

func newTestClientBuilder(t *testing.T) *fake.ClientBuilder {
	t.Helper()
	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("get fake client builder: %v", err)
	}
	return cb
}
