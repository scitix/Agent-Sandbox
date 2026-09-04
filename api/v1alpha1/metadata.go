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

import (
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

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

	// TemplateHashLabelKey carries the fnv32 revision hash of a Pool's
	// materialised idle-Pod identity (IdleImage + pod-spec body + NetworkPolicy
	// + template metadata; see ComputeRevisionHash). Stamped by the Env
	// renderer onto both SandboxPool.metadata.labels and
	// SandboxPool.spec.template.metadata.labels, from where it flows to every
	// Pod. The SandboxPool reconciler compares a Pod's value against the Pool
	// template's to decide which idle Pods are stale and must be rolled.
	TemplateHashLabelKey = "agentbox.navix.sh/template-hash"

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

	// SandboxEgressPolicyAnnotationKey carries the JSON-encoded effective egress
	// policy (pkg/egressproxy.Policy) resolved for a claimed sandbox: the merge
	// of the per-sandbox override and the Pool's Env-default networkPolicy. The
	// SandboxReady hook reads it and pushes it into the filter sidecar via exec.
	// Registered as a managed annotation key so it is stripped on release,
	// giving free reset-on-recycle.
	SandboxEgressPolicyAnnotationKey = "agentbox.navix.sh/egress-policy"

	// SandboxEgressInjectAnnotationKey carries the JSON-encoded credential
	// injection block (v1alpha1.SecretInjection) resolved for a claimed sandbox,
	// with per-claim placeholders filled in.
	//
	// It holds rule shapes, credential names, Secret references and decoys —
	// never a credential value. The SandboxReady hook resolves the referenced
	// Secrets and delivers the plaintext straight to the sidecar over exec, so
	// no credential is ever written to etcd or returned by the API. Registered
	// as a managed annotation key so release strips it.
	SandboxEgressInjectAnnotationKey = "agentbox.navix.sh/egress-inject"

	// SandboxArmedAnnotationKey marks a claimed sandbox as fully armed: its
	// runtimes answered their readiness probes, the post-start hooks ran (env
	// vars and, when injection is configured, the CA are in place), and the
	// egress policy and credentials were pushed into the filter sidecar. The
	// value is the sandbox ID, so a recycled Pod carrying a stale mark can never
	// be mistaken for an armed one.
	//
	// It is the single readiness judgement for a sandbox: the create path waits
	// for it before returning, and the data-plane router refuses to route to a
	// sandbox that does not carry it. Registered as a managed annotation key so
	// release strips it.
	SandboxArmedAnnotationKey = "agentbox.navix.sh/sandbox-armed"

	// SandboxArmErrorAnnotationKey records why arming failed, in place of
	// SandboxArmedAnnotationKey (the two are mutually exclusive). The create path
	// surfaces the reason to the caller instead of handing back a sandbox that
	// looks usable but has no env vars, no CA or no credentials.
	SandboxArmErrorAnnotationKey = "agentbox.navix.sh/sandbox-arm-error"

	// SI Scheduler labels and annotations
	LabelTeam = "scheduling.navix.sh/team"
	LabelUser = "scheduling.navix.sh/user"

	// AnnotationReservationReplicaQuota stores the per-replica reservation quota
	// as a JSON map[string]string of instancetype-name → whole-instance count,
	// e.g. {"sci.c23-2":"2"}. It is the bridge between the API server (which
	// derives sizing from EnvClusterMember.{InstanceType,Multiplier}) and the
	// closed-source SI Scheduler reservation plugin, which reads it to size the
	// reservation. When an InstanceType is used, the API server stamps the real
	// multiplier here so the reservation quota is charged per whole instance even
	// when the Pod's actual resource request is rounded down below the instance.
	//
	// The value MUST stay identical to the reservation plugin's own constant
	// (agentbox pkg/scitix/reservation/sischeduler.AnnotationReplicaQuota).
	AnnotationReservationReplicaQuota = "scheduling.navix.sh/reservation-replica-quota"

	// LabelEnv is stamped onto every member SandboxPool by the SandboxEnv
	// reconciler at materialisation time, with the owning Env's
	// metadata.name as value. Used by the Pool autoscaler to reverse-lookup
	// the owning Env (for reading scaling-group constraints) and to list
	// sibling Pools sharing the same Env without walking ownerReferences.
	LabelEnv = "agentbox.navix.sh/env"

	// LabelScalingGroup is stamped onto every member SandboxPool by the
	// SandboxEnv reconciler at materialisation time, carrying the member's
	// EnvClusterMember.Config.ScalingGroup. Members sharing a value belong to
	// the same Env autoscaling group. Surfaced on the gen.SandboxPool wire
	// shape so the dashboard can group Pools without re-reading the Env spec.
	// Absent when the member is excluded from autoscaling (empty ScalingGroup).
	LabelScalingGroup = "agentbox.navix.sh/scaling-group"

	// SandboxTemplateDocsAnnotationKey stores Markdown documentation for the template.
	// Read by the dashboard to display a documentation sheet.
	SandboxTemplateDocsAnnotationKey = "agentbox.navix.sh/docs"

	// SandboxTemplatePoolDocsAnnotationKey is the legacy annotation for pool-specific usage docs.
	//
	// Deprecated: ignored by the server; use SandboxTemplateDocsAnnotationKey instead.
	SandboxTemplatePoolDocsAnnotationKey = "agentbox.navix.sh/pool-docs"

	// RegistryRewriteAnnotationKey opts a SandboxTemplate into having its own
	// images (idleImage, containers, initContainers) rewritten to the registry
	// of whichever cluster the Pool is rendered for.
	//
	// Written by: the Template author (a human).
	// Read by: poolrender.RenderSandboxPool.
	// Format: a boolean literal parsable by strconv.ParseBool ("true", "1").
	// Absent: no rewriting of Template-owned images — the pre-existing
	// behaviour, where a Template authored against one region's registry keeps
	// that registry in every cluster it is broadcast to.
	// Unparseable: treated as false, logged at V(2), never an error.
	//
	// Opt-in rather than always-on because the rewrite is a bare host swap:
	// only the Template author knows whether the same repository path exists in
	// every region's mirror.
	RegistryRewriteAnnotationKey = "agentbox.navix.sh/registry-rewrite"

	// AllowUnenforceableReadOnlyVolumesAnnotationKey opts a SandboxTemplate out
	// of the check that refuses read-only Env volume mounts on a pod spec that
	// can defeat them (privileged, SYS_ADMIN, Bidirectional propagation,
	// hostPath, unmasked procMount).
	//
	// Written by: a cluster administrator. SandboxTemplate writes are
	// admin-scoped, which is the point: the person who chose `privileged: true`
	// is the person who may accept its consequence, not the user mounting the
	// dataset.
	// Read by: v1alpha1.ValidateReadOnlyEnforceable via its callers.
	// Format: a boolean literal parsable by strconv.ParseBool.
	// Absent: read-only mounts are refused on such pod specs.
	// Unparseable: treated as false (the safe direction).
	AllowUnenforceableReadOnlyVolumesAnnotationKey = "agentbox.navix.sh/allow-unenforceable-readonly-volumes"

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

// EnvSecretInjectionNamePrefix is prepended to an Env's name to form the Secret
// that holds its injected credentials — one Secret per Env, one key per
// credential (keyed by the credential's Name), not one Secret per credential.
const EnvSecretInjectionNamePrefix = "eis-"

// EnvSecretInjectionName returns the Secret backing an Env's declared
// credentials. Callers may also point a credential at a Secret of their own;
// this is only the one the platform materialises from values typed into the
// API, mirroring how imagePullSecret works.
func EnvSecretInjectionName(envName string) string {
	return EnvSecretInjectionNamePrefix + envName
}

// BoolAnnotation reads a boolean opt-in annotation off an object.
//
// It is deliberately fail-open in the "false" direction: a missing object, a
// missing key, or a value strconv.ParseBool rejects all yield false, and none
// of them is an error. Opt-in annotations in this repo gate behaviour that is
// safe to skip, so a typo must degrade to the pre-existing behaviour rather
// than fail a request.
func BoolAnnotation(obj metav1.Object, key string) bool {
	if obj == nil {
		return false
	}
	raw, ok := obj.GetAnnotations()[key]
	if !ok {
		return false
	}
	v, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		klog.V(2).InfoS("ignoring unparseable boolean annotation",
			"key", key, "value", raw, "object", obj.GetName())
		return false
	}
	return v
}
