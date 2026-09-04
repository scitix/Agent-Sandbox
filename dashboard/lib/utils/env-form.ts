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

const registryRowSchema = z.object({
  registry: z.preprocess(emptyToUndef, z.string().optional()),
  username: z.preprocess(emptyToUndef, z.string().optional()),
  password: z.preprocess(emptyToUndef, z.string().optional()),
})

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
  // The egress gateway is the environment's whole network decision: whether its
  // sandboxes get the proxy sidecar. What each sandbox may reach, and what gets
  // injected into which request, is chosen per sandbox on the create call.
  gatewayEnabled: z.boolean(),
  // Auto-update rollout policy (Env-level default; per-member override lives
  // on the pool sheet). maxUnavailable is a free-form int-or-percent string.
  autoUpdate: z.boolean(),
  maxUnavailable: z.preprocess(emptyToUndef, z.string().optional()),
  // Existing PersistentVolumeClaims mounted into the sandbox. Only claims in
  // the caller's own namespace are mountable, which is enforced server-side.
  volumeRows: z.array(volumeRowSchema),
})

export const formSchema = baseSchema

export type FormValues = z.infer<typeof formSchema>

/**
 * A blank create form. Same values `envToFormValues(null)` produces, named for
 * the callers that want defaults without pretending to convert an Env.
 */
export const envFormDefaults = (): FormValues => envToFormValues(null)
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
      gatewayEnabled: false,
      autoUpdate: true,
      maxUnavailable: undefined,
      volumeRows: [],
    }
  }
  const overrides = env.spec.overrides
  return {
    name: env.name,
    templateName: env.spec.templateRef.name,
    image: overrides?.image,
    podCreationImagePolicy: overrides?.podCreationImagePolicy ?? "IdleImage",
    defaultStartupTimeout: overrides?.defaultStartupTimeout,
    defaultIdleTimeout: overrides?.defaultIdleTimeout,
    imagePullSecretRows: [],
    gatewayEnabled: overrides?.gateway?.enabled ?? false,
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
  // Emitted only when on. An explicit `{enabled: false}` and an omitted key mean
  // the same thing to the server, and omitting keeps the stored CR clean.
  if (v.gatewayEnabled) o.gateway = { enabled: true }
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
