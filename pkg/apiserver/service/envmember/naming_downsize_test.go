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
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service/envmember"
	"github.com/scitix/agent-sandbox/pkg/framework/plugins"
	instancetypeplugin "github.com/scitix/agent-sandbox/pkg/framework/providers/instancetype"
)

// fakeInstProvider is a minimal enabled InstanceType catalog for the
// downsize/fits-within tests. Resolve scales BaseResources element-wise by the
// multiplier (cpu at milli precision, everything else at unit precision).
type fakeInstProvider struct {
	catalog map[string]*instancetypeplugin.InstanceType
}

func (f fakeInstProvider) Enabled() bool { return true }

func (f fakeInstProvider) Get(name string) (*instancetypeplugin.InstanceType, bool) {
	it, ok := f.catalog[name]
	return it, ok
}

func (f fakeInstProvider) List() []*instancetypeplugin.InstanceType {
	out := make([]*instancetypeplugin.InstanceType, 0, len(f.catalog))
	for _, it := range f.catalog {
		out = append(out, it)
	}
	return out
}

func (f fakeInstProvider) Resolve(_ context.Context, name string, mult int32) (corev1.ResourceRequirements, *domain.AppError) {
	it, ok := f.catalog[name]
	if !ok {
		return corev1.ResourceRequirements{}, domain.NewNotFound("instance type not found: " + name)
	}
	if mult < 1 {
		return corev1.ResourceRequirements{}, domain.NewBadRequest("multiplier must be >= 1")
	}
	scale := func(rl corev1.ResourceList) corev1.ResourceList {
		if len(rl) == 0 {
			return nil
		}
		out := corev1.ResourceList{}
		for k, v := range rl {
			if k == corev1.ResourceCPU {
				out[k] = *resource.NewMilliQuantity(v.MilliValue()*int64(mult), v.Format)
			} else {
				out[k] = *resource.NewQuantity(v.Value()*int64(mult), v.Format)
			}
		}
		return out
	}
	return corev1.ResourceRequirements{
		Requests: scale(it.BaseResources.Requests),
		Limits:   scale(it.BaseResources.Limits),
	}, nil
}

func (f fakeInstProvider) ResolveByResources(_ context.Context, _ corev1.ResourceRequirements) (*instancetypeplugin.InstanceType, int32, *domain.AppError) {
	return nil, 0, nil
}

func (f fakeInstProvider) DeriveScalingGroupName(observed corev1.ResourceRequirements) string {
	return instancetypeplugin.DeriveResourceKey(observed)
}

// c23_2 mirrors the 1c16Gi CPU-only shape from the design examples.
func newFakeInstProvider() fakeInstProvider {
	return fakeInstProvider{catalog: map[string]*instancetypeplugin.InstanceType{
		"sci.c23-2": {
			Name: "sci.c23-2",
			BaseResources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("16Gi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("16Gi"),
				},
			},
			MaxMultiplier: 8,
		},
	}}
}

func instanceMember(mult int32, req corev1.ResourceList) agentsv1alpha1.EnvClusterMember {
	m := agentsv1alpha1.EnvClusterMember{
		Spec: agentsv1alpha1.SandboxPoolSpec{Replicas: 1},
		Config: agentsv1alpha1.EnvClusterMemberConfig{
			InstanceType: "sci.c23-2",
			Multiplier:   mult,
		},
	}
	if req != nil {
		m.Config.InlineResources = &corev1.ResourceRequirements{Requests: req, Limits: req.DeepCopy()}
	}
	return m
}

// TestAdd_Downsize_FitsWithin verifies a rounded-down request (1c4Gi within a
// 1c16Gi instance) is accepted, the Pod carries the smaller request, and the
// reservation-replica-quota annotation carries the whole-instance multiplier.
func TestAdd_Downsize_FitsWithin(t *testing.T) {
	pl := &capturingPlugin{}
	cli := newClient(t, newEnvForPoolOps(), newTestTemplate())
	svc := envmember.New(cli, plugins.NewPluginManager(pl), newFakeInstProvider(), nil)

	member := instanceMember(1, corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("1"),
		corev1.ResourceMemory: resource.MustParse("4Gi"),
	})
	res, err := svc.AddMember(context.Background(), envTestNamespace, testEnvName, envLocalCluster, member)
	if err != nil {
		t.Fatalf("Add: %+v", err)
	}
	if pl.lastCreate == nil {
		t.Fatalf("PreCreatePool never called")
	}
	cand := pl.lastCreate

	// Pod request is the rounded-down value, not the envelope.
	got := cand.Spec.Template.Spec.Containers[0].Resources
	if got.Requests.Cpu().Cmp(resource.MustParse("1")) != 0 {
		t.Errorf("Pod cpu = %v, want 1", got.Requests.Cpu())
	}
	if got.Requests.Memory().Cmp(resource.MustParse("4Gi")) != 0 {
		t.Errorf("Pod memory = %v, want 4Gi (downsized)", got.Requests.Memory())
	}

	// Reservation annotation carries the whole-instance multiplier.
	if v := cand.Annotations[agentsv1alpha1.AnnotationReservationReplicaQuota]; v != `{"sci.c23-2":"1"}` {
		t.Errorf("reservation-replica-quota = %q, want {\"sci.c23-2\":\"1\"}", v)
	}

	// Grouping is by the envelope shape (1c16Gi), not the downsized request.
	if res.Name != "env-x-1c16gi" {
		t.Errorf("pool name = %q, want env-x-1c16gi (grouped by envelope)", res.Name)
	}
}

// TestAdd_Downsize_RejectsExceedingDimension verifies a request that exceeds
// the envelope in any dimension is rejected (1c16Gi instance cannot run 2c4G).
func TestAdd_Downsize_RejectsExceedingDimension(t *testing.T) {
	cli := newClient(t, newEnvForPoolOps(), newTestTemplate())
	svc := envmember.New(cli, nil, newFakeInstProvider(), nil)

	member := instanceMember(1, corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("2"), // > 1 × instance cpu
		corev1.ResourceMemory: resource.MustParse("4Gi"),
	})
	_, err := svc.AddMember(context.Background(), envTestNamespace, testEnvName, envLocalCluster, member)
	if err == nil || err.Code != domain.ErrCodeBadRequest {
		t.Fatalf("expected BadRequest for cpu overflow, got %+v", err)
	}
}

// TestAdd_Downsize_MultiplierEnvelope verifies the reproduction of the original
// failure: a 2Gi request against a ~1.6Gi-ish instance is fine at multiplier=2
// (envelope 32Gi), and the annotation carries multiplier 2.
func TestAdd_Downsize_MultiplierEnvelope(t *testing.T) {
	pl := &capturingPlugin{}
	cli := newClient(t, newEnvForPoolOps(), newTestTemplate())
	svc := envmember.New(cli, plugins.NewPluginManager(pl), newFakeInstProvider(), nil)

	// Envelope = 2 × (1c16Gi) = 2c32Gi; request 2c2Gi fits.
	member := instanceMember(2, corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("2"),
		corev1.ResourceMemory: resource.MustParse("2Gi"),
	})
	if _, err := svc.AddMember(context.Background(), envTestNamespace, testEnvName, envLocalCluster, member); err != nil {
		t.Fatalf("Add: %+v", err)
	}
	if v := pl.lastCreate.Annotations[agentsv1alpha1.AnnotationReservationReplicaQuota]; v != `{"sci.c23-2":"2"}` {
		t.Errorf("reservation-replica-quota = %q, want {\"sci.c23-2\":\"2\"}", v)
	}
}

// TestAdd_InstanceType_NoDownsize keeps the existing behavior: without
// inlineResources the Pod is sized to the full envelope.
func TestAdd_InstanceType_NoDownsize(t *testing.T) {
	pl := &capturingPlugin{}
	cli := newClient(t, newEnvForPoolOps(), newTestTemplate())
	svc := envmember.New(cli, plugins.NewPluginManager(pl), newFakeInstProvider(), nil)

	member := instanceMember(1, nil)
	if _, err := svc.AddMember(context.Background(), envTestNamespace, testEnvName, envLocalCluster, member); err != nil {
		t.Fatalf("Add: %+v", err)
	}
	got := pl.lastCreate.Spec.Template.Spec.Containers[0].Resources
	if got.Requests.Memory().Cmp(resource.MustParse("16Gi")) != 0 {
		t.Errorf("Pod memory = %v, want 16Gi (full envelope)", got.Requests.Memory())
	}
}
