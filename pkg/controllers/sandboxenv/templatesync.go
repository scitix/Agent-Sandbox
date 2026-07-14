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
	"maps"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/controllers/sandboxenv/poolrender"
)

// templateRefNameIndexKey indexes SandboxEnvs by spec.templateRef.name so a
// SandboxTemplate change can fan out to every Env that references it.
const templateRefNameIndexKey = "spec.templateRef.name"

// mapTemplateToEnvs is the Watch map function for SandboxTemplate → SandboxEnv.
// Templates are cluster-scoped, so a single edit enqueues every referencing Env
// across all namespaces (resolved via the templateRefNameIndexKey field index).
func (r *SandboxEnvReconciler) mapTemplateToEnvs(ctx context.Context, obj client.Object) []reconcile.Request {
	tmpl, ok := obj.(*agentsv1alpha1.SandboxTemplate)
	if !ok {
		return nil
	}
	envs := &agentsv1alpha1.SandboxEnvList{}
	if err := r.List(ctx, envs, client.MatchingFields{templateRefNameIndexKey: tmpl.Name}); err != nil {
		klog.FromContext(ctx).Error(err, "mapTemplateToEnvs: list envs by templateRef failed", "template", tmpl.Name)
		return nil
	}
	reqs := make([]reconcile.Request, 0, len(envs.Items))
	for i := range envs.Items {
		reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{
			Namespace: envs.Items[i].Namespace,
			Name:      envs.Items[i].Name,
		}})
	}
	return reqs
}

// refreshAutoUpdateMembers re-renders each auto-update member against the
// current Template + Env overrides and, when the resulting idle-Pod revision
// hash differs, rewrites that member's frozen Spec snapshot on the Env CR. The
// drift loop then propagates the change to the live Pool, and the SandboxPool
// reconciler rolls the Pool's stale idle Pods.
//
// Runs before reconcilePools; returns changed=true when it patched the Env, so
// the caller can requeue instead of reading a stale spec in the same pass.
//
// Preservation guarantees (why re-rendering does not drop scheduling state):
//   - The pod-spec body / IdleImage / Runtimes / NetworkPolicy are replaced
//     wholesale, so Template field *deletions* (e.g. dropping affinity) take
//     effect. Per-Pod scheduling (NodeAffinity, priority, qos) is re-injected by
//     the reservation plugin's PreCreatePod on every Pod create, so it does not
//     need to survive here.
//   - The pod-template ObjectMeta (labels/annotations) is MERGED, not replaced:
//     plugin-injected quota/reservation bookkeeping keys (entry-id,
//     instance-name, instance-quantity, worker-id, quota.data, ...) live only in
//     spec.template.metadata and are preserved as foreign keys.
//   - Member.Metadata (the Pool-level snapshot carrying entry-id etc.) is never
//     touched, and no Pool-level plugin admission (PreCreatePool) is re-run — so
//     reservation identity is preserved and no duplicate reservation is submitted.
func (r *SandboxEnvReconciler) refreshAutoUpdateMembers(ctx context.Context, env *agentsv1alpha1.SandboxEnv) (bool, error) {
	if env == nil || env.DeletionTimestamp != nil {
		return false, nil
	}
	templateName := env.Spec.TemplateRef.Name
	if templateName == "" {
		return false, nil
	}
	tmpl := &agentsv1alpha1.SandboxTemplate{}
	if err := r.Get(ctx, client.ObjectKey{Name: templateName}, tmpl); err != nil {
		if apierrors.IsNotFound(err) {
			// Template gone (or not yet created): nothing to converge towards.
			return false, nil
		}
		return false, err
	}

	ipsExists, err := poolrender.ImagePullSecretExists(ctx, r.Client, env.Namespace, agentsv1alpha1.EnvImagePullSecretName(env.Name))
	if err != nil {
		ipsExists = false
	}

	// Compute the desired new Spec for every member whose revision changed.
	newSpecByName := map[string]agentsv1alpha1.SandboxPoolSpec{}
	for _, m := range desiredLocalMembers(env, r.LocalClusterID) {
		if !agentsv1alpha1.ResolveAutoUpdate(env, m) {
			continue
		}
		candidate, rerr := poolrender.RenderSandboxPool(poolrender.Inputs{
			Env:                   env,
			Template:              tmpl,
			Member:                m,
			ImagePullSecretExists: ipsExists,
		})
		if rerr != nil {
			klog.FromContext(ctx).Error(rerr, "refreshAutoUpdateMembers: render failed", "member", m.Name)
			continue
		}
		newHash := candidate.Spec.Template.Labels[agentsv1alpha1.TemplateHashLabelKey]
		oldHash := m.Spec.Template.Labels[agentsv1alpha1.TemplateHashLabelKey]
		if newHash == oldHash {
			continue
		}
		newSpecByName[m.Name] = mergeRefreshedSpec(m.Spec, candidate.Spec)
	}
	if len(newSpecByName) == 0 {
		return false, nil
	}

	key := types.NamespacedName{Namespace: env.Namespace, Name: env.Name}
	changed := false
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cur := &agentsv1alpha1.SandboxEnv{}
		if err := r.Get(ctx, key, cur); err != nil {
			return err
		}
		base := cur.DeepCopy()
		applied := false
		for ci := range cur.Spec.Clusters {
			if cur.Spec.Clusters[ci].ClusterID != r.LocalClusterID {
				continue
			}
			for mi := range cur.Spec.Clusters[ci].Members {
				mem := &cur.Spec.Clusters[ci].Members[mi]
				ns, ok := newSpecByName[mem.Name]
				if !ok {
					continue
				}
				mem.Spec = *ns.DeepCopy()
				// Mirror the new hash onto the Pool-level snapshot labels so the
				// drift loop stamps it on the live Pool's top-level labels too
				// (MaterializeFromMember seeds Pool.Labels from Member.Metadata).
				if mem.Metadata.Labels == nil {
					mem.Metadata.Labels = map[string]string{}
				}
				mem.Metadata.Labels[agentsv1alpha1.TemplateHashLabelKey] = ns.Template.Labels[agentsv1alpha1.TemplateHashLabelKey]
				applied = true
			}
		}
		if !applied {
			return nil
		}
		if err := r.Patch(ctx, cur, client.MergeFrom(base)); err != nil {
			return err
		}
		changed = true
		return nil
	}); err != nil {
		return false, err
	}

	if changed && r.Recorder != nil {
		r.Recorder.Eventf(env, nil, corev1.EventTypeNormal, "TemplateRollout", "TemplateRollout",
			"re-rendered %d member(s) onto a new template revision", len(newSpecByName))
	}
	return changed, nil
}

// mergeRefreshedSpec produces the member's new frozen Spec: the freshly rendered
// candidate wins on everything except the pod-template ObjectMeta, where
// plugin-injected foreign keys from the old snapshot are preserved (candidate
// keys, including the new revision hash, win on conflict).
func mergeRefreshedSpec(old, candidate agentsv1alpha1.SandboxPoolSpec) agentsv1alpha1.SandboxPoolSpec {
	out := *candidate.DeepCopy()

	lbl := map[string]string{}
	maps.Copy(lbl, old.Template.Labels)                     // scheduler/quota keys + stale hash
	poolrender.MergeOwnedMapKeys(&lbl, out.Template.Labels) // new template keys + new hash win
	out.Template.Labels = lbl

	ann := map[string]string{}
	maps.Copy(ann, old.Template.Annotations)
	poolrender.MergeOwnedMapKeys(&ann, out.Template.Annotations)
	out.Template.Annotations = ann

	return out
}
