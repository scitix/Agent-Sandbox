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

package domain

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
)

func makeTestPool(name, namespace string, cpuMillis, memoryMiB int64) *agentsv1alpha1.SandboxPool { //nolint:unparam
	cpuQ := resource.NewMilliQuantity(cpuMillis, resource.DecimalSI)
	memQ := resource.NewQuantity(memoryMiB*1024*1024, resource.BinarySI)
	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				Template: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "main",
								Image: "ubuntu:22.04",
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    *cpuQ,
										corev1.ResourceMemory: *memQ,
									},
								},
							},
						},
					},
				},
			},
		},
		Status: agentsv1alpha1.SandboxPoolStatus{
			IdleReplicas:    2,
			RunningReplicas: 1,
		},
	}
	return pool
}

func TestToE2BSandbox_StateMapping(t *testing.T) {
	tests := []struct {
		name      string
		status    string
		wantState string
	}{
		{"running", "Running", "running"},
		{"starting", "Starting", "running"},
		{"stopping", "Stopping", "running"},
		{"failed", "Failed", "running"},
	}

	pool := makeTestPool("pool-a", "test-ns", 2000, 4096)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sb := &gen.Sandbox{
				SandboxId: "sbx-001",
				Namespace: "test-ns",
				PoolName:  "pool-a",
				Status:    gen.SandboxStatus(tc.status),
			}
			// ToE2BSandbox returns a Sandbox (no state field); use ToE2BSandboxDetail for state
			detail := ToE2BSandboxDetail(sb, pool, "example.com")
			if string(detail.State) != tc.wantState {
				t.Errorf("state = %q, want %q", detail.State, tc.wantState)
			}
		})
	}
}

func TestToE2BSandboxDetail_ResourceExtraction(t *testing.T) {
	// 2000 milliCPU = 2 CPU, 4096 MiB memory
	pool := makeTestPool("pool-a", "test-ns", 2000, 4096)

	sb := &gen.Sandbox{
		SandboxId: "sbx-001",
		Namespace: "test-ns",
		PoolName:  "pool-a",
		Status:    "Running",
	}

	result := ToE2BSandboxDetail(sb, pool, "example.com")
	if result.CpuCount != 2 {
		t.Errorf("cpuCount = %d, want 2", result.CpuCount)
	}
	if result.MemoryMB != 4096 {
		t.Errorf("memoryMB = %d, want 4096", result.MemoryMB)
	}
}

func TestToE2BSandbox_DomainField(t *testing.T) {
	pool := makeTestPool("pool-a", "test-ns", 1000, 2048)
	sb := &gen.Sandbox{
		SandboxId: "sbx-001",
		PoolName:  "pool-a",
		Status:    "Running",
	}

	// With domain
	result := ToE2BSandbox(sb, pool, "my.gateway.com")
	if result.Domain == nil {
		t.Fatal("expected non-nil domain")
	}
	if *result.Domain != "my.gateway.com" {
		t.Errorf("domain = %q, want %q", *result.Domain, "my.gateway.com")
	}

	// Without domain
	result2 := ToE2BSandbox(sb, pool, "")
	if result2.Domain != nil {
		t.Fatalf("expected nil domain when empty, got %q", *result2.Domain)
	}
}

func TestToE2BSandbox_TrafficAccessTokenNotNil(t *testing.T) {
	pool := makeTestPool("pool-a", "test-ns", 1000, 2048)
	sb := &gen.Sandbox{
		SandboxId: "sbx-001",
		PoolName:  "pool-a",
		Status:    "Running",
	}

	result := ToE2BSandbox(sb, pool, "example.com")
	if result.TrafficAccessToken == nil {
		t.Fatal("trafficAccessToken must be non-nil (required by E2B SDK)")
	}
}

func TestToE2BSandbox_EnvdVersion(t *testing.T) {
	pool := makeTestPool("pool-a", "test-ns", 1000, 2048)
	sb := &gen.Sandbox{
		SandboxId: "sbx-001",
		PoolName:  "pool-a",
		Status:    "Running",
	}

	result := ToE2BSandbox(sb, pool, "example.com")
	if result.EnvdVersion != EnvdVersion {
		t.Errorf("envdVersion = %q, want %q", result.EnvdVersion, EnvdVersion)
	}
}

func TestToE2BSandbox_NilPool(t *testing.T) {
	sb := &gen.Sandbox{
		SandboxId: "sbx-001",
		PoolName:  "pool-a",
		Namespace: "test-ns",
		Status:    "Running",
	}

	// Should not panic with nil pool
	result := ToE2BSandbox(sb, nil, "example.com")
	if result.SandboxID != "sbx-001" {
		t.Errorf("sandboxID = %q, want sbx-001", result.SandboxID)
	}
	if result.TemplateID != "pool-a" {
		t.Errorf("templateID = %q, want pool-a", result.TemplateID)
	}
}

func TestToE2BSandboxDetail_NilPool(t *testing.T) {
	sb := &gen.Sandbox{
		SandboxId: "sbx-001",
		PoolName:  "pool-a",
		Namespace: "test-ns",
		Status:    "Running",
	}

	result := ToE2BSandboxDetail(sb, nil, "example.com")
	if result.CpuCount != 0 {
		t.Errorf("cpuCount should be 0 with nil pool, got %d", result.CpuCount)
	}
}

func TestToE2BTemplate(t *testing.T) {
	pool := makeTestPool("my-pool", "test-ns", 4000, 8192)
	pool.Status.IdleReplicas = 3
	pool.Status.RunningReplicas = 2

	result := ToE2BTemplate(pool)
	if result.TemplateID != "my-pool" {
		t.Errorf("templateID = %q, want my-pool", result.TemplateID)
	}
	if result.CpuCount != 4 {
		t.Errorf("cpuCount = %d, want 4", result.CpuCount)
	}
	if result.MemoryMB != 8192 {
		t.Errorf("memoryMB = %d, want 8192", result.MemoryMB)
	}
	if result.SpawnCount != 2 {
		t.Errorf("spawnCount = %d, want 2 (RunningReplicas)", result.SpawnCount)
	}
}

func TestToE2BListedSandbox(t *testing.T) {
	pool := makeTestPool("pool-a", "test-ns", 2000, 4096)
	startedAt, _ := time.Parse(time.RFC3339, "2026-03-22T10:00:00Z")
	metadata := map[string]string{"key": "value"}
	sb := &gen.Sandbox{
		SandboxId: "sbx-001",
		PoolName:  "pool-a",
		Namespace: "test-ns",
		Status:    "Running",
		StartedAt: &startedAt,
		Metadata:  &metadata,
	}

	result := ToE2BListedSandbox(sb, pool)
	if result.SandboxID != "sbx-001" {
		t.Errorf("sandboxID = %q, want sbx-001", result.SandboxID)
	}
	if result.TemplateID != "pool-a" {
		t.Errorf("templateID = %q, want pool-a", result.TemplateID)
	}
	if result.ClientID != "test-ns" {
		t.Errorf("clientID = %q, want test-ns", result.ClientID)
	}
	if string(result.State) != "running" {
		t.Errorf("state = %q, want running", result.State)
	}
	if result.EnvdVersion == "" {
		t.Fatal("expected non-empty envdVersion")
	}
	if result.CpuCount != 2 {
		t.Errorf("cpuCount = %d, want 2", result.CpuCount)
	}
	// metadata should be set
	if result.Metadata == nil {
		t.Fatal("expected non-nil metadata")
	}
	if (*result.Metadata)["key"] != "value" {
		t.Errorf("metadata key = %q, want value", (*result.Metadata)["key"])
	}
}

func TestSandboxStateFromStatus(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"Running", "running"},
		{"Starting", "running"},
		{"Stopping", "running"},
		{"Failed", "running"},
		{"Completed", "running"},
		{"", "running"},
	}
	for _, tc := range tests {
		got := SandboxStateFromStatus(tc.status)
		if got != tc.want {
			t.Errorf("SandboxStateFromStatus(%q) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

// TestToE2BTemplate_Names verifies the pool name appears in the Names slice.
func TestToE2BTemplate_Names(t *testing.T) {
	pool := makeTestPool("my-pool", "test-ns", 1000, 2048)
	result := ToE2BTemplate(pool)
	if len(result.Names) == 0 || result.Names[0] != "my-pool" {
		t.Errorf("names = %v, want [my-pool]", result.Names)
	}
}

// TestToE2BListedSandbox_NilMetadata verifies nil metadata when sandbox has none.
func TestToE2BListedSandbox_NilMetadata(t *testing.T) {
	pool := makeTestPool("pool-a", "test-ns", 2000, 4096)
	sb := &gen.Sandbox{
		SandboxId: "sbx-001",
		PoolName:  "pool-a",
		Namespace: "test-ns",
		Status:    "Running",
	}
	result := ToE2BListedSandbox(sb, pool)
	if result.Metadata != nil {
		t.Errorf("expected nil metadata for sandbox without metadata, got %v", result.Metadata)
	}
}

// TestToE2BTemplate_TypeAlias ensures e2bgen type aliases are satisfied.
func TestToE2BTemplate_TypeAlias(t *testing.T) {
	pool := makeTestPool("pool-a", "test-ns", 2000, 4096)
	result := ToE2BTemplate(pool)
	// These casts verify the types are the expected e2bgen aliases
	var _ = result.CpuCount
	var _ = result.MemoryMB
	var _ = result.EnvdVersion
}
