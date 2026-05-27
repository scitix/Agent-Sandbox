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
//  1. Loader.Load(pool)  — build a Snapshot capturing every input the
//     decision logic needs (the Pool, its owning SandboxEnv, sibling Pools
//     in the same scaling group, the in-process PoolScheduler queue stats,
//     the in-process LastCreateTracker value, idle Pod ages).
//
//  2. Decision logic (lives in the parent sandboxpool package, added in a
//     later proposal step) consumes the Snapshot and accumulates writes
//     into a Mutator via PatchStatus / SetTargetReplicas / Mark* helpers.
//
//  3. Mutator.Commit(ctx, client, recorder) — applies the accumulated
//     writes in a single pass: one status sub-resource patch (with
//     retry-on-conflict), one spec patch, and N per-Pod annotation patches.
//
// This separation matters because:
//
//   - The decision logic stays pure: easy to unit-test by hand-building a
//     Snapshot and asserting on the resulting Mutator without standing up
//     a fake K8s client.
//
//   - All status writes coalesce: a single reconcile pass writes the
//     SandboxPool status at most once. This eliminates the cache-race
//     class of bugs that the proposal's §0.2 problem #3 documents, where
//     multiple intra-reconcile status patches against a slowly-propagating
//     informer cache could silently drop bookkeeping fields and allow the
//     cooldown gate to be bypassed.
//
// See ../agentbox/docs/proposals/20260527-pool-centric-autoscaling.md for the full
// design context.
package autoscalingstate
