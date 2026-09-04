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

// Package poolrender produces a fully rendered SandboxPool CR from an Env +
// Member + SandboxTemplate triple. It is the single source of truth for the
// "what should the member Pool look like" projection.
//
// Both the Env Reconciler and the API service call RenderSandboxPool — that
// way the SandboxPool object handed to plugin admission at the API edge is
// byte-equal to the one the Reconciler eventually persists, modulo
// Reconciler-only side-effects (e.g. the dynamic image-pull-secret stamp
// based on Secret existence) that flow through ImagePullSecretExists.
package poolrender

import (
	"context"
	"errors"
	"fmt"
	"maps"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/controllers/sandboxpool"
	"github.com/scitix/agent-sandbox/pkg/metrics"
	"github.com/scitix/agent-sandbox/pkg/sandboxrender"
)

// Inputs carries everything RenderSandboxPool needs to project an Env +
// Member into a complete SandboxPool CR.
type Inputs struct {
	// Env is the owning SandboxEnv. Required. Contributes the namespace,
	// owner reference, team/user identity labels, and Spec.Overrides.
	Env *agentsv1alpha1.SandboxEnv
	// Template is the source SandboxTemplate (resolved from Env.Spec.TemplateRef.Name).
	// Required. Contributes EmbeddedSandboxTemplate plus labels/annotations
	// that get synced onto the Pool.
	Template *agentsv1alpha1.SandboxTemplate
	// Member is the EnvClusterMember being projected. Member.Name becomes
	// the Pool's metadata.name; Member.Labels/Annotations land on the Pool.
	Member agentsv1alpha1.EnvClusterMember
	// ImagePullSecretExists reports whether the convention-named
	// dockerconfigjson Secret (agentsv1alpha1.EnvImagePullSecretName(env.Name))
	// currently exists in the namespace. When true the renderer appends a
	// LocalObjectReference to that Secret onto the pod's imagePullSecrets;
	// when false an existing reference (if any) is removed so deletion of
	// the Env-managed Secret propagates cleanly.
	ImagePullSecretExists bool
	// ImageRegistry, when non-nil, enables per-cluster registry rewriting.
	// Whether the Template's own images are actually rewritten additionally
	// depends on the Template carrying the registry-rewrite annotation; the
	// Env's caller-supplied image override is rewritten whenever this is set.
	// Nil disables rewriting entirely (no --local-cluster-id, or no cluster
	// config).
	ImageRegistry *sandboxrender.RegistryRewrite
}

// RenderSandboxPool produces the complete SandboxPool CR a member should
// look like.
//
// The output carries:
//   - ObjectMeta: Name (=member.Name), Namespace (=env.Namespace), OwnerRef
//     (controlling, blockOwnerDeletion), team/user identity labels, the
//     subset of Template labels/annotations that survive the sync filter,
//     member-supplied labels/annotations, plus template-name and
//     template-version provenance annotations.
//   - Spec: Replicas (=member.Replicas), TemplateName, PodCreationImagePolicy
//     and the default startup/idle timeouts (all from env.Spec.Overrides),
//     and a fully rendered EmbeddedSandboxTemplate.
//
// Rendering follows v0.0.3 SandboxPoolService.Create semantics: copy
// EmbeddedSandboxTemplate → apply overrides (image, inline resources) →
// stamp the imagePullSecret reference → sync labels/annotations → write
// provenance.
//
// Errors are deterministic input-validation failures; callers wrap them
// into domain.AppError at the HTTP boundary.
func RenderSandboxPool(in Inputs) (*agentsv1alpha1.SandboxPool, error) {
	if in.Env == nil {
		return nil, errors.New("env is required")
	}
	if in.Template == nil {
		return nil, errors.New("template is required")
	}
	if in.Member.Name == "" {
		return nil, errors.New("member.name is empty")
	}

	emb := *in.Template.Spec.EmbeddedSandboxTemplate.DeepCopy()

	envOv := in.Env.Spec.Overrides
	opts := sandboxrender.Options{}
	if envOv != nil {
		opts.Image = envOv.Image
		opts.Volumes = envOv.Volumes
	}
	// Registry rewriting is available whenever the operator knows its own
	// cluster, but nothing is rewritten unless the Template opts in — including
	// the Env's own image override, because an Env may point it at another
	// region's registry precisely because that is where the image lives.
	opts.ImageRegistry = in.ImageRegistry
	opts.RewriteImages = agentsv1alpha1.BoolAnnotation(
		in.Template, agentsv1alpha1.RegistryRewriteAnnotationKey)
	// InlineResources is the renderer's source of truth for per-Pool
	// resource sizing in Phase 1. The API service stamps the InstanceType
	// catalog's resolved resources into Config.InlineResources before
	// calling Render so this path covers both the catalog and the legacy
	// inline-resources cases.
	if in.Member.Config.InlineResources != nil {
		opts.InlineResources = in.Member.Config.InlineResources
	}
	// Volume checks that need the resolved Template. Both the API-time freeze
	// and the reconcile-time refresh come through here, so a hand-edited CR is
	// held to the same rules as an API write.
	if len(opts.Volumes) > 0 {
		if err := agentsv1alpha1.ValidateVolumeMounts(opts.Volumes, &emb.Template.Spec); err != nil {
			return nil, fmt.Errorf("overrides.volumes: %w", err)
		}
		allowUnenforceable := agentsv1alpha1.BoolAnnotation(
			in.Template, agentsv1alpha1.AllowUnenforceableReadOnlyVolumesAnnotationKey)
		if err := agentsv1alpha1.ValidateReadOnlyEnforceable(
			opts.Volumes, &emb.Template.Spec, allowUnenforceable); err != nil {
			return nil, fmt.Errorf("overrides.volumes: %w", err)
		}
		// A template that opted out is knowingly serving a read-only mount it
		// cannot enforce. Never let that be silent.
		if allowUnenforceable {
			if defeats := agentsv1alpha1.ReadOnlyDefeatingFeatures(&emb.Template.Spec); len(defeats) > 0 {
				metrics.EnvVolumeReadOnlyUnenforceableTotal.
					WithLabelValues(in.Env.Namespace, in.Env.Name, defeats[0]).Inc()
			}
		}
	}
	metrics.EnvVolumeMounts.
		WithLabelValues(in.Env.Namespace, in.Env.Name).Set(float64(len(opts.Volumes)))
	if err := sandboxrender.Apply(&emb, opts); err != nil {
		return nil, fmt.Errorf("apply overrides: %w", err)
	}

	stampImagePullSecretRef(&emb, agentsv1alpha1.EnvImagePullSecretName(in.Env.Name), in.ImagePullSecretExists)

	labels := envIdentityLabels(in.Env)
	sandboxpool.SyncLabelsFromTemplate(labels, in.Template.Labels)
	maps.Copy(labels, in.Member.Config.Labels)

	annotations := map[string]string{}
	sandboxpool.SyncAnnotationsFromTemplate(annotations, in.Template.Annotations)
	maps.Copy(annotations, in.Member.Config.Annotations)
	// System-managed provenance keys are written last so a Template that
	// carries them in its own annotations cannot stomp them.
	annotations[agentsv1alpha1.SandboxPoolTemplateNameAnnotationKey] = in.Template.Name
	if in.Template.Spec.Version != "" {
		annotations[agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey] = in.Template.Spec.Version
	}

	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:            in.Member.Name,
			Namespace:       in.Env.Namespace,
			Labels:          labels,
			Annotations:     annotations,
			OwnerReferences: []metav1.OwnerReference{OwnerReferenceForEnv(in.Env)},
		},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			Replicas:                in.Member.Spec.Replicas,
			TemplateName:            in.Template.Name,
			EmbeddedSandboxTemplate: emb,
		},
	}
	if envOv != nil {
		pool.Spec.PodCreationImagePolicy = envOv.PodCreationImagePolicy
		pool.Spec.DefaultStartupTimeout = envOv.DefaultStartupTimeout
		pool.Spec.DefaultIdleTimeout = envOv.DefaultIdleTimeout
		pool.Spec.Gateway = envOv.Gateway.DeepCopy()
	}
	mu := agentsv1alpha1.ResolveMaxUnavailable(in.Env, in.Member)
	pool.Spec.MaxUnavailable = &mu

	// Stamp the revision hash last, once the spec is fully assembled. It is
	// written to both the pod-template labels (so it flows onto every Pod via
	// createPod's SyncLabelsFromTemplate) and the Pool-level labels (so it
	// survives createPod's pool-label overlay and shows up in `kubectl get
	// sbp -L`). Both carry the same value; ComputeRevisionHash ignores the hash
	// label itself, so this is not self-referential.
	h := agentsv1alpha1.ComputeRevisionHash(&pool.Spec)
	if pool.Spec.Template.Labels == nil {
		pool.Spec.Template.Labels = map[string]string{}
	}
	pool.Spec.Template.Labels[agentsv1alpha1.TemplateHashLabelKey] = h
	pool.Labels[agentsv1alpha1.TemplateHashLabelKey] = h
	return pool, nil
}

// Validate runs cross-field checks the rendered SandboxPool spec must
// satisfy. Idle image must differ from the runtime container image so the
// idle / running state machine can distinguish the two. Other CRD-level
// constraints (replica bounds, etc.) are enforced by OpenAPI / the Pool
// Reconciler.
func Validate(spec *agentsv1alpha1.SandboxPoolSpec) error {
	if spec == nil {
		return errors.New("spec is nil")
	}
	if spec.IdleImage == "" {
		return errors.New("idleImage is required")
	}
	if len(spec.Template.Spec.Containers) > 0 {
		if spec.IdleImage == spec.Template.Spec.Containers[0].Image {
			return fmt.Errorf("idleImage (%q) must differ from the container image (%q)",
				spec.IdleImage, spec.Template.Spec.Containers[0].Image)
		}
	}
	return nil
}

// MaterializeFromMember projects a frozen EnvClusterMember snapshot onto a
// fresh SandboxPool object. The Member's Metadata (sanitised at API time)
// and Spec are copied verbatim; the Pool's OwnerReference is stamped from
// the supplied Env; the dynamic ImagePullSecret reference is recomputed
// from the supplied existence flag so a Secret created or deleted after
// AddMember still propagates onto the Pool.
//
// LabelEnv is stamped unconditionally and overwrites any caller-supplied
// value — the Env reconciler is the authoritative source for that
// indexing label, and downstream consumers (e.g. the Pool autoscaler's
// listSiblings) rely on the label being present on every Env-owned
// Pool. Pools created before this stamping was introduced get the
// label added the next time updateMemberPoolIfDrifted runs against them.
//
// Unlike RenderSandboxPool this function does NOT consult the Template or
// run plugin admission — plugin side-effects already live inside
// Member.Metadata + Member.Spec by construction (AddMember captures them
// post-PreCreatePool). The Env Reconciler is the only intended caller.
func MaterializeFromMember(env *agentsv1alpha1.SandboxEnv, member agentsv1alpha1.EnvClusterMember, ipsExists bool) *agentsv1alpha1.SandboxPool {
	labels := copyMapNonNil(member.Metadata.Labels)
	labels[agentsv1alpha1.LabelEnv] = env.Name
	if member.Config.ScalingGroup != "" {
		labels[agentsv1alpha1.LabelScalingGroup] = member.Config.ScalingGroup
	}
	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:            member.Name,
			Namespace:       env.Namespace,
			Labels:          labels,
			Annotations:     copyMapNonNil(member.Metadata.Annotations),
			OwnerReferences: []metav1.OwnerReference{OwnerReferenceForEnv(env)},
		},
		Spec: *member.Spec.DeepCopy(),
	}
	stampImagePullSecretRef(&pool.Spec.EmbeddedSandboxTemplate, agentsv1alpha1.EnvImagePullSecretName(env.Name), ipsExists)
	return pool
}

func copyMapNonNil(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}

// OwnerReferenceForEnv is the canonical controlling OwnerReference stamped
// onto every member SandboxPool. Exported so direct test set-up can produce
// identical references.
func OwnerReferenceForEnv(env *agentsv1alpha1.SandboxEnv) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion:         agentsv1alpha1.GroupVersion.String(),
		Kind:               agentsv1alpha1.SandboxEnvOwnerKind,
		Name:               env.Name,
		UID:                env.UID,
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}
}

// ImagePullSecretExists is a small helper for callers that need to compute
// the ImagePullSecretExists bit before calling RenderSandboxPool. Treats
// transient lookup errors as "missing" so a flaky API server doesn't make
// the renderer stamp a stale reference; callers can ignore the error or
// log it.
func ImagePullSecretExists(ctx context.Context, c client.Client, namespace, secretName string) (bool, error) {
	if secretName == "" {
		return false, nil
	}
	secret := &corev1.Secret{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("lookup image pull secret %q: %w", secretName, err)
	}
	return true, nil
}

// stampImagePullSecretRef adds or removes a LocalObjectReference{Name: secretName}
// on emb.Template.Spec.ImagePullSecrets based on exists. Idempotent.
func stampImagePullSecretRef(emb *agentsv1alpha1.EmbeddedSandboxTemplate, secretName string, exists bool) {
	if emb == nil || secretName == "" {
		return
	}
	refs := emb.Template.Spec.ImagePullSecrets
	idx := -1
	for i, r := range refs {
		if r.Name == secretName {
			idx = i
			break
		}
	}
	switch {
	case exists && idx < 0:
		emb.Template.Spec.ImagePullSecrets = append(refs, corev1.LocalObjectReference{Name: secretName})
	case !exists && idx >= 0:
		emb.Template.Spec.ImagePullSecrets = append(refs[:idx], refs[idx+1:]...)
	}
}

// envIdentityLabels returns a fresh map of team/user identity labels from
// the Env. Mirrors the helper in poolsync.go but produces an empty map
// (never nil) so the caller can unconditionally write more keys into it.
func envIdentityLabels(env *agentsv1alpha1.SandboxEnv) map[string]string {
	out := map[string]string{}
	if env == nil {
		return out
	}
	if v, ok := env.Labels[agentsv1alpha1.LabelTeam]; ok && v != "" {
		out[agentsv1alpha1.LabelTeam] = v
	}
	if v, ok := env.Labels[agentsv1alpha1.LabelUser]; ok && v != "" {
		out[agentsv1alpha1.LabelUser] = v
	}
	return out
}

// MergeOwnedMapKeys upserts every entry in desired into *dst. Foreign keys
// already present in *dst are preserved — the Env Reconciler only manages
// keys it has been asked to set; kubectl edits to unrelated keys survive.
// Exported for use from the Reconciler drift-merge path.
func MergeOwnedMapKeys(dst *map[string]string, desired map[string]string) {
	if dst == nil || len(desired) == 0 {
		return
	}
	if *dst == nil {
		*dst = map[string]string{}
	}
	maps.Copy(*dst, desired)
}
