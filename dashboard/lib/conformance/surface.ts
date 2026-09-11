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
 * What each surface can reach, and the one list of things neither is expected to.
 *
 * The console and `abx` are two renderings of one platform. Nothing enforces
 * that today, so they have already drifted: the CLI's deep link for a pool
 * silently lands on its env, `instancetypes` links to `templates`, and a hint
 * advertises a sub-resource that was never registered. The conformance test
 * beside this file turns "they should agree" into something that fails a build.
 *
 * An operation may be absent from a surface — but only deliberately, with a
 * reason recorded here. A gap that is not in INTENTIONALLY_ABSENT is a bug, not
 * a backlog item, because the failure mode is silent: a capability ships on one
 * surface and the other never learns it exists.
 */

/** `METHOD /path` exactly as the OpenAPI document spells it. */
export type OperationKey = string

export const op = (method: string, path: string): OperationKey =>
  `${method.toUpperCase()} ${path}`

/**
 * Operations neither surface is required to expose, each with the reason.
 *
 * Keep this list short and argued. "Not built yet" is not a reason — that is
 * exactly the drift the test exists to catch.
 */
export const INTENTIONALLY_ABSENT: Record<OperationKey, string> = {
  // Sandboxes are created and driven with the E2B SDK, not the platform CLI.
  // That boundary is the product's, not an oversight: `abx` speaks platform
  // concepts (envs, pools, quotas) and the sandbox itself speaks E2B.
  [op('POST', '/sandboxes')]: 'sandbox lifecycle is the E2B SDK surface',
  [op('DELETE', '/sandboxes/{sandboxId}')]: 'sandbox lifecycle is the E2B SDK surface',
  [op('POST', '/sandboxes/{sandboxId}/exec')]: 'sandbox lifecycle is the E2B SDK surface',
  [op('POST', '/sandboxes/{sandboxId}/exec-token')]: 'sandbox lifecycle is the E2B SDK surface',
  [op('GET', '/sandboxes/{sandboxId}/is_ready')]: 'sandbox lifecycle is the E2B SDK surface',
  [op('PUT', '/sandboxes/{sandboxId}/timeout')]: 'sandbox lifecycle is the E2B SDK surface',

  // Browser-session concerns. An agent authenticates with a platform key and
  // has no session to establish or tear down.
  [op('GET', '/auth/whoami')]: 'CLI exposes this as `abx whoami`, not as a resource',

  // The envelope around the same groups `abx envs <env> scaling-groups` reads.
  // Adding a second spelling for one list would give the CLI two commands that
  // answer identically, which is the vocabulary problem this file exists to
  // prevent — the console fetches it because its form edits the container.
  [op('GET', '/envs/{name}/autoscaling')]:
    'the CLI reads the same groups through /envs/{name}/autoscaling/groups',
}

/**
 * Operations an Agent-mode key may never perform, whatever the approval flow
 * says.
 *
 * Minting a key is the one write that can dissolve the approval boundary
 * itself: an agent that can issue a credential can issue one without the Agent
 * restriction and proceed unsupervised. No approval dialog can convey that,
 * because what is being approved looks like "create an API key" rather than
 * "stop gating me". The CLI answers these with a console link for the person to
 * act on themselves — deliberately NOT an approval request, since there is
 * nothing here a person should be able to wave through from a prompt.
 *
 * Vault secrets are deliberately NOT here: the credential lands in the acting
 * person's own vault and widens nobody's authority, and writing them is exactly
 * the integration work people want an agent to finish for them.
 */
export const AGENT_KEY_FORBIDDEN: Record<OperationKey, string> = {
  [op('POST', '/api-keys')]: 'minting a credential would let an agent escape its own approval gate',
  [op('POST', '/admin/api-keys')]: 'minting a credential would let an agent escape its own approval gate',
  [op('POST', '/admin/api-keys/{name}/promote')]:
    'promoting a key is minting authority by another name',
}
