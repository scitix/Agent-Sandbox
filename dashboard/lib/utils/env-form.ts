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

// Schema, defaults and payload mapping for the SandboxEnv form.
//
// Kept free of React so it can be unit-tested and reused by the clone
// (import/export) path, which needs the schema to validate a file and the
// defaults to fill the gaps an older export leaves — the same split as
// lib/utils/managed-agent-form.ts.

import { z } from "zod"

import type { AgentSandboxEnv } from "@/lib/api/client"

// ─── Form schema ─────────────────────────────────────────────────────────────
//
// The env-creation form holds only env-level identity + overrides. Member
// SandboxPools (with their resource choice + replica counts) are created
// afterwards from the Env detail page via /v1/envs/{name}/sandboxpools.
// Autoscaling lives at the env level but is edited per-pool (one
// scaling-group entry at a time) from the pool table — not from this sheet.

const emptyToUndef = (val: unknown) =>
  typeof val === "string" && val.trim() === "" ? undefined : val

const dnsLabel = /^[a-z]([a-z0-9-]*[a-z0-9])?$/

const injectionCredentialRowSchema = z.object({
  name: z.string().optional(),
  // Write-only. Left blank on an edit it means "keep what is stored" — the API
  // never returns a value, so the form cannot round-trip one.
  value: z.string().optional(),
  // configured marks a credential the server already holds a value for, so a
  // value can be required for new rows without demanding a re-type of old ones.
  configured: z.boolean().optional(),
  exposeAs: z.string().optional(),
  placeholder: z.string().optional(),
})

// The credential name doubles as the key inside the Env's credential Secret,
// so it is limited to what Kubernetes accepts there.
const credNameRe = /^[a-zA-Z0-9][a-zA-Z0-9_-]{0,62}$/

const injectionRuleRowSchema = z.object({
  host: z.string().optional(),
  headerName: z.string().optional(),
  headerValue: z.string().optional(),
  mode: z.enum(["Override", "IfAbsent"]).optional(),
  substitute: z.string().optional(),
  pathPrefixes: z.string().optional(),
})

const registryRowSchema = z.object({
  registry: z.preprocess(emptyToUndef, z.string().optional()),
  username: z.preprocess(emptyToUndef, z.string().optional()),
  password: z.preprocess(emptyToUndef, z.string().optional()),
})

// splitLines turns a textarea value into a trimmed, de-duplicated list, split on
// newlines or commas. Shared by validation, payload assembly, and read-back.
export function splitLines(s?: string): string[] {
  const seen = new Set<string>()
  return (s ?? "")
    .split(/[\n,]+/)
    .map((x) => x.trim())
    .filter((x) => x !== "" && !seen.has(x) && seen.add(x) !== undefined)
}

const domainRe = /^(\*|\*\.([a-z0-9-]+\.)+[a-z]{2,}|([a-z0-9-]+\.)+[a-z]{2,})$/i

function isCIDRorIP(s: string): boolean {
  if (s.includes(":")) return true // IPv6 (incl. CIDR) — accept loosely
  const m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})(\/(\d{1,2}))?$/.exec(s)
  if (!m) return false
  if (m[1]! > "255" || [m[1], m[2], m[3], m[4]].some((o) => Number(o) > 255)) return false
  if (m[6] !== undefined && Number(m[6]) > 32) return false
  return true
}

/** Field shapes without the cross-field refinement, so `.partial()` works. */
// A single PersistentVolumeClaim mount. readOnly defaults to true everywhere:
// the sandbox runs as root with passwordless sudo, so a writable mount means the
// agent can delete anything under it.
const volumeRowSchema = z.object({
  claimName: z.string().min(1, "envs.form.errors.volumeClaimRequired"),
  mountPath: z
    .string()
    .min(1, "envs.form.errors.volumeMountPathRequired")
    .startsWith("/", "envs.form.errors.volumeMountPathAbsolute"),
  subPath: z.preprocess(emptyToUndef, z.string().optional()),
  readOnly: z.boolean(),
})

export const baseSchema = z.object({
  name: z
    .string()
    .min(1, "envs.form.errors.nameRequired")
    .max(24, "envs.form.errors.nameTooLong")
    .regex(dnsLabel, "envs.form.errors.nameDnsLabel"),
  templateName: z.string().min(1, "envs.form.errors.templateRequired"),
  image: z.preprocess(emptyToUndef, z.string().optional()),
  podCreationImagePolicy: z.enum(["PoolDefaultImage", "IdleImage"]).optional(),
  imagePullSecretRows: z.array(registryRowSchema),
  defaultStartupTimeout: z.preprocess(emptyToUndef, z.string().optional()),
  defaultIdleTimeout: z.preprocess(emptyToUndef, z.string().optional()),
  // Network policy: a 3-way mode gates the egress rules.
  networkPolicyMode: z.enum(["unrestricted", "disable", "allowlist"]),
  allowedDomains: z.preprocess(emptyToUndef, z.string().optional()),
  allowedCIDRs: z.preprocess(emptyToUndef, z.string().optional()),
  deniedCIDRs: z.preprocess(emptyToUndef, z.string().optional()),
  allowPrivateNetworks: z.boolean(),
  // Credential injection: the sidecar adds these headers on the way out, so
  // the sandbox can use a credential it can never read.
  injectionCredentialRows: z.array(injectionCredentialRowSchema),
  injectionRuleRows: z.array(injectionRuleRowSchema),
  // Auto-update rollout policy (Env-level default; per-member override lives
  // on the pool sheet). maxUnavailable is a free-form int-or-percent string.
  autoUpdate: z.boolean(),
  maxUnavailable: z.preprocess(emptyToUndef, z.string().optional()),
  // Existing PersistentVolumeClaims mounted into the sandbox. Only claims in
  // the caller's own namespace are mountable, which is enforced server-side.
  volumeRows: z.array(volumeRowSchema),
})

export const formSchema = baseSchema.superRefine((v, ctx) => {
  validateInjection(v, ctx)
  if (v.networkPolicyMode !== "allowlist") return
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
})

export type FormValues = z.infer<typeof formSchema>

/**
 * A blank create form. Same values `envToFormValues(null)` produces, named for
 * the callers that want defaults without pretending to convert an Env.
 */
export const envFormDefaults = (): FormValues => envToFormValues(null)
// ─── Credential injection ───────────────────────────────────────────────────

// splitCommas parses a comma-separated free-text field into trimmed entries.
function splitCommas(v: string | undefined): string[] {
  return (v ?? "")
    .split(",")
    .map((x) => x.trim())
    .filter(Boolean)
}

// buildSecretInjection folds the credential and rule rows into the wire shape.
// Rows are keyed by host so several header rows on one host become one rule
// carrying several headers, matching how the proxy evaluates them.
function buildSecretInjection(v: FormValues): Record<string, unknown> | undefined {
  const credentials = v.injectionCredentialRows
    .filter((c) => c.name)
    .map((c) => ({
      name: c.name!,
      // Omitted when left blank on an edit, so the server keeps the stored
      // value rather than clearing it.
      ...(c.value ? { value: c.value } : {}),
      ...(c.exposeAs ? { exposeAs: c.exposeAs } : {}),
      ...(c.placeholder ? { placeholder: c.placeholder } : {}),
    }))

  const byHost = new Map<string, Record<string, unknown>>()
  for (const r of v.injectionRuleRows) {
    if (!r.host) continue
    let rule = byHost.get(r.host)
    if (!rule) {
      rule = { host: r.host, headers: [] as Record<string, unknown>[] }
      byHost.set(r.host, rule)
    }
    if (r.headerName && r.headerValue) {
      ;(rule.headers as Record<string, unknown>[]).push({
        name: r.headerName,
        value: r.headerValue,
        ...(r.mode && r.mode !== "Override" ? { mode: r.mode } : {}),
      })
    }
    const sub = splitCommas(r.substitute)
    if (sub.length) rule.substitute = sub
    const paths = splitCommas(r.pathPrefixes)
    if (paths.length) rule.pathPrefixes = paths
  }
  const rules = [...byHost.values()].map((r) => {
    const headers = r.headers as Record<string, unknown>[]
    if (headers.length === 0) delete r.headers
    return r
  })

  if (credentials.length === 0 && rules.length === 0) return undefined
  return { credentials, rules }
}

// validateInjection mirrors the server-side checks that would otherwise only
// surface when a sandbox is created hours later.
export function validateInjection(v: FormValues, ctx: z.RefinementCtx): void {
  const names = new Set<string>()
  v.injectionCredentialRows.forEach((c, i) => {
    if (!c.name && !c.value) return
    if (!c.name) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ["injectionCredentialRows", i, "name"],
        message: "envs.form.errors.injectionCredentialIncomplete",
      })
      return
    }
    if (!credNameRe.test(c.name)) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ["injectionCredentialRows", i, "name"],
        message: "envs.form.errors.injectionCredentialName",
      })
    }
    // Blank is fine only when the server already holds a value for this one.
    if (!c.value && !c.configured) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ["injectionCredentialRows", i, "value"],
        message: "envs.form.errors.injectionValueRequired",
      })
    }
    names.add(c.name)
    if (c.placeholder && c.placeholder.length < 16) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ["injectionCredentialRows", i, "placeholder"],
        message: "envs.form.errors.placeholderTooShort",
      })
    }
    if (c.placeholder && !c.exposeAs) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ["injectionCredentialRows", i, "exposeAs"],
        message: "envs.form.errors.placeholderNeedsExposeAs",
      })
    }
  })

  const allowed = new Set(splitLines(v.allowedDomains))
  v.injectionRuleRows.forEach((r, i) => {
    if (!r.host) return
    if (r.host.includes("*")) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ["injectionRuleRows", i, "host"],
        message: "envs.form.errors.injectionWildcardHost",
      })
    }
    // A host outside the allowlist is dropped before the L7 path ever runs.
    if (v.networkPolicyMode === "allowlist" && allowed.size > 0 && !allowed.has(r.host)) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ["injectionRuleRows", i, "host"],
        message: "envs.form.errors.injectionHostNotAllowed",
      })
    }
    if (r.headerValue) {
      const refs = [...r.headerValue.matchAll(/\{\{\s*([a-zA-Z0-9_-]+)\s*\}\}/g)].map((m) => m[1])
      if (refs.length === 0) {
        // A literal here would be a plaintext secret stored in the CR.
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ["injectionRuleRows", i, "headerValue"],
          message: "envs.form.errors.injectionValueNeedsCredential",
        })
      }
      for (const ref of refs) {
        if (!names.has(ref)) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            path: ["injectionRuleRows", i, "headerValue"],
            message: "envs.form.errors.injectionUnknownCredential",
          })
          break
        }
      }
    }
  })
}
// ─── Form ↔ API mapping ──────────────────────────────────────────────────────

export function envToFormValues(env: AgentSandboxEnv | null): FormValues {
  if (!env) {
    return {
      name: "",
      templateName: "",
      image: undefined,
      podCreationImagePolicy: "IdleImage",
      defaultStartupTimeout: undefined,
      defaultIdleTimeout: undefined,
      imagePullSecretRows: [],
      networkPolicyMode: "unrestricted",
      allowedDomains: undefined,
      allowedCIDRs: undefined,
      deniedCIDRs: undefined,
      allowPrivateNetworks: false,
      injectionCredentialRows: [],
      injectionRuleRows: [],
      autoUpdate: true,
      maxUnavailable: undefined,
      volumeRows: [],
    }
  }
  const overrides = env.spec.overrides
  const np = overrides?.networkPolicy
  const mode: FormValues["networkPolicyMode"] = np?.disableEgress
    ? "disable"
    : np?.egress
      ? "allowlist"
      : "unrestricted"
  return {
    name: env.name,
    templateName: env.spec.templateRef.name,
    image: overrides?.image,
    podCreationImagePolicy: overrides?.podCreationImagePolicy ?? "IdleImage",
    defaultStartupTimeout: overrides?.defaultStartupTimeout,
    defaultIdleTimeout: overrides?.defaultIdleTimeout,
    imagePullSecretRows: [],
    networkPolicyMode: mode,
    allowedDomains: (np?.egress?.allowedDomains ?? []).join("\n") || undefined,
    allowedCIDRs: (np?.egress?.allowedCIDRs ?? []).join("\n") || undefined,
    deniedCIDRs: (np?.egress?.deniedCIDRs ?? []).join("\n") || undefined,
    allowPrivateNetworks: np?.allowPrivateNetworks ?? false,
    injectionCredentialRows: (np?.secretInjection?.credentials ?? []).map((c) => ({
      name: c.name,
      value: "", // never returned by the API
      configured: Boolean(c.valueFrom),
      exposeAs: c.exposeAs ?? "",
      placeholder: c.placeholder ?? "",
    })),
    injectionRuleRows: (np?.secretInjection?.rules ?? []).flatMap((r) =>
      (r.headers && r.headers.length > 0
        ? r.headers
        : [{ name: "", value: "", mode: undefined }]
      ).map((h) => ({
        host: r.host,
        headerName: h.name,
        headerValue: h.value,
        mode: (h.mode as "Override" | "IfAbsent" | undefined) ?? "Override",
        substitute: (r.substitute ?? []).join(", "),
        pathPrefixes: (r.pathPrefixes ?? []).join(", "),
      })),
    ),
    autoUpdate: overrides?.updateStrategy?.autoUpdate ?? true,
    maxUnavailable: overrides?.updateStrategy?.maxUnavailable,
    // readOnly is always returned explicitly by the server, but default to the
    // safe value anyway so a response from an older server cannot silently
    // present a dataset as writable.
    volumeRows: (overrides?.volumes ?? []).map((v) => ({
      claimName: v.claimName,
      mountPath: v.mountPath,
      subPath: v.subPath,
      readOnly: v.readOnly ?? true,
    })),
  }
}

export function formValuesToCreateBody(v: FormValues) {
  return {
    name: v.name,
    mode: "WarmPool" as const,
    templateRef: { name: v.templateName },
    overrides: buildOverrides(v),
  }
}

export function formValuesToUpdateBody(v: FormValues) {
  return {
    overrides: buildOverrides(v),
  }
}

function buildOverrides(v: FormValues) {
  const o: Record<string, unknown> = {}
  if (v.image) o.image = v.image
  if (v.podCreationImagePolicy) o.podCreationImagePolicy = v.podCreationImagePolicy
  if (v.defaultStartupTimeout) o.defaultStartupTimeout = v.defaultStartupTimeout
  if (v.defaultIdleTimeout) o.defaultIdleTimeout = v.defaultIdleTimeout
  const registries = v.imagePullSecretRows
    .filter((r) => r.registry && r.username && r.password)
    .map((r) => ({ registry: r.registry!, username: r.username!, password: r.password! }))
  if (registries.length > 0) {
    o.imagePullSecret = { registries }
  }
  const np = buildNetworkPolicy(v)
  if (np) o.networkPolicy = np
  const us = buildUpdateStrategy(v)
  if (us) o.updateStrategy = us
  // Always emit volumes, including as an empty array. PATCH replaces the whole
  // overrides block, so an omitted key clears the field — which is how the user
  // removes their last mount.
  o.volumes = v.volumeRows.map((r) => ({
    claimName: r.claimName,
    mountPath: r.mountPath,
    ...(r.subPath ? { subPath: r.subPath } : {}),
    readOnly: r.readOnly,
  }))
  // Always return the object, never undefined. `undefined` makes
  // formValuesToUpdateBody send `{}`, which the server reads as "overrides not
  // supplied" and leaves untouched — so clearing every override used to be a
  // silent no-op. An explicit `overrides: {}` is what actually clears it.
  return o
}

// buildUpdateStrategy emits the rollout override only when it deviates from the
// inherited defaults (autoUpdate=true, maxUnavailable=20%), keeping the CR clean.
function buildUpdateStrategy(v: FormValues): Record<string, unknown> | undefined {
  const us: Record<string, unknown> = {}
  if (v.autoUpdate === false) us.autoUpdate = false
  if (v.maxUnavailable) us.maxUnavailable = v.maxUnavailable
  return Object.keys(us).length ? us : undefined
}

// buildNetworkPolicy maps the form's mode + fields onto the wire networkPolicy
// object, or undefined for "unrestricted" (omit the field).
function buildNetworkPolicy(v: FormValues): Record<string, unknown> | undefined {
  const injection = buildSecretInjection(v)
  // Injection alone is a valid configuration: it still needs the sidecar, which
  // is what "networkPolicy is set" means, but it filters nothing.
  if (v.networkPolicyMode === "unrestricted") {
    return injection ? { secretInjection: injection } : undefined
  }
  const np: Record<string, unknown> = {}
  if (injection) np.secretInjection = injection
  if (v.allowPrivateNetworks) np.allowPrivateNetworks = true
  if (v.networkPolicyMode === "disable") {
    np.disableEgress = true
    return np
  }
  // allowlist
  const egress: Record<string, string[]> = {}
  const domains = splitLines(v.allowedDomains)
  const allow = splitLines(v.allowedCIDRs)
  const deny = splitLines(v.deniedCIDRs)
  if (domains.length) egress.allowedDomains = domains
  if (allow.length) egress.allowedCIDRs = allow
  if (deny.length) egress.deniedCIDRs = deny
  np.egress = egress
  return np
}
