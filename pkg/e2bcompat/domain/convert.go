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

// Package domain provides E2B-compatible conversion utilities.
// It maps AgentBox native API models directly to E2B generated types.
package domain

import (
	"fmt"
	"maps"
	"regexp"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	e2bgen "github.com/scitix/agent-sandbox/pkg/e2bcompat/gen"
	utilresource "github.com/scitix/agent-sandbox/pkg/utils/resource"
)

// e2bStateRunning is the E2B state string for running/starting/stopping/failed sandboxes.
// E2B only has "running" and "paused" — we map everything active to "running".
const e2bStateRunning = string(e2bgen.Running)

const (
	// EnvdVersion is the fixed envd version reported in E2B API responses.
	// E2B SDK validates that this field is present and non-empty.
	EnvdVersion = "0.1.0"
)

// ToE2BSandbox converts a native gen.Sandbox + SandboxPool directly to an e2bgen.Sandbox.
// gatewayDomain is the gateway domain name used to build connection URLs.
func ToE2BSandbox(sb *gen.Sandbox, pool *agentsv1alpha1.SandboxPool, gatewayDomain string) e2bgen.Sandbox {
	var domainPtr *string
	if gatewayDomain != "" {
		d := gatewayDomain
		domainPtr = &d
	}

	// TrafficAccessToken is required by E2B SDK (must be non-nil, but can be empty string pointer)
	emptyToken := ""

	return e2bgen.Sandbox{
		TemplateID:         poolNameFromSandbox(sb, pool),
		SandboxID:          sb.SandboxId,
		ClientID:           sb.Namespace,
		TrafficAccessToken: &emptyToken,
		Domain:             domainPtr,
		EnvdVersion:        e2bgen.EnvdVersion(EnvdVersion),
	}
}

// ToE2BSandboxDetail converts a native gen.Sandbox + SandboxPool to an e2bgen.SandboxDetail.
// gatewayDomain is the gateway domain name used to build connection URLs.
func ToE2BSandboxDetail(sb *gen.Sandbox, pool *agentsv1alpha1.SandboxPool, gatewayDomain string) e2bgen.SandboxDetail {
	cpuCount, memoryMB := extractResourcesFromPool(pool)
	state := e2bgen.SandboxState(SandboxStateFromStatus(string(sb.Status)))

	endAtStr := ""
	if !sb.ClaimedAt.IsZero() && pool != nil {
		endAtStr = computeEndAt(sb, pool)
	}

	now := time.Now()
	startedAt := now
	if sb.StartedAt != nil {
		startedAt = *sb.StartedAt
	}
	endAt := now.Add(5 * time.Minute)
	if endAtStr != "" {
		if t, err := time.Parse(time.RFC3339, endAtStr); err == nil {
			endAt = t
		}
	}

	var domainPtr *string
	if gatewayDomain != "" {
		d := gatewayDomain
		domainPtr = &d
	}

	var metadata *e2bgen.SandboxMetadata
	merged := make(map[string]string)
	if sb.Metadata != nil {
		maps.Copy(merged, *sb.Metadata)
	}
	if sb.NodeName != nil && *sb.NodeName != "" {
		merged["agentbox.nodeName"] = *sb.NodeName
	}
	if sb.ContainerId != nil && *sb.ContainerId != "" {
		merged["agentbox.containerId"] = *sb.ContainerId
	}
	if len(merged) > 0 {
		m := e2bgen.SandboxMetadata(merged)
		metadata = &m
	}

	return e2bgen.SandboxDetail{
		SandboxID:   sb.SandboxId,
		TemplateID:  poolNameFromSandbox(sb, pool),
		ClientID:    sb.Namespace,
		StartedAt:   startedAt,
		EndAt:       endAt,
		CpuCount:    cpuCount,
		MemoryMB:    e2bgen.MemoryMB(memoryMB),
		DiskSizeMB:  0,
		Metadata:    metadata,
		EnvdVersion: e2bgen.EnvdVersion(EnvdVersion),
		State:       state,
		Domain:      domainPtr,
	}
}

// ToE2BListedSandbox converts a native gen.Sandbox directly to an e2bgen.ListedSandbox.
func ToE2BListedSandbox(sb *gen.Sandbox, pool *agentsv1alpha1.SandboxPool) e2bgen.ListedSandbox {
	cpuCount, memoryMB := extractResourcesFromPool(pool)
	state := e2bgen.SandboxState(SandboxStateFromStatus(string(sb.Status)))

	endAtStr := ""
	if !sb.ClaimedAt.IsZero() && pool != nil {
		endAtStr = computeEndAt(sb, pool)
	}

	startedAt := time.Now()
	if sb.StartedAt != nil {
		startedAt = *sb.StartedAt
	}
	endAt := startedAt.Add(5 * time.Minute)
	if endAtStr != "" {
		if t, err := time.Parse(time.RFC3339, endAtStr); err == nil {
			endAt = t
		}
	}

	var metadata *e2bgen.SandboxMetadata
	merged := make(map[string]string)
	if sb.Metadata != nil {
		maps.Copy(merged, *sb.Metadata)
	}
	if sb.NodeName != nil && *sb.NodeName != "" {
		merged["agentbox.nodeName"] = *sb.NodeName
	}
	if sb.ContainerId != nil && *sb.ContainerId != "" {
		merged["agentbox.containerId"] = *sb.ContainerId
	}
	if len(merged) > 0 {
		m := e2bgen.SandboxMetadata(merged)
		metadata = &m
	}

	return e2bgen.ListedSandbox{
		SandboxID:   sb.SandboxId,
		TemplateID:  poolNameFromSandbox(sb, pool),
		ClientID:    sb.Namespace,
		StartedAt:   startedAt,
		EndAt:       endAt,
		CpuCount:    cpuCount,
		MemoryMB:    e2bgen.MemoryMB(memoryMB),
		DiskSizeMB:  0,
		Metadata:    metadata,
		EnvdVersion: e2bgen.EnvdVersion(EnvdVersion),
		State:       state,
	}
}

// ToE2BTemplate converts an AgentBox SandboxPool directly to an e2bgen.Template.
func ToE2BTemplate(pool *agentsv1alpha1.SandboxPool) e2bgen.Template {
	cpuCount, memoryMB := extractResourcesFromPool(pool)

	description := ""
	// Pool description can be stored in annotations
	if pool.Annotations != nil {
		description = pool.Annotations["agentbox.navix.sh/description"]
	}
	_ = description // used for documentation; Template struct doesn't have a Description field

	createdAt := pool.CreationTimestamp.Time
	updatedAt := createdAt

	return e2bgen.Template{
		TemplateID:  pool.Name,
		BuildID:     string(pool.UID),
		CpuCount:    cpuCount,
		MemoryMB:    e2bgen.MemoryMB(memoryMB),
		DiskSizeMB:  0,
		Public:      true, // default to public; can be refined via visibility rules
		BuildCount:  0,
		BuildStatus: e2bgen.TemplateBuildStatus("ready"),
		SpawnCount:  int64(pool.Status.RunningReplicas),
		Aliases:     []string{},
		Names:       []string{pool.Name},
		EnvdVersion: e2bgen.EnvdVersion(EnvdVersion),
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}
}

// ---------------------------------------------------------------------------
// private helpers
// ---------------------------------------------------------------------------

// SandboxStateFromStatus maps AgentBox sandbox status to E2B state.
// E2B only has "running" and "paused" states.
func SandboxStateFromStatus(status string) string {
	switch strings.ToLower(status) {
	case "running", "starting":
		return e2bStateRunning
	default:
		return e2bStateRunning // E2B doesn't have a concept for stopping/failed – default to running
	}
}

// poolNameFromSandbox returns the pool name, preferring the pool object if available.
func poolNameFromSandbox(sb *gen.Sandbox, pool *agentsv1alpha1.SandboxPool) string {
	if pool != nil {
		return pool.Name
	}
	return sb.PoolName
}

// extractResourcesFromPool extracts cpuCount and memoryMB from the pool's containers
// by summing resource requests (with fallback to limits) across all containers.
// Returns 0,0 if not available.
func extractResourcesFromPool(pool *agentsv1alpha1.SandboxPool) (cpuCount int32, memoryMB int64) {
	if pool == nil || len(pool.Spec.Template.Spec.Containers) == 0 {
		return 0, 0
	}
	cpu, memory, err := utilresource.SumContainerResources(&pool.Spec.Template)
	if err != nil {
		return 0, 0
	}
	return ExtractCPUFromQuantity(*cpu), ExtractMemoryMBFromQuantity(*memory)
}

// computeEndAt calculates endAt from claimedAt + idle-timeout annotation.
// Returns empty string if timeout cannot be determined.
func computeEndAt(sb *gen.Sandbox, pool *agentsv1alpha1.SandboxPool) string {
	if pool == nil || sb.ClaimedAt.IsZero() {
		return ""
	}

	// Check for default idle timeout in pool annotations
	var timeoutSecs int64
	if pool.Annotations != nil {
		if ts, ok := pool.Annotations[agentsv1alpha1.SandboxIdleTimeoutAnnotationKey]; ok {
			if v, parseErr := strconv.ParseInt(ts, 10, 64); parseErr == nil {
				timeoutSecs = v
			}
		}
	}

	if timeoutSecs <= 0 {
		return ""
	}

	endAt := sb.ClaimedAt.Add(time.Duration(timeoutSecs) * time.Second)
	return endAt.UTC().Format(time.RFC3339)
}

// ExtractCPUFromQuantity converts a resource.Quantity to an int32 CPU count.
func ExtractCPUFromQuantity(q resource.Quantity) int32 {
	milliCPU := q.MilliValue()
	return int32((milliCPU + 500) / 1000)
}

// ExtractMemoryMBFromQuantity converts a resource.Quantity to int64 MB.
func ExtractMemoryMBFromQuantity(q resource.Quantity) int64 {
	return q.Value() / (1024 * 1024)
}

// sandboxIDPattern matches E2B-style sandbox IDs (alphanumeric + hyphens).
var sandboxIDPattern = regexp.MustCompile(`^[a-zA-Z0-9\-]+$`)

// IsValidSandboxID returns true if the sandbox ID matches the expected format.
func IsValidSandboxID(id string) bool {
	return sandboxIDPattern.MatchString(id) && len(id) > 0
}

// BuildE2BEndpointURL builds the E2B-style connection URL for a sandbox.
// Uses the private path protocol: scheme://domain/agentbox/<sandboxID>/<port>
func BuildE2BEndpointURL(scheme, gatewayDomain, sandboxID string, port int32) string {
	domain := strings.TrimRight(gatewayDomain, "/")
	return fmt.Sprintf("%s://%s/agentbox/%s/%d", scheme, domain, sandboxID, port)
}
