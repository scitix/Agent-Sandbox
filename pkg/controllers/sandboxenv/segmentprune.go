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
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// ClusterCatalog is the read-only view of the deployment's cluster list that
// segment pruning consults. Production wires it to utils/cluster.Store, which
// is fed by the hub over the cluster-config ConfigMap.
type ClusterCatalog interface {
	// KnownClusterIDs returns every cluster ID the catalog knows about.
	// An empty result means "not populated yet", not "no clusters exist".
	KnownClusterIDs() []string
}

// pruneStaleClusterSegments removes spec/status cluster segments whose
// ClusterID is absent from the deployment's cluster catalog — segments left
// behind when a cluster is renamed or decommissioned.
//
// A stale segment is not inert. Members inside it keep their names, and any
// lookup that scans every segment can resolve a Pool to the dead one instead
// of the live cluster's. That is how a cluster rename stalled autoscaling: the
// scale-up target was written into the departed segment, where no reconciler
// reads it, so the Pool never grew and the same decision repeated every
// cooldown window. The member lookups have since been scoped to the local
// segment; this prunes the wreckage so nothing else trips on it.
//
// Three guards keep this from eating live configuration:
//
//  1. No catalog, or an empty one, prunes nothing. The Store fills in
//     asynchronously from a ConfigMap informer, so "empty" is
//     indistinguishable from "not synced yet" — and acting on it would
//     delete every segment of every Env in the cluster.
//  2. The local segment is never pruned, even if the catalog omits it. An
//     operator whose own ID is missing from the catalog is misconfigured;
//     deleting the segment it is actively reconciling would destroy the
//     member list and cascade-delete its Pools.
//  3. Only segments the catalog positively does not list are removed.
//     Unknown-to-us is never inferred from absence of evidence elsewhere.
//
// Returns true when the Env was patched, in which case the caller should
// requeue rather than continue against its stale in-memory copy.
func (r *SandboxEnvReconciler) pruneStaleClusterSegments(
	ctx context.Context,
	env *agentsv1alpha1.SandboxEnv,
) (bool, error) {
	if r.Clusters == nil || env == nil || env.DeletionTimestamp != nil {
		return false, nil
	}
	known := r.Clusters.KnownClusterIDs()
	if len(known) == 0 {
		// Guard 1: catalog unpopulated. Never prune on an empty catalog.
		return false, nil
	}
	knownSet := make(map[string]struct{}, len(known))
	for _, id := range known {
		knownSet[id] = struct{}{}
	}

	staleSpec := staleSegmentIDs(clusterIDsOfSpec(env.Spec.Clusters), knownSet, r.LocalClusterID)
	staleStatus := staleSegmentIDs(clusterIDsOfStatus(env.Status.Clusters), knownSet, r.LocalClusterID)
	if len(staleSpec) == 0 && len(staleStatus) == 0 {
		return false, nil
	}

	log := klog.FromContext(ctx).WithValues("env", env.Namespace+"/"+env.Name)
	key := types.NamespacedName{Namespace: env.Namespace, Name: env.Name}

	// Spec and status are separate sub-resources, so they need separate
	// patches. Spec goes first: it is what the member lookups and the
	// materialisation loop read, so it is the half that actually misroutes
	// writes. A failure between the two leaves a stale status segment, which
	// the next reconcile re-attempts and which mirrorForeignSegments would
	// rebuild anyway.
	patchedSpec := false
	if len(staleSpec) > 0 {
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			cur := &agentsv1alpha1.SandboxEnv{}
			if err := r.Get(ctx, key, cur); err != nil {
				return err
			}
			kept := make([]agentsv1alpha1.EnvClusterSpec, 0, len(cur.Spec.Clusters))
			for i := range cur.Spec.Clusters {
				if _, drop := staleSpec[cur.Spec.Clusters[i].ClusterID]; drop {
					continue
				}
				kept = append(kept, cur.Spec.Clusters[i])
			}
			if len(kept) == len(cur.Spec.Clusters) {
				return nil
			}
			base := cur.DeepCopy()
			cur.Spec.Clusters = kept
			if err := r.Patch(ctx, cur, client.MergeFrom(base)); err != nil {
				return err
			}
			patchedSpec = true
			return nil
		}); err != nil {
			return false, err
		}
	}

	patchedStatus := false
	if len(staleStatus) > 0 {
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			cur := &agentsv1alpha1.SandboxEnv{}
			if err := r.Get(ctx, key, cur); err != nil {
				return err
			}
			kept := make([]agentsv1alpha1.EnvClusterStatus, 0, len(cur.Status.Clusters))
			for i := range cur.Status.Clusters {
				if _, drop := staleStatus[cur.Status.Clusters[i].ClusterID]; drop {
					continue
				}
				kept = append(kept, cur.Status.Clusters[i])
			}
			if len(kept) == len(cur.Status.Clusters) {
				return nil
			}
			base := cur.DeepCopy()
			cur.Status.Clusters = kept
			if err := r.Status().Patch(ctx, cur, client.MergeFrom(base)); err != nil {
				return err
			}
			patchedStatus = true
			return nil
		}); err != nil {
			return false, err
		}
	}

	if !patchedSpec && !patchedStatus {
		return false, nil
	}
	removed := append(sortedKeys(staleSpec), sortedKeys(staleStatus)...)
	log.Info("Pruned cluster segments absent from the cluster catalog",
		"removed", removed, "knownClusters", known, "localCluster", r.LocalClusterID)
	if r.Recorder != nil {
		r.Recorder.Eventf(env, nil, corev1.EventTypeNormal, "StaleClusterSegmentsPruned", "Prune",
			"removed cluster segment(s) %v: not present in the deployment's cluster catalog",
			removed)
	}
	return true, nil
}

// staleSegmentIDs returns the subset of ids that the catalog does not list,
// minus the local cluster (guard 2) and minus the empty ID (a segment with no
// ClusterID is malformed rather than stale; leave it for a human to notice).
func staleSegmentIDs(ids []string, known map[string]struct{}, localClusterID string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, id := range ids {
		if id == "" || id == localClusterID {
			continue
		}
		if _, ok := known[id]; ok {
			continue
		}
		out[id] = struct{}{}
	}
	return out
}

func clusterIDsOfSpec(segs []agentsv1alpha1.EnvClusterSpec) []string {
	out := make([]string, 0, len(segs))
	for i := range segs {
		out = append(out, segs[i].ClusterID)
	}
	return out
}

func clusterIDsOfStatus(segs []agentsv1alpha1.EnvClusterStatus) []string {
	out := make([]string, 0, len(segs))
	for i := range segs {
		out = append(out, segs[i].ClusterID)
	}
	return out
}

// sortedKeys returns the map's keys in a stable order so log lines and events
// don't reshuffle between reconciles.
func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
