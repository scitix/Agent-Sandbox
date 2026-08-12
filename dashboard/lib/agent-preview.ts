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

// Client for one agent's Brain, as reached through the console's streaming proxy.
//
// The Brain serves two ports and the proxy presents them as one base URL: the
// conversation surface at the root, and the workspace file API under `/_fs`. That
// is the proxy's arrangement, not this file's invention — publishing two base URLs
// made every caller carry two addresses and get one of them wrong.
//
// NOTHING here sends a user identity. The proxy pins it from the caller's session
// and the gateway prefers that over anything a request carries, so a `userKey` sent
// from the browser would be ignored — and sending one anyway would suggest to the
// next reader that it means something.

/** One conversation, as the thread list reports it. */
export interface AgentThread {
  id: string
  title?: string
  updatedAt: number
  backendId?: string
  /** Whether a turn is still running in it. */
  live?: boolean
}

/** A finished message from the transcript. */
export interface TranscriptEntry {
  role: string
  parts: TranscriptPart[]
}

export type TranscriptPart =
  | { type: "text"; text: string }
  | { type: "tool-call"; name: string; args?: unknown }
  | { type: string; [k: string]: unknown }

/** One entry in the sandbox workspace listing. */
export interface WorkspaceEntry {
  name: string
  path: string
  isDir: boolean
  size?: number
}

export function agentBase(name: string): string {
  return `/api/managed-agents/${encodeURIComponent(name)}/proxy`
}

function authHeaders(token: string | null): HeadersInit {
  return token ? { Authorization: `Bearer ${token}` } : {}
}

async function readJson<T>(res: Response, what: string): Promise<T> {
  if (!res.ok) {
    // The body carries the Brain's own reason (an unavailable harness, a missing
    // credential) and it is far more useful than the status alone, which is what
    // a bare `throw new Error(res.statusText)` would leave the user staring at.
    let detail = ""
    try {
      detail = (await res.text()).slice(0, 400)
    } catch {
      // Nothing to add; the status still carries the shape of the failure.
    }
    throw new Error(detail ? `${what}: ${res.status} ${detail}` : `${what}: ${res.status}`)
  }
  return (await res.json()) as T
}

export async function listThreads(agent: string, token: string | null): Promise<AgentThread[]> {
  const res = await fetch(`${agentBase(agent)}/threads`, { headers: authHeaders(token) })
  const body = await readJson<{ threads?: AgentThread[] }>(res, "list conversations")
  return body.threads ?? []
}

export async function createThread(agent: string, token: string | null): Promise<string> {
  const res = await fetch(`${agentBase(agent)}/threads`, {
    method: "POST",
    headers: { ...authHeaders(token), "Content-Type": "application/json" },
    body: "{}",
  })
  const body = await readJson<{ threadId: string }>(res, "create conversation")
  return body.threadId
}

export async function deleteThread(
  agent: string,
  token: string | null,
  threadId: string,
): Promise<void> {
  const res = await fetch(`${agentBase(agent)}/threads/${encodeURIComponent(threadId)}`, {
    method: "DELETE",
    headers: authHeaders(token),
  })
  await readJson<unknown>(res, "delete conversation")
}

export async function exportThread(
  agent: string,
  token: string | null,
  threadId: string,
): Promise<TranscriptEntry[]> {
  const res = await fetch(`${agentBase(agent)}/threads/${encodeURIComponent(threadId)}/export`, {
    headers: authHeaders(token),
  })
  // 501 is a backend that cannot export, which is a real configuration and not an
  // error: the conversation simply starts empty rather than refusing to open.
  if (res.status === 501) return []
  const body = await readJson<{ entries?: TranscriptEntry[] }>(res, "load transcript")
  return body.entries ?? []
}

export async function interruptThread(
  agent: string,
  token: string | null,
  threadId: string,
): Promise<void> {
  await fetch(`${agentBase(agent)}/threads/${encodeURIComponent(threadId)}/interrupt`, {
    method: "POST",
    headers: authHeaders(token),
  })
}

/** The events this UI acts on. Everything else on the wire is ignored by design:
 *  the gateway may add events, and an unknown one must not break a turn. */
export type StreamEvent =
  | { kind: "text"; delta: string }
  | { kind: "reasoning"; delta: string }
  | { kind: "tool"; id: string; name: string }
  | { kind: "tool-result"; id: string; result: string }
  | { kind: "error"; message: string }
  | { kind: "done" }

/**
 * Run one turn and yield the events as they arrive.
 *
 * The gateway speaks AG-UI over SSE. Only the handful of event types this UI
 * renders are translated; the rest are dropped rather than treated as errors,
 * because the wire is allowed to grow and a strict reader would turn every new
 * event into a broken conversation.
 */
export async function* runTurn(
  agent: string,
  token: string | null,
  opts: { threadId: string; text: string; signal?: AbortSignal },
): AsyncGenerator<StreamEvent> {
  const res = await fetch(`${agentBase(agent)}/run`, {
    method: "POST",
    headers: {
      ...authHeaders(token),
      "Content-Type": "application/json",
      Accept: "text/event-stream",
    },
    body: JSON.stringify({
      threadId: opts.threadId,
      // AG-UI resends the whole conversation; the gateway reads only the last user
      // message because the harness session already holds the history.
      messages: [{ role: "user", content: opts.text }],
    }),
    signal: opts.signal,
  })
  if (!res.ok || !res.body) {
    let detail = ""
    try {
      detail = (await res.text()).slice(0, 400)
    } catch {
      // As above: the status is still informative on its own.
    }
    yield { kind: "error", message: detail || `run failed: ${res.status}` }
    return
  }

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ""
  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    // SSE frames are separated by a blank line. A partial frame stays in the
    // buffer: splitting on newline alone would emit half a JSON object.
    let sep = buffer.indexOf("\n\n")
    while (sep !== -1) {
      const frame = buffer.slice(0, sep)
      buffer = buffer.slice(sep + 2)
      sep = buffer.indexOf("\n\n")
      const line = frame.split("\n").find((l) => l.startsWith("data:"))
      if (!line) continue // a comment frame: the heartbeat that keeps proxies open
      const translated = translate(line.slice(5).trim())
      if (translated) yield translated
    }
  }
  yield { kind: "done" }
}

function translate(payload: string): StreamEvent | null {
  let event: {
    type?: string
    delta?: string
    toolCallId?: string
    toolCallName?: string
    content?: string
    message?: string
  }
  try {
    event = JSON.parse(payload)
  } catch {
    return null
  }
  switch (event.type) {
    case "TEXT_MESSAGE_CONTENT":
      return event.delta ? { kind: "text", delta: event.delta } : null
    case "REASONING_MESSAGE_CONTENT":
      return event.delta ? { kind: "reasoning", delta: event.delta } : null
    case "TOOL_CALL_START":
      return {
        kind: "tool",
        id: event.toolCallId ?? "",
        name: event.toolCallName ?? "tool",
      }
    case "TOOL_CALL_RESULT":
      return { kind: "tool-result", id: event.toolCallId ?? "", result: event.content ?? "" }
    case "RUN_ERROR":
      return { kind: "error", message: event.message ?? "the agent reported an error" }
    case "RUN_FINISHED":
      return { kind: "done" }
    default:
      return null
  }
}

// --- capabilities -------------------------------------------------------------

export interface AgentCapabilities {
  backendId: string
  defaultBackendId: string
  /** Who the proxy decided the caller is. Not chosen here — see the note at the
   *  top of this file. */
  userKey: string
  /** The one directory the file API accepts. Every workspace and attachment call
   *  must carry it, and it cannot be derived on this side: the caller never learns
   *  which user key was substituted for it. */
  workspaceDir: string
}

export async function fetchCapabilities(
  agent: string,
  token: string | null,
): Promise<AgentCapabilities> {
  const res = await fetch(`${agentBase(agent)}/capabilities`, {
    headers: authHeaders(token),
  })
  return readJson<AgentCapabilities>(res, "read capabilities")
}

// --- workspace (the sandbox's filesystem, through the Brain's file API) -------
//
// Every call carries `dir` — the caller's workspace directory from
// `fetchCapabilities` — because the file API validates it against one fixed
// pattern and rejects anything else with a 400. It is a parameter rather than a
// value assembled here so there is exactly one place it comes from.

/**
 * The sandbox's own answer about a session's workspace.
 *
 * `status` matters as much as the entries: a session whose sandbox has not been
 * created yet is `inactive` and one whose sandbox was reclaimed is `expired`.
 * Neither is an error, and collapsing them into an empty list would tell the user
 * their files are gone when they were never created.
 */
export interface WorkspaceListing {
  status: "ok" | "inactive" | "expired" | string
  entries?: WorkspaceEntry[]
}

export async function listWorkspace(
  agent: string,
  token: string | null,
  opts: { threadId: string; dir: string; path?: string },
): Promise<WorkspaceListing> {
  const res = await fetch(`${agentBase(agent)}/_fs/list`, {
    method: "POST",
    headers: { ...authHeaders(token), "Content-Type": "application/json" },
    body: JSON.stringify({
      sessionID: opts.threadId,
      dir: opts.dir,
      path: opts.path ?? "",
    }),
  })
  return readJson<WorkspaceListing>(res, "list workspace")
}

export async function readWorkspaceFile(
  agent: string,
  token: string | null,
  opts: { threadId: string; dir: string; path: string },
): Promise<string> {
  const res = await fetch(`${agentBase(agent)}/_fs/read-file`, {
    method: "POST",
    headers: { ...authHeaders(token), "Content-Type": "application/json" },
    body: JSON.stringify({
      sessionID: opts.threadId,
      dir: opts.dir,
      path: opts.path,
      mode: "text",
    }),
  })
  const body = await readJson<{ content?: string }>(res, "read file")
  return body.content ?? ""
}

/**
 * Stage an attachment and get back the path it will have INSIDE the sandbox.
 *
 * The bytes land on the Brain's own disk first and are flushed into the sandbox on
 * the session's next tool call, so this returns immediately rather than waiting for
 * a sandbox to exist — which can be a minutes-long cold start. The returned path is
 * what the prompt should refer to: the agent reads it from the sandbox, not here.
 *
 * Text only, because the API takes the content as a JSON string. A binary
 * attachment needs a different endpoint, not a different encoding here.
 */
export async function stageAttachment(
  agent: string,
  token: string | null,
  opts: { threadId: string; dir: string; file: File },
): Promise<string> {
  const res = await fetch(`${agentBase(agent)}/_fs/attach`, {
    method: "POST",
    headers: { ...authHeaders(token), "Content-Type": "application/json" },
    body: JSON.stringify({
      sessionID: opts.threadId,
      dir: opts.dir,
      // One path segment: the API allows a name optionally one directory deep and
      // rejects anything that could walk out of its staging directory.
      sandboxName: opts.file.name.split("/").pop() ?? opts.file.name,
      content: await opts.file.text(),
    }),
  })
  const body = await readJson<{ path?: string }>(res, "upload attachment")
  return body.path ?? ""
}
