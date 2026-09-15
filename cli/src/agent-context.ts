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
 * The whole CLI, as one JSON document.
 *
 * An agent that runs `--help` per resource pays a round trip per question and
 * still ends up with prose it has to parse. This is the same information in the
 * shape it actually wants: every resource, every filter, every column, every
 * write it may attempt, and the grammar that assembles them.
 *
 * It is generated from the registry rather than written, so it cannot describe
 * a command the CLI does not have — which is the failure mode of every
 * hand-maintained tool manifest.
 */

import { RESOURCES, childrenOf, rootResources } from '@headless/index'
import type { ResourceSpec } from '@headless/types'

/**
 * Resources this deployment actually has.
 *
 * Gated resources are dropped rather than listed with a caveat: an agent that
 * reads this document treats everything in it as available, and a footnote it
 * has to reason about is a footnote it will get wrong. When the gate lookup
 * fails nothing is dropped — advertising one resource too many is recoverable
 * from the API's own error; hiding one is not recoverable at all.
 */
export function gatedOut(resources: readonly ResourceSpec[], gates: Record<string, boolean> | null) {
  if (!gates) return resources
  return resources.filter((r) => !r.gate || gates[r.gate] !== false)
}

export function agentContext(version: string, gates: Record<string, boolean> | null = null) {
  const resources = gatedOut(RESOURCES, gates)
  return {
    tool: 'abx',
    version,
    describe:
      'Platform CLI for AgentBox. Addresses envs, member pools, autoscaling groups, templates and quotas. Sandboxes themselves are created and driven with the E2B SDK — this CLI is about where they come from.',
    grammar: {
      read: [
        'abx <resource>',
        'abx <resource> <id>',
        'abx <resource> <id> <sub>',
        'abx <resource> <id> <sub> <sub-id>',
        'abx <resource> <id> [<sub> <sub-id>] <view>',
      ],
      write: [
        'abx <path…> apply -f FILE',
        'abx <path…> delete',
        'abx <path…> scale --replicas N',
      ],
      notes: [
        'apply is a PUT of the whole object: a field the file leaves out is a field you are asking to remove.',
        'scale changes size and nothing else — it re-sends the current bounds unchanged.',
        'Filters are client-side and uniform: --filter key=value, where key is a column heading.',
        '--json prints the raw API shape; the table view is a projection of it.',
      ],
    },
    flags: {
      '--cluster': 'cluster this command addresses',
      '--endpoint': 'API base; a {cluster} placeholder makes one endpoint route to every cluster',
      '--api-key': 'platform credential',
      '--filter': 'key=value, repeatable',
      '--limit': 'rows to print (default 200)',
      '--json': 'machine output',
      '--csv': 'machine output, flat',
      '--wide': 'include the columns held back by default',
    },
    auth: {
      note:
        'Only --endpoint and --api-key are ever required. Which header the key travels in is read off the endpoint: a console BFF address (/api/clusters/…) takes Authorization: Bearer, a cluster API takes AGENTBOX-API-KEY.',
      override: '--auth-scheme api-key|bearer forces it, for an address of neither shape.',
    },
    roots: gatedOut(rootResources(), gates).map((r) => r.plural),
    resources: resources.map((r) => ({
      plural: r.plural,
      kind: r.kind,
      describe: r.describe,
      parent: r.parent,
      detail: r.detail ?? false,
      gate: r.gate,
      writes: r.api.verbs ?? [],
      filters: (r.filters ?? []).map((f) => ({
        key: f.key,
        describe: f.describe,
        values: f.values,
      })),
      columns: r.columns.map((c) => ({ id: c.id, describe: c.describe, optional: c.optional ?? false })),
      subResources: [
        ...childrenOf(r.plural).map((c) => ({ segment: c.plural, describe: c.describe, collection: true })),
        ...(r.views ?? []).map((v) => ({ segment: v.segment, describe: v.describe, collection: false })),
      ],
    })),
  }
}
