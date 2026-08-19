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

package sandboxenv

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// TestMergeRefreshedSpec_PreservesSchedulerTemplateMeta locks in the core
// safety property: re-rendering replaces the pod-spec body (so field deletions
// take effect) while preserving plugin-injected quota/reservation bookkeeping
// keys that live only in spec.template.metadata (verified in production:
// instance-name, instance-quantity, worker-id, quota.data).
func TestMergeRefreshedSpec_PreservesSchedulerTemplateMeta(t *testing.T) {
	old := agentsv1alpha1.SandboxPoolSpec{
		EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
			IdleImage: "idle:v1",
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metaWith(
					map[string]string{
						"quota.scitix.ai/instance-name":     "sci.c23-2",
						"quota.scitix.ai/instance-quantity": "1",
						"scheduling.navix.sh/worker-id":     "sandbox",
						agentsv1alpha1.TemplateHashLabelKey: "oldhash",
					},
					map[string]string{"quota.scitix.ai/data": `{"sci.c23-2":"1"}`},
				),
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "sandbox", Image: "idle:v1"}},
					Affinity:   &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{}},
				},
			},
		},
	}

	// Fresh clean render: no scheduler keys, new hash, affinity dropped from body.
	candidate := agentsv1alpha1.SandboxPoolSpec{
		EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
			IdleImage: "idle:v2",
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metaWith(
					map[string]string{agentsv1alpha1.TemplateHashLabelKey: "newhash"},
					nil,
				),
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "sandbox", Image: "idle:v2"}},
					// affinity deleted in the new template
				},
			},
		},
	}

	got := mergeRefreshedSpec(old, candidate)

	// Scheduler-injected foreign keys preserved.
	for _, k := range []string{"quota.scitix.ai/instance-name", "quota.scitix.ai/instance-quantity", "scheduling.navix.sh/worker-id"} {
		if got.Template.Labels[k] == "" {
			t.Errorf("scheduler label %q was lost", k)
		}
	}
	if got.Template.Annotations["quota.scitix.ai/data"] == "" {
		t.Error("scheduler annotation quota.scitix.ai/data was lost")
	}
	// New hash wins.
	if got.Template.Labels[agentsv1alpha1.TemplateHashLabelKey] != "newhash" {
		t.Errorf("hash = %q, want newhash", got.Template.Labels[agentsv1alpha1.TemplateHashLabelKey])
	}
	// Body replaced: idle image updated and affinity deletion took effect.
	if got.IdleImage != "idle:v2" {
		t.Errorf("IdleImage = %q, want idle:v2", got.IdleImage)
	}
	if got.Template.Spec.Affinity != nil {
		t.Error("affinity should be deleted (spec body replaced wholesale)")
	}
}

func metaWith(labels, annotations map[string]string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Labels: labels, Annotations: annotations}
}
