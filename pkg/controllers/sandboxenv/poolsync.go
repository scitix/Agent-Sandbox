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

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/controllers/sandboxenv/poolrender"
)

// reconcilePools brings the live member-Pool set in env's namespace into
// alignment with the desired set derived from env.Spec.Clusters[local].Members.
//
//   - Pools in the desired set that don't exist are created from
//     Member.Metadata + Member.Spec (the frozen post-PreCreatePool
//     snapshot) via poolrender.MaterializeFromMember. The Reconciler does
//     NOT re-run plugin admission — plugin side-effects already live
//     inside Member.Spec by construction.
//   - Pools that exist and drifted from the Member snapshot in
//     labels / annotations / pod spec / replicas are patched. The Env
//     Reconciler is the sole writer of SandboxPool.Spec; both the API and
//     the autoscaler write Member.Spec on the Env CR and let this loop
//     propagate the change.
//   - Pools owned by this Env but not in the desired set are deleted (the
//     user removed the member from spec.clusters[local].members).
//
// An Env with no declared members is a bare shell — the Reconciler
// creates zero Pools. Members are added via the env-scoped Pool CRUD
// endpoints (POST /envs/{name}/sandboxpools), which gives plugin
// admission (e.g. quota reservation) a chance to gate the create before
// the Reconciler ever sees the request.
//
// Template upgrades do NOT auto-propagate through this path: a Template
// change to env.Spec.TemplateRef will not rewrite any existing
// Member.Spec. The (Phase 2) RefreshMember API is the explicit way to
// re-align an existing member with a newer Template revision.
func (r *SandboxEnvReconciler) reconcilePools(ctx context.Context, env *agentsv1alpha1.SandboxEnv) error {
	if env == nil || env.DeletionTimestamp != nil {
		return nil
	}
	log := klog.FromContext(ctx).WithValues("env", env.Namespace+"/"+env.Name)

	members := desiredLocalMembers(env, r.LocalClusterID)
	live, err := r.listOwnedPools(ctx, env)
	if err != nil {
		return err
	}

	// Compute the IPS bit once — the Secret lookup is cheap and identical
	// for every member of an Env. We re-stamp the resulting reference on
	// every materialise so a Secret created/deleted after AddMember
	// propagates onto the Pool without waiting for RefreshMember.
	ipsExists, err := poolrender.ImagePullSecretExists(ctx, r.Client, env.Namespace, agentsv1alpha1.EnvImagePullSecretName(env.Name))
	if err != nil {
		// Stale state is preferable to refusing to reconcile — log and
		// proceed as "missing". The next reconcile reattempts the lookup.
		log.V(1).Info("ImagePullSecret lookup failed; proceeding as missing", "err", err)
		ipsExists = false
	}

	desired := make(map[string]*agentsv1alpha1.SandboxPool, len(members))
	for _, m := range members {
		desired[m.Name] = poolrender.MaterializeFromMember(env, m, ipsExists)
	}

	for name, want := range desired {
		existing, found := live[name]
		if !found {
			if err := r.Create(ctx, want); err != nil {
				if apierrors.IsAlreadyExists(err) {
					// Race: someone else created it between List and Create.
					// Next reconcile picks it up.
					continue
				}
				return fmt.Errorf("create pool %q: %w", name, err)
			}
			log.Info("Created member SandboxPool", "pool", name)
			continue
		}
		if err := r.updateMemberPoolIfDrifted(ctx, existing, want); err != nil {
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

// desiredLocalMembers returns the slice of EnvClusterMembers declared on
// the env's local cluster segment. Returns nil (empty) when the segment
// is absent or has no members — Envs created without explicit members
// (e.g. via POST /v1/envs) start as bare shells; the user adds members
// through the env-scoped Pool CRUD path (POST /envs/{name}/sandboxpools)
// so plugin admission (quota reservation etc.) gates each create.
//
// Historical note: an earlier revision synthesised a single namesake
// member when this slice was empty, to preserve the Phase 1 adopter
// shape. That fallback was removed once the adopter started populating
// members directly — it produced "ghost" Pools that bypassed quota
// admission and could not be deleted through the member CRUD endpoints
// (because no matching member entry existed in spec). See:
//
//	pkg/controllers/sandboxenv/poolmigration/adopter.go.
func desiredLocalMembers(env *agentsv1alpha1.SandboxEnv, localClusterID string) []agentsv1alpha1.EnvClusterMember {
	if env == nil {
		return nil
	}
	for i := range env.Spec.Clusters {
		if env.Spec.Clusters[i].ClusterID == localClusterID {
			return append([]agentsv1alpha1.EnvClusterMember(nil), env.Spec.Clusters[i].Members...)
		}
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

// updateMemberPoolIfDrifted patches a live Pool when its labels, annotations,
// PodCreationImagePolicy, default timeouts, replicas, or pod spec drift from
// the desired projection (Member.Metadata + Member.Spec, with current IPS
// state re-stamped).
//
// The Env Reconciler is the sole writer of SandboxPool.Spec — both the API
// (UpdateMember) and the Env autoscaler express their intent by patching
// Member.Spec on the SandboxEnv CR, and this drift loop propagates it to
// the live Pool.
func (r *SandboxEnvReconciler) updateMemberPoolIfDrifted(
	ctx context.Context,
	pool *agentsv1alpha1.SandboxPool,
	want *agentsv1alpha1.SandboxPool,
) error {
	labelDrift := mapsDifferOnKeys(pool.Labels, want.Labels)
	annotationDrift := mapsDifferOnKeys(pool.Annotations, want.Annotations)
	policyDrift := want.Spec.PodCreationImagePolicy != "" && pool.Spec.PodCreationImagePolicy != want.Spec.PodCreationImagePolicy
	startupDrift := !durationsEqual(pool.Spec.DefaultStartupTimeout, want.Spec.DefaultStartupTimeout)
	idleDrift := !durationsEqual(pool.Spec.DefaultIdleTimeout, want.Spec.DefaultIdleTimeout)
	replicasDrift := pool.Spec.Replicas != want.Spec.Replicas
	embeddedDrift := !equality.Semantic.DeepEqual(pool.Spec.EmbeddedSandboxTemplate, want.Spec.EmbeddedSandboxTemplate)

	if !labelDrift && !annotationDrift && !policyDrift && !startupDrift && !idleDrift && !replicasDrift && !embeddedDrift {
		return nil
	}

	key := types.NamespacedName{Namespace: pool.Namespace, Name: pool.Name}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &agentsv1alpha1.SandboxPool{}
		if err := r.Get(ctx, key, current); err != nil {
			return err
		}
		base := current.DeepCopy()
		poolrender.MergeOwnedMapKeys(&current.Labels, want.Labels)
		poolrender.MergeOwnedMapKeys(&current.Annotations, want.Annotations)
		if want.Spec.PodCreationImagePolicy != "" {
			current.Spec.PodCreationImagePolicy = want.Spec.PodCreationImagePolicy
		}
		current.Spec.DefaultStartupTimeout = want.Spec.DefaultStartupTimeout
		current.Spec.DefaultIdleTimeout = want.Spec.DefaultIdleTimeout
		current.Spec.Replicas = want.Spec.Replicas
		// Defensive: never overwrite a live Pod template that has
		// containers with a desired snapshot whose template has none. An
		// empty Member.Spec snapshot would otherwise wipe out the live
		// Pool's containers, breaking every code path that iterates
		// Pool.Spec.Template.Spec.Containers (release, idle-image lookup,
		// pod creation). The Pool stays on its previous spec until
		// poolmigration's syncMember repopulates the Member snapshot.
		if len(want.Spec.Template.Spec.Containers) > 0 || len(current.Spec.Template.Spec.Containers) == 0 {
			current.Spec.EmbeddedSandboxTemplate = *want.Spec.EmbeddedSandboxTemplate.DeepCopy()
		}
		return r.Patch(ctx, current, client.MergeFrom(base))
	})
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
