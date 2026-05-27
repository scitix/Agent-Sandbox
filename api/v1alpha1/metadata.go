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

package v1alpha1

type SandboxStopReason string

const (
	SandboxStopReasonCompleted SandboxStopReason = "Completed" // Normal completion of the sandbox workload.
	SandboxStopReasonCanceled  SandboxStopReason = "Canceled"  // Premature stop before ever reaching Running (e.g. deleted while Starting).
	SandboxStopReasonReleased  SandboxStopReason = "Released"  // Explicit release by API call or idle timeout.
	SandboxStopReasonFailed    SandboxStopReason = "Failed"    // Stopped due to pod failure (OOMKilled, Evicted, etc.).
)

const (
	SandboxPoolLabelKey  = "agentbox.navix.sh/sandbox-pool"
	SandboxPhaseLabelKey = "agentbox.navix.sh/sandbox-phase"
	SandboxIDLabelKey    = "agentbox.navix.sh/sandbox-id"
	ManagedByLabelKey    = "agentbox.navix.sh/managed-by"

	// SandboxPhase values for the agentbox sandbox lifecycle.
	SandboxPhaseIdle     = "idle"
	SandboxPhaseRunning  = "running"
	SandboxPhaseStarting = "starting" // Idle → (image pull) → Running
	SandboxPhaseStopping = "stopping" // Running → (image reset) → Idle
	SandboxPhaseFailed   = "failed"

	ManagedBySandboxAPIServer = "sandbox-api-server"

	SandboxIDAnnotationKey        = "agentbox.navix.sh/sandbox-id"
	SandboxClaimedAtAnnotationKey = "agentbox.navix.sh/claimed-at"
	SandboxStartedAtAnnotationKey = "agentbox.navix.sh/started-at"
	// SandboxIdleTimeoutAnnotationKey stores the idle timeout duration in seconds (e.g. "600").
	// Written at claim time if TTL > 0. Read by IdleTimeoutReconciler.
	SandboxIdleTimeoutAnnotationKey = "agentbox.navix.sh/idle-timeout"
	// SandboxStartupTimeoutAnnotationKey stores the startup timeout duration in seconds (e.g. "120").
	// Written at claim time when a startup timeout is resolved (from request or pool default).
	// Read by IdleTimeoutReconciler.cleanupTimedOutStartingPods to determine per-pod timeout.
	// Takes priority over the pool-level StartupTimeout when both are set.
	SandboxStartupTimeoutAnnotationKey = "agentbox.navix.sh/startup-timeout"
	// SandboxLastActiveAnnotationKey stores the RFC3339 time of the last HTTP request
	// proxied through ExtProc. Written asynchronously by ActivityTracker.
	SandboxLastActiveAnnotationKey            = "agentbox.navix.sh/last-active"
	SandboxMetadataAnnotationKey              = "agentbox.navix.sh/sandbox-metadata"
	SandboxManagedLabelKeysAnnotationKey      = "agentbox.navix.sh/managed-label-keys"
	SandboxManagedAnnotationKeysAnnotationKey = "agentbox.navix.sh/managed-annotation-keys"

	// SandboxStopReasonAnnotationKey records why the sandbox was stopped.
	// Values: "Completed" | "Released" | "Failed" | "Canceled". Written by ReleaseSandboxPod.
	// Read by syncInplaceUpdatePhases on Stopping→Idle to perform deferred KV write.
	SandboxStopReasonAnnotationKey = "agentbox.navix.sh/stop-reason"

	// SandboxTerminatedAtAnnotationKey records the RFC3339 termination timestamp.
	SandboxTerminatedAtAnnotationKey = "agentbox.navix.sh/terminated-at"

	// SandboxFailureReasonAnnotationKey records the machine-readable failure cause.
	// e.g. "IdleTimeout", "OOMKilled", "Evicted"
	SandboxFailureReasonAnnotationKey = "agentbox.navix.sh/failure-reason"

	// SandboxFailureMessageAnnotationKey records the human-readable failure description.
	SandboxFailureMessageAnnotationKey = "agentbox.navix.sh/failure-message"

	// SandboxExitCodeAnnotationKey records the container exit code (decimal string).
	SandboxExitCodeAnnotationKey = "agentbox.navix.sh/exit-code"

	// SandboxRunningImagesAnnotationKey stores a JSON map[string]string of container
	// name → image captured at release time (before the idle image reset).
	SandboxRunningImagesAnnotationKey = "agentbox.navix.sh/running-images"

	// SandboxContainerIDAnnotationKey stores the runtime container ID (e.g.
	// "containerd://abc123…") captured at release time, before the in-place
	// update resets the pod to idle and clears StableContainerStatuses.
	SandboxContainerIDAnnotationKey = "agentbox.navix.sh/container-id"

	// SI Scheduler labels and annotations
	LabelTeam = "scheduling.navix.sh/team"
	LabelUser = "scheduling.navix.sh/user"

	// LabelEnv is stamped onto every member SandboxPool by the SandboxEnv
	// reconciler at materialisation time, with the owning Env's
	// metadata.name as value. Used by the Pool autoscaler to reverse-lookup
	// the owning Env (for reading scaling-group constraints) and to list
	// sibling Pools sharing the same Env without walking ownerReferences.
	LabelEnv = "agentbox.navix.sh/env"

	// SandboxTemplateDocsAnnotationKey stores Markdown documentation for the template.
	// Read by the dashboard to display a documentation sheet.
	SandboxTemplateDocsAnnotationKey = "agentbox.navix.sh/docs"

	// SandboxTemplatePoolDocsAnnotationKey is the legacy annotation for pool-specific usage docs.
	//
	// Deprecated: ignored by the server; use SandboxTemplateDocsAnnotationKey instead.
	SandboxTemplatePoolDocsAnnotationKey = "agentbox.navix.sh/pool-docs"

	// SandboxPoolTemplateNameAnnotationKey records the source SandboxTemplate name.
	SandboxPoolTemplateNameAnnotationKey = "agentbox.navix.sh/template-name"
	// SandboxPoolTemplateVersionAnnotationKey records the source SandboxTemplate version at creation time.
	SandboxPoolTemplateVersionAnnotationKey = "agentbox.navix.sh/template-version"
	// SandboxPoolOverridesAnnotationKey stores a JSON-encoded PoolTemplateOverrides
	// object so SyncTemplate can re-apply all pool-level overrides on top of newer
	// template revisions. A single blob avoids per-field annotation proliferation as
	// the override surface grows (image, resourceMultiplier, imagePullSecret, PVCs, …).
	SandboxPoolOverridesAnnotationKey = "agentbox.navix.sh/overrides"

	// SandboxProtectionFinalizer is added to every Pool-managed Pod at creation time,
	// and reconcile backfills it onto pre-existing Pods after upgrade.
	// It guarantees the controller sees a DeletionTimestamp window before the pod is GC'd,
	// allowing sandbox history records to be written even when a pod is deleted externally
	// (e.g. kubectl delete pod, kubelet eviction). Without this finalizer an external pod
	// deletion may race past the controller's reconcile loop, permanently losing the
	// sandbox history record and stop metrics.
	// The finalizer stays attached for the pod lifetime and is removed only when
	// the pod is actually being deleted:
	//   - syncDeletingPods after writing the terminal record for a terminating pod
	//   - syncFailedPods before explicitly deleting an evicted/failed pod
	//   - Controller scale-down, pool-deletion, and startup-timeout cleanup paths before Delete
	SandboxProtectionFinalizer = "agentbox.navix.sh/sandbox-protection"

	// SandboxPostStartHooksAnnotationKey stores JSON-encoded []PostStartHookAction.
	// Written at claim time when post-start hooks are requested (e.g. envd /init for env vars).
	// Consumed by the controller after Starting→Running; deleted on Stopping→Idle.
	SandboxPostStartHooksAnnotationKey = "agentbox.navix.sh/post-start-hooks"

	// SandboxScaleDownProtectedAnnotationKey is set on Idle Pods that have been
	// selected as scale-down candidates. The value is the RFC3339 timestamp when
	// the protection window started. Cleared if the Pod is claimed before deletion.
	SandboxScaleDownProtectedAnnotationKey = "agentbox.navix.sh/scale-down-protected"

	// LastSandboxCreateTimeAnnotationKey is the throttled persistent mirror
	// of the in-process LastCreateTracker: the most recent wall-clock time
	// the apiserver served a Sandbox.Create request for this Pool. Written
	// by a periodic flush (≈ every 5 s, only when the in-memory value
	// advanced past the last-flushed value) so high-QPS Create traffic
	// does not produce a per-request annotation patch.
	//
	// The Pool autoscaler reads this annotation as a fallback when the
	// in-process tracker is empty (e.g. shortly after a process restart);
	// the in-memory value always takes precedence when both exist.
	//
	// The value is RFC3339 UTC. Absence is treated as "never observed".
	LastSandboxCreateTimeAnnotationKey = "agentbox.navix.sh/last-sandbox-create-time"

	// LabelSyncSource marks the origin of a resource.
	// "global" means the resource was created/synced via ws-proxy (global key manager).
	// Resources without this label (locally-created or legacy) are treated as non-global.
	// Intentionally mirrors the constant in pkg/utils/apikey so that the api/v1alpha1 package
	// can be used as the canonical source for all agentbox label/annotation keys.
	LabelSyncSource = "agentbox.io/sync-source"
	// LabelSyncSourceGlobal is the value for LabelSyncSource that indicates a globally-managed resource.
	LabelSyncSourceGlobal = "global"

	// ImagePullSecretNamePrefix is prepended to a parent resource's name to
	// derive the deterministic dockerconfigjson Secret created alongside
	// it. Used by both the legacy SandboxPool Create flow (Secret owned by
	// the Pool) and the SandboxEnv flow (Secret owned by the Env and
	// referenced by every member Pool). The full name is
	// "ips-{ownerName}".
	ImagePullSecretNamePrefix = "ips-"
)

// EnvImagePullSecretName returns the deterministic Secret name for the
// dockerconfigjson Secret that backs an Env's overrides.imagePullSecret.
// One Secret per Env; the Env Reconciler stamps a LocalObjectReference
// for this name into every member Pool's spec.template.spec.imagePullSecrets.
func EnvImagePullSecretName(envName string) string {
	return ImagePullSecretNamePrefix + envName
}
