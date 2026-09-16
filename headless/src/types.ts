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
  /**
   * Where the value lives in the API response, as a dot path, when that is not
   * the column id itself.
   *
   * The id stays the short name a person types and reads — `replicas`, not
   * `spec.replicas` — because it is also the `--filter` key and the CSV header.
   * The wire shape is an implementation detail of the response and is allowed
   * to be nested without dragging its nesting into the vocabulary.
   */
  path?: string
  /** When set, this column is also filterable under the named FilterSpec. */
  filter?: string
  /** Hidden until asked for. Keeps the default view narrow enough to read. */
  optional?: boolean
}

/**
 * One line of a detail view.
 *
 * A get and a list are two different projections of the same object, and the
 * CLI used to render both with the list's columns — which is why `abx envs x`
 * printed a row of dashes: `templateName`/`mode`/`memberCount` live under
 * `spec`/`status` in the get response, not at its top level. Naming the fields
 * a get actually has is what lets the detail view be right about the wire
 * shape while the list view stays narrow.
 *
 * `path` is the wire location, `id` is the name a person reads — the same
 * split ColumnSpec makes, for the same reason.
 */
export interface DetailFieldSpec {
  id: string
  /** Where the value lives in the response. Defaults to the id. */
  path?: string
  describe: string
  /**
   * Print the whole value rather than one line of it.
   *
   * For the fields that ARE the answer to a question — a template's rendered
   * docs, a pool's pod-template YAML. Truncating them here would only move the
   * truncation into whatever harness reads the output, with less information
   * about what was cut.
   */
  text?: boolean
}

/**
 * A view that is not a collection — metrics, logs.
 *
 * Kept distinct from resources because neither surface may address these as if
 * they held items: `abx sandboxes x logs y` names nothing. They are registered
 * so route derivation can tell a legitimate segment from a typo.
 */
export interface ViewSpec {
  segment: Segment
  describe: string
  /** Native API path, when the view has one. Metrics come from Prometheus instead. */
  api?: string
  /**
   * The response is not a row set, and the table renderer would make it look
   * like a broken one: `GET /sandboxes/{id}/logs` answers with a snapshot —
   * container list, source, and lines — and rendering that as "one sandbox"
   * printed a table of dashes with the logs sitting unread inside it.
   *
   * `docs` is the other shape. The field printed is Markdown, and a document
   * handed to a person or an agent has to arrive as the document — not as one
   * cell of a table with its newlines eaten.
   */
  shape?: 'logs' | 'docs'
  /**
   * Which field of the response a `docs` view prints. Named on the view rather
   * than guessed from the segment, because "the document lives here" is a fact
   * about the resource and not about the renderer.
   */
  field?: string
}

/** What a surface may do to a resource, beyond reading it. */
export type Verb = 'create' | 'update' | 'delete' | 'scale'

/**
 * Where a resource lives in the API.
 *
 * Path templates are written in the OpenAPI document's own spelling —
 * `/envs/{name}/sandboxpools/{poolName}`, not a normalised `{id}` — because
 * these strings do double duty: the CLI fills the placeholders positionally,
 * and the conformance test compares them against the spec verbatim. One string
 * cannot drift from itself.
 *
 * The route segment is NOT derived from this path, and the two differ on
 * purpose: `scaling-groups` is what the concept is called everywhere a person
 * reads it, while the API still spells it `/autoscaling/groups`. Renaming a URL
 * is cheap; renaming a published API operation is not.
 */
export interface ApiSpec {
  /** Collection path. Absent for a resource with no list endpoint. */
  list?: string
  /** Item path. Absent for a pure list. */
  item?: string
  /**
   * The response field the list lives under, when it is not the obvious one.
   *
   * `/approvals` returns two lists in one envelope — what is being asked for
   * and what has already been granted — so each is registered as the resource
   * it is, reading its own field of the same response. Guessing by convention
   * would have picked one and silently hidden the other.
   */
  listField?: string
  /**
   * False when the item path exists for writes but cannot be read.
   *
   * Not a detail worth hiding: an API key can be deleted by name and never
   * fetched by name, and a CLI that assumed otherwise would offer a `get` that
   * 404s on every call.
   */
  itemReadable?: boolean
  /**
   * Verbs beyond reading. `create` is POST on `list`; `update` and `delete` act
   * on `item`. `scale` adds no operation of its own — it is a narrow client of
   * the same PUT as `update`, which is why it is a verb here and not a path.
   */
  verbs?: readonly Verb[]
  /**
   * Verbs an Agent-mode key may never perform, with the reason.
   *
   * Distinct from the approval gate, which holds a write for a person to
   * release. These are refused outright, because what makes them dangerous is
   * not the single action but that performing it dissolves the gate itself —
   * and no approval dialog can convey that.
   */
  agentForbidden?: Partial<Record<Verb, string>>
}

/**
 * A named action on one item: something that is neither a read nor a
 * whole-object write.
 *
 * Deliberately rare. Every action is a word a person and an agent both have to
 * learn, and one that does not appear in any URL, so it cannot be derived from
 * a console address the way the rest of the grammar can. An action earns its
 * place only when the thing it does has no honest spelling as state — deciding
 * an approval is an event, not a field you can set.
 */
export interface ActionSpec {
  /** The CLI's trailing verb, and the label the console uses for the button. */
  name: string
  describe: string
  method: 'POST' | 'PUT' | 'DELETE'
  /** Path template, filled positionally like any other. */
  path: string
  /** Fixed request body, when the action IS the payload. */
  body?: Record<string, unknown>
  /** Set when an Agent-mode key may never perform it, with the reason. */
  agentForbidden?: string
}

export interface ResourceSpec {
  /** Singular kind, as the API names it. */
  kind: string
  /** Plural — the console route segment AND the CLI's first positional token. */
  plural: Segment
  describe: string
  api: ApiSpec
  /**
   * The resource this one hangs off, by plural.
   *
   * A child is never addressable on its own: a pool exists within an env, and
   * both surfaces spell it that way — `/envs/{env}/pools/{pool}` and
   * `abx envs {env} pools {pool}`. Declaring the relationship here is what lets
   * one registry entry serve as both the child resource and the parent's
   * sub-resource, instead of the two copies that drifted before.
   */
  parent?: Segment
  /** Absent when the resource has no per-item page (a pure list). */
  detail?: boolean
  columns: readonly ColumnSpec[]
  /**
   * The shape a get renders, one line per field.
   *
   * Absent means the get has nothing of its own to show and the columns are
   * the honest answer. Present means the two projections genuinely differ and
   * the list's summary columns would answer with dashes.
   */
  detailFields?: readonly DetailFieldSpec[]
  filters?: readonly FilterSpec[]
  views?: readonly ViewSpec[]
  actions?: readonly ActionSpec[]
  /**
   * False for a resource that is not addressed *within* a cluster — today only
   * `clusters` itself, which supplies the prefix every other resource sits
   * under. Without this the console link for the cluster list comes out as
   * `/clusters/{cluster}/clusters`, which is a real page nowhere.
   */
  clusterScoped?: boolean
  /**
   * False when the console has no page for this resource, so nothing offers a
   * "view in console" link to a URL that 404s. The CLI is then the only place
   * it is reachable, which is a deliberate state and not a gap.
   */
  consolePage?: boolean
  /**
   * True for a resource only an admin credential reaches.
   *
   * This is about what a caller is SHOWN, not about what the API enforces —
   * the server still refuses an admin route to a tenant key. Advertising a
   * command whose answer is always `admin api key required` costs an agent a
   * round trip per attempt and teaches it that the help text lies.
   */
  admin?: boolean
  /**
  * Whether this child appears as a table on its parent's detail view.
   *
   * A pool list is worth reading next to the env that owns it; an env's events
   * are a separate page. Marking it here rather than in the CLI keeps the
   * decision with the resource, where the rest of its shape already lives.
   */
  inParentDetail?: boolean
  /**
   * One sentence for this resource's `--help`, when the registry knows
   * something the columns and filters cannot say.
   *
   * Today that is the read/write split of templates: this entry is the catalog
   * everyone may read, and the thing that writes templates is a different
   * resource. Without the sentence, a tenant reads `templates --help` and
   * concludes the platform's templates are immutable.
   */
  helpNote?: string
  /**
   * The feature gate this resource depends on, from GET /feature-gates. Absent
   * means always available. Both surfaces hide the resource when the gate is
   * off, so a deployment without the feature never advertises it.
   */
  gate?: string
}

/** One API operation a surface reaches, derived from the registry. */
export interface ApiOperation {
  method: 'GET' | 'POST' | 'PUT' | 'DELETE'
  path: string
  resource: Segment
}
