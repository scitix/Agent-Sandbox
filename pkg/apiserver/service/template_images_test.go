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

package service

import (
	"context"
	"testing"
	"time"

	api "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/sandboxrender"
	"github.com/scitix/agent-sandbox/pkg/templateimages"
	"github.com/scitix/agent-sandbox/pkg/utils/imageresolver"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type imageCheckRegistry struct{}

func (imageCheckRegistry) LookupRegistry(string) (string, string, bool) { return "remote", "", true }
func (imageCheckRegistry) RegistryForType(string, string) (string, bool) {
	return "local.example.com", true
}

type imageProbe struct {
	calls  int
	reason string
}

func (p *imageProbe) CheckAvailability(context.Context, string, ...imageresolver.ResolveOption) error {
	p.calls++
	if p.reason != "" {
		return &imageresolver.AvailabilityError{Reason: p.reason}
	}
	return nil
}
func TestEnvImagePreflightCacheAndFailures(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = api.AddToScheme(scheme)
	registry := &sandboxrender.RegistryRewrite{LocalClusterID: "local", Store: imageCheckRegistry{}}
	template := &api.SandboxTemplate{ObjectMeta: metav1.ObjectMeta{Name: "template", Annotations: map[string]string{api.RegistryRewriteAnnotationKey: "true"}}}
	template.Spec.Template.Spec.Containers = []corev1.Container{{Name: "sandbox", Image: "remote.example.com/main:v1"}}
	plan := templateimages.Build(template, registry, "")
	template.Status.ImageAvailability = templateimages.Check(context.Background(), &imageProbe{}, plan, "local")
	for _, tc := range []struct {
		name, reason string
		age          time.Duration
		override     string
		calls        int
		code         domain.ErrorCode
	}{
		{name: "fresh", age: 47 * time.Hour, calls: 0},
		{name: "expired", age: 49 * time.Hour, calls: 1},
		{name: "missing", age: 49 * time.Hour, reason: "Missing", calls: 1, code: 422},
		{name: "unauthorized", age: 49 * time.Hour, reason: "Unauthorized", calls: 1, code: 503},
		{name: "override", age: time.Hour, override: "remote.example.com/other:v1", reason: "Missing", calls: 1, code: 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmpl := template.DeepCopy()
			tmpl.Status.ImageAvailability.CheckedAt = metav1.NewTime(time.Now().Add(-tc.age))
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tmpl).Build()
			probe := &imageProbe{reason: tc.reason}
			s := &k8sSandboxEnvService{client: c, imageRegistry: registry, imageChecker: probe}
			input := CreateSandboxEnvInput{TemplateRef: api.SandboxEnvTemplateRef{Name: "template"}}
			if tc.override != "" {
				input.Overrides = &api.EnvOverridesSpec{Image: tc.override}
			}
			err := s.preflightTemplateImages(context.Background(), input)
			if probe.calls != tc.calls {
				t.Fatalf("calls %d want %d", probe.calls, tc.calls)
			}
			if tc.code == 0 && err != nil {
				t.Fatal(err)
			}
			if tc.code != 0 && (err == nil || err.Code != tc.code) {
				t.Fatalf("error %v want HTTP %d", err, tc.code)
			}
		})
	}
}

func TestGlobalTemplateSyncPreservesLocalImageStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = api.AddToScheme(scheme)
	existing := &api.SandboxTemplate{ObjectMeta: metav1.ObjectMeta{Name: "sync"}, Status: api.SandboxTemplateStatus{ImageAvailability: &api.TemplateImageCheck{ClusterID: "local", InputHash: "local-hash"}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&api.SandboxTemplate{}).WithObjects(existing).Build()
	incoming := existing.DeepCopy()
	incoming.Status.ImageAvailability = &api.TemplateImageCheck{ClusterID: "hub", InputHash: "hub-hash"}
	incoming.Spec.Version = "v2"
	svc := NewSandboxTemplateService(c, nil)
	if err := svc.CreateOrUpdate(context.Background(), incoming); err != nil {
		t.Fatal(err)
	}
	got := &api.SandboxTemplate{}
	if err := c.Get(context.Background(), client.ObjectKey{Name: "sync"}, got); err != nil {
		t.Fatal(err)
	}
	if got.Status.ImageAvailability.ClusterID != "local" {
		t.Fatal("global sync replaced worker status")
	}
}
