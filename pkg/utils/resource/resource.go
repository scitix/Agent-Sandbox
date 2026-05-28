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

// Package resource provides utilities for computing resource sums across Pod containers.
package resource

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// SumContainers sums CPU and memory across the given containers.
// For each container, requests take priority over limits.
// Returns an error if any container has neither requests nor limits set.
func SumContainers(containers []corev1.Container) (cpu *resource.Quantity, memory *resource.Quantity, err error) {
	if len(containers) == 0 {
		return nil, nil, fmt.Errorf("no containers provided")
	}

	totalCPU := resource.NewMilliQuantity(0, resource.DecimalSI)
	totalMemory := resource.NewQuantity(0, resource.BinarySI)

	for _, c := range containers {
		// CPU: requests → limits → error
		if cpuReq, ok := c.Resources.Requests[corev1.ResourceCPU]; ok {
			totalCPU.Add(cpuReq)
		} else if cpuLim, ok := c.Resources.Limits[corev1.ResourceCPU]; ok {
			totalCPU.Add(cpuLim)
		} else {
			return nil, nil, fmt.Errorf("container %q has no CPU requests or limits", c.Name)
		}

		// Memory: requests → limits → error
		if memReq, ok := c.Resources.Requests[corev1.ResourceMemory]; ok {
			totalMemory.Add(memReq)
		} else if memLim, ok := c.Resources.Limits[corev1.ResourceMemory]; ok {
			totalMemory.Add(memLim)
		} else {
			return nil, nil, fmt.Errorf("container %q has no memory requests or limits", c.Name)
		}
	}

	return totalCPU, totalMemory, nil
}

// SumContainerResources sums CPU and memory across all containers (not initContainers)
// in a PodTemplateSpec. For each container, requests takes priority, falling back to
// limits. If neither is set for any container, an error is returned.
//
// Returns raw Quantity strings (e.g. "8000m", "8Gi") suitable for API responses.
// Callers can call .String() on the returned Quantities.
func SumContainerResources(template *corev1.PodTemplateSpec) (cpu *resource.Quantity, memory *resource.Quantity, err error) {
	return SumContainers(template.Spec.Containers)
}
