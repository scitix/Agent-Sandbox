import { useAui, useAuiState } from '@assistant-ui/react'
import { useCreateAssistantSession } from '@/components/assistant-ui/runtime-provider'
import { useTranslation } from '@/lib/i18n'
import { toast } from 'sonner'

/** Serialize the currently-loaded thread to Markdown (role headings + text
 *  parts). Reads the runtime's in-memory messages — the same content shown — so
 *  it needs no extra fetch and matches what the user sees. */
function buildSessionMarkdown(
  messages: readonly { role: string; parts: readonly { type: string }[] }[]
): string {
  return messages
    .map(m => {
      const text = m.parts
        .filter((p): p is { type: 'text'; text: string } => p.type === 'text')
        .map(p => p.text)
        .join('\n\n')
      const who =
        m.role === 'user'
          ? 'User'
          : m.role === 'assistant'
            ? 'Assistant'
            : m.role
      return `## ${who}\n\n${text}`
    })
    .join('\n\n---\n\n')
}

export interface SessionActions {
  /** The current thread holds no messages — New is a no-op, Export has nothing
   *  to work on. */
  isEmpty: boolean
  newSession: () => void
  newBusy: boolean
  exportMarkdown: () => void
}

/**
 * The behavior behind the assistant's session controls (New / Export), shared by
 * the side panel header and the dedicated full page so the two can't drift.
 * Requires the runtime provider (it reads thread state), so only call it from a
 * component rendered after the session gate.
 *
 * There is deliberately no manual "compact" action: opencode folds the context on
 * its own when the window fills up, and asking for it by hand never turned out to
 * be something anyone wanted to decide. The automatic ones still announce
 * themselves in the thread (see CompactionDivider).
 */
export function useSessionActions(): SessionActions {
  const { t } = useTranslation()
  const aui = useAui()
  const { create, busy } = useCreateAssistantSession()
  // Already on an empty, unused session — a New click would just spawn another
  // blank one. Block it and tell the user instead.
  const isEmpty = useAuiState(s => s.thread.messages.length === 0)
  // The session id of the CURRENTLY-viewed thread (may differ from the one
  // AssistantHost first resolved if the user switched via history) — same source
  // ActiveSessionPersistence mirrors out.
  const sessionId = useAuiState(
    s => s.threadListItem?.externalId ?? s.threadListItem?.remoteId
  )

  const newSession = () =>
    isEmpty ? toast(t('assistant.alreadyNewSession')) : create()

  // Export the whole conversation as a Markdown file (the per-message
  // ActionBarPrimitive.ExportMarkdown only covers one message).
  const exportMarkdown = () => {
    const md = buildSessionMarkdown(aui.thread().getState().messages)
    const blob = new Blob([md], { type: 'text/markdown;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `assistant-session-${sessionId ?? 'export'}.md`
    a.click()
    URL.revokeObjectURL(url)
  }

  return {
    isEmpty,
    newSession,
    newBusy: busy,
    exportMarkdown,
  }
}

// The buttons themselves live in the assistant surface's header, which renders
// every action as a ghost icon and decides which of them a given mode shows. What
// is left here is the BEHAVIOUR behind them, so the page and the panel cannot
// disagree about what "new conversation" or "export" means.
