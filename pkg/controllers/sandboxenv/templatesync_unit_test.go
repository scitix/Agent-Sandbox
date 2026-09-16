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
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	quotaplugin "github.com/scitix/agent-sandbox/pkg/framework/providers/quota"
	"github.com/scitix/agent-sandbox/pkg/framework/providers/quota/quotatest"
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

// TestSyncRequiresQuota covers the whole point of stamping the sizing state
// onto the Env: the console and the CLI read it from the Env rather than
// re-deriving the rule, so it has to be there, it has to follow the template,
// and it has to appear on Envs that predate the label.
func TestSyncRequiresQuota(t *testing.T) {
	billedTemplate := func() *agentsv1alpha1.SandboxTemplate {
		return &agentsv1alpha1.SandboxTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "tmpl",
				Annotations: map[string]string{quotatest.BillingKey: `{"sci.c23-2":"1"}`},
			},
		}
	}
	freeTemplate := func() *agentsv1alpha1.SandboxTemplate {
		return &agentsv1alpha1.SandboxTemplate{ObjectMeta: metav1.ObjectMeta{Name: "tmpl"}}
	}
	newEnv := func(labels map[string]string) *agentsv1alpha1.SandboxEnv {
		return &agentsv1alpha1.SandboxEnv{
			ObjectMeta: metav1.ObjectMeta{Name: "env-a", Namespace: "ns-a", Labels: labels},
			Spec: agentsv1alpha1.SandboxEnvSpec{
				TemplateRef: agentsv1alpha1.SandboxEnvTemplateRef{Name: "tmpl"},
				Mode:        agentsv1alpha1.SandboxEnvModeWarmPool,
			},
		}
	}

	tests := []struct {
		name       string
		tmpl       *agentsv1alpha1.SandboxTemplate
		labels     map[string]string
		want       string
		wantChange bool
	}{
		{
			name:       "billed template stamps billed",
			tmpl:       billedTemplate(),
			want:       string(quotaplugin.PoolSizingBilled),
			wantChange: true,
		},
		{
			name:       "unbilled template stamps free-form",
			tmpl:       freeTemplate(),
			want:       string(quotaplugin.PoolSizingFreeForm),
			wantChange: true,
		},
		{
			name:       "an already-correct stamp is left alone",
			tmpl:       billedTemplate(),
			labels:     map[string]string{agentsv1alpha1.LabelEnvPoolSizing: string(quotaplugin.PoolSizingBilled)},
			want:       string(quotaplugin.PoolSizingBilled),
			wantChange: false,
		},
		{
			// The template lost its billing annotation — the Env has to follow,
			// or the console keeps asking for a quota the server no longer wants.
			name:       "a stale stamp is rewritten",
			tmpl:       freeTemplate(),
			labels:     map[string]string{agentsv1alpha1.LabelEnvPoolSizing: string(quotaplugin.PoolSizingBilled)},
			want:       string(quotaplugin.PoolSizingFreeForm),
			wantChange: true,
		},
		{
			// The upgrade case: an Env that predates the label, on a billed
			// template. Absence must not read as "either" forever.
			name:       "an unstamped env is backfilled",
			tmpl:       billedTemplate(),
			labels:     map[string]string{agentsv1alpha1.LabelTeam: "t1"},
			want:       string(quotaplugin.PoolSizingBilled),
			wantChange: true,
		},
		{
			// No Provider at all (the open-source deployment): the stamp still
			// has to be written, or clients would see an unstamped Env and
			// could not tell it from a stale one.
			name:       "no provider stamps either",
			tmpl:       billedTemplate(),
			want:       string(quotaplugin.PoolSizingEither),
			wantChange: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scheme := newReconcileTestScheme(t)
			e := newEnv(tc.labels)
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(e, tc.tmpl).Build()
			r := &SandboxEnvReconciler{
				Client: c, Scheme: scheme, LocalClusterID: testLocalCluster,
			}
			if tc.name != "no provider stamps either" {
				r.QuotaProvider = quotatest.Billing()
			}

			changed, err := r.syncRequiresQuota(context.Background(), e)
			if err != nil {
				t.Fatalf("syncRequiresQuota: %v", err)
			}
			if changed != tc.wantChange {
				t.Errorf("changed = %v, want %v", changed, tc.wantChange)
			}

			got := &agentsv1alpha1.SandboxEnv{}
			key := types.NamespacedName{Namespace: e.Namespace, Name: e.Name}
			if err := c.Get(context.Background(), key, got); err != nil {
				t.Fatalf("get env: %v", err)
			}
			if got.Labels[agentsv1alpha1.LabelEnvPoolSizing] != tc.want {
				t.Errorf("%s = %q, want %q", agentsv1alpha1.LabelEnvPoolSizing,
					got.Labels[agentsv1alpha1.LabelEnvPoolSizing], tc.want)
			}
			// Everything else the Env carried has to survive the patch.
			if tc.labels != nil {
				for k, v := range tc.labels {
					if k == agentsv1alpha1.LabelEnvPoolSizing {
						continue
					}
					if got.Labels[k] != v {
						t.Errorf("label %q = %q, want %q", k, got.Labels[k], v)
					}
				}
			}
		})
	}
}

// TestSyncRequiresQuota_MissingTemplate pins the not-found branch: nothing to
// converge towards is not an error, and it must not invent a stamp either.
func TestSyncRequiresQuota_MissingTemplate(t *testing.T) {
	scheme := newReconcileTestScheme(t)
	e := &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{Name: "env-a", Namespace: "ns-a"},
		Spec: agentsv1alpha1.SandboxEnvSpec{
			TemplateRef: agentsv1alpha1.SandboxEnvTemplateRef{Name: "gone"},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(e).Build()
	r := &SandboxEnvReconciler{
		Client: c, Scheme: scheme, LocalClusterID: testLocalCluster,
		QuotaProvider: quotatest.Billing(),
	}

	changed, err := r.syncRequiresQuota(context.Background(), e)
	if err != nil {
		t.Fatalf("syncRequiresQuota: %v", err)
	}
	if changed {
		t.Error("changed = true, want false")
	}
	got := &agentsv1alpha1.SandboxEnv{}
	key := types.NamespacedName{Namespace: e.Namespace, Name: e.Name}
	if err := c.Get(context.Background(), key, got); err != nil {
		t.Fatalf("get env: %v", err)
	}
	if _, ok := got.Labels[agentsv1alpha1.LabelEnvPoolSizing]; ok {
		t.Error("a missing template must not produce a stamp")
	}
}
