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

package sandboxtemplate

import (
	"context"
	"testing"
	"time"

	api "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/sandboxrender"
	"github.com/scitix/agent-sandbox/pkg/templateimages"
	"github.com/scitix/agent-sandbox/pkg/utils/imageresolver"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type probeRegistry struct{}

func (probeRegistry) LookupRegistry(string) (string, string, bool)  { return "remote", "", true }
func (probeRegistry) RegistryForType(string, string) (string, bool) { return "local.example.com", true }

type probe struct {
	calls  int
	reason string
}

func (p *probe) CheckAvailability(context.Context, string, ...imageresolver.ResolveOption) error {
	p.calls++
	if p.reason != "" {
		return &imageresolver.AvailabilityError{Reason: p.reason}
	}
	return nil
}
func TestAvailabilityReconcilePersistsAndReusesStatus(t *testing.T) {
	for _, reason := range []string{"", "Missing", "Unauthorized"} {
		t.Run(reason, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = api.AddToScheme(scheme)
			tmpl := &api.SandboxTemplate{ObjectMeta: metav1.ObjectMeta{Name: "test", Annotations: map[string]string{api.RegistryRewriteAnnotationKey: "true"}}}
			tmpl.Spec.Template.Spec.Containers = []corev1.Container{{Name: "main", Image: "remote.example.com/main:v1"}}
			c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&api.SandboxTemplate{}).WithObjects(tmpl).Build()
			p := &probe{reason: reason}
			r := &SandboxTemplateReconciler{Client: c, Registry: &sandboxrender.RegistryRewrite{LocalClusterID: "local", Store: probeRegistry{}}, Checker: p}
			req := ctrl.Request{NamespacedName: client.ObjectKey{Name: "test"}}
			if _, err := r.Reconcile(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			got := &api.SandboxTemplate{}
			if err := c.Get(context.Background(), req.NamespacedName, got); err != nil {
				t.Fatal(err)
			}
			result := got.Status.ImageAvailability
			if result == nil || result.ClusterID != "local" || len(got.Status.Conditions) != 1 {
				t.Fatalf("missing local status: %+v", got.Status)
			}
			interval := templateimages.RefreshInterval
			status := metav1.ConditionTrue
			if reason != "" {
				interval = templateimages.FailureRetry
				status = metav1.ConditionUnknown
			}
			if reason == "Missing" {
				status = metav1.ConditionFalse
			}
			if got.Status.Conditions[0].Status != status {
				t.Fatal("incorrect condition status")
			}
			if delta := result.NextCheckAt.Sub(result.CheckedAt.Time); delta != interval {
				t.Fatalf("interval %v want %v", delta, interval)
			}
			if _, err := r.Reconcile(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			if p.calls != 1 {
				t.Fatal("fresh persisted result was probed again")
			}
			before := got.DeepCopy()
			got.Status.ImageAvailability.NextCheckAt = metav1.NewTime(time.Now().Add(-time.Hour))
			if err := c.Status().Patch(context.Background(), got, client.MergeFrom(before)); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Reconcile(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			if p.calls != 2 {
				t.Fatal("due result not refreshed")
			}
		})
	}
}
