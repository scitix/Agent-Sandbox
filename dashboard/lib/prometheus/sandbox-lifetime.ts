/**
 * Copyright 2026 ScitiX
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

/**
 * Pure helpers for computing the time range of a sandbox's lifetime when
 * rendering its CPU/memory charts. Kept free of React imports so it can be
 * unit-tested under the node vitest environment.
 */

function toUnixSeconds(iso: string): number {
  return Math.floor(new Date(iso).getTime() / 1000)
}

export interface SandboxTimestamps {
  claimedAt?: string
  startedAt?: string
  terminatedAt?: string
  recycledAt?: string
}

/**
 * Resolve start/end (unix seconds) for a sandbox metrics chart.
 *
 * - `start` = startedAt if present, else claimedAt. We prefer startedAt so the
 *   window covers only the time the workload was actually running, not the
 *   scheduling / image-pull latency before it.
 * - `end`   = recycledAt if present, else terminatedAt. recycledAt is the moment
 *   the sandbox finished recycling (persistence record written); for most
 *   terminal records it is strictly >= terminatedAt. Falling back to
 *   terminatedAt keeps older records (pre-recycle field) rendering.
 */
export function sandboxLifetimeBounds(sandbox: SandboxTimestamps): {
  start?: number
  end?: number
} {
  const startIso = sandbox.startedAt ?? sandbox.claimedAt
  const endIso = sandbox.recycledAt ?? sandbox.terminatedAt
  return {
    start: startIso ? toUnixSeconds(startIso) : undefined,
    end: endIso ? toUnixSeconds(endIso) : undefined,
  }
}
