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

// Package poolmigration implements the transitional PoolAdoptionReconciler.
//
// During the SandboxEnv Phase 1 rollout, every existing SandboxPool must be
// wrapped by a same-named SandboxEnv so that:
//
//   - the Pool carries a non-controlling OwnerReference back to its Env
//     (this is the authoritative signal the Pool Reconciler uses to gate its
//     legacy autoscaler — see agentsv1alpha1.HasEnvOwner);
//   - autoscaling decisions move from the Pool to the Env;
//   - Sandbox.create requests can route through Env → Pool.
//
// This package owns ONLY that migration. Steady-state Env behaviour
// (status aggregation + autoscaler) lives in the parent sandboxenv package.
//
// Removal path: once we cut over to a flow where SandboxEnv is the user-facing
// primary and the Pool is created by the Env (Phase 2+), this whole package
// becomes dead code and can be deleted. The Pool Reconciler's HasEnvOwner gate
// keeps working unchanged.
package poolmigration

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
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

// PoolAdoptionReconciler ensures every SandboxPool is wrapped in a same-named
// SandboxEnv. It watches SandboxPool only — Env updates do not enqueue work
// here because adoption is a one-way Pool→Env state machine.
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

// Reconcile owns one Pool at a time: it makes sure the Pool's owning Env
// exists, contains a matching member entry, and that the Pool itself carries
// an OwnerReference back to that Env.
//
// Phases:
//
//  1. Fast path — `poolFullyAdopted(pool)` returns true iff the Pool has an
//     OwnerRef whose UID matches a live SandboxEnv. When true, return early
//     without API calls beyond the GET that delivered us this Pool.
//
//  2. Slow path — call `adoptOrphanPool(pool)` which:
//     a. Resolves the Pool's (InstanceType, Multiplier) when the catalog is
//     enabled; otherwise sets member.InlineResources from the PodSpec.
//     b. Looks up an existing SandboxEnv by Pool.Name; if absent, creates one
//     using the source Pool's TemplateRef + autoscaling.
//     c. Appends the Pool to env.Spec.Clusters[localClusterID].Members when
//     missing.
//     d. Patches the Pool with a non-controlling OwnerReference to the Env.
//
// Steps (a)–(d) are independently idempotent so a Reconcile that crashes
// mid-way (Env created but Pool ref not stamped) heals on the next pass —
// this is **scenario B** in the test plan.
func (r *PoolAdoptionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	pool := &agentsv1alpha1.SandboxPool{}
	if err := r.Get(ctx, req.NamespacedName, pool); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if pool.DeletionTimestamp != nil {
		return ctrl.Result{}, nil
	}

	if ok, err := r.poolFullyAdopted(ctx, pool); err != nil {
		return ctrl.Result{}, err
	} else if ok {
		return ctrl.Result{}, nil
	}

	if err := r.adoptOrphanPool(ctx, pool); err != nil {
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

// poolFullyAdopted returns true when the Pool carries an OwnerReference to a
// SandboxEnv whose UID still resolves on the API server.
//
// "Still resolves" guards against scenario D: the Env was deleted but the
// stale OwnerReference lingers (Kubernetes GC removes dangling refs eventually
// but we want migration to heal immediately when the user re-applies). On any
// non-NotFound Get error we surface the failure so the controller-runtime
// requeues with backoff — partial outages must not make us flap-stamp owners.
func (r *PoolAdoptionReconciler) poolFullyAdopted(ctx context.Context, pool *agentsv1alpha1.SandboxPool) (bool, error) {
	for _, ref := range pool.OwnerReferences {
		if ref.Kind != agentsv1alpha1.SandboxEnvOwnerKind {
			continue
		}
		env := &agentsv1alpha1.SandboxEnv{}
		err := r.Get(ctx, types.NamespacedName{Namespace: pool.Namespace, Name: ref.Name}, env)
		switch {
		case apierrors.IsNotFound(err):
			// Dangling reference — fall through and re-adopt below.
			continue
		case err != nil:
			return false, err
		}
		if env.UID == ref.UID {
			return true, nil
		}
		// UID mismatch ≈ same name, different object (Env was recreated). Treat
		// as not-adopted so we re-stamp with the current UID.
	}
	return false, nil
}

// adoptOrphanPool runs the slow path described in Reconcile's doc.
//
// Conflict errors from any of the writes are propagated; Reconcile maps them
// to a Requeue so the next pass re-reads fresh objects.
func (r *PoolAdoptionReconciler) adoptOrphanPool(ctx context.Context, pool *agentsv1alpha1.SandboxPool) error {
	log := klog.FromContext(ctx).WithValues("pool", pool.Namespace+"/"+pool.Name)

	itName, multiplier, err := resolvePoolShape(ctx, r.InstanceTypes, pool)
	if err != nil {
		return fmt.Errorf("resolve pool shape: %w", err)
	}
	groupName := deriveScalingGroupName(r.InstanceTypes, pool)
	member := buildMemberFromPool(pool, itName, multiplier, groupName)
	envName := pool.Name // Phase 1: 1:1 same-name Env.

	env := &agentsv1alpha1.SandboxEnv{}
	getErr := r.Get(ctx, types.NamespacedName{Namespace: pool.Namespace, Name: envName}, env)
	switch {
	case apierrors.IsNotFound(getErr):
		env = buildEnvFromPool(pool, itName, multiplier, member, r.LocalClusterID, groupName)
		if err := r.Create(ctx, env); err != nil {
			if apierrors.IsAlreadyExists(err) {
				if err := r.Get(ctx, types.NamespacedName{Namespace: pool.Namespace, Name: envName}, env); err != nil {
					return err
				}
			} else {
				return err
			}
		} else {
			log.Info("Created SandboxEnv from orphan Pool", "env", env.Name)
		}
	case getErr != nil:
		return getErr
	}

	// Ensure local cluster segment exists and contains this member.
	if err := r.ensureMember(ctx, env, member); err != nil {
		return err
	}

	// Stamp Pool with a non-controlling OwnerReference back to the Env.
	return r.ensureEnvOwnerReference(ctx, pool, env)
}

// ensureMember appends member into env.Spec.Clusters[local].Members when not
// already present. Caller is the only writer of the local segment, so a
// non-conflict result means the change is visible to everyone on the next
// fetch.
func (r *PoolAdoptionReconciler) ensureMember(ctx context.Context, env *agentsv1alpha1.SandboxEnv, member agentsv1alpha1.EnvClusterMember) error {
	base := env.DeepCopy()
	updated := false
	mutateLocalClusterSpec(env, r.LocalClusterID, func(local *agentsv1alpha1.EnvClusterSpec) {
		if memberIndex(local.Members, member.Name) < 0 {
			local.Members = append(local.Members, member)
			updated = true
		}
	})
	if !updated {
		return nil
	}
	if err := r.Patch(ctx, env, client.MergeFrom(base)); err != nil {
		return err
	}
	klog.FromContext(ctx).Info("Appended Pool as member of SandboxEnv", "env", env.Name)
	return nil
}

// ensureEnvOwnerReference adds a non-controlling OwnerReference to env when
// not already present. Existing refs (controlling or not) with matching UID
// are left untouched.
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
// Env cascades to its member Pools via Kubernetes garbage collection. Phase 1
// adopter and Phase A1 Env Reconciler both stamp the same shape so the
// cluster converges regardless of which path created the Pool.
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
		// derr.Cause carries the original error; preserve the wrap so callers
		// see the context.
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

// buildEnvFromPool constructs a fresh SandboxEnv that adopts the given Pool
// as its sole member in the local cluster segment. groupName labels the
// ScalingGroup associated with this Pool and is mirrored into the Env's
// autoscaling.groups[0] entry so the autoscaler config and the member's
// ScalingGroup field agree by construction.
func buildEnvFromPool(
	pool *agentsv1alpha1.SandboxPool,
	itName string,
	multiplier int32,
	member agentsv1alpha1.EnvClusterMember,
	localClusterID string,
	groupName string,
) *agentsv1alpha1.SandboxEnv {
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
				Enabled: false,
				Groups:  []agentsv1alpha1.EnvAutoscalingGroup{{Name: groupName}},
			},
		},
	}
	if itName != "" {
		env.Spec.Defaults = &agentsv1alpha1.SandboxEnvDefaults{
			InstanceType: itName,
			Multiplier:   multiplier,
		}
	}
	return env
}

// buildMemberFromPool produces the EnvClusterMember entry that represents the
// given Pool. When the Provider supplied (InstanceType, Multiplier), those
// fields are filled and InlineResources is left empty. Otherwise the Pool's
// first-container resources are copied verbatim.
//
// groupName is the resolved ScalingGroup name (typically derived from the
// effective resources via the InstanceType provider).
func buildMemberFromPool(pool *agentsv1alpha1.SandboxPool, itName string, multiplier int32, groupName string) agentsv1alpha1.EnvClusterMember {
	m := agentsv1alpha1.EnvClusterMember{
		Name:         pool.Name,
		ScalingGroup: groupName,
	}
	if itName != "" {
		m.InstanceType = itName
		m.Multiplier = multiplier
		return m
	}
	if res := firstContainerResources(pool); res != nil {
		m.InlineResources = res.DeepCopy()
	}
	return m
}

// deriveScalingGroupName resolves the ScalingGroup name for a Pool by asking
// the InstanceType provider to derive a stable identifier from the Pool's
// effective resources. Returns the fallback "default" name when the provider
// is nil, returns its own "default", or the Pool has no observable resources.
func deriveScalingGroupName(provider instancetype.Provider, pool *agentsv1alpha1.SandboxPool) string {
	if provider == nil {
		return fallbackScalingGroup
	}
	res := firstContainerResources(pool)
	if res == nil {
		return fallbackScalingGroup
	}
	name := provider.DeriveScalingGroupName(*res)
	if name == "" {
		return fallbackScalingGroup
	}
	return name
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
	tmpl := pool.Spec.Template
	if tmpl == nil || len(tmpl.Spec.Containers) == 0 {
		return nil
	}
	res := tmpl.Spec.Containers[0].Resources
	return &res
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
