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
	"context"

	corev1 "k8s.io/api/core/v1"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/framework"
)

const NoopName = "noop"

// Noop is a Provider that reports the catalog feature as disabled. It is the
// default for open-source builds where no closed-source catalog is wired in.
type Noop struct{}

// NewNoop returns a disabled Provider. Safe to share.
func NewNoop() Provider { return Noop{} }

// NoopFactory is the Factory form of NewNoop — no Handle or Args are used.
func NoopFactory(_ framework.Handle, _ framework.Args) (Provider, error) {
	return NewNoop(), nil
}

func (Noop) Enabled() bool                      { return false }
func (Noop) Get(_ string) (*InstanceType, bool) { return nil, false }
func (Noop) List() []*InstanceType              { return nil }

func (Noop) Resolve(_ context.Context, name string, _ int32) (corev1.ResourceRequirements, *domain.AppError) {
	return corev1.ResourceRequirements{}, domain.NewNotFound("instance type not found: " + name)
}

func (Noop) ResolveByResources(_ context.Context, _ corev1.ResourceRequirements) (*InstanceType, int32, *domain.AppError) {
	return nil, 0, nil
}

func (Noop) DeriveScalingGroupName(observed corev1.ResourceRequirements) string {
	return DeriveDefaultScalingGroupName(observed)
}

// DeriveDefaultScalingGroupName is the open-source canonical naming used by
// the Noop provider and (fallback path) any caller that doesn't have access
// to a richer catalog. It is exported so other providers can delegate to it
// for resource shapes outside their own catalog.
//
// Output shape: cpu + memory rendered in their largest lossless unit, plus
// an exact NVIDIA gpu suffix or a resource-identity digest for other extended
// resources. Existing stored groups are not renamed. Examples:
//
//	{cpu:1, mem:4Gi}                                  → "1c4Gi"
//	{cpu:22, mem:220Gi, nvidia.com/gpu:1}             → "22c220Gi-1gpu"
//	{cpu:8, mem:32Gi, "example.com/tpu":4}          → "8c32Gi-r<digest>"
//	{cpu:20m, mem:128Mi}                              → "20mc128Mi"
//	{}                                                → "default"
//
// Sub-core / sub-GiB shapes keep their precision (milli-cores and MiB) so they
// land in distinct scaling groups instead of collapsing onto one whole-unit
// name. Extended resources are still whole-numbered via Quantity.Value().
func DeriveDefaultScalingGroupName(observed corev1.ResourceRequirements) string {
	reqs := observed.Requests
	if len(reqs) == 0 {
		return "default"
	}
	parts := []string{
		formatCPUKey(reqs.Cpu().MilliValue(), "c", "mc") +
			formatMemoryKey(reqs.Memory().Value(), "Gi", "Mi"),
	}

	// Keep the historical CPU/memory spelling while using the same device
	// identity rules as new member resource keys.
	return CompactResourceName(parts[0]+extendedResourceSuffix(observed), 63)
}

func init() {
	Register(NoopName, NoopFactory)
}
