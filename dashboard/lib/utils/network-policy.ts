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

// Shape checks for the egress-policy fields, mirroring what the server would
// otherwise reject on the create call. They belong next to the create form:
// an environment decides only whether the egress gateway exists, so every rule
// and every allowlist entry is typed here.

import { z } from "zod"

/** Splits a textarea into trimmed, de-duplicated entries on newlines or commas. */
export function splitLines(s?: string): string[] {
  const seen = new Set<string>()
  return (s ?? "")
    .split(/[\n,]+/)
    .map((x) => x.trim())
    .filter((x) => x !== "" && !seen.has(x) && seen.add(x) !== undefined)
}

const domainRe = /^(\*|\*\.([a-z0-9-]+\.)+[a-z]{2,}|([a-z0-9-]+\.)+[a-z]{2,})$/i

export function isCIDRorIP(s: string): boolean {
  if (s.includes(":")) return true // IPv6 (incl. CIDR) — accept loosely
  const m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})(\/(\d{1,2}))?$/.exec(s)
  if (!m) return false
  if ([m[1], m[2], m[3], m[4]].some((o) => Number(o) > 255)) return false
  if (m[6] !== undefined && Number(m[6]) > 32) return false
  return true
}

/** The subset of a form's values these checks read. */
export interface NetworkPolicyValues {
  networkPolicyMode: "unrestricted" | "disable" | "allowlist"
  allowedDomains?: string
  allowedCIDRs?: string
  deniedCIDRs?: string
  injectionRuleRows?: { host?: string; headerName?: string; secretName?: string }[]
}

/**
 * Checks the egress fields and the injection rules against each other. Both come
 * from the same request, so they can genuinely contradict: a rule host outside
 * the allowlist is dropped before the L7 path ever runs, leaving a rule that
 * looks configured and never fires.
 */
export function validateNetworkPolicy(v: NetworkPolicyValues, ctx: z.RefinementCtx): void {
  if (v.networkPolicyMode === "allowlist") {
    for (const d of splitLines(v.allowedDomains)) {
      if (!domainRe.test(d)) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ["allowedDomains"],
          message: "envs.form.errors.invalidDomain",
        })
        break
      }
    }
    for (const key of ["allowedCIDRs", "deniedCIDRs"] as const) {
      for (const c of splitLines(v[key])) {
        if (!isCIDRorIP(c)) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            path: [key],
            message: "envs.form.errors.invalidCidr",
          })
          break
        }
      }
    }
  }

  const allowed = new Set(splitLines(v.allowedDomains))
  ;(v.injectionRuleRows ?? []).forEach((r, i) => {
    if (!r.host) return
    if (r.host.includes("*")) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ["injectionRuleRows", i, "host"],
        message: "envs.form.errors.injectionWildcardHost",
      })
    }
    if (v.networkPolicyMode === "allowlist" && allowed.size > 0 && !allowed.has(r.host)) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ["injectionRuleRows", i, "host"],
        message: "envs.form.errors.injectionHostNotAllowed",
      })
    }
  })
}
