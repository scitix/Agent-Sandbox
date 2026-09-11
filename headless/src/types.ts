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
 * The shapes both surfaces derive from.
 *
 * Framework-free on purpose: the console compiles this under Next.js, the CLI
 * compiles the same files under Bun, and neither can introduce a dependency the
 * other cannot load. A React import here would quietly make the CLI
 * unbuildable.
 */

/** A URL path segment, which is also the CLI's positional token for the same thing. */
export type Segment = string

/**
 * One filter a resource accepts.
 *
 * `values`, when present, is the closed set. It exists so an error can name
 * what would have worked: a rejection that lists the valid values is one an
 * agent can recover from in a single retry, where a bare "invalid filter" costs
 * a round trip per guess.
 */
export interface FilterSpec {
  key: string
  describe: string
  values?: readonly string[]
}

/**
 * One column of a list view.
 *
 * There is deliberately no `header` field. The CLI prints `id` verbatim, so the
 * heading a person reads IS the `--filter` key they would type; the console
 * renders its own i18n label against the same id. A separate header string
 * would be a third name for one concept and would drift from both.
 */
export interface ColumnSpec {
  id: string
  describe: string
  /** When set, this column is also filterable under the named FilterSpec. */
  filter?: string
  /** Hidden until asked for. Keeps the default view narrow enough to read. */
  optional?: boolean
}

/**
 * A sub-resource: a collection that hangs off one instance of a parent.
 *
 * `segment` is used by BOTH surfaces — it is the console's route segment and
 * the CLI's third positional token — which is what allows a console URL and an
 * `abx` invocation to be derived from one another without a lookup table.
 */
export interface SubResourceSpec {
  segment: Segment
  describe: string
  columns?: readonly ColumnSpec[]
}

/**
 * A view that is not a collection — metrics, logs.
 *
 * Kept distinct from sub-resources because the CLI must NOT try to address
 * these positionally: `abx envs x metrics` would imply a list of metrics, which
 * is not a thing. They exist here so route derivation knows the segment is
 * legitimate rather than a typo.
 */
export interface ViewSpec {
  segment: Segment
  describe: string
}

export interface ResourceSpec {
  /** Singular kind, as the API names it. */
  kind: string
  /** Plural — the console route segment AND the CLI's first positional token. */
  plural: Segment
  describe: string
  /** Absent when the resource has no per-item page (a pure list). */
  detail?: boolean
  columns: readonly ColumnSpec[]
  filters?: readonly FilterSpec[]
  subResources?: readonly SubResourceSpec[]
  views?: readonly ViewSpec[]
  /**
   * The feature gate this resource depends on, from GET /feature-gates. Absent
   * means always available. Both surfaces hide the resource when the gate is
   * off, so a deployment without the feature never advertises it.
   */
  gate?: string
}
