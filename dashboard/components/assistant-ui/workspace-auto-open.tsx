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

import { useAuiState } from '@assistant-ui/react'
import {
  atomWorkspaceAutoOpen,
  atomWorkspaceOpen,
} from '@/lib/assistant/store'
import { usePathname } from 'next/navigation'
import { useAtomValue, useSetAtom } from 'jotai'
import { useEffect } from 'react'

// The agent's file-writing tools. `bash` is deliberately absent: a shell
// redirect isn't reliably detectable from the tool arguments, and guessing would
// pop the panel open on reads and greps.
const WRITE_TOOLS = new Set(['write', 'edit', 'patch', 'multiedit'])

// Absolute prefixes that are NOT the session workspace, so a write there says
// nothing about there being files worth showing. Everything else counts: the
// agent's cwd IS the session workspace, so its writes land inside it.
const NON_WORKSPACE_PREFIXES = ['/tmp/', '/tmp']

// Writes already acted on, keyed by toolCallId. Module-level so one write opens
// the panel exactly once, regardless of re-renders or a session reload.
const handled = new Set<string>()

/** The path argument a write tool carries, under whichever key it uses. */
function writtenPath(args: unknown): string | undefined {
  if (!args || typeof args !== 'object') return undefined
  const record = args as Record<string, unknown>
  for (const key of ['filePath', 'path', 'file']) {
    const value = record[key]
    if (typeof value === 'string' && value.length > 0) return value
  }
  return undefined
}

function insideWorkspace(path: string): boolean {
  return !NON_WORKSPACE_PREFIXES.some(
    prefix => path === prefix || path.startsWith(prefix)
  )
}

/**
 * Opens the agent workspace panel the first time the agent actually writes a
 * file — and only then. The panel starts collapsed because most turns produce no
 * files at all; a turn that does produce one is exactly when the file browser is
 * worth the screen space.
 *
 * Three gates, all necessary:
 *  - the workspace panel only exists on the DEDICATED full-page assistant, so a
 *    write made while the user is in the side panel must not arm anything;
 *  - `atomWorkspaceAutoOpen` is cleared once the user closes the panel by hand,
 *    so we never re-open what they deliberately collapsed;
 *  - each tool call is considered once (`handled`), so re-renders and history
 *    reloads don't re-open a panel the user closed in between.
 *
 * Reads completed tool calls off the thread rather than registering a
 * `makeAssistantToolUI` for `write` — that would replace how write calls render.
 */
export function WorkspaceAutoOpenBridge() {
  const pathname = usePathname()
  const onDedicatedAssistant = pathname.endsWith('/assistant')
  const autoOpen = useAtomValue(atomWorkspaceAutoOpen)
  const setWorkspaceOpen = useSetAtom(atomWorkspaceOpen)
  const messages = useAuiState(s => s.thread.messages)

  useEffect(() => {
    let wrote = false
    for (const message of messages) {
      if (message.role !== 'assistant') continue
      for (const part of message.parts) {
        if (part.type !== 'tool-call') continue
        if (!WRITE_TOOLS.has(part.toolName)) continue
        // Finished AND successful. react-opencode projects a failed tool with a
        // result too (`isError`), which assistant-ui also reports as complete —
        // a write that failed produced no file worth showing.
        if (part.status.type !== 'complete' || part.isError) continue
        if (handled.has(part.toolCallId)) continue
        handled.add(part.toolCallId)
        const path = writtenPath(part.args)
        // A path that failed to parse still counts — the tool ran, and the
        // workspace is where its output goes by default.
        if (!path || insideWorkspace(path)) wrote = true
      }
    }
    if (wrote && onDedicatedAssistant && autoOpen) setWorkspaceOpen(true)
  }, [messages, onDedicatedAssistant, autoOpen, setWorkspaceOpen])

  return null
}
