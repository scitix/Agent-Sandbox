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
}

export interface E2BNetworkConfig {
  allowOut?: string[]
  denyOut?: string[]
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

  switch (values.networkPolicyMode) {
    case "disable":
      // E2B's sugar for "deny all egress"; the backend gives it precedence over
      // any allow rules, so nothing else is worth sending alongside it.
      body.allow_internet_access = false
      break
    case "allowlist": {
      // Domains and CIDRs go out as one list — the backend's splitAllowOut()
      // partitions them again by parsing each entry.
      const allowOut = [...splitList(values.allowedDomains), ...splitList(values.allowedCIDRs)]
      const denyOut = splitList(values.deniedCIDRs)
      const network: E2BNetworkConfig = {}
      if (allowOut.length > 0) network.allowOut = allowOut
      if (denyOut.length > 0) network.denyOut = denyOut
      if (Object.keys(network).length > 0) body.network = network
      break
    }
    default:
      // Unrestricted: send no network config at all. An empty object would still
      // mean "policy declared", which switches on the anti-SSRF baseline.
      break
  }

  return body
}
