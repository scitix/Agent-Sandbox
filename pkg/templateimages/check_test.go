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

package templateimages

import (
	"context"
	"testing"
	"time"

	api "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/sandboxrender"
	"github.com/scitix/agent-sandbox/pkg/utils/imageresolver"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type registry struct{ target string }

func (r *registry) LookupRegistry(string) (string, string, bool)  { return "other", "", true }
func (r *registry) RegistryForType(string, string) (string, bool) { return r.target, true }

type checker struct {
	calls int
	err   error
}

func (c *checker) CheckAvailability(context.Context, string, ...imageresolver.ResolveOption) error {
	c.calls++
	return c.err
}
func TestPlanInvalidationAndSuccessWindow(t *testing.T) {
	store := &registry{target: "local.example.com"}
	rewrite := &sandboxrender.RegistryRewrite{LocalClusterID: "local", Store: store}
	tmpl := &api.SandboxTemplate{}
	tmpl.Spec.IdleImage = "remote.example.com/repo:v1"
	tmpl.Spec.Template.Spec.Containers = []corev1.Container{{Name: "main", Image: tmpl.Spec.IdleImage}}
	plan := Build(tmpl, rewrite, "")
	probe := &checker{}
	result := Check(context.Background(), probe, plan, "local")
	if probe.calls != 1 {
		t.Fatalf("duplicate image was probed %d times", probe.calls)
	}
	now := time.Now()
	result.CheckedAt = metav1.NewTime(now.Add(-47 * time.Hour))
	if !FreshSuccess(result, plan, "local", now) {
		t.Fatal("47h success rejected")
	}
	if FreshSuccess(result, plan, "local", now.Add(time.Hour)) {
		t.Fatal("48h success accepted")
	}
	if FreshSuccess(result, plan, "other", now) {
		t.Fatal("other cluster accepted")
	}
	if Build(tmpl, rewrite, "remote.example.com/override:v1").Hash == plan.Hash {
		t.Fatal("override did not invalidate")
	}
	store.target = "new.example.com"
	if Build(tmpl, rewrite, "").Hash == plan.Hash {
		t.Fatal("mapping did not invalidate")
	}
	probe.err = &imageresolver.AvailabilityError{Reason: "Missing"}
	result = Check(context.Background(), probe, plan, "local")
	if FreshSuccess(result, plan, "local", time.Now()) {
		t.Fatal("failure accepted")
	}
	if delta := result.NextCheckAt.Sub(result.CheckedAt.Time); delta != FailureRetry {
		t.Fatalf("failure retry %v", delta)
	}
}
