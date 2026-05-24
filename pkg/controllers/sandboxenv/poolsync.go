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

package sandboxenv

import (
	"context"
	"fmt"
	"maps"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/sandboxrender"
)

// desiredPool captures everything the Env Reconciler wants to be true about a
// single member Pool. Stays internal — the persistence shape on the Pool is
// determined by the helpers below (labels / annotations / spec fields).
type desiredPool struct {
	// Name is the Pool's metadata.name.
	Name string
	// Labels / Annotations are the metadata projected onto the Pool. Built
	// by merging Env-level identity labels (team / user) with the
	// per-member Labels / Annotations declared in the Env spec.
	Labels      map[string]string
	Annotations map[string]string
	// TemplateName is taken from env.Spec.TemplateRef.Name.
	TemplateName string
	// Replicas is the initial replica count taken from member.Replicas.
	// NOT forced on subsequent reconciles — the autoscaler owns replica
	// state from there on.
	Replicas int32
	// PodCreationImagePolicy / DefaultStartupTimeout / DefaultIdleTimeout
	// come from env.Spec.Overrides (Env-uniform).
	PodCreationImagePolicy agentsv1alpha1.PodCreationImagePolicy
	DefaultStartupTimeout  *metav1.Duration
	DefaultIdleTimeout     *metav1.Duration
	// RenderOptions is the composed image+resourceMultiplier input passed
	// to sandboxrender.Apply: image comes from the Env-level overrides,
	// resourceMultiplier from this Member's overrides.
	RenderOptions sandboxrender.Options
}

// buildDesiredPools is the single source of truth for the desired member-Pool
// set given an Env spec. Deterministic in the input — repeated calls produce
// the same map, which keeps the reconcile loop idempotent.
//
// Membership comes from env.Spec.Clusters[localClusterID].Members — there is
// no quota / autoscaler magic here, the Reconciler just stamps whatever the
// CRD says. Plugin-relevant metadata (quota URLs, reservation hints, …)
// rides in each member's Labels / Annotations and lands verbatim on the
// generated Pool.
//
// Naming rule: each member.Name maps to one Pool of the same name. When the
// Env has no members (e.g. created via kubectl with only TemplateRef), the
// helper falls back to a single Pool named exactly env.Name — preserving the
// legacy single-pool shape the Phase 1 adopter produces.
func buildDesiredPools(env *agentsv1alpha1.SandboxEnv, localClusterID string) map[string]desiredPool {
	out := map[string]desiredPool{}
	if env == nil {
		return out
	}

	envOv := env.Spec.Overrides
	base := desiredPool{
		TemplateName:           env.Spec.TemplateRef.Name,
		PodCreationImagePolicy: envOverridePolicy(envOv),
		DefaultStartupTimeout:  envOverrideStartup(envOv),
		DefaultIdleTimeout:     envOverrideIdle(envOv),
	}

	members := localMembers(env, localClusterID)
	if len(members) == 0 {
		d := base
		d.Name = env.Name
		d.Labels = envIdentityLabels(env)
		d.RenderOptions = composeRenderOptions(envOv, nil)
		out[env.Name] = d
		return out
	}

	for _, m := range members {
		d := base
		d.Name = m.Name
		d.Labels = mergeStringMaps(envIdentityLabels(env), m.Labels)
		d.Annotations = mergeStringMaps(nil, m.Annotations)
		d.Replicas = m.Replicas
		d.RenderOptions = composeRenderOptions(envOv, &m)
		out[m.Name] = d
	}
	return out
}

// composeRenderOptions builds the sandboxrender.Apply input by layering
// Env-level overrides (image) and Member-level resource sizing
// (InlineResources). InstanceType + Multiplier from the catalog will land
// here once the catalog provider is wired into the renderer.
func composeRenderOptions(envOv *agentsv1alpha1.EnvOverridesSpec, member *agentsv1alpha1.EnvClusterMember) sandboxrender.Options {
	var opts sandboxrender.Options
	if envOv != nil {
		opts.Image = envOv.Image
	}
	if member != nil && member.InlineResources != nil {
		opts.InlineResources = member.InlineResources
	}
	return opts
}

func envOverridePolicy(o *agentsv1alpha1.EnvOverridesSpec) agentsv1alpha1.PodCreationImagePolicy {
	if o == nil {
		return ""
	}
	return o.PodCreationImagePolicy
}

func envOverrideStartup(o *agentsv1alpha1.EnvOverridesSpec) *metav1.Duration {
	if o == nil {
		return nil
	}
	return o.DefaultStartupTimeout
}

func envOverrideIdle(o *agentsv1alpha1.EnvOverridesSpec) *metav1.Duration {
	if o == nil {
		return nil
	}
	return o.DefaultIdleTimeout
}

// localMembers returns the Members slice from the cluster segment matching
// localClusterID. Returns nil when no such segment exists. Mirrors
// findLocalClusterSpec but exposes the underlying slice (read-only).
func localMembers(env *agentsv1alpha1.SandboxEnv, localClusterID string) []agentsv1alpha1.EnvClusterMember {
	if env == nil || localClusterID == "" {
		return nil
	}
	for i := range env.Spec.Clusters {
		if env.Spec.Clusters[i].ClusterID == localClusterID {
			return env.Spec.Clusters[i].Members
		}
	}
	return nil
}

// envIdentityLabels copies the team / user labels off the Env so every member
// Pool can be filtered by the same selectors the Pool list API uses today.
// Returns nil when neither label is set so the caller can decide whether to
// emit an empty map.
func envIdentityLabels(env *agentsv1alpha1.SandboxEnv) map[string]string {
	if env == nil {
		return nil
	}
	out := map[string]string{}
	if v, ok := env.Labels[agentsv1alpha1.LabelTeam]; ok && v != "" {
		out[agentsv1alpha1.LabelTeam] = v
	}
	if v, ok := env.Labels[agentsv1alpha1.LabelUser]; ok && v != "" {
		out[agentsv1alpha1.LabelUser] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// mergeStringMaps returns a fresh map containing every entry from base
// overridden by the corresponding entry in overlay. nil + nil → nil so the
// caller can keep its Pool.metadata clean.
func mergeStringMaps(base, overlay map[string]string) map[string]string {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(overlay))
	maps.Copy(out, base)
	maps.Copy(out, overlay)
	return out
}

// reconcilePools brings the live member-Pool set in env's namespace into
// alignment with buildDesiredPools(env). Idempotent:
//
//   - Pools in the desired set that don't exist are created.
//   - Pools that exist and drifted from the desired labels / annotations /
//     overrides / pod-creation-policy / default-timeouts are updated.
//     Replicas is intentionally NOT forced — the autoscaler owns that.
//   - Pools owned by this Env but not in the desired set are deleted (the
//     user removed the member from spec.clusters[local].members).
//
// Always runs — no QuotaProvider gate. When the Env spec carries no members,
// the helper materialises a single namesake Pool to preserve the legacy
// adopter shape.
func (r *SandboxEnvReconciler) reconcilePools(ctx context.Context, env *agentsv1alpha1.SandboxEnv) error {
	if env == nil || env.DeletionTimestamp != nil {
		return nil
	}
	log := klog.FromContext(ctx).WithValues("env", env.Namespace+"/"+env.Name)

	desired := buildDesiredPools(env, r.LocalClusterID)
	live, err := r.listOwnedPools(ctx, env)
	if err != nil {
		return err
	}

	for name, d := range desired {
		existing, found := live[name]
		if !found {
			if err := r.createMemberPool(ctx, env, d); err != nil {
				return fmt.Errorf("create pool %q: %w", name, err)
			}
			log.Info("Created member SandboxPool", "pool", name)
			continue
		}
		if err := r.updateMemberPoolIfDrifted(ctx, existing, d); err != nil {
			return fmt.Errorf("update pool %q: %w", name, err)
		}
	}

	for name, pool := range live {
		if _, keep := desired[name]; keep {
			continue
		}
		if err := r.Delete(ctx, pool); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete obsolete pool %q: %w", name, err)
		}
		log.Info("Deleted obsolete member SandboxPool", "pool", name)
	}
	return nil
}

// listOwnedPools returns every SandboxPool in env.Namespace whose
// OwnerReferences include this Env, keyed by Pool name. Stale refs (UID
// mismatch) are excluded — the OwnerRef will be re-stamped on the next
// adoption pass.
func (r *SandboxEnvReconciler) listOwnedPools(ctx context.Context, env *agentsv1alpha1.SandboxEnv) (map[string]*agentsv1alpha1.SandboxPool, error) {
	pools := &agentsv1alpha1.SandboxPoolList{}
	if err := r.List(ctx, pools, client.InNamespace(env.Namespace)); err != nil {
		return nil, err
	}
	out := make(map[string]*agentsv1alpha1.SandboxPool)
	for i := range pools.Items {
		p := &pools.Items[i]
		for _, ref := range p.OwnerReferences {
			if ref.Kind != agentsv1alpha1.SandboxEnvOwnerKind {
				continue
			}
			if ref.UID == env.UID && ref.Name == env.Name {
				out[p.Name] = p
				break
			}
		}
	}
	return out, nil
}

// createMemberPool fetches the linked SandboxTemplate, renders the embedded
// pod spec with env.Spec.Overrides applied, and persists the fully rendered
// Pool. Stamps the controlling OwnerReference so cascade-delete works and
// records template provenance via the template-name / -version annotations
// so the Env-level sync-template endpoint can advance them later.
func (r *SandboxEnvReconciler) createMemberPool(ctx context.Context, env *agentsv1alpha1.SandboxEnv, d desiredPool) error {
	tmpl, err := r.fetchTemplate(ctx, d.TemplateName)
	if err != nil {
		return err
	}
	spec, err := renderPoolSpec(tmpl, d)
	if err != nil {
		return err
	}
	if err := r.stampEnvImagePullSecret(ctx, env, &spec); err != nil {
		return err
	}
	annotations := mergeStringMaps(d.Annotations, templateProvenance(tmpl))
	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:            d.Name,
			Namespace:       env.Namespace,
			Labels:          d.Labels,
			Annotations:     annotations,
			OwnerReferences: []metav1.OwnerReference{ownerReferenceForEnv(env)},
		},
		Spec: spec,
	}
	if err := r.Create(ctx, pool); err != nil {
		if apierrors.IsAlreadyExists(err) {
			// Race: someone else created it between List and Create. Treat
			// as success — the next reconcile picks it up via the listing.
			return nil
		}
		return err
	}
	return nil
}

// updateMemberPoolIfDrifted patches a live Pool when its labels, annotations,
// PodCreationImagePolicy, default timeouts, or rendered pod spec drift from
// the Env's desired state.
//
// Drift handling:
//   - Labels / Annotations from member.Labels/Annotations: merged into the
//     Pool. Foreign keys set by other actors (kubectl, ad-hoc scripts) are
//     preserved — the Reconciler only touches keys it has been asked to set.
//   - Overrides drift (env.Spec.Overrides changed): re-render the embedded
//     pod spec using the Pool's pinned template version. Template body
//     changes are NOT auto-applied; those require an explicit
//     `POST /envs/{name}/sync-template` (see envSyncMemberTemplate).
//   - Replicas is never forced — the autoscaler owns it.
func (r *SandboxEnvReconciler) updateMemberPoolIfDrifted(ctx context.Context, pool *agentsv1alpha1.SandboxPool, d desiredPool) error {
	labelDrift := mapsDifferOnKeys(pool.Labels, d.Labels)
	annotationDrift := mapsDifferOnKeys(pool.Annotations, d.Annotations)
	policyDrift := pool.Spec.PodCreationImagePolicy != d.PodCreationImagePolicy && d.PodCreationImagePolicy != ""
	startupDrift := !durationsEqual(pool.Spec.DefaultStartupTimeout, d.DefaultStartupTimeout)
	idleDrift := !durationsEqual(pool.Spec.DefaultIdleTimeout, d.DefaultIdleTimeout)

	// Decide whether to re-render the embedded pod spec. Skipped when the
	// Pool's pinned template version doesn't match the current Template —
	// that case requires the explicit sync-template flow.
	var renderedEmbedded *agentsv1alpha1.EmbeddedSandboxTemplate
	tmpl, err := r.fetchTemplateIfPinned(ctx, pool, d.TemplateName)
	if err != nil {
		return err
	}
	if tmpl != nil {
		emb := tmpl.Spec.EmbeddedSandboxTemplate
		if err := sandboxrender.Apply(&emb, d.RenderOptions); err != nil {
			return fmt.Errorf("render overrides: %w", err)
		}
		// Stamp the Env-level ImagePullSecret onto the freshly rendered pod
		// template before drift comparison so a missing reference also
		// counts as drift.
		if err := stampImagePullSecretOnEmbedded(ctx, r.Client, pool.Namespace, agentsv1alpha1.EnvImagePullSecretName(envNameFromOwner(pool)), &emb); err != nil {
			return err
		}
		renderedEmbedded = &emb
	}
	embeddedDrift := renderedEmbedded != nil && !embeddedTemplateEqual(pool.Spec.EmbeddedSandboxTemplate, *renderedEmbedded)

	if !labelDrift && !annotationDrift && !policyDrift && !startupDrift && !idleDrift && !embeddedDrift {
		return nil
	}

	key := types.NamespacedName{Namespace: pool.Namespace, Name: pool.Name}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &agentsv1alpha1.SandboxPool{}
		if err := r.Get(ctx, key, current); err != nil {
			return err
		}
		base := current.DeepCopy()
		mergeOwnedMapKeys(&current.Labels, d.Labels)
		mergeOwnedMapKeys(&current.Annotations, d.Annotations)
		if d.PodCreationImagePolicy != "" {
			current.Spec.PodCreationImagePolicy = d.PodCreationImagePolicy
		}
		current.Spec.DefaultStartupTimeout = d.DefaultStartupTimeout
		current.Spec.DefaultIdleTimeout = d.DefaultIdleTimeout
		if renderedEmbedded != nil {
			current.Spec.EmbeddedSandboxTemplate = *renderedEmbedded
		}
		return r.Patch(ctx, current, client.MergeFrom(base))
	})
}

// fetchTemplate gets a SandboxTemplate by name; friendly errors on missing.
func (r *SandboxEnvReconciler) fetchTemplate(ctx context.Context, name string) (*agentsv1alpha1.SandboxTemplate, error) {
	if name == "" {
		return nil, fmt.Errorf("env.spec.templateRef.name is empty")
	}
	tmpl := &agentsv1alpha1.SandboxTemplate{}
	if err := r.Get(ctx, types.NamespacedName{Name: name}, tmpl); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("source template %q not found", name)
		}
		return nil, err
	}
	return tmpl, nil
}

// fetchTemplateIfPinned returns the source Template when the Pool's pinned
// template-version annotation still matches the current Template's
// spec.version, otherwise nil. A nil result means "the user must explicitly
// run sync-template before we re-render". A missing pin (no annotation) is
// treated as matching — the Pool was just created or migrated and the next
// render establishes the baseline.
func (r *SandboxEnvReconciler) fetchTemplateIfPinned(ctx context.Context, pool *agentsv1alpha1.SandboxPool, templateName string) (*agentsv1alpha1.SandboxTemplate, error) {
	if templateName == "" {
		return nil, nil
	}
	tmpl, err := r.fetchTemplate(ctx, templateName)
	if err != nil {
		return nil, err
	}
	pin, hasPin := pool.Annotations[agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey]
	if hasPin && pin != "" && pin != tmpl.Spec.Version {
		return nil, nil
	}
	return tmpl, nil
}

// renderPoolSpec produces a SandboxPoolSpec for a fresh member pool given
// the source Template and the desired Env-level settings.
func renderPoolSpec(tmpl *agentsv1alpha1.SandboxTemplate, d desiredPool) (agentsv1alpha1.SandboxPoolSpec, error) {
	emb := tmpl.Spec.EmbeddedSandboxTemplate
	if err := sandboxrender.Apply(&emb, d.RenderOptions); err != nil {
		return agentsv1alpha1.SandboxPoolSpec{}, fmt.Errorf("render overrides: %w", err)
	}
	return agentsv1alpha1.SandboxPoolSpec{
		Replicas:                d.Replicas,
		TemplateName:            tmpl.Name,
		PodCreationImagePolicy:  d.PodCreationImagePolicy,
		DefaultStartupTimeout:   d.DefaultStartupTimeout,
		DefaultIdleTimeout:      d.DefaultIdleTimeout,
		EmbeddedSandboxTemplate: emb,
	}, nil
}

// templateProvenance returns the template-name / -version annotations the
// Env Reconciler stamps on every member Pool so the sync-template endpoint
// can advance the version later.
func templateProvenance(tmpl *agentsv1alpha1.SandboxTemplate) map[string]string {
	if tmpl == nil {
		return nil
	}
	out := map[string]string{
		agentsv1alpha1.SandboxPoolTemplateNameAnnotationKey: tmpl.Name,
	}
	if tmpl.Spec.Version != "" {
		out[agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey] = tmpl.Spec.Version
	}
	return out
}

// embeddedTemplateEqual compares two EmbeddedSandboxTemplates structurally
// so the drift check can skip Patch calls when the rendered spec is
// unchanged.
func embeddedTemplateEqual(a, b agentsv1alpha1.EmbeddedSandboxTemplate) bool {
	return equality.Semantic.DeepEqual(a, b)
}

// mergeOwnedMapKeys upserts every entry in desired into *dst. Foreign keys
// already present in *dst are preserved — the Env Reconciler only manages
// keys it has been asked to set; kubectl edits to unrelated keys survive.
func mergeOwnedMapKeys(dst *map[string]string, desired map[string]string) {
	if dst == nil || len(desired) == 0 {
		return
	}
	if *dst == nil {
		*dst = map[string]string{}
	}
	maps.Copy(*dst, desired)
}

// mapsDifferOnKeys returns true when any key in desired is missing from
// live or has a different value. Foreign keys on live are ignored — they
// are not Env-managed.
func mapsDifferOnKeys(live, desired map[string]string) bool {
	for k, v := range desired {
		if live[k] != v {
			return true
		}
	}
	return false
}

// ownerReferenceForEnv mirrors the poolmigration adopter's controlling owner
// reference so all Pool→Env stamps in the cluster converge to the same shape.
func ownerReferenceForEnv(env *agentsv1alpha1.SandboxEnv) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion:         agentsv1alpha1.GroupVersion.String(),
		Kind:               agentsv1alpha1.SandboxEnvOwnerKind,
		Name:               env.Name,
		UID:                env.UID,
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}
}

// stampEnvImagePullSecret appends a LocalObjectReference to the Env-managed
// dockerconfigjson Secret (ips-{envName}) onto the rendered Pool spec's
// imagePullSecrets list — but only when that Secret actually exists in the
// namespace. Idempotent: a duplicate reference is skipped.
//
// Called from createMemberPool against a freshly rendered SandboxPoolSpec;
// updateMemberPoolIfDrifted re-stamps via stampImagePullSecretOnEmbedded
// against the rendered EmbeddedSandboxTemplate.
func (r *SandboxEnvReconciler) stampEnvImagePullSecret(
	ctx context.Context,
	env *agentsv1alpha1.SandboxEnv,
	spec *agentsv1alpha1.SandboxPoolSpec,
) error {
	if spec == nil {
		return nil
	}
	return stampImagePullSecretOnEmbedded(ctx, r.Client, env.Namespace, agentsv1alpha1.EnvImagePullSecretName(env.Name), &spec.EmbeddedSandboxTemplate)
}

// stampImagePullSecretOnEmbedded mutates emb.Template.Spec.ImagePullSecrets
// to include {Name: secretName} when the Secret actually exists in the
// given namespace. Missing Secret = no-op (also clears any existing
// reference to that exact name, so removing the Env's ImagePullSecret
// propagates cleanly).
func stampImagePullSecretOnEmbedded(
	ctx context.Context,
	c client.Client,
	namespace, secretName string,
	emb *agentsv1alpha1.EmbeddedSandboxTemplate,
) error {
	if emb == nil || emb.Template == nil {
		return nil
	}
	secret := &corev1.Secret{}
	err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, secret)
	want := err == nil
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("lookup image pull secret %q: %w", secretName, err)
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
	case want && idx < 0:
		emb.Template.Spec.ImagePullSecrets = append(refs, corev1.LocalObjectReference{Name: secretName})
	case !want && idx >= 0:
		emb.Template.Spec.ImagePullSecrets = append(refs[:idx], refs[idx+1:]...)
	}
	return nil
}

// envNameFromOwner returns the SandboxEnv's name from a Pool's
// OwnerReferences. Returns "" when the Pool is not owned by an Env (e.g.
// during the Phase 1 adoption window before the OwnerRef lands).
func envNameFromOwner(pool *agentsv1alpha1.SandboxPool) string {
	for _, ref := range pool.OwnerReferences {
		if ref.Kind == agentsv1alpha1.SandboxEnvOwnerKind {
			return ref.Name
		}
	}
	return ""
}

// durationsEqual compares two *metav1.Duration pointers as values, treating
// nil and the zero duration as distinct (because Pool.Spec uses nil to mean
// "unset" but a 0s duration is a meaningful explicit choice).
func durationsEqual(a, b *metav1.Duration) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Duration == b.Duration
}
