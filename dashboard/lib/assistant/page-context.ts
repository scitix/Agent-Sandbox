// The `<page …/>` marker the dashboard appends to every message it sends, and
// the stripper that takes it back out.
//
// WHY it exists: the agent otherwise has no idea what the user is looking at, so
// "the current cluster" / "this node" have no referent unless the conversation
// already named one. WHY it is a marker in the message text rather than a side
// channel: that is the only field the opencode prompt API carries end-to-end and
// stores, so it survives a reload and a session branch — the same reason the
// staged-attachment reference is a marker (see the dashboard's attachment.tsx).
//
// It is a HINT, not a scope. The assistant prompt is explicit that a named object
// still goes through `navix search` (which spans every cluster), because a
// current-cluster default is exactly what made an earlier version of this feature
// send the agent to the wrong cluster.
//
// Two consumers strip it: the dashboard, before handing a message to the
// topic-switch classifier (a page change is not a topic change), and the
// classifier endpoint itself as a backstop.

/**
 * Does this request carry an outgoing user message (and so want the marker)?
 *
 * Lives here, tested, because the opencode endpoint is NOT called `/prompt`: the
 * v2 SDK posts to `/session/{id}/prompt_async`. A first cut matched
 * `prompt(\?|$)`, which excludes exactly that, so the marker was silently never
 * injected — silently, because a non-match is indistinguishable from a page the
 * catalog does not claim. Both spellings are matched now, and a query string is
 * tolerated.
 */
const PROMPT_ENDPOINT = /\/session\/[^/]+\/prompt(_async)?(\?|$)/

export function isPromptRequest(url: string, method: string): boolean {
  return method.toUpperCase() === 'POST' && PROMPT_ENDPOINT.test(url)
}

/** A page the user is on, as the dashboard resolves it from the URL. */
export interface PageContext {
  /** Catalog page key — the same vocabulary `open_page` accepts. Absent on a
   *  route no catalog page claims (the cluster home redirects to the assistant,
   *  so this is the normal state for the first question of a session); the
   *  marker then reports the cluster alone rather than nothing at all. */
  key?: string
  cluster?: string
  /** Route path params (`id`, `namespace`/`name`, …). */
  params?: Record<string, string>
}

/** Matches one `<page … />` marker. Attributes are quoted, so the value may hold
 *  anything but a quote; the tag is self-closing and never nests. */
const PAGE_MARKER = /<page\s+[^>]*\/>/g
const ATTR = /([A-Za-z_][A-Za-z0-9_]*)="([^"]*)"/g

/** Quote-safe attribute value: the marker is parsed back by regex, so a stray
 *  quote or newline in a resource name must not be able to end the tag. */
function attr(value: string): string {
  return value.replace(/["\n\r]/g, ' ').trim()
}

/**
 * Render the marker appended to an outgoing message. Returns '' when there is no
 * page to report, so the caller can concatenate unconditionally.
 *
 * `key` first and `cluster` second because those are the two the reader acts on;
 * the remaining path params follow in their own order. A cluster with no key is
 * a valid marker — an uncatalogued route still pins which cluster the user is
 * looking at, and that alone resolves "the current cluster".
 */
export function renderPageMarker(page?: PageContext): string {
  if (!page?.key && !page?.cluster) return ''
  const parts = page.key ? [`key="${attr(page.key)}"`] : []
  if (page.cluster) parts.push(`cluster="${attr(page.cluster)}"`)
  for (const [k, v] of Object.entries(page.params ?? {})) {
    if (v) parts.push(`${attr(k)}="${attr(v)}"`)
  }
  return `<page ${parts.join(' ')} />`
}

/** Append the marker to a message body. */
export function withPageMarker(text: string, page?: PageContext): string {
  const marker = renderPageMarker(page)
  if (!marker) return text
  return text ? `${text}\n\n${marker}` : marker
}

/**
 * Remove every `<page …/>` marker from a message.
 *
 * Used before topic classification: the user walking from the node list to a pod
 * detail is not a change of subject, and leaving the markers in made the two
 * axes (object / goal) look like they had moved when only the browser had.
 */
export function stripPageMarkers(text: string): string {
  return text
    .replace(PAGE_MARKER, '')
    .replace(/\n{3,}/g, '\n\n')
    .trim()
}

/** Parse the LAST marker out of a message — the last one is the page as of the
 *  moment it was sent. Returns undefined when the message carries none. */
export function parsePageMarker(text: string): PageContext | undefined {
  const found = text.match(PAGE_MARKER)
  if (!found?.length) return undefined
  const attrs: Record<string, string> = {}
  for (const m of found[found.length - 1].matchAll(ATTR)) {
    attrs[m[1]] = m[2]
  }
  const { key, cluster, ...params } = attrs
  if (!key && !cluster) return undefined
  return {
    ...(key ? { key } : {}),
    ...(cluster ? { cluster } : {}),
    ...(Object.keys(params).length ? { params } : {}),
  }
}
