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

import { BackendMark } from '@/components/assistant-ui/backend-marks'
import {
  type AssistantSessionSummary,
  useAssistantLoadState,
  useAssistantSessionStore,
} from '@/components/assistant-ui/backend-port'
import { groupByDay } from '@/components/assistant-ui/session-groups'
import { SessionRenameDialog } from '@/components/assistant-ui/session-rename-dialog'
import { SearchInput } from '@/components/custom-ui/search-input'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useTranslation } from '@/lib/i18n'
import { atomAssistantCurrentSessionId } from '@/lib/assistant/store'
import { cn } from '@/lib/utils'
import { useAtomValue } from 'jotai'
import {
  Loader2Icon,
  MoreHorizontalIcon,
  PencilIcon,
  Trash2Icon,
} from 'lucide-react'
import { type ReactElement, useCallback, useMemo, useState } from 'react'

// Session history scoped to the current user's directory namespace, listed
// through the backend port so both harnesses feed the same component.
//
// Three things this list refuses to show, each for the same reason — a history
// list should contain conversations, and nothing else:
//   * threads nobody has spoken in (the port filters them: a thread exists from
//     the moment "new conversation" is clicked, its harness session only from the
//     first turn);
//   * a harness's placeholder title (the gateway normalises those away, and the
//     row shows a spinner while the real one is being generated);
//   * an id, when a conversation genuinely has no title yet.

type SessionItem = AssistantSessionSummary

/**
 * Whether to draw the harness logo on each row.
 *
 * Only when the list actually mixes harnesses. On a single-backend deployment
 * every row would carry the same icon, which is pure noise — and the icon's job
 * here is precisely to tell two otherwise identical rows apart.
 */
function useMixedBackends(items: SessionItem[]): boolean {
  return useMemo(
    () => new Set(items.map(s => s.backendId).filter(Boolean)).size > 1,
    [items]
  )
}

/** Stable identity for "no store yet", so the empty case does not hand out a
 *  fresh array on every render. */
const NO_SESSIONS: SessionItem[] = []

/**
 * Shared session list + mutations.
 *
 * The list is READ STRAIGHT OFF the port — it is the runtime's own state, which
 * every push from the gateway (a title landing, a rename, a delete in another
 * tab) already updates. Nothing is copied into local state and nothing is
 * re-fetched after a mutation, which is what makes the two places a conversation
 * can be renamed agree: the sidebar row and the header title render the same
 * array, so one render moves both.
 *
 * Keeping a private copy is what broke that. Each caller held its own `items`,
 * and a rename ended by re-reading through the store captured at click time —
 * i.e. the PRE-rename list — writing the old title back over the view that had
 * just renamed it, while the other view (which only followed the push) showed
 * the new one.
 */
function useUserSessions() {
  const store = useAssistantSessionStore()
  // The RUNTIME's current conversation (updates when one is switched), not the
  // one the host first opened — so the highlight follows the active chat.
  const currentId = useAtomValue(atomAssistantCurrentSessionId)
  // Not a list state of its own: 'loading' is the session gate still resolving
  // which conversation to open, which is exactly the window in which an empty
  // history means "not known yet" rather than "none".
  const loading = useAssistantLoadState() === 'loading'
  const items = store?.sessions ?? NO_SESSIONS

  // Delete the given conversations, then re-home the runtime if the active one
  // was among them: switch to the most recent survivor, or open a fresh one when
  // nothing remains (the runtime cannot point at a deleted id).
  const deleteSessions = useCallback(
    async (ids: string[]) => {
      if (!ids.length || !store) return
      const idSet = new Set(ids)
      const deletingCurrent = !!currentId && idSet.has(currentId)
      await Promise.all(ids.map(id => store.remove(id).catch(() => undefined)))
      if (!deletingCurrent) return
      const survivor = items.find(s => !idSet.has(s.id))
      if (survivor) {
        store.switchTo(survivor.id)
        return
      }
      try {
        const created = await store.create()
        if (created) store.switchTo(created)
      } catch {
        // Leave the runtime as-is; the deletions are already reflected.
      }
    },
    [store, currentId, items]
  )

  const renameSession = useCallback(
    async (id: string, title: string) => {
      await store?.rename?.(id, title)
    },
    [store]
  )

  return {
    items,
    loading,
    currentId,
    deleteSessions,
    // Absent capability stays absent, so a caller renders the action or not
    // rather than offering one that quietly fails.
    renameSession: store?.rename ? renameSession : undefined,
    switchTo: (id: string) => store?.switchTo(id),
  }
}

/**
 * Whether this user has anything in their conversation history.
 *
 * Deliberately the SAME predicate the list renders from — an unstarted thread is
 * not history (the port filters those out), so clicking "New" and leaving it
 * blank does not count as having one. Anything deciding chrome off the history
 * must read it through here, or "the sidebar is empty" and "the user has no
 * conversations" drift apart.
 */
export function useHasSessionHistory(): boolean {
  const { items, loading } = useUserSessions()
  return !loading && items.length > 0
}

/** The conversation currently open, as the history knows it. Used by the header
 *  to title the page and to offer rename/delete on it. */
export function useCurrentSession(): {
  session?: SessionItem
  rename?: (title: string) => Promise<void>
  remove?: () => Promise<void>
} {
  const { items, currentId, renameSession, deleteSessions } = useUserSessions()
  const session = items.find(s => s.id === currentId)
  if (!session) return {}
  return {
    session,
    ...(renameSession
      ? { rename: (title: string) => renameSession(session.id, title) }
      : {}),
    remove: () => deleteSessions([session.id]),
  }
}

/** A conversation's display name, or the placeholder for one still being named. */
export function sessionLabel(s: SessionItem, fallback: string): string {
  return s.title || s.slug || fallback
}

/**
 * The conversation list inside the assistant menu: search, grouped rows, a
 * per-row menu (rename / delete), and a shift-click range selection.
 *
 * There is no "Select" mode to enter. Selecting several conversations works the
 * way it does in a file list — click one, shift-click another, and the range
 * between them is selected — because that is a gesture people already have, and
 * a permanent column of checkboxes would make the common case (open a
 * conversation) look like a form. The delete bar appears only once a range
 * exists, and Escape or a plain click puts it away.
 *
 * `onOpenChat` is how the list gets back to the conversation. Picking one is a
 * request to READ it, so it cannot leave whatever page the menu was opened from
 * (the settings, the analysis viewer) sitting over the top — which is exactly
 * what used to happen: the thread switched underneath and the screen did not
 * change.
 */
export function SessionHistoryList({
  onOpenChat,
}: {
  onOpenChat?: () => void
}) {
  const { t, locale } = useTranslation()
  const { items, loading, currentId, deleteSessions, renameSession, switchTo } =
    useUserSessions()
  const mixed = useMixedBackends(items)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  /** Where the last plain click landed — one end of a shift-click range. */
  const [anchor, setAnchor] = useState<string | null>(null)
  const [query, setQuery] = useState('')
  const [renaming, setRenaming] = useState<SessionItem | null>(null)

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return items
    return items.filter(s =>
      `${s.title ?? ''} ${s.slug ?? ''} ${s.id}`.toLowerCase().includes(q)
    )
  }, [items, query])
  const groups = useMemo(
    () => groupByDay(filtered, t, locale),
    [filtered, t, locale]
  )
  const selecting = selected.size > 0

  const clearSelection = () => {
    setSelected(new Set())
    setAnchor(null)
  }

  /** Plain click opens; shift-click selects everything between the anchor and
   *  here, over the list AS DISPLAYED (so it follows the current search and
   *  grouping rather than some hidden order). */
  const onRowClick = (id: string, shift: boolean) => {
    if (!shift) {
      setAnchor(id)
      if (selecting) clearSelection()
      onOpenChat?.()
      switchTo(id)
      return
    }
    const order = groups.flatMap(g => g.items.map(i => i.id))
    const from = order.indexOf(anchor ?? id)
    const to = order.indexOf(id)
    if (from < 0 || to < 0) return
    const [lo, hi] = from <= to ? [from, to] : [to, from]
    setSelected(new Set(order.slice(lo, hi + 1)))
  }

  return (
    <div
      className="flex min-h-0 flex-1 flex-col"
      // Escape is the way out of a selection that was entered by accident.
      onKeyDown={e => e.key === 'Escape' && clearSelection()}
    >
      {/* Nothing above the list until there IS a list: an empty menu should read
          as "no conversations yet", not as a search box with nothing to search
          and a heading over a void. */}
      {items.length > 0 && (
        <div className="px-4 pb-2">
          {/* The app's one search box, not a third hand-rolled magnifier. On the
              canvas-coloured menu it takes the canvas fill back, so the field
              reads as a well rather than a floating white card. */}
          <SearchInput
            value={query}
            onChange={setQuery}
            placeholder={t('assistant.searchSessions')}
            fill="background"
          />
        </div>
      )}

      <div className="min-h-0 flex-1 overflow-y-auto px-4 pb-2">
        {loading && items.length === 0 ? (
          <div className="text-muted-foreground flex items-center gap-2 px-3 py-2 text-xs">
            <Loader2Icon className="size-4 animate-spin" />
          </div>
        ) : items.length === 0 ? null : filtered.length === 0 ? (
          <div className="text-muted-foreground px-3 py-6 text-center text-xs">
            {t('assistant.noSessions')}
          </div>
        ) : (
          groups.map(group => (
            <div key={group.key} className="mb-1">
              <div className="text-muted-foreground px-2 py-1.5 text-[11px]">
                {group.label}
              </div>
              {/* A hairline of air between rows. Flush, their hover and active
                  fills merge into one block and the list stops reading as a
                  list of conversations. */}
              <div className="flex flex-col gap-0.5">
                {group.items.map(s => (
                  <SessionRow
                    key={s.id}
                    session={s}
                    active={!selecting && s.id === currentId}
                    mixed={mixed}
                    selecting={selecting}
                    selected={selected.has(s.id)}
                    onClick={shift => onRowClick(s.id, shift)}
                    onDelete={() => void deleteSessions([s.id])}
                    {...(renameSession
                      ? { onRename: () => setRenaming(s) }
                      : {})}
                  />
                ))}
              </div>
            </div>
          ))
        )}
      </div>

      {/* Floats in only while a range is held. */}
      {selecting && (
        <div className="flex items-center gap-2 border-t p-2">
          <ConfirmDeleteDialog
            count={selected.size}
            onConfirm={() => {
              void deleteSessions([...selected])
              clearSelection()
            }}
            trigger={
              <Button
                variant="destructive"
                size="sm"
                className="flex-1 gap-1.5"
              >
                <Trash2Icon className="size-3.5" />
                {t('assistant.deleteSelected', { count: selected.size })}
              </Button>
            }
          />
          <Button
            variant="ghost"
            size="sm"
            className="text-muted-foreground"
            onClick={clearSelection}
          >
            {t('common.cancel')}
          </Button>
        </div>
      )}

      {renameSession && (
        <SessionRenameDialog
          open={!!renaming}
          onOpenChange={open => !open && setRenaming(null)}
          currentTitle={
            renaming ? sessionLabel(renaming, t('assistant.newSession')) : ''
          }
          onRename={title =>
            renaming ? renameSession(renaming.id, title) : undefined
          }
        />
      )}
    </div>
  )
}

function SessionRow({
  session,
  active,
  mixed,
  selecting,
  selected,
  onClick,
  onRename,
  onDelete,
}: {
  session: SessionItem
  active: boolean
  mixed: boolean
  selecting: boolean
  selected: boolean
  onClick: (shiftKey: boolean) => void
  onRename?: () => void
  onDelete: () => void
}) {
  const { t } = useTranslation()
  return (
    <div
      className={cn(
        'hover:bg-muted group flex items-center gap-1.5 rounded-lg pe-1 transition-colors',
        active && 'bg-muted',
        selected && 'bg-primary/10 hover:bg-primary/10'
      )}
    >
      <button
        type="button"
        onClick={e => onClick(e.shiftKey)}
        className="flex min-w-0 flex-1 items-center gap-1.5 px-2 py-1.5 text-left"
        title={sessionLabel(session, t('assistant.newSession'))}
      >
        {mixed && <BackendMark id={session.backendId} size={14} labelled />}
        <span className="min-w-0 flex-1 truncate text-[13px]">
          {sessionLabel(session, t('assistant.newSession'))}
        </span>
      </button>
      {/* While the harness is naming the conversation there is nothing to act on
          yet, and the spinner says why the title is about to change under the
          user's eyes. It occupies the menu's slot so the row never reflows. */}
      {session.titlePending ? (
        <Loader2Icon
          className="text-muted-foreground me-1.5 size-3.5 shrink-0 animate-spin"
          aria-label={t('assistant.history.naming')}
        />
      ) : (
        !selecting && (
          <SessionRowMenu
            {...(onRename ? { onRename } : {})}
            onDelete={onDelete}
          />
        )
      )}
    </div>
  )
}

/** Per-row actions. Rename is absent — not disabled — when the harness cannot do
 *  it, so the menu never offers an action that would fail. */
function SessionRowMenu({
  onRename,
  onDelete,
}: {
  onRename?: () => void
  onDelete: () => void
}) {
  const { t } = useTranslation()
  const [confirming, setConfirming] = useState(false)
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              variant="ghost"
              size="icon"
              className="text-muted-foreground size-7 shrink-0 rounded-full opacity-0 transition-opacity focus-visible:opacity-100 group-hover:opacity-100 data-[popup-open]:opacity-100"
              aria-label={t('assistant.moreActions')}
            />
          }
        >
          <MoreHorizontalIcon className="size-4" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="min-w-40">
          {onRename && (
            <DropdownMenuItem onClick={onRename}>
              <PencilIcon className="size-4" />
              {t('assistant.renameSession')}
            </DropdownMenuItem>
          )}
          <DropdownMenuItem
            variant="destructive"
            onClick={() => setConfirming(true)}
          >
            <Trash2Icon className="size-4" />
            {t('assistant.deleteSession')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      {/* Outside the menu: the menu unmounts its content on select, which would
          take an AlertDialog rendered inside it with it. */}
      <ConfirmDeleteDialog
        count={1}
        open={confirming}
        onOpenChange={setConfirming}
        onConfirm={onDelete}
      />
    </>
  )
}

/** Confirm-before-delete. Either driven by a trigger element (the batch button)
 *  or controlled by a caller that opened it from a menu item. */
export function ConfirmDeleteDialog({
  count,
  onConfirm,
  trigger,
  open,
  onOpenChange,
}: {
  count: number
  onConfirm: () => void
  trigger?: ReactElement
  open?: boolean
  onOpenChange?: (open: boolean) => void
}) {
  const { t } = useTranslation()
  return (
    <AlertDialog
      {...(open !== undefined ? { open } : {})}
      {...(onOpenChange ? { onOpenChange } : {})}
    >
      {trigger && <AlertDialogTrigger render={trigger} />}
      <AlertDialogContent>
        <>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('assistant.deleteSessionsTitle', { count })}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t('assistant.deleteSessionsBody')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('common.cancel')}</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={onConfirm}>
              {t('common.delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </>
      </AlertDialogContent>
    </AlertDialog>
  )
}
