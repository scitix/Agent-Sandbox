import { ASSISTANT_FS_BASE_URL } from '@/components/assistant-ui/runtime-provider'
import { authHeaders } from '@/components/assistant-ui/gw/client'

// Read-only client for the agent's sandbox workspace, served by the same
// workspace-fs endpoint as attachment staging (see runtime-provider). The two
// helpers mirror `readStagedAttachment`: they POST { sessionID, dir, path } and
// the daemon confines every path to the session's workspace root.

export type WorkspaceStatus = 'inactive' | 'expired' | 'ok'

export interface WorkspaceEntry {
  name: string
  type: 'dir' | 'file'
  size: number
  modifiedTime: string // ISO 8601
  mode?: string
}

export interface ListWorkspaceResult {
  status: WorkspaceStatus
  entries?: WorkspaceEntry[]
}

// List one directory of the workspace. `path` is relative to the root ('' = root).
// `inactive`/`expired` come back as a `status` in a 200 body (semantic empty
// states); a network / server fault throws (a generic error, kept distinct).
export async function listWorkspace(
  sessionID: string,
  dir: string,
  path: string
): Promise<ListWorkspaceResult> {
  const res = await fetch(`${ASSISTANT_FS_BASE_URL}/list`, {
    method: 'POST',
    headers: { 'content-type': 'application/json', ...authHeaders() },
    body: JSON.stringify({ sessionID, dir, path }),
  })
  if (!res.ok) throw new Error(`list failed: ${res.status}`)
  return (await res.json()) as ListWorkspaceResult
}

export interface ReadWorkspaceFileResult {
  content: string // utf-8 text OR base64, per `mode`
  size: number
}

// Read one workspace file. `text` for code/markdown, `base64` for images and
// downloads. Throws on any non-2xx (410 = sandbox reclaimed, 413 = too large).
export async function readWorkspaceFile(
  sessionID: string,
  dir: string,
  path: string,
  mode: 'text' | 'base64'
): Promise<ReadWorkspaceFileResult> {
  const res = await fetch(`${ASSISTANT_FS_BASE_URL}/read-file`, {
    method: 'POST',
    headers: { 'content-type': 'application/json', ...authHeaders() },
    body: JSON.stringify({ sessionID, dir, path, mode }),
  })
  if (!res.ok) {
    const err = new Error(`read-file failed: ${res.status}`) as Error & {
      status?: number
    }
    err.status = res.status
    throw err
  }
  return (await res.json()) as ReadWorkspaceFileResult
}
