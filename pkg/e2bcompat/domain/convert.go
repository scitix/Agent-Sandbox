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
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
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
	// DefaultEnvdVersion is what an E2B response reports when the version of
	// the sandbox's own envd cannot be derived from its template.
	//
	// It is the version this platform builds and ships — see
	// docs/e2b/envd-upgrade-runbook.md, whose "当前状态" table is the record and
	// which this constant is checked against by hack/check-envd-version.sh
	// before every develop push. That check is the whole reason the value is
	// safe to hold: a fallback that drifts from the images is a claim the SDK
	// acts on, and the SDK's version gates decide how it talks to envd (the
	// `user` parameter, octet-stream uploads, file metadata, recursive watch)
	// as well as whether `get_metrics()` is allowed at all.
	//
	// (The floor the SDK accepts at create time is 0.1.0, and 0.1.0 is what a
	// response used to say. It is deliberately not the fallback: claiming the
	// floor is what made `get_metrics()` answer "You need to update the
	// template to use the new SDK" on images whose envd was 0.9.0.)
	DefaultEnvdVersion = "0.9.0"

	// EnvdVersionAnnotation overrides the derived version, for a template whose
	// envd does not come from a tagged image.
	EnvdVersionAnnotation = "agentbox.navix.sh/envd-version"
)

// EnvdVersionForPool reports the version of the envd the sandboxes from this
// pool are running.
//
// The platform does not choose envd — the TEMPLATE does, by copying the binary
// into the pod from an init container (`/mnt/agentbox/envd`), and that
// container's image tag is where the version already lives. Reading it there
// means a bump moves both things at once: the image the init container copies
// from, and the version every E2B response reports. Nothing else has to be
// remembered, which is the property that makes this stay true.
//
// The tag is a TAG, though, not a version (`0.9.0-2` names a build of 0.9.0),
// so what is reported is the version part of it. The SDK parses this string
// with PEP 440 and raises on what it cannot parse — in `connect()` and in the
// list path, not just in the call that wanted it — so a tag that is not a
// version at all falls back to the known-good floor instead of being passed
// through.
func EnvdVersionForPool(pool *agentsv1alpha1.SandboxPool) string {
	if pool == nil {
		return DefaultEnvdVersion
	}
	return envdVersionFor(pool.Annotations, &pool.Spec.Template)
}

// EnvdVersionForEnv reports the version for the Envs a template listing shows.
// Members of one Env are all rendered from the same SandboxTemplate, so the
// first member that carries a snapshot answers for the Env.
func EnvdVersionForEnv(env *agentsv1alpha1.SandboxEnv) string {
	if env == nil {
		return DefaultEnvdVersion
	}
	for i := range env.Spec.Clusters {
		members := env.Spec.Clusters[i].Members
		for j := range members {
			if v := envdVersionFor(members[j].Metadata.Annotations, &members[j].Spec.Template); v != DefaultEnvdVersion {
				return v
			}
		}
	}
	return DefaultEnvdVersion
}

// envdVersionFor is the one derivation: an explicit annotation first, then the
// init container that injects envd.
func envdVersionFor(annotations map[string]string, template *corev1.PodTemplateSpec) string {
	if v := versionFromTag(annotations[EnvdVersionAnnotation]); v != "" {
		return v
	}
	if template == nil {
		return DefaultEnvdVersion
	}
	for i := range template.Spec.InitContainers {
		c := &template.Spec.InitContainers[i]
		if !injectsEnvd(c) {
			continue
		}
		if v := versionFromTag(imageTag(c.Image)); v != "" {
			return v
		}
	}
	return DefaultEnvdVersion
}

// injectsEnvd recognises the init container whose job is to put envd in place.
//
// By name or by repository, because both are conventions an operator can
// reasonably follow and neither is declared anywhere: `envd-injector`, and
// images published as `…/navix-agent-sandbox-envd:<tag>`.
func injectsEnvd(c *corev1.Container) bool {
	if strings.Contains(c.Name, "envd") {
		return true
	}
	return strings.Contains(repository(c.Image), "envd")
}

// imageTag returns the tag of an image reference, empty when it is untagged.
// The colon searched for is the one after the last slash: a registry port
// (`registry:5000/img`) is not a tag.
func imageTag(image string) string {
	slash := strings.LastIndex(image, "/")
	colon := strings.LastIndex(image, ":")
	if colon <= slash {
		return ""
	}
	return image[colon+1:]
}

func repository(image string) string {
	if tag := imageTag(image); tag != "" {
		return strings.TrimSuffix(image, ":"+tag)
	}
	return image
}

// versionFromTag keeps the version part of a tag and nothing else: `0.9.0-2`
// and `0.9.0-1-test` are both builds of 0.9.0, and `latest` or a git sha is
// not a version at all.
func versionFromTag(tag string) string {
	tag = strings.TrimPrefix(strings.TrimSpace(tag), "v")
	end := 0
	for end < len(tag) && (tag[end] == '.' || (tag[end] >= '0' && tag[end] <= '9')) {
		end++
	}
	version := strings.Trim(tag[:end], ".")
	if version == "" {
		return ""
	}
	if slices.Contains(strings.Split(version, "."), "") {
		return ""
	}
	return version
}

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
		EnvdVersion:        EnvdVersionForPool(pool),
	}
}

// ToE2BSandboxDetail converts a native gen.Sandbox + SandboxPool to an e2bgen.SandboxDetail.
// gatewayDomain is the gateway domain name used to build connection URLs.
func ToE2BSandboxDetail(sb *gen.Sandbox, pool *agentsv1alpha1.SandboxPool, gatewayDomain string) e2bgen.SandboxDetail {
	cpuCount, memoryMB := extractResourcesFromPool(pool)
	state := e2bgen.SandboxState(SandboxStateFromStatus(string(sb.Status)))

	startedAt := e2bStartedAt(sb)
	endAt := e2bEndAt(sb, pool)

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
		EnvdVersion: EnvdVersionForPool(pool),
		State:       state,
		Domain:      domainPtr,
	}
}

// ToE2BListedSandbox converts a native gen.Sandbox directly to an e2bgen.ListedSandbox.
func ToE2BListedSandbox(sb *gen.Sandbox, pool *agentsv1alpha1.SandboxPool) e2bgen.ListedSandbox {
	cpuCount, memoryMB := extractResourcesFromPool(pool)
	state := e2bgen.SandboxState(SandboxStateFromStatus(string(sb.Status)))

	startedAt := e2bStartedAt(sb)
	endAt := e2bEndAt(sb, pool)

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
		EnvdVersion: EnvdVersionForPool(pool),
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
		EnvdVersion: EnvdVersionForPool(pool),
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
	// The E2B state enum is `running` | `paused` and nothing else, and AgentBox has
	// no pause, so every sandbox this surface is willing to describe is `running`.
	//
	// That is only sound because terminated sandboxes are filtered out before they
	// get here — see IsLiveStatus. Mapping a finished sandbox onto `running`
	// instead reports it as usable forever, which is how a reclaimed sandbox came
	// to look alive in a sandbox listing.
	return e2bStateRunning
}

// IsLiveStatus reports whether a native sandbox status describes a sandbox that
// still exists to be acted on.
//
// The native API keeps terminated sandboxes queryable, merging historical records
// into its listing so a finished run stays inspectable. The E2B surface has no way
// to express "finished": its state enum holds only `running` and `paused`. So the
// compatibility layer has to drop what it cannot describe rather than mislabel it —
// upstream E2B likewise does not list a killed sandbox. Callers that want the
// history should read the native API, which is where it lives.
func IsLiveStatus(status string) bool {
	switch gen.SandboxStatus(status) {
	case gen.SandboxStatusCompleted,
		gen.SandboxStatusFailed,
		gen.SandboxStatusCanceled,
		gen.SandboxStatusReleased:
		return false
	default:
		// Pending / Starting / Running / Stopping are all still claimed by their
		// caller. Unknown values are treated as live on purpose: a status added
		// later should surface as an odd entry in a listing rather than silently
		// vanish from it.
		return true
	}
}

// IsLive reports whether a sandbox record describes a live sandbox.
//
// Checks the termination timestamp as well as the status, because they are set on
// different paths: a historical record carries terminatedAt, while a status alone
// can lag behind a release that has already happened.
func IsLive(sb *gen.Sandbox) bool {
	if sb == nil {
		return false
	}
	if sb.TerminatedAt != nil && !sb.TerminatedAt.IsZero() {
		return false
	}
	return IsLiveStatus(string(sb.Status))
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

// defaultSandboxLifetime is the deadline assumed when the pool declares no idle
// timeout. Matches the E2B SDKs' own default sandbox timeout, so a caller that set
// nothing anywhere sees the number it would have expected.
const defaultSandboxLifetime = 5 * time.Minute

// e2bStartedAt is the sandbox's start instant, anchored to recorded data.
//
// Never `time.Now()`. A start time that answers "now" every time it is asked is not
// a fact about the sandbox, and anything derived from it — notably endAt — then
// moves forward on every read.
func e2bStartedAt(sb *gen.Sandbox) time.Time {
	if sb.StartedAt != nil && !sb.StartedAt.IsZero() {
		return *sb.StartedAt
	}
	// Always present on a live sandbox; zero only on a record that never got
	// claimed, in which case there is genuinely no timing information to report and
	// the zero time says so honestly.
	return sb.ClaimedAt
}

// e2bEndAt is the sandbox's deadline: its idle timeout counted from the claim, or
// the default lifetime counted from its start.
//
// One function rather than one per response shape. The detail and listing
// converters each carried their own copy of this and drifted apart — the detail
// copy fell back to `now + 5m`, so every read of a sandbox whose pool declared no
// idle timeout reported a deadline five minutes into the future, and the sandbox
// looked like it was about to expire and never did.
func e2bEndAt(sb *gen.Sandbox, pool *agentsv1alpha1.SandboxPool) time.Time {
	if raw := computeEndAt(sb, pool); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			return t
		}
	}
	return e2bStartedAt(sb).Add(defaultSandboxLifetime)
}

// computeEndAt calculates endAt from claimedAt + the sandbox's idle timeout.
// Returns empty string if the timeout cannot be determined.
func computeEndAt(sb *gen.Sandbox, pool *agentsv1alpha1.SandboxPool) string {
	if sb.ClaimedAt.IsZero() {
		return ""
	}
	timeout := sandboxIdleTimeout(sb, pool)
	if timeout <= 0 {
		return ""
	}
	return sb.ClaimedAt.Add(timeout).UTC().Format(time.RFC3339)
}

// sandboxIdleTimeout is the idle timeout in force for this sandbox.
//
// The sandbox's own resolved value wins over the pool default, mirroring the
// precedence the create path already applies when it decides what to enforce
// (request value > pool default). Reporting the pool default instead contradicts
// the timeout actually being enforced.
//
// This used to read the per-pod idle-timeout annotation key off the *pool* object.
// Pools never carry that key — their default lives in spec.defaultIdleTimeout — so
// the lookup always missed and every sandbox reported the fallback lifetime
// regardless of the timeout it was created with, making `create(timeout=...)` and
// `setTimeout` look like they had been ignored.
func sandboxIdleTimeout(sb *gen.Sandbox, pool *agentsv1alpha1.SandboxPool) time.Duration {
	if sb.IdleTimeoutSeconds != nil && *sb.IdleTimeoutSeconds > 0 {
		return time.Duration(*sb.IdleTimeoutSeconds) * time.Second
	}
	if pool != nil && pool.Spec.DefaultIdleTimeout != nil && pool.Spec.DefaultIdleTimeout.Duration > 0 {
		return pool.Spec.DefaultIdleTimeout.Duration
	}
	return 0
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
