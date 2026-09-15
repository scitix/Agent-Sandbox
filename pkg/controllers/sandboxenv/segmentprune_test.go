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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// staticCatalog is a ClusterCatalog double. A nil/empty ids slice models the
// catalog before its ConfigMap informer has synced.
type staticCatalog struct{ ids []string }

func (s staticCatalog) KnownClusterIDs() []string { return s.ids }

// envWithSegments builds an Env carrying the named spec and status cluster
// segments, each with one member.
func envWithSegments(specIDs, statusIDs []string) *agentsv1alpha1.SandboxEnv {
	env := &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{Name: "env-a", Namespace: "default"},
		Spec: agentsv1alpha1.SandboxEnvSpec{
			TemplateRef: agentsv1alpha1.SandboxEnvTemplateRef{Name: "tmpl"},
		},
	}
	for _, id := range specIDs {
		env.Spec.Clusters = append(env.Spec.Clusters, agentsv1alpha1.EnvClusterSpec{
			ClusterID: id,
			Members:   []agentsv1alpha1.EnvClusterMember{{Name: "pool-1"}},
		})
	}
	for _, id := range statusIDs {
		env.Status.Clusters = append(env.Status.Clusters, agentsv1alpha1.EnvClusterStatus{
			ClusterID: id,
			IsLocal:   id == testLocalCluster,
		})
	}
	return env
}

func specSegmentIDs(env *agentsv1alpha1.SandboxEnv) []string {
	out := make([]string, 0, len(env.Spec.Clusters))
	for i := range env.Spec.Clusters {
		out = append(out, env.Spec.Clusters[i].ClusterID)
	}
	return out
}

func statusSegmentIDs(env *agentsv1alpha1.SandboxEnv) []string {
	out := make([]string, 0, len(env.Status.Clusters))
	for i := range env.Status.Clusters {
		out = append(out, env.Status.Clusters[i].ClusterID)
	}
	return out
}

func equalIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// newPruneTestReconciler is newReconcileTestReconciler plus the SandboxEnv
// status subresource, which pruning patches separately from spec.
func newPruneTestReconciler(t *testing.T, seed ...client.Object) *SandboxEnvReconciler {
	t.Helper()
	scheme := newReconcileTestScheme(t)
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(seed...).
		WithStatusSubresource(&agentsv1alpha1.SandboxEnv{}).
		Build()
	return &SandboxEnvReconciler{
		Client:         c,
		Scheme:         scheme,
		LocalClusterID: testLocalCluster,
	}
}

func reloadEnv(t *testing.T, r *SandboxEnvReconciler, env *agentsv1alpha1.SandboxEnv) *agentsv1alpha1.SandboxEnv {
	t.Helper()
	got := &agentsv1alpha1.SandboxEnv{}
	if err := r.Get(context.Background(),
		types.NamespacedName{Namespace: env.Namespace, Name: env.Name}, got); err != nil {
		t.Fatalf("get env: %v", err)
	}
	return got
}

// The rename case: the old cluster ID is gone from the catalog, so its
// leftover segment goes too, in both spec and status.
func TestPruneStaleClusterSegments_RemovesDepartedCluster(t *testing.T) {
	env := envWithSegments(
		[]string{"old-name", testLocalCluster},
		[]string{"old-name", testLocalCluster},
	)
	r := newPruneTestReconciler(t, env)
	r.Clusters = staticCatalog{ids: []string{testLocalCluster, "peer"}}

	pruned, err := r.pruneStaleClusterSegments(context.Background(), env)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if !pruned {
		t.Fatal("expected the stale segment to be pruned")
	}
	got := reloadEnv(t, r, env)
	if want := []string{testLocalCluster}; !equalIDs(specSegmentIDs(got), want) {
		t.Errorf("spec segments = %v, want %v", specSegmentIDs(got), want)
	}
	if want := []string{testLocalCluster}; !equalIDs(statusSegmentIDs(got), want) {
		t.Errorf("status segments = %v, want %v", statusSegmentIDs(got), want)
	}
}

// A live peer cluster is not stale. Pruning it would break cross-cluster
// routing for an Env that legitimately spans clusters.
func TestPruneStaleClusterSegments_KeepsKnownPeers(t *testing.T) {
	env := envWithSegments([]string{testLocalCluster, "peer"}, []string{testLocalCluster, "peer"})
	r := newPruneTestReconciler(t, env)
	r.Clusters = staticCatalog{ids: []string{testLocalCluster, "peer"}}

	pruned, err := r.pruneStaleClusterSegments(context.Background(), env)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if pruned {
		t.Error("no segment should have been pruned")
	}
	got := reloadEnv(t, r, env)
	if want := []string{testLocalCluster, "peer"}; !equalIDs(specSegmentIDs(got), want) {
		t.Errorf("spec segments = %v, want %v", specSegmentIDs(got), want)
	}
}

// Guard 1. The Store fills in asynchronously, so an empty catalog is
// indistinguishable from "not synced yet" — acting on it would delete every
// segment of every Env in the cluster.
func TestPruneStaleClusterSegments_EmptyCatalogPrunesNothing(t *testing.T) {
	env := envWithSegments([]string{"old-name", testLocalCluster}, nil)
	r := newPruneTestReconciler(t, env)
	r.Clusters = staticCatalog{} // not populated yet

	pruned, err := r.pruneStaleClusterSegments(context.Background(), env)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if pruned {
		t.Fatal("an unpopulated catalog must never authorise a deletion")
	}
	got := reloadEnv(t, r, env)
	if want := []string{"old-name", testLocalCluster}; !equalIDs(specSegmentIDs(got), want) {
		t.Errorf("spec segments = %v, want %v", specSegmentIDs(got), want)
	}
}

// Guard 2. An operator missing from its own catalog is misconfigured; deleting
// the segment it actively reconciles would drop the member list and cascade
// into its Pools.
func TestPruneStaleClusterSegments_NeverPrunesLocalSegment(t *testing.T) {
	env := envWithSegments([]string{testLocalCluster, "old-name"}, []string{testLocalCluster})
	r := newPruneTestReconciler(t, env)
	r.Clusters = staticCatalog{ids: []string{"peer"}} // local absent from the catalog

	if _, err := r.pruneStaleClusterSegments(context.Background(), env); err != nil {
		t.Fatalf("prune: %v", err)
	}
	got := reloadEnv(t, r, env)
	if want := []string{testLocalCluster}; !equalIDs(specSegmentIDs(got), want) {
		t.Errorf("spec segments = %v, want %v (the local segment must survive)", specSegmentIDs(got), want)
	}
}

// No catalog wired (single-cluster, or an embedder that never sets it) leaves
// everything alone.
func TestPruneStaleClusterSegments_NilCatalogIsNoOp(t *testing.T) {
	env := envWithSegments([]string{"whatever", testLocalCluster}, nil)
	r := newPruneTestReconciler(t, env)

	pruned, err := r.pruneStaleClusterSegments(context.Background(), env)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if pruned {
		t.Error("a nil catalog must prune nothing")
	}
}

// A segment with no ClusterID is malformed rather than stale; leave it for a
// human rather than silently deleting members.
func TestPruneStaleClusterSegments_IgnoresEmptyClusterID(t *testing.T) {
	env := envWithSegments([]string{"", testLocalCluster}, nil)
	r := newPruneTestReconciler(t, env)
	r.Clusters = staticCatalog{ids: []string{testLocalCluster}}

	pruned, err := r.pruneStaleClusterSegments(context.Background(), env)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if pruned {
		t.Error("an empty ClusterID must not be treated as stale")
	}
}

// An Env being deleted is not worth patching.
func TestPruneStaleClusterSegments_SkipsDeletingEnv(t *testing.T) {
	env := envWithSegments([]string{"old-name", testLocalCluster}, nil)
	now := metav1.Now()
	env.DeletionTimestamp = &now
	env.Finalizers = []string{"test/hold"}
	r := newPruneTestReconciler(t, env)
	r.Clusters = staticCatalog{ids: []string{testLocalCluster}}

	pruned, err := r.pruneStaleClusterSegments(context.Background(), env)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if pruned {
		t.Error("an Env under deletion must not be patched")
	}
}

func TestStaleSegmentIDs(t *testing.T) {
	known := map[string]struct{}{"a": {}, "local": {}}
	got := staleSegmentIDs([]string{"a", "local", "gone", "", "gone"}, known, "local")
	if len(got) != 1 {
		t.Fatalf("stale set = %v, want exactly {gone}", sortedKeys(got))
	}
	if _, ok := got["gone"]; !ok {
		t.Errorf("stale set = %v, want {gone}", sortedKeys(got))
	}
}
