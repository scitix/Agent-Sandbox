// The `<attachment …/>` marker a staged chat attachment leaves in the message
// text, and the parser that takes it back out.
//
// WHY a marker in the message text: the attachment's content lives in the
// agent's sandbox, not in the prompt (proposal 0055 — a 500 KB table must not
// land in the model's context). The message therefore carries only a reference,
// and the message text is the one field the opencode prompt API stores
// end-to-end, so the reference survives a reload and a session branch — the same
// reason the page context travels as a marker (see `page-context.ts`).
//
// Renderer and parser live TOGETHER here because they are one protocol with
// three readers: the dashboard writes the marker on send, renders it back as a
// chip instead of raw text, and strips it before topic classification; the agent
// reads it in the prompt (its AGENTS.md documents the shape).
import { stripPageMarkers } from './page-context'

/** One attachment as it travels in the message text. */
export interface AttachmentMarker {
  /** Display label — data, so it may be localized/non-ASCII. */
  name: string
  /** Absolute sandbox path of the staged file (reference form). */
  path?: string
  /** Human-readable size and line count, so the agent can pick read vs grep. */
  size?: string
  lines?: string
  /** Inline content — the legacy fallback form, where the file travels along. */
  content?: string
}

/** Reference form: `<attachment name="…" path="…" size="…" lines="…" />`.
 *  Attribute values are quote- and angle-sanitized by {@link renderAttachmentMarker},
 *  so `[^>]*` cannot overrun the tag. */
const REF_MARKER = /<attachment\s+[^>]*\/>/g
/** Legacy inline form, whole-part only: the content is arbitrary text, so this
 *  can never be scanned for safely — it is anchored where it is used. */
const INLINE_MARKER = /^<attachment name=([^>]*)>\n([\s\S]*)\n<\/attachment>$/
const INLINE_MARKER_G = /<attachment name=[^>]*>\n[\s\S]*?\n<\/attachment>/g
const ATTR = /([A-Za-z_][A-Za-z0-9_]*)="([^"]*)"/g

/** Attribute value that cannot end the tag or break the scan regexes. */
function attr(value: string): string {
  return value.replace(/["<>\n\r]/g, ' ').trim()
}

/** Render the reference marker for a staged attachment. */
export function renderAttachmentMarker(m: AttachmentMarker): string {
  const parts = [`name="${attr(m.name) || 'attachment'}"`]
  if (m.path) parts.push(`path="${attr(m.path)}"`)
  if (m.size) parts.push(`size="${attr(m.size)}"`)
  if (m.lines) parts.push(`lines="${attr(m.lines)}"`)
  return `<attachment ${parts.join(' ')} />`
}

function parseRefMarker(marker: string): AttachmentMarker | null {
  const attrs: Record<string, string> = {}
  for (const m of marker.matchAll(ATTR)) attrs[m[1]] = m[2]
  if (!attrs.path) return null
  return {
    name: attrs.name || 'attachment',
    path: attrs.path,
    ...(attrs.size ? { size: attrs.size } : {}),
    ...(attrs.lines ? { lines: attrs.lines } : {}),
  }
}

/**
 * Parse a message text part that is NOTHING BUT an attachment marker — the form
 * an attachment arrives in, since assistant-ui appends each attachment's content
 * as its own text part.
 *
 * Page markers are stripped first: the dashboard appends `<page …/>` to an
 * outgoing prompt, and an attachment part that happened to be last used to come
 * back as `<attachment …/>\n\n<page …/>`, which an anchored match rejects — the
 * chip silently degraded into raw marker text in the bubble. Injection now skips
 * attachment parts, and tolerating the marker here also repairs the messages
 * already stored that way.
 */
export function parseAttachmentPart(
  text: string | undefined | null
): AttachmentMarker | null {
  if (!text) return null
  const t = text.trim()
  // Only the reference form is page-marker-tolerant: the legacy form carries the
  // file itself, and the stripper's blank-line collapsing would corrupt the
  // content handed to the download button.
  const bare = stripPageMarkers(t)
  const refs = bare.match(REF_MARKER)
  if (refs?.length === 1 && !bare.replace(REF_MARKER, '').trim()) {
    const parsed = parseRefMarker(refs[0])
    if (parsed) return parsed
  }
  const m = INLINE_MARKER.exec(t)
  return m ? { name: m[1], content: m[2] } : null
}

/** The staged file's name inside the sandbox: the last path segment (the path
 *  carries no session id — one sandbox serves one session). This is the handle the
 *  pod-side staged copy is read back by. */
export function sandboxNameFromPath(path: string): string | undefined {
  return path.split('/').filter(Boolean).pop()
}

/** Every reference marker in a text, in order. Used where a message is flattened
 *  into one string (the topic-switch re-send) and the attachments have to be
 *  carried over as attachments rather than as prose. */
export function collectAttachmentMarkers(text: string): AttachmentMarker[] {
  const out: AttachmentMarker[] = []
  for (const m of text.matchAll(REF_MARKER)) {
    const parsed = parseRefMarker(m[0])
    if (parsed) out.push(parsed)
  }
  return out
}

/**
 * Remove every attachment marker from a message, leaving the user's own prose.
 *
 * Used before topic classification (a file reference is not the subject of the
 * question, and its path is noise) and before the topic-switch re-send, where the
 * marker must not be flattened into the new message's text: the reference points
 * at the OLD session's sandbox, and pasted into prose it renders as raw text
 * instead of a chip.
 */
export function stripAttachmentMarkers(text: string): string {
  return text
    .replace(REF_MARKER, '')
    .replace(INLINE_MARKER_G, '')
    .replace(/[ \t]+\n/g, '\n')
    .replace(/\n{3,}/g, '\n\n')
    .trim()
}
