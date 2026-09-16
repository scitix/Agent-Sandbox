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

package envmember_test

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service/envmember"
	"github.com/scitix/agent-sandbox/pkg/framework/providers/quota/quotatest"
)

// newBilledTestTemplate is newTestTemplate with the billing annotation the
// Scitix quota Provider keys on — the template a normal user is expected to
// pick, and the one whose Pools must name a quota and an instance type.
func newBilledTestTemplate() *agentsv1alpha1.SandboxTemplate {
	tmpl := newTestTemplate()
	tmpl.Annotations = map[string]string{quotatest.BillingKey: `{"sci.c23-2":"1"}`}
	return tmpl
}

// newBilledService wires the member-Pool service the way a closed-source
// deployment does: a quota Provider that bills annotated templates, plus the
// instance type catalog a billed Pool is sized from.
func newBilledService(t *testing.T, objs ...client.Object) envmember.MemberPoolService {
	t.Helper()
	objs = append(objs, newBilledTestTemplate())
	return envmember.New(newClient(t, objs...), nil, newFakeInstProvider(), quotatest.Billing())
}

// billedMember is the body a billed Env accepts: the quota it spends and the
// instance its units are counted in.
func billedMember() agentsv1alpha1.EnvClusterMember {
	return agentsv1alpha1.EnvClusterMember{
		Spec: agentsv1alpha1.SandboxPoolSpec{Replicas: 1},
		Config: agentsv1alpha1.EnvClusterMemberConfig{
			InstanceType: "sci.c23-2",
			Multiplier:   1,
			Labels:       map[string]string{envmember.QuotaURLLabel: "alice.42.team-a.ondemand"},
		},
	}
}

// TestAdd_BilledEnvRequiresQuotaAndInstanceType is the core rule: on an Env
// whose template is billed, neither half of the pair may be left out.
//
// Both halves are checked separately because each one is a different mistake
// with a different fix — naming no quota means the Pool has nothing to spend,
// naming no instance type means the quota has no unit to be counted in — and a
// single "invalid sizing" error would leave the caller guessing which one.
func TestAdd_BilledEnvRequiresQuotaAndInstanceType(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(m *agentsv1alpha1.EnvClusterMember)
		wantSubstr string
	}{
		{
			name:       "no quota label",
			mutate:     func(m *agentsv1alpha1.EnvClusterMember) { m.Config.Labels = nil },
			wantSubstr: envmember.QuotaURLLabel,
		},
		{
			name: "empty quota label",
			mutate: func(m *agentsv1alpha1.EnvClusterMember) {
				m.Config.Labels = map[string]string{envmember.QuotaURLLabel: ""}
			},
			wantSubstr: envmember.QuotaURLLabel,
		},
		{
			name: "no instance type",
			mutate: func(m *agentsv1alpha1.EnvClusterMember) {
				m.Config.InstanceType = ""
				m.Config.InlineResources = &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				}
			},
			wantSubstr: "instanceType",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := newBilledService(t, newEnvForPoolOps())
			member := billedMember()
			tc.mutate(&member)

			_, err := svc.AddMember(context.Background(), envTestNamespace, testEnvName, envLocalCluster, member)
			if err == nil {
				t.Fatal("expected BadRequest, got success")
			}
			if err.Code != domain.ErrCodeBadRequest {
				t.Fatalf("code = %v, want BadRequest (%+v)", err.Code, err)
			}
			if !strings.Contains(err.Message, tc.wantSubstr) {
				t.Errorf("message %q should name %q", err.Message, tc.wantSubstr)
			}
		})
	}
}

// TestAdd_BilledEnvAcceptsQuotaAndInstanceType is the other half of the rule:
// the shape the console and the CLI now produce has to go through, name and
// all (the pool name is derived from the effective request, so a rejected
// validation would also have been visible as a missing quota suffix).
func TestAdd_BilledEnvAcceptsQuotaAndInstanceType(t *testing.T) {
	svc := newBilledService(t, newEnvForPoolOps())

	res, err := svc.AddMember(context.Background(), envTestNamespace, testEnvName, envLocalCluster, billedMember())
	if err != nil {
		t.Fatalf("Add: %+v", err)
	}
	if res.Name != "env-x-1c16gi" {
		t.Errorf("derived name = %q, want env-x-1c16gi", res.Name)
	}
}

// TestAdd_FreeFormEnvRejectsBilledFields pins the direction that is easy to
// leave permissive: a template reserved for a handful of callers is not billed,
// so an instance type on one of its Pools would claim a reservation nobody
// made, and a quota label would name a quota that is never charged.
func TestAdd_FreeFormEnvRejectsBilledFields(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(m *agentsv1alpha1.EnvClusterMember)
		wantSubstr string
	}{
		{
			name:       "instance type",
			mutate:     func(m *agentsv1alpha1.EnvClusterMember) { m.Config.Labels = nil },
			wantSubstr: "instanceType",
		},
		{
			name:       "quota label",
			mutate:     func(m *agentsv1alpha1.EnvClusterMember) { m.Config.InstanceType = "" },
			wantSubstr: envmember.QuotaURLLabel,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// quotatest.Billing is wired in but the template carries no
			// annotation: the check must follow the Provider's ANSWER, not the
			// fact that a billing-capable Provider is installed.
			env := newEnvForPoolOps()
			svc := envmember.New(newClient(t, env, newTestTemplate()), nil, nil, quotatest.Billing())

			member := billedMember()
			member.Config.InlineResources = &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
			}
			tc.mutate(&member)

			_, err := svc.AddMember(context.Background(), envTestNamespace, testEnvName, envLocalCluster, member)
			if err == nil {
				t.Fatal("expected BadRequest, got success")
			}
			if err.Code != domain.ErrCodeBadRequest {
				t.Fatalf("code = %v, want BadRequest (%+v)", err.Code, err)
			}
			if !strings.Contains(err.Message, tc.wantSubstr) {
				t.Errorf("message %q should name %q", err.Message, tc.wantSubstr)
			}
		})
	}
}

// TestAdd_FreeFormEnvAcceptsInlineResources is the control: the free-form
// template still takes the shape it always took, even with a billing-capable
// Provider installed.
func TestAdd_FreeFormEnvAcceptsInlineResources(t *testing.T) {
	env := newEnvForPoolOps()
	svc := envmember.New(newClient(t, env, newTestTemplate()), nil, nil, quotatest.Billing())

	res, err := svc.AddMember(context.Background(), envTestNamespace, testEnvName, envLocalCluster, memberWithResources(1))
	if err != nil {
		t.Fatalf("Add: %+v", err)
	}
	if res.Name != memberName2c8Gi {
		t.Errorf("derived name = %q, want env-x-2c8gi", res.Name)
	}
}

// TestAdd_MissingTemplateIsReportedAsMissing guards the one branch where the
// billing check deliberately answers nothing: a template that does not exist
// cannot be billed, and the caller should be told the template is missing
// rather than be handed a sizing complaint about a template nobody can read.
func TestAdd_MissingTemplateIsReportedAsMissing(t *testing.T) {
	svc := envmember.New(newClient(t, newEnvForPoolOps()), nil, nil, quotatest.Billing())

	_, err := svc.AddMember(context.Background(), envTestNamespace, testEnvName, envLocalCluster, memberWithResources(1))
	if err == nil {
		t.Fatal("expected NotFound, got success")
	}
	if err.Code != domain.ErrCodeNotFound {
		t.Fatalf("code = %v, want NotFound (%+v)", err.Code, err)
	}
	if !strings.Contains(err.Message, testTemplateName) {
		t.Errorf("message %q should name the missing template", err.Message)
	}
}

// TestUpdateMember_SkipsBillingCheck pins the deliberate asymmetry: the shape
// is fixed at create, so an update cannot introduce a wrong one — and a member
// that predates a template gaining its billing annotation must still be
// editable. Re-checking here would turn a re-annotated template into a Pool
// nobody can resize.
func TestUpdateMember_SkipsBillingCheck(t *testing.T) {
	// A member created before the template was billed: no quota, no instance
	// type, inlineResources only.
	env := newEnvForPoolOps()
	legacy := memberWithResources(1)
	legacy.Name = memberName2c8Gi
	legacy.Config.ScalingGroup = "2c8gi"
	env.Spec.Clusters[0].Members = []agentsv1alpha1.EnvClusterMember{legacy}

	svc := newBilledService(t, env)
	min := int32(2)
	// The shape travels back with the update, as every real client sends it —
	// unchanged, because it is fixed at create.
	inline := legacy.Config.InlineResources.DeepCopy()
	res, err := svc.UpdateMember(context.Background(), envTestNamespace, testEnvName, memberName2c8Gi, envLocalCluster,
		envmember.MemberPoolPatch{MinReplicas: &min, InlineResources: inline})
	if err != nil {
		t.Fatalf("Update: %+v", err)
	}
	if res.Name != memberName2c8Gi {
		t.Errorf("name = %q, want env-x-2c8gi", res.Name)
	}
}

// TestAdd_BilledEnvUsesTheEnvsTemplateNotTheCallers is a small guard on what
// the check reads: the template named by the Env, never anything the caller
// supplied. A caller cannot opt out of billing by naming a template in the
// body — the body has no template field at all, which is the point.
func TestAdd_BilledEnvUsesTheEnvsTemplateNotTheCallers(t *testing.T) {
	otherFree := &agentsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "some-other-template"},
	}
	svc := newBilledService(t, newEnvForPoolOps(), otherFree)

	// The free template exists in the cluster; the Env is bound to the billed
	// one, so free-form sizing is refused all the same.
	_, err := svc.AddMember(context.Background(), envTestNamespace, testEnvName, envLocalCluster, memberWithResources(1))
	if err == nil {
		t.Fatal("expected BadRequest, got success")
	}
	if err.Code != domain.ErrCodeBadRequest {
		t.Fatalf("code = %v, want BadRequest (%+v)", err.Code, err)
	}
}
