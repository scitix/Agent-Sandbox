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

import { RESOURCES, WRITE_DOCS, childrenOf, rootResources } from '@headless/index'
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

/**
 * Resources this credential can actually reach.
 *
 * An admin-only resource is dropped for anyone who is not an admin, for the
 * same reason a gated one is: an agent reading this document treats everything
 * in it as available, and a command whose only possible answer is `admin api
 * key required` costs a round trip and teaches it that the document is wrong.
 * `null` — no answer yet — shows everything, because hiding a command the
 * caller does have is not recoverable from the API's own error.
 */
function roleOut(
  resources: readonly ResourceSpec[],
  gates: Record<string, boolean> | null,
  role: string | null,
) {
  const visible = gatedOut(resources, gates)
  return role === null || role === 'admin' ? visible : visible.filter((r) => !r.admin)
}

export function agentContext(
  version: string,
  gates: Record<string, boolean> | null = null,
  role: string | null = null,
) {
  const resources = roleOut(RESOURCES, gates, role)
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
        'abx create <collection…> -f FILE',
        'abx update <item…> -f FILE',
        'abx delete <item…>',
        'abx scale <item…> --replicas N',
      ],
      notes: [
        'A verb is read in the FIRST position and nowhere else: everything after it is an address, so `abx envs apply` is the env called apply.',
        'create and update take the SAME file; what differs is the address (a collection vs one object) and whether the object exists yet.',
        'The file is the request body, NOT the object `--json` prints. `abx <address> --editable` prints the current values in exactly that form — it is the starting point for a create and the safe one for an update.',
        '`abx create <address> --schema` and `abx update <address> --schema` print that file, field by field, as JSON — no deployment, key or cluster needed. Only the verbs that take a file have one.',
        'update is a PUT of the whole object: a field the file leaves out is a field you are asking to remove.',
        'scale changes size and nothing else — it re-sends the current bounds unchanged.',
        'Filters are client-side and uniform: --filter key=value, where key is a column heading.',
        '--json prints the raw API shape; the table view is a projection of it.',
      ],
    },
    flags: {
      '--cluster': 'cluster this command addresses',
      '--endpoint': "the console's address; one address reaches every cluster",
      '--cluster-api': "one cluster's own API base, bypassing the console; --cluster is then refused",
      '--api-key': 'platform credential',
      '--filter': 'key=value, repeatable',
      '--limit': 'rows to print (default 200)',
      '--json': 'machine output',
      '--editable': 'the file a write takes, as JSON — the same document create and update accept',
      '--schema': 'the file a create/update takes, as JSON, instead of writing — the reads have none',
      '--csv': 'machine output, flat',
      '--wide': 'include the columns held back by default',
    },
    auth: {
      note:
        'Only --endpoint and --api-key are ever required. Which header the key travels in follows from the mode: the console takes Authorization: Bearer, a cluster API takes AGENTBOX-API-KEY.',
      override: '--auth-scheme api-key|bearer forces it, for a deployment that answers to neither.',
      modes:
        'Console mode is the default and the norm: --endpoint is the console address, and --cluster picks any cluster behind it. Direct mode is the exception, entered by setting --cluster-api (AGENTBOX_CLUSTER_API) to one cluster\'s own API — for a sandbox with no route to the console. That address answers for one cluster, so --cluster is refused rather than silently ignored.',
    },
    roots: roleOut(rootResources(), gates, role).map((r) => r.plural),
    resources: resources.map((r) => ({
      plural: r.plural,
      kind: r.kind,
      describe: r.describe,
      parent: r.parent,
      detail: r.detail ?? false,
      gate: r.gate,
      writes: r.api.verbs ?? [],
      /**
       * The file a write takes, field by field, generated from the API schema.
       *
       * This is the reason an agent never has to be told the shape in prose:
       * the same data renders `abx create <address> --help` for a person, so
       * the two cannot drift — and neither can a skill that says "read
       * agent-context" instead of transcribing it.
       */
      body: WRITE_DOCS.find((d) => d.plural === r.plural),
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
