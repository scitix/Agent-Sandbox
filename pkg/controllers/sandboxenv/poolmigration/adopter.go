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

// Package poolmigration owns the steady-state sync that keeps legacy
// (same-name) SandboxPool ↔ SandboxEnv pairs aligned.
//
// Two populations of SandboxPool coexist in the cluster:
//
//   - Legacy / orphan Pools: created directly as a SandboxPool CR (no
//     owning SandboxEnv). This reconciler wraps each one in a same-name
//     SandboxEnv and keeps the Env's local-cluster member entry consistent
//     with the live Pool on every reconcile.
//
//   - Env-managed Pools: created by the SandboxEnv reconciler from a
//     member entry. Their owning Env has a *different* name than the Pool
//     (Phase-2 onwards). This reconciler intentionally leaves them alone —
//     their Member.Spec is the post-PreCreatePool frozen snapshot and
//     must not be re-derived here.
//
// The legacy population is identified by the absence of any OwnerReference
// to a different-named SandboxEnv. For each such Pool the reconciler:
//
//  1. Looks up — or creates — the same-name SandboxEnv.
//  2. Builds a desired EnvClusterMember from the live Pool (and, when the
//     Pool only carries TemplateName, by resolving that SandboxTemplate).
//  3. Patches Member.Spec / Member.Config drift into the Env. Once a
//     Member.Spec field is non-empty it is treated as frozen ("fill once")
//     so SandboxTemplate upgrades do not auto-propagate to legacy Pools;
//     adopter-owned Config fields (ScalingGroup / InstanceType / Multiplier
//     / InlineResources) are re-derived on every pass.
//  4. Stamps a non-controlling OwnerReference from the Pool to the Env.
//
// Removal path: once every legacy Pool has been migrated to a Phase-2
// member, the reconciler becomes a no-op and this package can be deleted.
package poolmigration

import (
	"context"
	"fmt"
	"maps"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlbuilder "sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/framework/providers/instancetype"
)

// fallbackScalingGroup names the autoscaling group used when the
// InstanceType provider can't supply a derived name (e.g. zero-resources
// pool, provider returns the canonical "default" string). Kept in sync
// with sandboxenv.defaultScalingGroup (intentionally duplicated to avoid an
// upward import cycle).
const fallbackScalingGroup = "default"

// PoolAdoptionReconciler keeps every legacy same-name SandboxPool ↔
// SandboxEnv pair in sync. It watches SandboxPool only — Env updates do
// not enqueue work here because the desired Member is derived from the
// Pool, not the Env.
type PoolAdoptionReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// LocalClusterID identifies the cluster the Pool's member entry belongs to.
	// Defaults to "local" in SetupWithManager when empty.
	LocalClusterID string

	// InstanceTypes is the catalog used to round-trip a Pool's PodSpec
	// resources back to (InstanceType, Multiplier). A nil or Disabled provider
	// causes adoption to fall back to member.InlineResources.
	InstanceTypes instancetype.Provider
}

// +kubebuilder:rbac:groups=agents.navix.sh,resources=sandboxenvs,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=agents.navix.sh,resources=sandboxpools,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=agents.navix.sh,resources=sandboxtemplates,verbs=get;list;watch

// Reconcile owns one Pool at a time.
//
// Steps:
//
//  1. Early-exit when the Pool is Env-managed (an OwnerReference points at
//     a SandboxEnv whose name differs from the Pool's name). Phase-2 Pools
//     do not participate in legacy adoption.
//
//  2. Look up the same-name SandboxEnv; create it when missing.
//
//  3. Compose the desired Member from the live Pool (resolving the
//     SandboxTemplate the first time the Member's Spec.Template is empty)
//     and patch any drift back onto the Env.
//
//  4. Stamp the OwnerReference from Pool → Env.
//
// Steps (2)–(4) are independently idempotent so a Reconcile that crashes
// mid-way heals on the next pass.
func (r *PoolAdoptionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	pool := &agentsv1alpha1.SandboxPool{}
	if err := r.Get(ctx, req.NamespacedName, pool); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if pool.DeletionTimestamp != nil {
		return ctrl.Result{}, nil
	}

	// Phase-2 early-exit: Env-managed Pool (owned by a SandboxEnv with a
	// different name). Adopter must not re-derive its Member snapshot.
	if isEnvManagedPool(pool) {
		return ctrl.Result{}, nil
	}

	envName := pool.Name
	env := &agentsv1alpha1.SandboxEnv{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: pool.Namespace, Name: envName}, env); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		if err := r.createEnvForPool(ctx, pool); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, err
		}
		// Re-fetch on next pass so we work against the persisted Env (UID etc).
		return ctrl.Result{Requeue: true}, nil
	}

	if err := r.syncMember(ctx, pool, env); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	if err := r.ensureEnvOwnerReference(ctx, pool, env); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager wires the reconciler with a SandboxPool primary watch.
// No secondary watches: an Env edit (e.g. an admin tweaking the Env's
// autoscaling group) is not adoption-relevant.
func (r *PoolAdoptionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.LocalClusterID == "" {
		r.LocalClusterID = "local"
	}
	if r.InstanceTypes == nil {
		r.InstanceTypes = instancetype.NewNoop()
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named("pool-adoption").
		For(&agentsv1alpha1.SandboxPool{}, ctrlbuilder.WithPredicates(predicate.Or(
			// Spec changes — e.g. autoscaling tweaks on an unadopted Pool.
			predicate.GenerationChangedPredicate{},
			// Label edits — e.g. operator adds team/user labels we want to
			// propagate onto the Env on a re-adoption.
			predicate.LabelChangedPredicate{},
			// OwnerReference edits — primary signal that adoption state may
			// have changed (e.g. user deleted the Env so the owner ref dangles).
			ownerRefChangedPredicate{},
		))).
		Complete(r)
}

// ownerRefChangedPredicate fires when the OwnerReferences slice on a Pool
// changes. controller-runtime ships GenerationChangedPredicate (spec) and
// LabelChangedPredicate but no built-in for owner refs.
type ownerRefChangedPredicate struct {
	predicate.Funcs
}

func (ownerRefChangedPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return false
	}
	return !sameOwnerRefs(e.ObjectOld.GetOwnerReferences(), e.ObjectNew.GetOwnerReferences())
}

func sameOwnerRefs(a, b []metav1.OwnerReference) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].UID != b[i].UID ||
			a[i].Kind != b[i].Kind ||
			a[i].Name != b[i].Name ||
			a[i].APIVersion != b[i].APIVersion {
			return false
		}
	}
	return true
}

// isEnvManagedPool returns true when the Pool already has an
// OwnerReference to a SandboxEnv whose name differs from the Pool's own
// name — the unambiguous signal that the Pool was materialised by the
// SandboxEnv reconciler from a member entry, not by direct CR creation.
//
// Stale same-name owner refs (Env deleted) are treated as "still legacy"
// and fall through to the normal adoption flow: the reconciler will
// re-create the Env and re-stamp the ref.
func isEnvManagedPool(pool *agentsv1alpha1.SandboxPool) bool {
	for _, ref := range pool.OwnerReferences {
		if ref.Kind == agentsv1alpha1.SandboxEnvOwnerKind && ref.Name != pool.Name {
			return true
		}
	}
	return false
}

// createEnvForPool builds a new SandboxEnv that wraps the orphan Pool as
// its sole local-cluster member. The Env is created in one shot so the
// next reconcile sees a fully-populated object; a stale read on the next
// pass triggers nothing because the desired member equals the one we just
// persisted.
func (r *PoolAdoptionReconciler) createEnvForPool(ctx context.Context, pool *agentsv1alpha1.SandboxPool) error {
	member, _, err := r.composeDesiredMember(ctx, pool, nil)
	if err != nil {
		return fmt.Errorf("compose member: %w", err)
	}
	env := buildEnvFromPool(pool, member, r.LocalClusterID)
	if err := r.Create(ctx, env); err != nil {
		return err
	}
	klog.FromContext(ctx).Info("Created SandboxEnv from orphan Pool",
		"env", env.Namespace+"/"+env.Name)
	return nil
}

// syncMember computes the desired Member from the live Pool, patches the
// Env when it differs from what's stored, and stamps the template-version
// annotation onto the Pool the first time we resolve a SandboxTemplate to
// fill the Member's Spec.Template.
func (r *PoolAdoptionReconciler) syncMember(ctx context.Context, pool *agentsv1alpha1.SandboxPool, env *agentsv1alpha1.SandboxEnv) error {
	existing := findMember(env, r.LocalClusterID, pool.Name)
	desired, resolvedTmpl, err := r.composeDesiredMember(ctx, pool, existing)
	if err != nil {
		return err
	}

	if existing == nil || !equality.Semantic.DeepEqual(*existing, desired) {
		base := env.DeepCopy()
		mutateLocalClusterSpec(env, r.LocalClusterID, func(local *agentsv1alpha1.EnvClusterSpec) {
			idx := memberIndex(local.Members, pool.Name)
			if idx < 0 {
				local.Members = append(local.Members, desired)
			} else {
				local.Members[idx] = desired
			}
		})
		if err := r.Patch(ctx, env, client.MergeFrom(base)); err != nil {
			return err
		}
		klog.FromContext(ctx).V(1).Info("Synced legacy member onto Env",
			"env", env.Namespace+"/"+env.Name, "member", pool.Name)
	}

	// Stamp template-version provenance on the Pool the first time we
	// resolved a SandboxTemplate to fill the Member.Spec.Template. This
	// is the version anchor that keeps "Template upgrades do not
	// auto-propagate" honest: once the annotation is set the next
	// composeDesiredMember sees a non-empty Member.Spec.Template and
	// short-circuits the sbt fetch, so a later sbt edit doesn't drift the
	// Member.
	if resolvedTmpl != nil {
		if err := r.stampTemplateProvenance(ctx, pool, resolvedTmpl); err != nil {
			return err
		}
	}
	return nil
}

// composeDesiredMember produces the EnvClusterMember the Env should carry
// for this Pool. Mutation policy follows the two-bucket split:
//
//   - Member.Metadata + Member.Spec are *frozen snapshots*: a field is
//     filled exactly once (when the Member is first created or when a
//     pre-existing Member is observed to have it empty) and never
//     overwritten afterwards. This is what makes "Template upgrades do
//     not auto-propagate to legacy Pools" hold.
//
//   - Member.Config carries adopter-derived intent. ScalingGroup /
//     InstanceType / Multiplier / InlineResources are re-derived every
//     pass and overwrite the stored value when the derivation differs.
//     User-supplied Config fields (Labels, Annotations, MaxReplicas,
//     priorities) are preserved.
//
// When the Pool only carries TemplateName (no inline Template) and the
// existing Member.Spec.Template is empty, the function fetches the
// referenced SandboxTemplate and returns it as the second value so the
// caller can stamp the template-version annotation onto the Pool. The
// returned template is nil whenever no fetch happened (sbt unchanged or
// not needed).
func (r *PoolAdoptionReconciler) composeDesiredMember(
	ctx context.Context,
	pool *agentsv1alpha1.SandboxPool,
	existing *agentsv1alpha1.EnvClusterMember,
) (agentsv1alpha1.EnvClusterMember, *agentsv1alpha1.SandboxTemplate, error) {
	firstTime := existing == nil

	var desired agentsv1alpha1.EnvClusterMember
	if existing != nil {
		desired = *existing.DeepCopy()
	}
	desired.Name = pool.Name

	// --- Metadata (fill once) ---
	if len(desired.Metadata.Labels) == 0 && len(pool.Labels) > 0 {
		desired.Metadata.Labels = cloneStringMap(pool.Labels)
	}
	if len(desired.Metadata.Annotations) == 0 && len(pool.Annotations) > 0 {
		desired.Metadata.Annotations = cloneStringMap(pool.Annotations)
	}

	// --- Spec primitive fields (fill once) ---
	if desired.Spec.PodCreationImagePolicy == "" {
		desired.Spec.PodCreationImagePolicy = pool.Spec.PodCreationImagePolicy
	}
	if desired.Spec.DefaultStartupTimeout == nil && pool.Spec.DefaultStartupTimeout != nil {
		desired.Spec.DefaultStartupTimeout = pool.Spec.DefaultStartupTimeout.DeepCopy()
	}
	if desired.Spec.DefaultIdleTimeout == nil && pool.Spec.DefaultIdleTimeout != nil {
		desired.Spec.DefaultIdleTimeout = pool.Spec.DefaultIdleTimeout.DeepCopy()
	}
	if desired.Spec.TemplateName == "" {
		desired.Spec.TemplateName = pool.Spec.TemplateName
	}

	// Replicas is owned by the autoscaler / user once the Member exists;
	// only seed it on the very first sync from the live Pool.
	if firstTime {
		desired.Spec.Replicas = pool.Spec.Replicas
	}

	// --- EmbeddedSandboxTemplate (fill once) ---
	//
	// Three sources, in priority order:
	//   1. existing Member already has a usable template → keep it.
	//   2. Pool has containers inline → copy the embedded template.
	//   3. Pool only has TemplateName → resolve the SandboxTemplate and
	//      copy its EmbeddedSandboxTemplate.
	//
	// Once any of (1)-(3) yields a non-empty Containers list we never
	// refresh it from the source — that's the "upgrade does not
	// auto-propagate" invariant.
	var resolvedTmpl *agentsv1alpha1.SandboxTemplate
	if len(desired.Spec.Template.Spec.Containers) == 0 {
		switch {
		case len(pool.Spec.Template.Spec.Containers) > 0:
			desired.Spec.EmbeddedSandboxTemplate = *pool.Spec.EmbeddedSandboxTemplate.DeepCopy()
		case pool.Spec.TemplateName != "":
			tmpl := &agentsv1alpha1.SandboxTemplate{}
			if err := r.Get(ctx, types.NamespacedName{Name: pool.Spec.TemplateName}, tmpl); err != nil {
				return desired, nil, fmt.Errorf("resolve SandboxTemplate %q: %w", pool.Spec.TemplateName, err)
			}
			desired.Spec.EmbeddedSandboxTemplate = *tmpl.Spec.EmbeddedSandboxTemplate.DeepCopy()
			resolvedTmpl = tmpl
		}
	}

	// --- Config (adopter-derived; overwrite on drift) ---
	itName, multiplier, derr := resolvePoolShape(ctx, r.InstanceTypes, pool)
	if derr != nil {
		return desired, resolvedTmpl, derr
	}
	if itName != "" {
		if desired.Config.InstanceType != itName {
			desired.Config.InstanceType = itName
		}
		if desired.Config.Multiplier != multiplier {
			desired.Config.Multiplier = multiplier
		}
		// Catalog match makes InlineResources redundant; drop it so the
		// renderer uses the catalog path.
		desired.Config.InlineResources = nil
	} else if desired.Config.InlineResources == nil {
		// No catalog entry: keep the legacy fallback. Only fill when
		// empty — once a sizing source exists we leave it alone.
		if res := firstContainerResources(pool); res != nil {
			desired.Config.InlineResources = res.DeepCopy()
		}
	}

	derivedGroup := deriveScalingGroupName(r.InstanceTypes, pool)
	if derivedGroup != "" && desired.Config.ScalingGroup != derivedGroup {
		desired.Config.ScalingGroup = derivedGroup
	}

	return desired, resolvedTmpl, nil
}

// stampTemplateProvenance writes the SandboxTemplate name + version onto
// the Pool's annotations. Idempotent: when the annotation already matches
// the resolved version we skip the patch entirely.
func (r *PoolAdoptionReconciler) stampTemplateProvenance(
	ctx context.Context,
	pool *agentsv1alpha1.SandboxPool,
	tmpl *agentsv1alpha1.SandboxTemplate,
) error {
	wantName := tmpl.Name
	wantVersion := tmpl.Spec.Version
	if wantName == "" && wantVersion == "" {
		return nil
	}
	curName := pool.Annotations[agentsv1alpha1.SandboxPoolTemplateNameAnnotationKey]
	curVersion := pool.Annotations[agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey]
	if curName == wantName && curVersion == wantVersion {
		return nil
	}
	base := pool.DeepCopy()
	if pool.Annotations == nil {
		pool.Annotations = map[string]string{}
	}
	if wantName != "" {
		pool.Annotations[agentsv1alpha1.SandboxPoolTemplateNameAnnotationKey] = wantName
	}
	if wantVersion != "" {
		pool.Annotations[agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey] = wantVersion
	}
	return r.Patch(ctx, pool, client.MergeFrom(base))
}

// ensureEnvOwnerReference adds a controlling OwnerReference to env when
// not already present. Existing refs with matching UID are left untouched.
func (r *PoolAdoptionReconciler) ensureEnvOwnerReference(ctx context.Context, pool *agentsv1alpha1.SandboxPool, env *agentsv1alpha1.SandboxEnv) error {
	if hasOwningEnvReference(pool.OwnerReferences, env) {
		return nil
	}
	base := pool.DeepCopy()
	pool.OwnerReferences = append(pool.OwnerReferences, ownerReferenceForEnv(env))
	return r.Patch(ctx, pool, client.MergeFrom(base))
}

// hasOwningEnvReference returns true when ownerRefs already includes a
// reference (controller or not) to the given Env.
func hasOwningEnvReference(ownerRefs []metav1.OwnerReference, env *agentsv1alpha1.SandboxEnv) bool {
	for _, ref := range ownerRefs {
		if ref.UID == env.UID && ref.Kind == agentsv1alpha1.SandboxEnvOwnerKind {
			return true
		}
	}
	return false
}

// ownerReferenceForEnv builds a controlling OwnerReference so deleting the
// Env cascades to its member Pools via Kubernetes garbage collection.
func ownerReferenceForEnv(env *agentsv1alpha1.SandboxEnv) metav1.OwnerReference {
	isController := true
	blockOwnerDeletion := true
	return metav1.OwnerReference{
		APIVersion:         agentsv1alpha1.GroupVersion.String(),
		Kind:               agentsv1alpha1.SandboxEnvOwnerKind,
		Name:               env.Name,
		UID:                env.UID,
		Controller:         &isController,
		BlockOwnerDeletion: &blockOwnerDeletion,
	}
}

// resolvePoolShape consults the InstanceType Provider to round-trip the
// Pool's first-container resources back to (InstanceType, Multiplier). When
// the provider is disabled or no entry matches, returns ("", 0, nil) and the
// caller falls back to the InlineResources path.
func resolvePoolShape(ctx context.Context, p instancetype.Provider, pool *agentsv1alpha1.SandboxPool) (string, int32, error) {
	if p == nil || !p.Enabled() {
		return "", 0, nil
	}
	res := firstContainerResources(pool)
	if res == nil {
		return "", 0, nil
	}
	it, mul, derr := p.ResolveByResources(ctx, *res)
	if derr != nil {
		if derr.Cause != nil {
			return "", 0, derr.Cause
		}
		return "", 0, fmt.Errorf("%s", derr.Message)
	}
	if it == nil {
		return "", 0, nil
	}
	return it.Name, mul, nil
}

// buildEnvFromPool constructs a fresh SandboxEnv that adopts the given
// Pool as its sole local-cluster member. The member is passed in
// pre-composed so the Env is created with a consistent snapshot in one
// shot.
func buildEnvFromPool(
	pool *agentsv1alpha1.SandboxPool,
	member agentsv1alpha1.EnvClusterMember,
	localClusterID string,
) *agentsv1alpha1.SandboxEnv {
	groupName := member.Config.ScalingGroup
	if groupName == "" {
		groupName = fallbackScalingGroup
	}
	env := &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pool.Name,
			Namespace: pool.Namespace,
			Labels:    envLabelsFromPool(pool),
		},
		Spec: agentsv1alpha1.SandboxEnvSpec{
			TemplateRef: agentsv1alpha1.SandboxEnvTemplateRef{
				Name:    pool.Spec.TemplateName,
				Version: pool.Annotations[agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey],
			},
			Mode: agentsv1alpha1.SandboxEnvModeWarmPool,
			Clusters: []agentsv1alpha1.EnvClusterSpec{
				{
					ClusterID: localClusterID,
					Members:   []agentsv1alpha1.EnvClusterMember{member},
				},
			},
			Autoscaling: &agentsv1alpha1.EnvAutoscalingSpec{
				// Per-group Enabled flag stays false at adoption time — the
				// operator (or dashboard) opts into autoscaling by patching
				// the group's Enabled bit through the dedicated endpoint.
				Groups: []agentsv1alpha1.EnvAutoscalingGroup{{Name: groupName, Enabled: false}},
			},
		},
	}
	if member.Config.InstanceType != "" {
		env.Spec.Defaults = &agentsv1alpha1.SandboxEnvDefaults{
			InstanceType: member.Config.InstanceType,
			Multiplier:   member.Config.Multiplier,
		}
	}
	return env
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}

// deriveScalingGroupName resolves the ScalingGroup name for a Pool. Prefers
// the InstanceType provider's DeriveScalingGroupName when the catalog is
// enabled (it may return a backend-specific name such as "sci.c22-2"); else
// falls back to the resource-key form ("2c8Gi"). Returns
// fallbackScalingGroup only when the Pool has no observable resources at
// all — a state the controller fixes by reading the Pool spec later.
func deriveScalingGroupName(provider instancetype.Provider, pool *agentsv1alpha1.SandboxPool) string {
	res := firstContainerResources(pool)
	if res == nil {
		return fallbackScalingGroup
	}
	if provider != nil {
		if name := provider.DeriveScalingGroupName(*res); name != "" {
			return name
		}
	}
	if key := instancetype.DeriveResourceKey(*res); key != "" && key != "default" {
		return key
	}
	return fallbackScalingGroup
}

// envLabelsFromPool propagates the team/user labels (and any other discovery
// labels) from a Pool onto the adopting Env. Kept minimal so Hub Sync in
// Phase 2 can merge label sets without surprises.
func envLabelsFromPool(pool *agentsv1alpha1.SandboxPool) map[string]string {
	labels := map[string]string{}
	if v, ok := pool.Labels[agentsv1alpha1.LabelTeam]; ok {
		labels[agentsv1alpha1.LabelTeam] = v
	}
	if v, ok := pool.Labels[agentsv1alpha1.LabelUser]; ok {
		labels[agentsv1alpha1.LabelUser] = v
	}
	if len(labels) == 0 {
		return nil
	}
	return labels
}

// firstContainerResources returns a deep-copyable handle to the first
// container's ResourceRequirements on the Pool's embedded template. Returns
// nil when the embedded PodSpec has no containers.
func firstContainerResources(pool *agentsv1alpha1.SandboxPool) *corev1.ResourceRequirements {
	containers := pool.Spec.Template.Spec.Containers
	if len(containers) == 0 {
		return nil
	}
	res := containers[0].Resources
	return &res
}

// findMember returns a pointer to the matching member entry on env's
// local cluster segment, or nil when the member is absent.
func findMember(env *agentsv1alpha1.SandboxEnv, localClusterID, name string) *agentsv1alpha1.EnvClusterMember {
	if env == nil {
		return nil
	}
	for i := range env.Spec.Clusters {
		if env.Spec.Clusters[i].ClusterID != localClusterID {
			continue
		}
		ms := env.Spec.Clusters[i].Members
		for j := range ms {
			if ms[j].Name == name {
				return &ms[j]
			}
		}
	}
	return nil
}

// memberIndex returns the position of a member by name, or -1 when absent.
func memberIndex(members []agentsv1alpha1.EnvClusterMember, name string) int {
	for i := range members {
		if members[i].Name == name {
			return i
		}
	}
	return -1
}

// mutateLocalClusterSpec is a copy of the helper in the parent package, kept
// inline to avoid an upward import cycle (parent imports nothing from
// poolmigration; poolmigration must not pull parent in).
func mutateLocalClusterSpec(env *agentsv1alpha1.SandboxEnv, localClusterID string, mutator func(*agentsv1alpha1.EnvClusterSpec)) {
	if env == nil || localClusterID == "" || mutator == nil {
		return
	}
	idx := -1
	for i := range env.Spec.Clusters {
		if env.Spec.Clusters[i].ClusterID == localClusterID {
			idx = i
			break
		}
	}
	if idx >= 0 {
		mutator(&env.Spec.Clusters[idx])
		return
	}
	seg := agentsv1alpha1.EnvClusterSpec{ClusterID: localClusterID}
	mutator(&seg)
	if len(seg.Members) > 0 {
		env.Spec.Clusters = append(env.Spec.Clusters, seg)
	}
}
