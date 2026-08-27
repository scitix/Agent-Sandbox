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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

// pvc builds a claim with the knobs the projection reads.
func pvc(name, ns string, phase corev1.PersistentVolumeClaimPhase, labels map[string]string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
			StorageClassName: ptr.To("example-shared-fs"),
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("5Ti")},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase:    phase,
			Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("5Ti")},
		},
	}
}

func newVolumeSvc(t *testing.T, cfg VolumeConfig, objs ...*corev1.PersistentVolumeClaim) VolumeService {
	t.Helper()
	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("client builder: %v", err)
	}
	for _, o := range objs {
		cb = cb.WithObjects(o)
	}
	return NewVolumeService(cb.Build(), cfg)
}

func TestVolumeService_OnlyBoundClaims(t *testing.T) {
	svc := newVolumeSvc(t, VolumeConfig{Enabled: true},
		pvc("bound-a", envTestNamespace, corev1.ClaimBound, nil),
		pvc("pending-b", envTestNamespace, corev1.ClaimPending, nil),
		pvc("lost-c", envTestNamespace, corev1.ClaimLost, nil),
	)
	items, err := svc.List(context.Background(), envTestNamespace)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].ClaimName != "bound-a" {
		t.Fatalf("only Bound claims are mountable; got %+v", items)
	}
}

// TestVolumeService_NamespaceScoped is the authorisation boundary: the service
// only ever sees the namespace it was handed, and a claim elsewhere is invisible.
func TestVolumeService_NamespaceScoped(t *testing.T) {
	svc := newVolumeSvc(t, VolumeConfig{Enabled: true},
		pvc("mine", envTestNamespace, corev1.ClaimBound, nil),
		pvc("theirs", "someone-else", corev1.ClaimBound, nil),
	)
	items, err := svc.List(context.Background(), envTestNamespace)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].ClaimName != "mine" {
		t.Fatalf("claims from other namespaces must not be listed; got %+v", items)
	}
}

func TestVolumeService_EmptyNamespaceListsNothing(t *testing.T) {
	svc := newVolumeSvc(t, VolumeConfig{Enabled: true},
		pvc("mine", envTestNamespace, corev1.ClaimBound, nil))
	items, err := svc.List(context.Background(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("an unresolved namespace must list nothing, got %+v", items)
	}
}

func TestVolumeService_DisabledListsNothing(t *testing.T) {
	svc := newVolumeSvc(t, VolumeConfig{Enabled: false},
		pvc("mine", envTestNamespace, corev1.ClaimBound, nil))
	items, err := svc.List(context.Background(), envTestNamespace)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("while the feature is off nothing is mountable, got %+v", items)
	}
}

// TestVolumeService_DisplayName covers the three cases the fallback exists for:
// a configured label present, configured labels all absent, and no configuration
// at all. Getting this wrong shows users an opaque claim id.
func TestVolumeService_DisplayName(t *testing.T) {
	const nameLabel = "example.com/volume-name"
	const altLabel = "example.com/legacy-name"

	cases := []struct {
		name   string
		cfg    VolumeConfig
		labels map[string]string
		want   string
	}{
		{
			name:   "first configured label wins",
			cfg:    VolumeConfig{Enabled: true, DisplayNameLabels: []string{nameLabel, altLabel}},
			labels: map[string]string{nameLabel: "zystore", altLabel: "old-name"},
			want:   "zystore",
		},
		{
			name:   "falls through to the next label",
			cfg:    VolumeConfig{Enabled: true, DisplayNameLabels: []string{nameLabel, altLabel}},
			labels: map[string]string{altLabel: "old-name"},
			want:   "old-name",
		},
		{
			name:   "empty label value is skipped",
			cfg:    VolumeConfig{Enabled: true, DisplayNameLabels: []string{nameLabel, altLabel}},
			labels: map[string]string{nameLabel: "", altLabel: "old-name"},
			want:   "old-name",
		},
		{
			name:   "no matching label falls back to the claim name",
			cfg:    VolumeConfig{Enabled: true, DisplayNameLabels: []string{nameLabel}},
			labels: map[string]string{"unrelated": "x"},
			want:   "team-user-41",
		},
		{
			name:   "no configuration falls back to the claim name",
			cfg:    VolumeConfig{Enabled: true},
			labels: map[string]string{nameLabel: "zystore"},
			want:   "team-user-41",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newVolumeSvc(t, tc.cfg,
				pvc("team-user-41", envTestNamespace, corev1.ClaimBound, tc.labels))
			items, err := svc.List(context.Background(), envTestNamespace)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(items) != 1 {
				t.Fatalf("want 1 item, got %d", len(items))
			}
			if items[0].DisplayName == nil {
				t.Fatal("displayName must always be set so clients can render it directly")
			}
			if *items[0].DisplayName != tc.want {
				t.Errorf("displayName = %q, want %q", *items[0].DisplayName, tc.want)
			}
		})
	}
}

func TestVolumeService_ProjectsClaimDetail(t *testing.T) {
	svc := newVolumeSvc(t, VolumeConfig{Enabled: true},
		pvc("ds", envTestNamespace, corev1.ClaimBound, map[string]string{"team": "ai"}))
	items, err := svc.List(context.Background(), envTestNamespace)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	it := items[0]
	if it.Capacity == nil || *it.Capacity != "5Ti" {
		t.Errorf("capacity = %v, want 5Ti", it.Capacity)
	}
	if it.AccessModes == nil || len(*it.AccessModes) != 1 || (*it.AccessModes)[0] != "ReadWriteMany" {
		t.Errorf("accessModes = %v, want [ReadWriteMany]", it.AccessModes)
	}
	if it.StorageClass == nil || *it.StorageClass != "example-shared-fs" {
		t.Errorf("storageClass = %v", it.StorageClass)
	}
	if it.Phase != "Bound" {
		t.Errorf("phase = %q, want Bound", it.Phase)
	}
	if it.Labels == nil || (*it.Labels)["team"] != "ai" {
		t.Errorf("labels must be returned verbatim, got %v", it.Labels)
	}
}

// TestVolumeService_SortedByClaimName keeps the response deterministic; the
// dashboard renders it directly.
func TestVolumeService_SortedByClaimName(t *testing.T) {
	svc := newVolumeSvc(t, VolumeConfig{Enabled: true},
		pvc("zeta", envTestNamespace, corev1.ClaimBound, nil),
		pvc("alpha", envTestNamespace, corev1.ClaimBound, nil),
		pvc("mid", envTestNamespace, corev1.ClaimBound, nil),
	)
	items, err := svc.List(context.Background(), envTestNamespace)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := []string{items[0].ClaimName, items[1].ClaimName, items[2].ClaimName}
	want := []string{"alpha", "mid", "zeta"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unsorted result: got %v, want %v", got, want)
		}
	}
}
