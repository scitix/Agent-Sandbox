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

package indexer

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

const (
	indexFieldPrefix = "indexer."

	// Pod indexes
	IndexFieldSandboxID    = indexFieldPrefix + agentsv1alpha1.SandboxIDLabelKey
	IndexFieldSandboxPool  = indexFieldPrefix + agentsv1alpha1.SandboxPoolLabelKey
	IndexFieldSandboxPhase = indexFieldPrefix + agentsv1alpha1.SandboxPhaseLabelKey

	// IndexFieldSandboxPoolPhase is a composite index combining pool name and phase,
	// stored as "poolName\x00phase" (NUL-byte separated to avoid collisions).
	// Use ListIdlePodsForPool() or SandboxPoolPhaseIndexValue() to query.
	IndexFieldSandboxPoolPhase = indexFieldPrefix + "sandbox-pool-phase"

	// Team/User indexes — shared across Pod and SandboxPool
	IndexFieldTeam = indexFieldPrefix + agentsv1alpha1.LabelTeam
	IndexFieldUser = indexFieldPrefix + agentsv1alpha1.LabelUser
)

// ---------------------------------------------------------------------------
// Pod indexer functions
// ---------------------------------------------------------------------------

func IndexerFuncSandboxID(obj client.Object) []string {
	pod := obj.(*corev1.Pod)
	if id, ok := pod.Labels[agentsv1alpha1.SandboxIDLabelKey]; ok {
		return []string{id}
	}
	return nil
}

func IndexerFuncSandboxPool(obj client.Object) []string {
	pod := obj.(*corev1.Pod)
	if pool, ok := pod.Labels[agentsv1alpha1.SandboxPoolLabelKey]; ok {
		return []string{pool}
	}
	return nil
}

func IndexerFuncSandboxPhase(obj client.Object) []string {
	pod := obj.(*corev1.Pod)
	if phase, ok := pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey]; ok {
		return []string{phase}
	}
	return nil
}

// IndexerFuncSandboxPoolPhase returns the composite "poolName\x00phase" value,
// enabling efficient single-index queries for a specific pool+phase combination.
func IndexerFuncSandboxPoolPhase(obj client.Object) []string {
	pod := obj.(*corev1.Pod)
	pool := pod.Labels[agentsv1alpha1.SandboxPoolLabelKey]
	phase := pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey]
	if pool == "" || phase == "" {
		return nil
	}
	return []string{pool + "\x00" + phase}
}

// SandboxPoolPhaseIndexValue builds the composite index lookup value.
func SandboxPoolPhaseIndexValue(poolName, phase string) string {
	return poolName + "\x00" + phase
}

// IndexerFuncPodTeam indexes a Pod by its scheduling team label.
func IndexerFuncPodTeam(obj client.Object) []string {
	pod := obj.(*corev1.Pod)
	if team, ok := pod.Labels[agentsv1alpha1.LabelTeam]; ok && team != "" {
		return []string{team}
	}
	return nil
}

// IndexerFuncPodUser indexes a Pod by its scheduling user label.
func IndexerFuncPodUser(obj client.Object) []string {
	pod := obj.(*corev1.Pod)
	if user, ok := pod.Labels[agentsv1alpha1.LabelUser]; ok && user != "" {
		return []string{user}
	}
	return nil
}

// ---------------------------------------------------------------------------
// SandboxPool indexer functions
// ---------------------------------------------------------------------------

// IndexerFuncPoolTeam indexes a SandboxPool by its scheduling team label.
func IndexerFuncPoolTeam(obj client.Object) []string {
	pool := obj.(*agentsv1alpha1.SandboxPool)
	if team, ok := pool.Labels[agentsv1alpha1.LabelTeam]; ok && team != "" {
		return []string{team}
	}
	return nil
}

// IndexerFuncPoolUser indexes a SandboxPool by its scheduling user label.
func IndexerFuncPoolUser(obj client.Object) []string {
	pool := obj.(*agentsv1alpha1.SandboxPool)
	if user, ok := pool.Labels[agentsv1alpha1.LabelUser]; ok && user != "" {
		return []string{user}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Setup
// ---------------------------------------------------------------------------

// SetupIndexers registers all required indexes for the controller.
// This must be called before the manager starts, typically during setup.
func SetupIndexers(ctx context.Context, mgr ctrl.Manager) error {
	// Pod: SandboxIDLabelKey
	if err := mgr.GetFieldIndexer().IndexField(ctx, &corev1.Pod{}, IndexFieldSandboxID, IndexerFuncSandboxID); err != nil {
		return err
	}

	// Pod: SandboxPoolLabelKey
	if err := mgr.GetFieldIndexer().IndexField(ctx, &corev1.Pod{}, IndexFieldSandboxPool, IndexerFuncSandboxPool); err != nil {
		return err
	}

	// Pod: PodPhaseLabelKey
	if err := mgr.GetFieldIndexer().IndexField(ctx, &corev1.Pod{}, IndexFieldSandboxPhase, IndexerFuncSandboxPhase); err != nil {
		return err
	}

	// Pod: composite Pool+Phase index
	if err := mgr.GetFieldIndexer().IndexField(ctx, &corev1.Pod{}, IndexFieldSandboxPoolPhase, IndexerFuncSandboxPoolPhase); err != nil {
		return err
	}

	// Pod: Team label
	if err := mgr.GetFieldIndexer().IndexField(ctx, &corev1.Pod{}, IndexFieldTeam, IndexerFuncPodTeam); err != nil {
		return err
	}

	// Pod: User label
	if err := mgr.GetFieldIndexer().IndexField(ctx, &corev1.Pod{}, IndexFieldUser, IndexerFuncPodUser); err != nil {
		return err
	}

	// SandboxPool: Team label
	if err := mgr.GetFieldIndexer().IndexField(ctx, &agentsv1alpha1.SandboxPool{}, IndexFieldTeam, IndexerFuncPoolTeam); err != nil {
		return err
	}

	// SandboxPool: User label
	if err := mgr.GetFieldIndexer().IndexField(ctx, &agentsv1alpha1.SandboxPool{}, IndexFieldUser, IndexerFuncPoolUser); err != nil {
		return err
	}

	return nil
}

// ---------------------------------------------------------------------------
// Query helpers — Pod
// ---------------------------------------------------------------------------

func GetPodBySandboxID(ctx context.Context, c client.Client, sandboxID string) (*corev1.Pod, error) {
	podList := &corev1.PodList{}
	if err := c.List(ctx, podList, client.MatchingFields{
		IndexFieldSandboxID: sandboxID,
	}); err != nil {
		return nil, err
	}
	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("no pod found for sandbox ID %q", sandboxID)
	}
	if len(podList.Items) > 1 {
		podKeys := make([]string, len(podList.Items))
		for i, pod := range podList.Items {
			podKeys[i] = client.ObjectKeyFromObject(&pod).String()
		}
		return nil, fmt.Errorf("multiple pods found for sandbox ID %q: %v", sandboxID, podKeys)
	}

	return &podList.Items[0], nil
}

func ListPodsBySandboxPool(ctx context.Context, c client.Client, namespace, poolName string) ([]corev1.Pod, error) {
	podList := &corev1.PodList{}
	if err := c.List(ctx, podList,
		client.InNamespace(namespace),
		client.MatchingFields{IndexFieldSandboxPool: poolName}); err != nil {
		return nil, err
	}
	return podList.Items, nil
}

func ListPodsBySandboxPoolAndPhase(ctx context.Context, c client.Client, namespace, poolName, phase string) ([]corev1.Pod, error) {
	podList := &corev1.PodList{}
	if err := c.List(ctx, podList,
		client.InNamespace(namespace),
		client.MatchingFields{
			IndexFieldSandboxPool:  poolName,
			IndexFieldSandboxPhase: phase,
		}); err != nil {
		return nil, err
	}
	return podList.Items, nil
}

// ListIdlePodsForPool uses the composite pool+phase index for an efficient
// single-index query returning only idle pods in the given pool.
func ListIdlePodsForPool(ctx context.Context, c client.Client, namespace, poolName string) ([]corev1.Pod, error) {
	podList := &corev1.PodList{}
	if err := c.List(ctx, podList,
		client.InNamespace(namespace),
		client.MatchingFields{
			IndexFieldSandboxPoolPhase: SandboxPoolPhaseIndexValue(poolName, agentsv1alpha1.SandboxPhaseIdle),
		}); err != nil {
		return nil, err
	}
	return podList.Items, nil
}

// ---------------------------------------------------------------------------
// Query helpers — SandboxPool
// ---------------------------------------------------------------------------

// ListSandboxPoolsByTeamUser returns all SandboxPools in the given namespace
// that match both team and user labels. Pass empty strings to skip a filter.
// When namespace is empty, the search spans all namespaces.
func ListSandboxPoolsByTeamUser(ctx context.Context, c client.Client, namespace, team, user string) ([]agentsv1alpha1.SandboxPool, error) {
	poolList := &agentsv1alpha1.SandboxPoolList{}
	opts := []client.ListOption{}
	if namespace != "" {
		opts = append(opts, client.InNamespace(namespace))
	}
	// Use label-based matching for team/user (MatchingLabels is the simplest
	// correct approach since field indexes only support MatchingFields with a
	// single field per query in controller-runtime). For two-field filtering we
	// fall through to post-filter.
	if team != "" && user == "" {
		opts = append(opts, client.MatchingLabels{agentsv1alpha1.LabelTeam: team})
	} else if team == "" && user != "" {
		opts = append(opts, client.MatchingLabels{agentsv1alpha1.LabelUser: user})
	} else if team != "" && user != "" {
		opts = append(opts, client.MatchingLabels{
			agentsv1alpha1.LabelTeam: team,
			agentsv1alpha1.LabelUser: user,
		})
	}
	if err := c.List(ctx, poolList, opts...); err != nil {
		return nil, err
	}
	return poolList.Items, nil
}
