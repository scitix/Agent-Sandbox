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

func mkReq(reqs corev1.ResourceList) corev1.ResourceRequirements {
	return corev1.ResourceRequirements{Requests: reqs}
}

func TestNoop_DeriveScalingGroupName(t *testing.T) {
	tests := []struct {
		name string
		in   corev1.ResourceRequirements
		want string
	}{
		{
			name: "zero resources fall back to default",
			in:   corev1.ResourceRequirements{},
			want: "default",
		},
		{
			name: "1c4Gi",
			in: mkReq(corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			}),
			want: "1c4Gi",
		},
		{
			name: "22c220Gi with one nvidia gpu",
			in: mkReq(corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("22"),
				corev1.ResourceMemory: resource.MustParse("220Gi"),
				"nvidia.com/gpu":      resource.MustParse("1"),
			}),
			want: "22c220Gi-1gpu",
		},
		{
			name: "extended resource with non-gpu suffix gets safe encoding",
			in: mkReq(corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("8"),
				corev1.ResourceMemory: resource.MustParse("32Gi"),
				"scitix.ai/tpu":       resource.MustParse("4"),
			}),
			want: "8c32Gi-4scitix.ai-tpu",
		},
		{
			name: "zero gpu count is omitted",
			in: mkReq(corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("16Gi"),
				"nvidia.com/gpu":      resource.MustParse("0"),
			}),
			want: "4c16Gi",
		},
		{
			// Quantity.Value() rounds up for sub-unit values (so 500m → 1).
			// Documented behavior; we accept the conservative rounding.
			name: "sub-core CPU rounds up",
			in: mkReq(corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			}),
			want: "1c2Gi",
		},
	}
	p := Noop{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.DeriveScalingGroupName(tt.in); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
