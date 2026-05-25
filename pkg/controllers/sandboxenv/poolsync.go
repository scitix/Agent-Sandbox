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
//   - Pools in the desired set that don't exist are created (fully rendered
//     via poolrender.RenderSandboxPool).
//   - Pools that exist and drifted from the desired labels / annotations /
//     pod spec are patched. Replicas is intentionally NOT forced — the
//     autoscaler owns it.
//   - Pools owned by this Env but not in the desired set are deleted (the
//     user removed the member from spec.clusters[local].members).
//
// When the Env spec carries no members, a single namesake Pool is
// materialised to preserve the legacy adopter shape.
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

	// Fetch the source Template once per reconcile — every member shares it.
	// A missing Template aborts the whole pass (no partial renders).
	tmpl, err := r.fetchTemplate(ctx, env.Spec.TemplateRef.Name)
	if err != nil {
		return err
	}

	// Compute the IPS bit once — the Secret lookup is cheap and identical
	// for every member of an Env.
	ipsExists, err := poolrender.ImagePullSecretExists(ctx, r.Client, env.Namespace, agentsv1alpha1.EnvImagePullSecretName(env.Name))
	if err != nil {
		// Stale state is preferable to refusing to reconcile — log and
		// proceed as "missing". The next reconcile reattempts the lookup.
		log.V(1).Info("ImagePullSecret lookup failed; proceeding as missing", "err", err)
		ipsExists = false
	}

	desired := make(map[string]*agentsv1alpha1.SandboxPool, len(members))
	for _, m := range members {
		rendered, err := poolrender.RenderSandboxPool(poolrender.Inputs{
			Env:                   env,
			Template:              tmpl,
			Member:                m,
			ImagePullSecretExists: ipsExists,
		})
		if err != nil {
			return fmt.Errorf("render member %q: %w", m.Name, err)
		}
		desired[m.Name] = rendered
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
		if err := r.updateMemberPoolIfDrifted(ctx, existing, want, tmpl); err != nil {
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

// desiredLocalMembers returns the slice of EnvClusterMembers from the local
// cluster segment. When the segment is absent or empty, a single namesake
// member (Name=env.Name) is synthesised so adopter-created Envs still
// converge to a single Pool.
func desiredLocalMembers(env *agentsv1alpha1.SandboxEnv, localClusterID string) []agentsv1alpha1.EnvClusterMember {
	if env == nil {
		return nil
	}
	for i := range env.Spec.Clusters {
		if env.Spec.Clusters[i].ClusterID == localClusterID {
			ms := env.Spec.Clusters[i].Members
			if len(ms) > 0 {
				return append([]agentsv1alpha1.EnvClusterMember(nil), ms...)
			}
			break
		}
	}
	return []agentsv1alpha1.EnvClusterMember{{Name: env.Name}}
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
// PodCreationImagePolicy, default timeouts, or rendered pod spec drift from
// the freshly rendered want. The pinned-template-version guard ensures
// template-body drift is ignored until an explicit sync-template — Env-level
// overrides edits (which mutate the rendered EmbeddedSandboxTemplate)
// always propagate.
func (r *SandboxEnvReconciler) updateMemberPoolIfDrifted(
	ctx context.Context,
	pool *agentsv1alpha1.SandboxPool,
	want *agentsv1alpha1.SandboxPool,
	tmpl *agentsv1alpha1.SandboxTemplate,
) error {
	// Skip embedded-template drift / provenance advancement when the live
	// Pool is pinned to a different Template version — that case requires
	// an explicit sync-template flow. A missing pin counts as matching:
	// the Pool was just created (or migrated) and the next render
	// establishes the baseline.
	applyEmbedded := true
	if pin, ok := pool.Annotations[agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey]; ok && pin != "" && pin != tmpl.Spec.Version {
		applyEmbedded = false
	}
	// desiredAnnotations omits the template-version pin when the live Pool
	// is on a different version — the pin only advances via sync-template.
	desiredAnnotations := want.Annotations
	if !applyEmbedded {
		desiredAnnotations = filterMapKey(want.Annotations, agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey)
	}

	labelDrift := mapsDifferOnKeys(pool.Labels, want.Labels)
	annotationDrift := mapsDifferOnKeys(pool.Annotations, desiredAnnotations)
	policyDrift := want.Spec.PodCreationImagePolicy != "" && pool.Spec.PodCreationImagePolicy != want.Spec.PodCreationImagePolicy
	startupDrift := !durationsEqual(pool.Spec.DefaultStartupTimeout, want.Spec.DefaultStartupTimeout)
	idleDrift := !durationsEqual(pool.Spec.DefaultIdleTimeout, want.Spec.DefaultIdleTimeout)
	embeddedDrift := applyEmbedded && !equality.Semantic.DeepEqual(pool.Spec.EmbeddedSandboxTemplate, want.Spec.EmbeddedSandboxTemplate)

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
		poolrender.MergeOwnedMapKeys(&current.Labels, want.Labels)
		poolrender.MergeOwnedMapKeys(&current.Annotations, desiredAnnotations)
		if want.Spec.PodCreationImagePolicy != "" {
			current.Spec.PodCreationImagePolicy = want.Spec.PodCreationImagePolicy
		}
		current.Spec.DefaultStartupTimeout = want.Spec.DefaultStartupTimeout
		current.Spec.DefaultIdleTimeout = want.Spec.DefaultIdleTimeout
		if applyEmbedded {
			current.Spec.EmbeddedSandboxTemplate = *want.Spec.EmbeddedSandboxTemplate.DeepCopy()
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

// filterMapKey returns a copy of m with the supplied key removed. Useful
// for excluding a single managed annotation from a merge pass without
// rebuilding the rest of the map.
func filterMapKey(m map[string]string, key string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		if k == key {
			continue
		}
		out[k] = v
	}
	return out
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
