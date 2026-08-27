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

// Attachment staging.
//
// Content is written to a pod-local staging dir keyed by (directory, sessionKey);
// the sandbox daemon flushes it into the sandbox on the session's FIRST tool call,
// as root into a world-readable root-owned dir, so the agent can read but not
// modify or delete it. The message the agent sees carries only a one-line marker,
// never the bytes — that is the whole point: a 1 MB log must not enter the context.
//
// ORDERING IS LOAD-BEARING: staging must happen BEFORE the first prompt. The
// daemon flushes on the first tool call, so a file staged afterwards is invisible
// for that turn. The bot flows depend on this (evidence is collected, staged, and
// only then is the prompt sent).
import { proxyUrl } from '@scitix/agentbox-hands'

export interface StagedAttachment {
  /** The path the file will have inside the sandbox. */
  path: string
  sandboxName: string
}

export interface AttachmentStager {
  stage(input: {
    sessionKey: string
    directory: string
    sandboxName: string
    content: string
  }): Promise<StagedAttachment>
}

/** Where the workspace-fs server listens. Same pod; loopback by default. */
function fsBaseUrl(): string {
  return (
    process.env.ASSISTANT_FS_BASE_URL ||
    // The daemon and the fs server are siblings; derive from the daemon's host.
    proxyUrl().replace(/:\d+$/, ':8766')
  )
}

export function createAttachmentStager(): AttachmentStager {
  return {
    async stage({ sessionKey, directory, sandboxName, content }) {
      const res = await fetch(`${fsBaseUrl()}/attach`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({
          // The fs server's field is still called sessionID; for the gateway the
          // value is the thread id, which is also the sandbox key.
          sessionID: sessionKey,
          dir: directory,
          sandboxName,
          content,
        }),
      })
      const txt = await res.text()
      if (!res.ok)
        throw new Error(`attachment staging failed (${res.status}): ${txt}`)
      const body = JSON.parse(txt) as { path?: string; sandboxName?: string }
      if (!body.path) throw new Error('attachment staging returned no path')
      return { path: body.path, sandboxName: body.sandboxName || sandboxName }
    },
  }
}

// The per-user directory is a fixed path defined once, in the package both the
// gateway and the browser consume — re-exported here because staging is its
// busiest caller and every backend already imports from this module.
export {
  USER_DIR_ROOT,
  userDirectory,
} from './workspace.ts'
