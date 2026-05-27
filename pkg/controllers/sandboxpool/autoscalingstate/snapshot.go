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

package autoscalingstate

import (
	"context"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/inplaceupdate"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/schedule"
)

// Snapshot is the read-only view of every input the Pool autoscaler's
// decision logic needs in one reconcile cycle. Built by Loader.Load; once
// returned, the decision logic treats it as immutable.
//
// Pointers are used for "may be absent" inputs (Env not found, group not
// configured, no PoolScheduler running yet, etc.) so the decision logic
// can distinguish "missing" from "zero".
type Snapshot struct {
	// Pool is the SandboxPool being reconciled. Always non-nil. The
	// reconciler hands a deep copy to Load; the Snapshot retains the
	// reference for downstream readers.
	Pool *agentsv1alpha1.SandboxPool

	// Env is the SandboxEnv that owns Pool, resolved via the
	// agentbox.navix.sh/env label first, falling back to ownerReferences.
	// nil when the Pool has no owning Env (treated as "unmanaged Pool,
	// autoscaling disabled"). nil also when the Env was deleted between
	// Pool list and Env get — the loader does not treat this as an error
	// because the next reconcile will see the Pool's own deletion soon.
	Env *agentsv1alpha1.SandboxEnv

	// MemberConfig is the Pool's EnvClusterMemberConfig from
	// env.spec.clusters[].members[].config. nil when Env is nil OR when
	// the Pool's name is not in the Env's member list (which would be a
	// stale orphan — autoscaling is disabled in that case).
	MemberConfig *agentsv1alpha1.EnvClusterMemberConfig

	// Group is env.spec.autoscaling.groups[name == MemberConfig.ScalingGroup].
	// nil when:
	//   - Env is nil
	//   - MemberConfig.ScalingGroup is empty
	//   - The named group is not declared on env.spec.autoscaling.Groups
	//   - env.spec.autoscaling is nil
	// nil means autoscaling is disabled for this Pool; the decision logic
	// must skip every scale-up / scale-down evaluation.
	Group *agentsv1alpha1.EnvAutoscalingGroup

	// SiblingPools is the set of Pools in the same Env and same scaling
	// group (this Pool included). Sorted by metadata.name for stable
	// tie-breaks in cross-Pool priority comparisons. Empty when Group is
	// nil. Pools are deep copies returned from the cache via client.List.
	SiblingPools []*agentsv1alpha1.SandboxPool

	// PoolSchedSnap is the in-process snapshot of this Pool's
	// PoolScheduler (queue length, idle ready count, reserved count,
	// last dispatch). nil when no scheduler has been registered for this
	// Pool yet (first-ever Create has not been observed by the apiserver).
	PoolSchedSnap *schedule.Snapshot

	// LastCreateAt is the timestamp of the most recent Sandbox.Create
	// request for this Pool, resolved with the following priority:
	//   1. In-process LastCreateTracker (fresh; updated synchronously by
	//      the Create handler);
	//   2. Pool.Annotations[LastSandboxCreateTimeAnnotationKey] (the
	//      throttled flush mirror; survives process restart).
	// nil when no Create has ever been recorded for this Pool.
	LastCreateAt *time.Time

	// IdlePodAges contains one entry per idle Pod owned by this Pool, in
	// descending order (oldest first). Each entry is `Now - GetPodPhaseSince(p, Idle)`.
	// Pods whose phase-since timestamp could not be resolved are skipped.
	// Used by the scale-down candidate selector to compare against
	// scaleDownPolicy.idleTimeoutSeconds.
	IdlePodAges []time.Duration

	// Now is the wall-clock time captured at Load entry. Used by every
	// duration comparison downstream so a single Snapshot evaluates
	// consistently end-to-end even if the decision logic runs slowly.
	Now time.Time
}

// IsAutoscalingEnabled reports whether the decision logic should consider
// this Pool for scale-up / scale-down. Both an owning group and that
// group's Enabled flag must be true.
func (s *Snapshot) IsAutoscalingEnabled() bool {
	return s.Group != nil && s.Group.Enabled
}

// GroupDesiredTotal returns the sum of spec.replicas across every Pool in
// the same scaling group, including self. Used by the group MaxReplicas
// ceiling check before a scale-up is committed.
func (s *Snapshot) GroupDesiredTotal() int32 {
	total := int32(0)
	for _, p := range s.SiblingPools {
		total += p.Spec.Replicas
	}
	return total
}

// GroupIdleTotal returns the sum of status.idleReplicas across every Pool
// in the same scaling group, including self.
func (s *Snapshot) GroupIdleTotal() int32 {
	total := int32(0)
	for _, p := range s.SiblingPools {
		total += p.Status.IdleReplicas
	}
	return total
}

// IsReactiveDemand reports whether the in-process scheduler currently has
// at least one claim request queued AND no idle Pod available to serve it.
// This is the reactive (immediate) scale-up signal that replaces the
// previous PoolScaleUpPendingAnnotationKey doorbell.
//
// Returns false when no PoolScheduler has been registered for this Pool
// yet — in that case there can be no queued claims either.
func (s *Snapshot) IsReactiveDemand() bool {
	if s.PoolSchedSnap == nil {
		return false
	}
	return s.PoolSchedSnap.QueueLen > 0 && s.PoolSchedSnap.IdleReady == 0
}

// OldestIdleAge returns the age of the oldest idle Pod (longest time
// spent in idle phase). Returns 0 and false when there are no idle Pods
// with a resolvable phase-since timestamp.
func (s *Snapshot) OldestIdleAge() (time.Duration, bool) {
	if len(s.IdlePodAges) == 0 {
		return 0, false
	}
	// IdlePodAges is sorted descending by Load; element 0 is the oldest.
	return s.IdlePodAges[0], true
}

// Loader builds Snapshots from the K8s cache plus the two in-process
// state sources. All dependencies are interfaces so unit tests can
// inject fakes; production wires controller-runtime client +
// k8sSandboxService + lifecycle/lastcreate.Tracker.
type Loader struct {
	// Client is the controller-runtime client (typically an informer-cache
	// backed reader). Required.
	Client client.Reader

	// Schedulers exposes the in-process PoolScheduler registry. nil is
	// allowed; the loader then leaves Snapshot.PoolSchedSnap nil for
	// every Pool, which the decision logic treats as "no reactive
	// signal".
	Schedulers SchedulerLookup

	// LastCreate exposes the in-process Create-time tracker. nil is
	// allowed; the loader then falls back to the persisted annotation
	// on the Pool object.
	LastCreate LastCreateTracker

	// Clock provides Now(). When nil, SystemClock() is used.
	Clock Clock
}

// Load assembles a Snapshot for the given Pool. The Pool argument is
// stored by reference; callers must not mutate it after Load returns.
//
// Errors are returned only for unexpected I/O failures (cache list /
// get returning a non-NotFound error). "Soft" misses — Env missing,
// scaling group not configured, no idle Pods — produce a successful
// Snapshot with the corresponding fields left nil/empty and are signalled
// to the decision logic via the IsAutoscalingEnabled helper.
func (l *Loader) Load(ctx context.Context, pool *agentsv1alpha1.SandboxPool) (*Snapshot, error) {
	if l == nil {
		return nil, fmt.Errorf("autoscalingstate: nil Loader")
	}
	if pool == nil {
		return nil, fmt.Errorf("autoscalingstate: nil Pool")
	}
	if l.Client == nil {
		return nil, fmt.Errorf("autoscalingstate: Loader.Client is required")
	}
	clk := l.Clock
	if clk == nil {
		clk = SystemClock()
	}

	snap := &Snapshot{Pool: pool, Now: clk.Now()}

	// 1) In-process signals first — these are pure memory reads so we
	//    never short-circuit them on later errors.
	if l.Schedulers != nil {
		if sched := l.Schedulers.GetScheduler(pool.Namespace, pool.Name); sched != nil {
			s := sched.Snapshot()
			snap.PoolSchedSnap = &s
		}
	}
	snap.LastCreateAt = resolveLastCreate(pool, l.LastCreate)

	// 2) Reverse-lookup the owning Env. Prefer the LabelEnv index; fall
	//    back to ownerReferences for Pools created before the redesign.
	envName, hasEnv := resolveEnvName(pool)
	if !hasEnv {
		return snap, nil
	}

	env := &agentsv1alpha1.SandboxEnv{}
	if err := l.Client.Get(ctx, client.ObjectKey{Namespace: pool.Namespace, Name: envName}, env); err != nil {
		if apierrors.IsNotFound(err) {
			// Env vanished mid-flight; treat as unmanaged. The Pool
			// will be GC'd by ownerRef cascade if the Env deletion
			// was final, so the next reconcile is the right place
			// to converge.
			return snap, nil
		}
		return nil, fmt.Errorf("get env %s/%s: %w", pool.Namespace, envName, err)
	}
	snap.Env = env

	// 3) Resolve the member config and the scaling group.
	snap.MemberConfig = findMemberConfig(env, pool.Name)
	if snap.MemberConfig != nil && snap.MemberConfig.ScalingGroup != "" && env.Spec.Autoscaling != nil {
		snap.Group = findGroup(env.Spec.Autoscaling.Groups, snap.MemberConfig.ScalingGroup)
	}

	// 4) Sibling Pools — list those carrying the same env label, then
	//    filter down to the same scaling group. We always include the
	//    Pool itself (using the in-hand object, not the listed copy, so
	//    callers see the version they passed in).
	if snap.Group != nil {
		var err error
		snap.SiblingPools, err = l.listSiblings(ctx, pool, envName, snap.Group.Name, env)
		if err != nil {
			return nil, err
		}
	}

	// 5) Idle Pod ages — only needed when autoscaling is on. Pods are
	//    selected by the SandboxPool label + idle phase label so the
	//    cache filter does the work without us walking every Pod.
	if snap.IsAutoscalingEnabled() {
		ages, err := l.loadIdlePodAges(ctx, pool, snap.Now)
		if err != nil {
			return nil, err
		}
		snap.IdlePodAges = ages
	}

	return snap, nil
}

// resolveEnvName returns the owning Env name and whether the Pool is
// claimed at all. Preference order:
//  1. metadata.labels[LabelEnv]
//  2. ownerReferences entry with Kind == SandboxEnv
//
// Returns ("", false) when neither is present.
func resolveEnvName(pool *agentsv1alpha1.SandboxPool) (string, bool) {
	if v := pool.Labels[agentsv1alpha1.LabelEnv]; v != "" {
		return v, true
	}
	for _, or := range pool.OwnerReferences {
		if or.Kind == "SandboxEnv" && or.Name != "" {
			return or.Name, true
		}
	}
	return "", false
}

// findMemberConfig returns a pointer into env.Spec.Clusters[].Members[]
// matching poolName, or nil when no match is found. The caller treats the
// pointer as read-only.
func findMemberConfig(env *agentsv1alpha1.SandboxEnv, poolName string) *agentsv1alpha1.EnvClusterMemberConfig {
	if env == nil {
		return nil
	}
	for i := range env.Spec.Clusters {
		ms := env.Spec.Clusters[i].Members
		for j := range ms {
			if ms[j].Name == poolName {
				return &ms[j].Config
			}
		}
	}
	return nil
}

// findGroup returns a pointer into groups[name == wanted], or nil.
func findGroup(groups []agentsv1alpha1.EnvAutoscalingGroup, wanted string) *agentsv1alpha1.EnvAutoscalingGroup {
	for i := range groups {
		if groups[i].Name == wanted {
			return &groups[i]
		}
	}
	return nil
}

// listSiblings returns every Pool owned by the same Env whose member
// config places it in the named scaling group. Self is included by name.
//
// The list is sorted by metadata.name so cross-Pool priority comparisons
// downstream are deterministic.
func (l *Loader) listSiblings(
	ctx context.Context,
	self *agentsv1alpha1.SandboxPool,
	envName, groupName string,
	env *agentsv1alpha1.SandboxEnv,
) ([]*agentsv1alpha1.SandboxPool, error) {
	// Build the index of {pool name -> scaling group} from the Env so
	// we can filter the listed Pools without re-resolving member configs
	// per pool. This is O(members) once vs O(members * pools) inline.
	groupByName := map[string]string{}
	for ci := range env.Spec.Clusters {
		for mi := range env.Spec.Clusters[ci].Members {
			m := &env.Spec.Clusters[ci].Members[mi]
			groupByName[m.Name] = m.Config.ScalingGroup
		}
	}

	list := &agentsv1alpha1.SandboxPoolList{}
	if err := l.Client.List(ctx, list,
		client.InNamespace(self.Namespace),
		client.MatchingLabels{agentsv1alpha1.LabelEnv: envName},
	); err != nil {
		return nil, fmt.Errorf("list sibling pools: %w", err)
	}

	out := make([]*agentsv1alpha1.SandboxPool, 0, len(list.Items))
	seenSelf := false
	for i := range list.Items {
		p := &list.Items[i]
		if groupByName[p.Name] != groupName {
			continue
		}
		if p.Name == self.Name {
			// Use the caller's pointer to preserve any in-flight
			// edits the caller made before invoking Load.
			out = append(out, self)
			seenSelf = true
			continue
		}
		out = append(out, p)
	}
	// Defensive: if the cache hadn't observed self yet (rare; happens
	// during the first reconcile of a freshly-materialised Pool), still
	// include self so the math is correct.
	if !seenSelf {
		out = append(out, self)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// loadIdlePodAges lists every idle Pod owned by pool and returns their
// ages relative to `now`, sorted descending (oldest first). Pods whose
// idle-since timestamp could not be resolved are skipped.
func (l *Loader) loadIdlePodAges(ctx context.Context, pool *agentsv1alpha1.SandboxPool, now time.Time) ([]time.Duration, error) {
	pods := &corev1.PodList{}
	if err := l.Client.List(ctx, pods,
		client.InNamespace(pool.Namespace),
		client.MatchingLabels{
			agentsv1alpha1.SandboxPoolLabelKey:  pool.Name,
			agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseIdle,
		},
	); err != nil {
		return nil, fmt.Errorf("list idle pods: %w", err)
	}
	ages := make([]time.Duration, 0, len(pods.Items))
	for i := range pods.Items {
		since, ok, _ := inplaceupdate.GetPodPhaseSince(&pods.Items[i], agentsv1alpha1.SandboxPhaseIdle)
		if !ok || since.IsZero() {
			continue
		}
		ages = append(ages, now.Sub(since))
	}
	sort.Slice(ages, func(i, j int) bool { return ages[i] > ages[j] })
	return ages, nil
}

// resolveLastCreate applies the in-memory tracker then the persisted
// annotation fallback. Returns nil when neither source has a value.
func resolveLastCreate(pool *agentsv1alpha1.SandboxPool, tracker LastCreateTracker) *time.Time {
	if tracker != nil {
		if t, ok := tracker.Get(pool.Namespace, pool.Name); ok && !t.IsZero() {
			return &t
		}
	}
	raw := pool.Annotations[agentsv1alpha1.LastSandboxCreateTimeAnnotationKey]
	if raw == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil || t.IsZero() {
		return nil
	}
	return &t
}
