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
package autoscalingstate
