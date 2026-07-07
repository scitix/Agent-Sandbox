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

package instancetype

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestFitsWithin(t *testing.T) {
	cap2c32 := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("2"),
		corev1.ResourceMemory: resource.MustParse("32Gi"),
	}
	tests := []struct {
		name     string
		pod      corev1.ResourceList
		capacity corev1.ResourceList
		wantOK   bool
		wantDim  corev1.ResourceName
	}{
		{"exact", cap2c32, cap2c32, true, ""},
		{"rounded-down both dims", corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("4Gi"),
		}, cap2c32, true, ""},
		{"cpu exceeds", corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("3"), corev1.ResourceMemory: resource.MustParse("4Gi"),
		}, cap2c32, false, corev1.ResourceCPU},
		{"memory exceeds", corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("64Gi"),
		}, cap2c32, false, corev1.ResourceMemory},
		{"gpu absent from capacity", corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("1"), "nvidia.com/gpu": resource.MustParse("1"),
		}, cap2c32, false, "nvidia.com/gpu"},
		{"zero dim ignored", corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("1"), "nvidia.com/gpu": resource.MustParse("0"),
		}, cap2c32, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dim, ok := FitsWithin(tt.pod, tt.capacity)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (dim=%q)", ok, tt.wantOK, dim)
			}
			if !ok && dim != tt.wantDim {
				t.Errorf("exceeded dim = %q, want %q", dim, tt.wantDim)
			}
		})
	}
}
