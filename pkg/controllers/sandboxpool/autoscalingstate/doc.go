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

// Package autoscalingstate hosts the read-snapshot / write-accumulator
// abstraction used by the SandboxPool autoscaler.
//
// The package follows a three-step pipeline:
//
//  1. Loader.Load(pool) — build a Snapshot capturing every input the
//     decision logic needs (the Pool, its owning SandboxEnv, sibling Pools
//     in the same scaling group, the in-process PoolScheduler queue stats,
//     the in-process LastCreateTracker value, idle Pod ages).
//
//  2. Decide(snap, mut) — pure function in this same package consumes
//     the Snapshot and accumulates writes into a Mutator via
//     PatchStatus / SetTargetReplicas / Mark* helpers. No K8s I/O.
//
//  3. Mutator.Commit(ctx, client, recorder) — applies the accumulated
//     writes in a single pass: at most one Env-spec patch, one
//     Pool-status sub-resource patch, and N per-Pod annotation patches,
//     each wrapped in retry.RetryOnConflict.
//
// The separation matters because:
//
//   - The decision logic stays pure and is easy to unit-test by
//     hand-building a Snapshot and asserting on the resulting Mutator
//     without standing up a fake K8s client.
//
//   - All status writes coalesce: a single reconcile pass writes the
//     SandboxPool status at most once. This avoids the cache-race
//     class of bugs where multiple intra-reconcile status patches
//     against a slowly-propagating informer cache silently drop
//     bookkeeping fields and let the cooldown gate be bypassed.
//
// # Cluster scoping
//
// Every member lookup — this Pool's config, a sibling's config, and the
// replica write itself — is scoped to the SandboxEnv cluster segment whose
// ClusterID equals Loader.LocalClusterID. Pool names are unique within a
// segment but repeat across them: one Env fanned out to several clusters
// gives the same name an entry under each, and a renamed cluster leaves its
// old segment behind with every name still in it.
//
// An unscoped lookup therefore resolves to whichever segment is listed first,
// which may belong to another cluster. For a read that silently applies a
// foreign scaling group; for the write it is worse, because only the Worker
// owning a segment materialises Pools from it — the target lands where nothing
// reads it, the Pool never grows, and the autoscaler re-decides the same
// scale-up every cooldown window forever.
//
// Scaling a Pool in another cluster is not this operator's job in the first
// place: cross-cluster placement is a routing decision (see
// apiserver/service/envscheduler), and the owning cluster's own autoscaler
// grows the Pool once a request lands there.
//
// # Event volume
//
// The autoscaler is edge-triggered in intent but level-triggered in fact: when
// a decision cannot take effect it is reached again on the next cooldown, and
// each conclusion would otherwise post an Event — an unbounded stream of
// identical API-server writes for exactly the Pools that are already in
// trouble. EventThrottle suppresses a repeat of an identical message while
// letting any changed message through immediately. It governs reporting only;
// a throttled cycle still performs its spec and status writes.
package autoscalingstate
