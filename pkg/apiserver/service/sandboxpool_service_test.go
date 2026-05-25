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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

// SandboxPoolService is read-only now: write operations moved to the
// env-scoped surface on SandboxEnvService (AddMemberPool / UpdateMemberPool /
// DeleteMemberPool). These tests cover List and Get only.

func newTestSandboxPoolService(t *testing.T, objs ...any) SandboxPoolService {
	t.Helper()
	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("get fake client builder: %v", err)
	}
	for _, o := range objs {
		switch v := o.(type) {
		case *agentsv1alpha1.SandboxPool:
			cb = cb.WithObjects(v)
		case *corev1.Pod:
			cb = cb.WithObjects(v)
		case *agentsv1alpha1.SandboxTemplate:
			cb = cb.WithObjects(v)
		}
	}
	cli := cb.Build()
	return NewSandboxPoolService(cli, nil, nil)
}

func makePoolObj(name string, replicas int32) *agentsv1alpha1.SandboxPool {
	return &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			Replicas: replicas,
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "pause:3.10",
				Template: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "sandbox", Image: "busybox:1.36"}},
					},
				},
			},
		},
	}
}

func TestSandboxPoolService_List(t *testing.T) {
	svc := newTestSandboxPoolService(t, makePoolObj("pool-a", 1), makePoolObj("pool-b", 2))

	items, appErr := svc.List(context.Background(), "default", "", "")
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Name != "pool-a" || items[1].Name != "pool-b" {
		t.Fatalf("expected sorted by name, got %s, %s", items[0].Name, items[1].Name)
	}
}

func TestSandboxPoolService_List_FilteredByTeamUser(t *testing.T) {
	p1 := makePoolObj("p1", 1)
	p1.Labels = map[string]string{
		agentsv1alpha1.LabelTeam: "team-x",
		agentsv1alpha1.LabelUser: "alice",
	}
	p2 := makePoolObj("p2", 1)
	p2.Labels = map[string]string{
		agentsv1alpha1.LabelTeam: "team-y",
		agentsv1alpha1.LabelUser: "bob",
	}
	svc := newTestSandboxPoolService(t, p1, p2)

	items, appErr := svc.List(context.Background(), "default", "team-x", "alice")
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if len(items) != 1 || items[0].Name != "p1" {
		t.Fatalf("expected only p1, got %+v", items)
	}
}

func TestSandboxPoolService_Get(t *testing.T) {
	svc := newTestSandboxPoolService(t, makePoolObj("pool-a", 3))

	result, appErr := svc.Get(context.Background(), "default", "pool-a")
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if result.Name != "pool-a" || result.Spec.Replicas != 3 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestSandboxPoolService_Get_NotFound(t *testing.T) {
	svc := newTestSandboxPoolService(t)

	_, appErr := svc.Get(context.Background(), "default", "nonexistent")
	if appErr == nil {
		t.Fatal("expected error, got nil")
	}
	if appErr.Code != domain.ErrCodeNotFound {
		t.Fatalf("expected ErrCodeNotFound, got %d", appErr.Code)
	}
}

func TestSandboxPoolService_Get_ProjectsOwningEnv(t *testing.T) {
	pool := makePoolObj("p-owned", 1)
	pool.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: agentsv1alpha1.GroupVersion.Group + "/v1alpha1",
		Kind:       agentsv1alpha1.SandboxEnvOwnerKind,
		Name:       "my-env",
	}}
	svc := newTestSandboxPoolService(t, pool)

	result, appErr := svc.Get(context.Background(), "default", "p-owned")
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if result.OwningEnv == nil || *result.OwningEnv != "my-env" {
		t.Fatalf("expected owningEnv=my-env, got %v", result.OwningEnv)
	}
}

var _ client.Client // keep import referenced when ConfMap helpers grow
