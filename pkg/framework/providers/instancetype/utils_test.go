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

func TestDeriveResourceKey(t *testing.T) {
	req := func(cpu, mem string) corev1.ResourceRequirements {
		return corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(cpu),
			corev1.ResourceMemory: resource.MustParse(mem),
		}}
	}
	tests := []struct {
		name string
		in   corev1.ResourceRequirements
		want string
	}{
		// Whole-unit shapes must render exactly as they always have — every
		// existing Pool name and ScalingGroup depends on it.
		{"whole core and GiB", req("1", "16Gi"), "1c16gi"},
		{"multi core", req("4", "64Gi"), "4c64gi"},
		{"sub-core keeps milli", req("500m", "2Gi"), "500mc2gi"},
		{"sub-GiB keeps MiB", req("20m", "128Mi"), "20mc128mi"},
		{"non-whole GiB renders MiB", req("2", "1536Mi"), "2c1536mi"},
		{"milli that divides into cores collapses", req("2000m", "8192Mi"), "2c8gi"},
		{"empty falls back to default", corev1.ResourceRequirements{}, "default"},
		{
			name: "limits are used when requests are absent",
			in: corev1.ResourceRequirements{Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("20m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			}},
			want: "20mc128mi",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DeriveResourceKey(tt.in); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// Sub-GiB shapes must not collapse onto one key, or two differently-sized
// Pools would collide on PoolName.
func TestDeriveResourceKey_SubUnitShapesStayDistinct(t *testing.T) {
	mk := func(cpu, mem string) corev1.ResourceRequirements {
		return corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(cpu),
			corev1.ResourceMemory: resource.MustParse(mem),
		}}
	}
	seen := map[string]string{}
	for _, shape := range [][2]string{
		{"20m", "128Mi"}, {"50m", "256Mi"}, {"100m", "512Mi"}, {"1", "1Gi"},
	} {
		key := DeriveResourceKey(mk(shape[0], shape[1]))
		if prev, dup := seen[key]; dup {
			t.Fatalf("key %q collides: %s/%s and %s", key, shape[0], shape[1], prev)
		}
		seen[key] = shape[0] + "/" + shape[1]
	}
}

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
