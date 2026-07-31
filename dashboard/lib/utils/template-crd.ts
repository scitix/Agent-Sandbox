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

/** Pure SandboxTemplate CRD YAML helpers shared by the template editor. */

import { stringify as yamlStringify, parse as yamlParse } from "yaml"

/** Annotation holding the template's Markdown documentation. */
export const DOCS_ANNOTATION = "agentbox.navix.sh/docs"

/** Superseded docs annotation, ignored by the server but still present on older templates. */
export const LEGACY_POOL_DOCS_ANNOTATION = "agentbox.navix.sh/pool-docs"

/** Annotations managed by Kubernetes or kubectl that should never be shown or submitted. */
const K8S_MANAGED_ANNOTATIONS = new Set(["kubectl.kubernetes.io/last-applied-configuration"])

/** Metadata fields written by the API server, never by the editor. */
const SERVER_MANAGED_METADATA = [
  "resourceVersion",
  "uid",
  "creationTimestamp",
  "generation",
  "managedFields",
  "selfLink",
]

/** Return a copy of the annotations map without any K8s-managed keys. */
export function stripManagedAnnotations(
  annotations: Record<string, string> | undefined,
): Record<string, string> | undefined {
  if (!annotations) return undefined
  const filtered = Object.fromEntries(
    Object.entries(annotations).filter(([k]) => !K8S_MANAGED_ANNOTATIONS.has(k)),
  )
  return Object.keys(filtered).length > 0 ? filtered : undefined
}

/** Recursively remove null values from an object/array so the diff doesn't show
 *  noise from server fields serialized as null due to missing omitempty tags. */
export function removeNulls(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(removeNulls)
  if (value !== null && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value as Record<string, unknown>)
        .filter(([, v]) => v !== null)
        .map(([k, v]) => [k, removeNulls(v)]),
    )
  }
  return value
}

/**
 * Strip server-managed read-only metadata fields before showing the diff.
 * Also normalizes the object to match what formToCrdObject produces:
 *   - Ensures apiVersion/kind are present (server YAML may omit them)
 *   - Removes null values recursively (server may serialize omitempty-missing fields as null)
 */
export function stripServerManagedFields(yamlStr: string): string {
  if (!yamlStr) return yamlStr
  try {
    const parsed = yamlParse(yamlStr) as Record<string, unknown>

    // Ensure top-level K8s type fields are present — formToCrdObject always emits them.
    if (!parsed.apiVersion) parsed.apiVersion = "agents.navix.sh/v1alpha1"
    if (!parsed.kind) parsed.kind = "SandboxTemplate"

    const meta = parsed.metadata as Record<string, unknown> | undefined
    if (meta) {
      const cleaned = { ...meta }
      delete cleaned.generation
      delete cleaned.managedFields
      delete cleaned.selfLink
      parsed.metadata = cleaned
    }
    delete parsed.status

    return yamlStringify(removeNulls(parsed))
  } catch {
    return yamlStr
  }
}

/** JSON with object keys sorted, so key order never affects equality. */
export function stableStringify(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(stableStringify).join(",")}]`
  if (value !== null && typeof value === "object") {
    const entries = Object.entries(value as Record<string, unknown>).sort(([a], [b]) =>
      a.localeCompare(b),
    )
    return `{${entries.map(([k, v]) => `${JSON.stringify(k)}:${stableStringify(v)}`).join(",")}}`
  }
  return JSON.stringify(value) ?? "null"
}

/**
 * Canonicalise a template CRD for "did anything other than the docs change?".
 * Drops the docs annotations, server-managed metadata and null-valued fields so
 * the raw YAML the server returned can be compared against the YAML the form
 * produces. Returns null when the input is empty or unparseable.
 */
export function normalizeForDocsComparison(yamlStr: string): string | null {
  if (!yamlStr || !yamlStr.trim()) return null
  try {
    const parsed = yamlParse(yamlStr) as Record<string, unknown>
    if (!parsed || typeof parsed !== "object") return null
    delete parsed.status
    if (!parsed.apiVersion) parsed.apiVersion = "agents.navix.sh/v1alpha1"
    if (!parsed.kind) parsed.kind = "SandboxTemplate"

    const meta = { ...((parsed.metadata as Record<string, unknown> | undefined) ?? {}) }
    for (const k of SERVER_MANAGED_METADATA) delete meta[k]
    const annotations = {
      ...(stripManagedAnnotations(meta.annotations as Record<string, string> | undefined) ?? {}),
    }
    delete annotations[DOCS_ANNOTATION]
    delete annotations[LEGACY_POOL_DOCS_ANNOTATION]
    if (Object.keys(annotations).length > 0) meta.annotations = annotations
    else delete meta.annotations
    parsed.metadata = meta

    return stableStringify(removeNulls(parsed))
  } catch {
    return null
  }
}

/**
 * True when nextYaml differs from baselineYaml in nothing but the docs.
 *
 * Drives the version-bump carve-out in the template editor: a docs-only edit may
 * reuse the current spec.version, because documentation never reaches the
 * rendered Pod and so names no new template revision. The API server applies the
 * same rule, so anything this returns false for is rejected there too.
 */
export function isDocsOnlyChange(baselineYaml: string, nextYaml: string): boolean {
  const before = normalizeForDocsComparison(baselineYaml)
  const after = normalizeForDocsComparison(nextYaml)
  return before !== null && after !== null && before === after
}
