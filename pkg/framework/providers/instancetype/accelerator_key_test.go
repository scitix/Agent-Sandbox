// Copyright 2026 ScitiX
// SPDX-License-Identifier: Apache-2.0
package instancetype

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/validation"
)

const acceleratorCPUOnlyKey = "8c16gi"

func deviceShape(devices map[corev1.ResourceName]string) corev1.ResourceRequirements {
	r := corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("8"), corev1.ResourceMemory: resource.MustParse("16Gi")}
	for name, count := range devices {
		r[name] = resource.MustParse(count)
	}
	return corev1.ResourceRequirements{Requests: r}
}

func TestResourceKeyDeviceIdentity(t *testing.T) {
	cpu := deviceShape(nil)
	if got := DeriveResourceKey(cpu); got != acceleratorCPUOnlyKey {
		t.Fatalf("CPU key changed: %s", got)
	}
	gpu := deviceShape(map[corev1.ResourceName]string{"nvidia.com/gpu": "1"})
	if got := DeriveResourceKey(gpu); got != "8c16gi-1gpu" {
		t.Fatalf("NVIDIA key=%s", got)
	}
	seen := map[string]string{DeriveResourceKey(cpu): "cpu", DeriveResourceKey(gpu): "nvidia"}
	for _, tc := range []struct {
		name  corev1.ResourceName
		count string
	}{
		{"amd.com/gpu", "1"}, {"huawei.com/Ascend910", "1"},
		{"example.com/custom-device", "1"}, {"example.com/custom-device", "2"},
		{"rdma.example.com/hca", "1"},
	} {
		key := DeriveResourceKey(deviceShape(map[corev1.ResourceName]string{tc.name: tc.count}))
		if previous, exists := seen[key]; exists {
			t.Fatalf("%s/%s collides with %s: %s", tc.name, tc.count, previous, key)
		}
		if !strings.Contains(key, "-r") || strings.Contains(key, "gpu") {
			t.Fatalf("non-NVIDIA resource mislabeled: %s", key)
		}
		if errs := validation.IsDNS1123Label(key); len(errs) > 0 {
			t.Fatalf("invalid key %q: %v", key, errs)
		}
		seen[key] = string(tc.name) + tc.count
	}
}

func TestResourceKeyLimitsAndOrder(t *testing.T) {
	r := deviceShape(nil)
	r.Limits = corev1.ResourceList{"nvidia.com/gpu": resource.MustParse("1")}
	if got := DeriveResourceKey(r); got != "8c16gi-1gpu" {
		t.Fatalf("limits-only device lost: %s", got)
	}
	zero := deviceShape(map[corev1.ResourceName]string{"nvidia.com/gpu": "0", "example.com/custom-device": "0"})
	if got := DeriveResourceKey(zero); got != acceleratorCPUOnlyKey {
		t.Fatalf("zero device changed key: %s", got)
	}
	multi := deviceShape(map[corev1.ResourceName]string{"nvidia.com/gpu": "1", "rdma.example.com/hca": "2", "example.com/custom-device": "4"})
	want := DeriveResourceKey(multi)
	if !strings.HasPrefix(want, "8c16gi-1gpu-r") {
		t.Fatalf("RDMA counted as GPU: %s", want)
	}
	for range 100 {
		if got := DeriveResourceKey(multi); got != want {
			t.Fatal("map order changed key")
		}
	}
	multi.Requests["rdma.example.com/hca"] = resource.MustParse("3")
	if DeriveResourceKey(multi) == want {
		t.Fatal("extended quantity did not affect identity")
	}
}

func TestResourceKeyNativeStorageCompatibility(t *testing.T) {
	r := deviceShape(nil)
	r.Requests[corev1.ResourceEphemeralStorage] = resource.MustParse("32Gi")
	r.Requests["hugepages-2Mi"] = resource.MustParse("1Gi")
	if got := DeriveResourceKey(r); got != acceleratorCPUOnlyKey {
		t.Fatalf("native dimensions changed existing CPU group: %s", got)
	}
}

func TestCompactGeneratedPoolName(t *testing.T) {
	a := strings.Repeat("env", 8) + "-8c16gi-1gpu-r0123456789abcdef-quota-shared-long"
	b := strings.TrimSuffix(a, "long") + "other"
	x, y := CompactResourceName(a, 63), CompactResourceName(b, 63)
	if len(x) > 63 || x == y {
		t.Fatalf("invalid or colliding compact names %s %s", x, y)
	}
	if errs := validation.IsDNS1123Label(x); len(errs) > 0 {
		t.Fatal(errs)
	}
}
