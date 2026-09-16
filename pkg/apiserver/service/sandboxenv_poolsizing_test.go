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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	quotaplugin "github.com/scitix/agent-sandbox/pkg/framework/providers/quota"
	"github.com/scitix/agent-sandbox/pkg/framework/providers/quota/quotatest"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

// newPoolSizingService builds an Env service over a fake client seeded with one
// template and an optional quota Provider. The template is what the stamp reads;
// the Provider is what decides whether it is billed.
func newPoolSizingService(
	t *testing.T, quotaProv quotaplugin.Provider, templateAnnotations map[string]string,
) (SandboxEnvService, client.Client) {
	t.Helper()
	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("client builder: %v", err)
	}
	tmpl := &agentsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: envTestTemplate, Annotations: templateAnnotations},
	}
	c := cb.WithObjects(tmpl).Build()
	return NewSandboxEnvService(c, nil, nil, quotaProv, nil, nil, VolumeConfig{}), c
}

func createEnvOnTemplate(t *testing.T, svc SandboxEnvService, name string, labels map[string]string) *gen.SandboxEnv {
	t.Helper()
	created, appErr := svc.Create(context.Background(), CreateSandboxEnvInput{
		Name:        name,
		Namespace:   envTestNamespace,
		Team:        "team-1",
		User:        "user-1",
		TemplateRef: agentsv1alpha1.SandboxEnvTemplateRef{Name: envTestTemplate},
		Mode:        agentsv1alpha1.SandboxEnvModeWarmPool,
		Labels:      labels,
	})
	if appErr != nil {
		t.Fatalf("create env: %+v", appErr)
	}
	return created
}

// TestCreateEnv_StampsPoolSizing is the create half of the contract: the Env
// carries its sizing state from the moment it exists, because the caller opens
// the Pool form on the Env this very response describes.
//
// The reconciler re-stamps later (that is what keeps it honest when a template
// changes), but a create response without the stamp would leave the console
// rendering the wrong form until the next reconcile.
func TestCreateEnv_StampsPoolSizing(t *testing.T) {
	tests := []struct {
		name       string
		provider   quotaplugin.Provider
		annotation map[string]string
		want       gen.PoolSizing
	}{
		{
			name:       "billed template",
			provider:   quotatest.Billing(),
			annotation: map[string]string{quotatest.BillingKey: `{"sci.c23-2":"1"}`},
			want:       gen.Billed,
		},
		{
			name:     "unbilled template under a billing provider",
			provider: quotatest.Billing(),
			want:     gen.FreeForm,
		},
		{
			name:       "no quota provider",
			provider:   nil,
			annotation: map[string]string{quotatest.BillingKey: `{"sci.c23-2":"1"}`},
			want:       gen.Either,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, c := newPoolSizingService(t, tc.provider, tc.annotation)
			created := createEnvOnTemplate(t, svc, "env-"+t.Name(), nil)

			if created.PoolSizing == nil {
				t.Fatalf("create response has no poolSizing, want %q", tc.want)
			}
			if *created.PoolSizing != tc.want {
				t.Errorf("create response poolSizing = %q, want %q", *created.PoolSizing, tc.want)
			}

			// The stamp is on the object, not only in the response: the
			// reconciler and every read path work off the label.
			live := &agentsv1alpha1.SandboxEnv{}
			key := types.NamespacedName{Namespace: envTestNamespace, Name: created.Name}
			if err := c.Get(context.Background(), key, live); err != nil {
				t.Fatalf("get env: %v", err)
			}
			if got := live.Labels[agentsv1alpha1.LabelEnvPoolSizing]; got != string(tc.want) {
				t.Errorf("%s = %q, want %q", agentsv1alpha1.LabelEnvPoolSizing, got, tc.want)
			}
		})
	}
}

// TestCreateEnv_PoolSizingIsServerOwned pins that the label is not something a
// caller gets to set: a body that claims "either" for a billed template must
// not change what the Env is, or the console would render the free-form form
// and the server would then refuse what it produced.
func TestCreateEnv_PoolSizingIsServerOwned(t *testing.T) {
	svc, c := newPoolSizingService(t, quotatest.Billing(),
		map[string]string{quotatest.BillingKey: `{"sci.c23-2":"1"}`})

	created := createEnvOnTemplate(t, svc, "env-owned", map[string]string{
		agentsv1alpha1.LabelEnvPoolSizing: string(gen.Either),
	})

	if created.PoolSizing == nil || *created.PoolSizing != gen.Billed {
		t.Errorf("poolSizing = %v, want billed", created.PoolSizing)
	}
	live := &agentsv1alpha1.SandboxEnv{}
	key := types.NamespacedName{Namespace: envTestNamespace, Name: created.Name}
	if err := c.Get(context.Background(), key, live); err != nil {
		t.Fatalf("get env: %v", err)
	}
	if got := live.Labels[agentsv1alpha1.LabelEnvPoolSizing]; got != string(gen.Billed) {
		t.Errorf("%s = %q, want billed (a caller-supplied value must be overwritten)",
			agentsv1alpha1.LabelEnvPoolSizing, got)
	}
}

// TestGetEnv_ProjectsPoolSizing covers the read side, including the one case
// that must NOT be defaulted: an Env the reconciler has not stamped yet omits
// the field entirely, so a client can fall back to the behaviour that predates
// it rather than being told something wrong.
func TestGetEnv_ProjectsPoolSizing(t *testing.T) {
	stamped := newEnv(envTestName, "team-1", "user-1")
	stamped.Labels[agentsv1alpha1.LabelEnvPoolSizing] = string(gen.FreeForm)
	unstamped := newEnv("env-unstamped", "team-1", "user-1")

	svc := newEnvService(t, stamped, unstamped)
	ctx := context.Background()

	got, appErr := svc.Get(ctx, envTestNamespace, envTestName)
	if appErr != nil {
		t.Fatalf("get stamped: %+v", appErr)
	}
	if got.PoolSizing == nil || *got.PoolSizing != gen.FreeForm {
		t.Errorf("stamped poolSizing = %v, want free-form", got.PoolSizing)
	}

	got, appErr = svc.Get(ctx, envTestNamespace, "env-unstamped")
	if appErr != nil {
		t.Fatalf("get unstamped: %+v", appErr)
	}
	if got.PoolSizing != nil {
		t.Errorf("unstamped poolSizing = %v, want omitted", *got.PoolSizing)
	}
}

// TestListEnvs_ProjectsPoolSizing keeps the list shape in step with the detail
// shape: `abx envs` is where a caller finds out which envs need a quota before
// asking for one of them.
func TestListEnvs_ProjectsPoolSizing(t *testing.T) {
	billed := newEnv(envTestName, "team-1", "user-1")
	billed.Labels[agentsv1alpha1.LabelEnvPoolSizing] = string(gen.Billed)

	svc := newEnvService(t, billed)
	items, appErr := svc.List(context.Background(), envTestNamespace, "team-1", "user-1")
	if appErr != nil {
		t.Fatalf("list: %+v", appErr)
	}
	if len(items) != 1 || items[0].PoolSizing == nil || *items[0].PoolSizing != gen.Billed {
		t.Fatalf("list poolSizing = %v, want one billed env", items)
	}
}
