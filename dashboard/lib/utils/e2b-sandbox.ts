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

// Maps the create-sandbox form onto E2B's NewSandbox body.
//
// AgentBox-specific inputs that E2B has no field for travel as reserved metadata
// keys, which the E2B handler consumes and strips before the remainder is stored
// as the sandbox's own metadata (pkg/e2bcompat/handlers/server.go).

import { durationToSeconds } from "@/lib/utils/duration"

/** Reserved metadata keys the E2B handler consumes rather than stores. */
export const E2B_META_IMAGE = "agentbox.scitix.ai/image"
export const E2B_META_STARTUP_TIMEOUT = "agentbox.scitix.ai/startup-timeout"
export const E2B_META_ALLOW_PRIVATE_NETWORKS = "agentbox.scitix.ai/allow-private-networks"

export interface KeyValueRow {
  key?: string
  value?: string
}

export interface E2BSandboxFormValues {
  /** Env name. Becomes `templateID`; `cluster::env` is accepted by the backend. */
  poolName: string
  image?: string
  startupTimeout?: string
  idleTimeout?: string
  envVarRows?: KeyValueRow[]
  metadataRows?: KeyValueRow[]
  autoPause?: boolean
  secure?: boolean
  networkPolicyMode?: "unrestricted" | "disable" | "allowlist"
  allowedDomains?: string
  allowedCIDRs?: string
  deniedCIDRs?: string
  allowPrivateNetworks?: boolean
  injectionRuleRows?: InjectionRuleRow[]
}

/**
 * One header the gateway sets on the way out to `host`, carrying the value of a
 * vault entry the sandbox never sees.
 *
 * `secretName` is a vault entry name, not a value: the wire carries
 * `${e2b.secrets.<name>}`, which is exactly what the E2B SDK's `Secret.fill()`
 * produces. The server refuses a literal, so there is no shape of this form that
 * can put a credential into a request body or an access log.
 */
export interface InjectionRuleRow {
  host?: string
  headerName?: string
  secretName?: string
}

export interface E2BNetworkConfig {
  allowOut?: string[]
  denyOut?: string[]
  rules?: Record<string, Array<{ transform: { headers: Record<string, string> } }>>
}

export interface E2BCreateBody {
  templateID: string
  timeout?: number
  metadata?: Record<string, string>
  envVars?: Record<string, string>
  autoPause?: boolean
  secure?: boolean
  allow_internet_access?: boolean
  network?: E2BNetworkConfig
}

/** Splits a textarea into trimmed, de-duplicated entries on newlines or commas. */
function splitList(value?: string): string[] {
  const seen = new Set<string>()
  return (value ?? "")
    .split(/[\n,]+/)
    .map((entry) => entry.trim())
    .filter((entry) => entry !== "" && !seen.has(entry) && seen.add(entry) !== undefined)
}

function rowsToRecord(rows?: KeyValueRow[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const row of rows ?? []) {
    const key = row.key?.trim()
    if (!key) continue
    out[key] = row.value ?? ""
  }
  return out
}

export function buildE2BCreateBody(values: E2BSandboxFormValues): E2BCreateBody {
  const body: E2BCreateBody = { templateID: values.poolName.trim() }

  const idleSeconds = durationToSeconds(values.idleTimeout)
  if (idleSeconds !== undefined) body.timeout = idleSeconds

  // User metadata first, so a reserved key set below always wins over a hand-typed
  // one — otherwise the image field and a stray metadata row could disagree.
  const metadata = rowsToRecord(values.metadataRows)
  const image = values.image?.trim()
  if (image) metadata[E2B_META_IMAGE] = image
  const startupSeconds = durationToSeconds(values.startupTimeout)
  if (startupSeconds !== undefined) metadata[E2B_META_STARTUP_TIMEOUT] = String(startupSeconds)
  if (Object.keys(metadata).length > 0) body.metadata = metadata

  const envVars = rowsToRecord(values.envVarRows)
  if (Object.keys(envVars).length > 0) body.envVars = envVars

  if (values.autoPause) body.autoPause = true
  if (values.secure) body.secure = true

  const rules = buildInjectionRules(values.injectionRuleRows)

  if (values.networkPolicyMode === "disable") {
    // E2B's sugar for "deny all egress"; the backend gives it precedence over
    // any allow rules, so nothing else is worth sending alongside it. Rules are
    // refused server-side in this combination rather than silently dropped, and
    // the form blocks it before that.
    body.allow_internet_access = false
    return body
  }

  const network: E2BNetworkConfig = {}
  if (values.networkPolicyMode === "allowlist") {
    // Domains and CIDRs go out as one list — the backend's splitAllowOut()
    // partitions them again by parsing each entry.
    const allowOut = [...splitList(values.allowedDomains), ...splitList(values.allowedCIDRs)]
    const denyOut = splitList(values.deniedCIDRs)
    if (allowOut.length > 0) network.allowOut = allowOut
    if (denyOut.length > 0) network.denyOut = denyOut
  }
  if (rules) network.rules = rules
  // Unrestricted with no rules sends no network config at all. An empty object
  // would still mean "policy declared", which switches on the anti-SSRF baseline
  // and would cut the sandbox off from everything that resolves inside the
  // cluster.
  if (Object.keys(network).length > 0) body.network = network

  // Lifting the anti-SSRF baseline has no field in E2B's network config, so it
  // travels as a reserved metadata key the server consumes. Only meaningful
  // alongside a network config: with none there is no filter to relax.
  if (values.allowPrivateNetworks && body.network) {
    body.metadata = { ...(body.metadata ?? {}), [E2B_META_ALLOW_PRIVATE_NETWORKS]: "true" }
  }

  return body
}

/**
 * Folds the rule rows into E2B's per-domain transform map. Several header rows
 * on one host collapse into one entry, which is how the proxy evaluates them.
 * Returns undefined when nothing complete was entered, so an empty editor never
 * turns an unrestricted create into a filtered one.
 */
function buildInjectionRules(
  rows: InjectionRuleRow[] | undefined,
): E2BNetworkConfig["rules"] | undefined {
  const byHost: Record<string, Record<string, string>> = {}
  for (const row of rows ?? []) {
    const host = row.host?.trim()
    const header = row.headerName?.trim()
    const secret = row.secretName?.trim()
    if (!host || !header || !secret) continue
    byHost[host] = { ...(byHost[host] ?? {}), [header]: `\${e2b.secrets.${secret}}` }
  }
  const hosts = Object.keys(byHost)
  if (hosts.length === 0) return undefined
  const out: NonNullable<E2BNetworkConfig["rules"]> = {}
  for (const host of hosts) out[host] = [{ transform: { headers: byHost[host]! } }]
  return out
}
