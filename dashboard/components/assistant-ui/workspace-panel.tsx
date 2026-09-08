import {
  FilePreviewSheet,
  type FilePreviewSource,
} from '@/components/assistant-ui/file-preview-sheet'
import { sessionDirectory } from '@/components/assistant-ui/runtime-provider'
import { TimeDistance } from '@/components/badge/time-distance'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  iconForEntry,
  isImageName,
  isPreviewableText,
} from '@/lib/file-icons'
import { useTranslation } from '@/lib/i18n'
import {
  atomAssistantCurrentSessionId,
  atomAssistantRunActive,
  atomAssistantUserKey,
  atomWorkspaceAutoOpen,
  atomWorkspaceOpen,
} from '@/lib/assistant/store'
import { downloadBase64File } from '@/lib/table-export'
import { cn, formatBytes } from '@/lib/utils'
import {
  type WorkspaceEntry,
  type WorkspaceStatus,
  listWorkspace,
  readWorkspaceFile,
} from '@/lib/workspace-fs'
import { useAtomValue, useSetAtom } from 'jotai'
import {
  ChevronRightIcon,
  DownloadIcon,
  FolderOpenIcon,
  HardDriveIcon,
  Loader2Icon,
  RefreshCwIcon,
  XIcon,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'

const EMPTY_ENTRIES: WorkspaceEntry[] = []

export function WorkspacePanel() {
  const { t } = useTranslation()
  const sessionID = useAtomValue(atomAssistantCurrentSessionId)
  const userKey = useAtomValue(atomAssistantUserKey)
  const dir = sessionDirectory(userKey)
  const setWorkspaceOpen = useSetAtom(atomWorkspaceOpen)
  const setWorkspaceAutoOpen = useSetAtom(atomWorkspaceAutoOpen)

  const [stack, setStack] = useState<string[]>([])
  const path = stack.join('/')
  const [loadedStatus, setStatus] = useState<WorkspaceStatus | null>(null)
  const [loadedEntries, setEntries] = useState<WorkspaceEntry[]>([])
  const [loadedLoading, setLoading] = useState(false)
  const [loadedErrored, setErrored] = useState(false)
  // Derived, not mirrored: without a session there is nothing loaded, and an
  // effect that wrote these left one render showing the previous session's
  // listing under the new session's header.
  const status: WorkspaceStatus | null = sessionID ? loadedStatus : 'inactive'
  const entries = sessionID ? loadedEntries : EMPTY_ENTRIES
  const loading = sessionID ? loadedLoading : false
  const errored = sessionID ? loadedErrored : false
  const [nonce, setNonce] = useState(0)
  const [preview, setPreview] = useState<FilePreviewSource | null>(null)

  useEffect(() => {
    if (!sessionID) {
      // Nothing to write: the four values above are derived for this case.
      return
    }
    let cancelled = false
    // This effect IS the fetch, and the flag is what says it is in progress;
    // there is no render-time value to derive it from before the request
    // exists.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setLoading(true)
    setErrored(false)
    listWorkspace(sessionID, dir, path)
      .then(res => {
        if (cancelled) return
        setStatus(res.status)
        setEntries(res.entries ?? [])
      })
      .catch(() => {
        if (!cancelled) setErrored(true)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [sessionID, dir, path, nonce])

  const sorted = useMemo(
    () =>
      [...entries].sort((a, b) => {
        if (a.type !== b.type) return a.type === 'dir' ? -1 : 1
        return a.name.localeCompare(b.name)
      }),
    [entries]
  )

  const filePath = (name: string) => (path ? `${path}/${name}` : name)

  const downloadEntry = async (entry: WorkspaceEntry) => {
    if (!sessionID) return
    try {
      const r = await readWorkspaceFile(
        sessionID,
        dir,
        filePath(entry.name),
        'base64'
      )
      downloadBase64File(r.content, entry.name)
    } catch {
      toast.error(t('workspace.error'))
    }
  }

  const openEntry = (entry: WorkspaceEntry) => {
    if (entry.type === 'dir') {
      setStack(s => [...s, entry.name])
      return
    }
    if (!sessionID) return
    const p = filePath(entry.name)
    const textual = isPreviewableText(entry.name)
    setPreview({
      name: entry.name,
      sizeHint: entry.size,
      getText:
        textual && !isImageName(entry.name)
          ? () =>
              readWorkspaceFile(sessionID, dir, p, 'text').then(r => r.content)
          : undefined,
      getBytesBase64: () =>
        readWorkspaceFile(sessionID, dir, p, 'base64').then(r => r.content),
      onDownload: () => downloadEntry(entry),
    })
  }

  const refresh = () => setNonce(n => n + 1)

  // Auto-refresh when the agent finishes a turn (isRunning true→false), mirrored
  // into atomAssistantRunActive by RunActiveBridge. isRunning stays true across
  // the whole turn — including tool calls — so this fires once per completed
  // round, not per tool call, picking up whatever files the turn produced.
  const runActive = useAtomValue(atomAssistantRunActive)
  const prevRunActive = useRef(runActive)
  useEffect(() => {
    if (prevRunActive.current && !runActive) setNonce(n => n + 1)
    prevRunActive.current = runActive
  }, [runActive])

  return (
    // A container-query context, because this panel is resizable and its rows
    // have to give up columns as it narrows. The window's width says nothing
    // about how wide THIS is — a `sm:` breakpoint here would keep showing every
    // column on a 4K screen while the panel itself was dragged down to 240px.
    <div className="@container/workspace bg-card flex h-full min-h-0 flex-col overflow-hidden rounded-xl border">
      {/* A panel, like the conversation beside it — same fill, same radius, same
          hairline. It used to take the canvas colour with a card-coloured header
          strip, which inverted the layering: the recessed shade was on the
          outside and the raised one on the inside, so the column read as a hole
          in the page rather than as a thing on it.

          `h-11` matches the conversation's own header row, so the two line up
          across the seam. */}
      <header className="flex h-11 shrink-0 items-center gap-1 border-b px-3">
        <HardDriveIcon className="size-4" />
        <span className="text-[13px] font-medium">{t('workspace.title')}</span>
        <div className="flex-1" />
        <Button
          variant="ghost"
          size="icon"
          className="size-8"
          onClick={refresh}
          disabled={loading || !sessionID}
          aria-label={t('workspace.refresh')}
          title={t('workspace.refresh')}
        >
          <RefreshCwIcon className={cn('size-4', loading && 'animate-spin')} />
        </Button>
        {/* Closable from its own corner, not only from the button that opened
            it. Every other panel on screen shuts from the inside, and a user
            reaching for the X here was reaching for the obvious thing. Closing
            by hand also disarms the auto-open: a later file write must not
            re-raise a panel that was deliberately dismissed. */}
        <Button
          variant="ghost"
          size="icon"
          className="-me-1 size-8"
          onClick={() => {
            setWorkspaceOpen(false)
            setWorkspaceAutoOpen(false)
          }}
          aria-label={t('layout.workspace.hide')}
          title={t('layout.workspace.hide')}
        >
          <XIcon className="size-4" />
        </Button>
      </header>

      {/* Breadcrumb — only meaningful once we have a live listing */}
      {status === 'ok' && (
        <div className="text-muted-foreground flex flex-wrap items-center gap-0.5 border-b px-3 py-2 text-xs">
          <button
            type="button"
            className="hover:text-foreground max-w-[10rem] truncate"
            onClick={() => setStack([])}
          >
            {t('workspace.root')}
          </button>
          {stack.map((seg, i) => (
            <span key={i} className="flex items-center gap-0.5">
              <ChevronRightIcon className="size-3 shrink-0 opacity-60" />
              <button
                type="button"
                className={cn(
                  'hover:text-foreground max-w-[10rem] truncate',
                  i === stack.length - 1 && 'text-foreground font-medium'
                )}
                onClick={() => setStack(s => s.slice(0, i + 1))}
              >
                {seg}
              </button>
            </span>
          ))}
        </div>
      )}

      <div className="min-h-0 flex-1">
        <WorkspaceBody
          sessionID={sessionID}
          status={status}
          loading={loading}
          errored={errored}
          entries={sorted}
          onOpen={openEntry}
          onDownload={downloadEntry}
          onRefresh={refresh}
        />
      </div>

      {/* Only where there is something to lose. On an inactive or errored panel
          this footer was a second paragraph of grey text under the first, and
          the warning it carries is about files that do not exist yet. */}
      {status === 'ok' && (
        <p className="text-muted-foreground/80 border-t px-3 py-2 text-[11px]">
          {t('workspace.ephemeralHint')}
        </p>
      )}

      <FilePreviewSheet
        source={preview}
        open={preview != null}
        onOpenChange={open => !open && setPreview(null)}
      />
    </div>
  )
}

function CenteredState({
  title,
  body,
  tone = 'muted',
  action,
}: {
  title: string
  body?: string
  tone?: 'muted' | 'destructive'
  action?: React.ReactNode
}) {
  return (
    <div
      className={cn(
        'flex h-full flex-col items-center justify-center gap-3 px-8 text-center',
        tone === 'destructive' ? 'text-destructive' : 'text-muted-foreground'
      )}
    >
      {/* The glyph sits on a recessed disc rather than floating at 30% opacity.
          A large faded icon on a bare panel reads as something that failed to
          load; a small solid one in a well reads as a placeholder, which is
          what this is. */}
      <span
        className={cn(
          'flex size-11 items-center justify-center rounded-full',
          tone === 'destructive' ? 'bg-destructive/10' : 'bg-muted'
        )}
      >
        <FolderOpenIcon className="size-5" />
      </span>
      <div className="flex flex-col gap-1">
        <p className="text-foreground text-[13px] font-medium">{title}</p>
        {body && (
          <p className="max-w-[16rem] text-xs leading-relaxed">{body}</p>
        )}
      </div>
      {action}
    </div>
  )
}

function WorkspaceBody({
  sessionID,
  status,
  loading,
  errored,
  entries,
  onOpen,
  onDownload,
  onRefresh,
}: {
  sessionID: string | null
  status: WorkspaceStatus | null
  loading: boolean
  errored: boolean
  entries: WorkspaceEntry[]
  onOpen: (e: WorkspaceEntry) => void
  onDownload: (e: WorkspaceEntry) => void
  onRefresh: () => void
}) {
  const { t } = useTranslation()

  if (!sessionID || status === 'inactive') {
    return (
      <CenteredState
        title={t('workspace.inactiveTitle')}
        body={t('workspace.inactiveBody')}
      />
    )
  }
  if (errored) {
    return (
      <CenteredState
        title={t('workspace.error')}
        tone="destructive"
        action={
          <Button variant="outline" size="sm" onClick={onRefresh}>
            <RefreshCwIcon className="mr-1.5 size-4" />
            {t('workspace.refresh')}
          </Button>
        }
      />
    )
  }
  if (loading && entries.length === 0) {
    return (
      <div className="text-muted-foreground flex h-full items-center justify-center gap-2 p-6 text-sm">
        <Loader2Icon className="size-4 animate-spin" />
        {t('workspace.loading')}
      </div>
    )
  }
  if (status === 'expired' && entries.length === 0) {
    return (
      <CenteredState
        title={t('workspace.expiredTitle')}
        body={t('workspace.expiredBody')}
        action={
          <Button variant="outline" size="sm" onClick={onRefresh}>
            <RefreshCwIcon className="mr-1.5 size-4" />
            {t('workspace.refresh')}
          </Button>
        }
      />
    )
  }
  if (entries.length === 0) {
    return <CenteredState title={t('workspace.emptyDir')} />
  }

  return (
    <ScrollArea className="h-full">
      <div className="p-1.5">
        {status === 'expired' && (
          <p className="text-muted-foreground px-2 py-1.5 text-xs">
            {t('workspace.expiredBody')}
          </p>
        )}
        {entries.map(entry => {
          const Icon = iconForEntry(entry)
          return (
            <div
              key={entry.name}
              className="hover:bg-accent group flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm"
              onClick={() => onOpen(entry)}
              role="button"
              tabIndex={0}
              onKeyDown={e => {
                if (e.key === 'Enter' || e.key === ' ') onOpen(entry)
              }}
            >
              {/* Folders are yellow, the way a folder is yellow everywhere else
                  a file tree is drawn. The accent colour is the app's "this is
                  interactive / this is selected" signal, and spending it on
                  every directory row said neither. */}
              <Icon
                className={cn(
                  'size-4 shrink-0',
                  entry.type === 'dir'
                    ? 'text-highlight-yellow'
                    : 'text-muted-foreground'
                )}
              />
              {/* The name is the only column that is never worth losing, so it
                  is the only one with no width of its own: the other two drop
                  out from the right as the panel narrows and hand back their
                  fixed columns. */}
              <span className="min-w-0 flex-1 truncate">{entry.name}</span>
              <span className="text-muted-foreground @[19rem]/workspace:block hidden w-16 shrink-0 text-right text-xs tabular-nums">
                {entry.type === 'dir' ? '' : formatBytes(entry.size)}
              </span>
              <span className="text-muted-foreground @[28rem]/workspace:flex hidden w-24 shrink-0 justify-end">
                <TimeDistance
                  date={entry.modifiedTime || null}
                  withIcon={false}
                />
              </span>
              {entry.type === 'file' ? (
                <Button
                  variant="ghost"
                  size="icon"
                  className="size-6 shrink-0 opacity-0 group-hover:opacity-100"
                  aria-label={t('workspace.download')}
                  onClick={e => {
                    e.stopPropagation()
                    onDownload(entry)
                  }}
                >
                  <DownloadIcon className="size-3.5" />
                </Button>
              ) : (
                // Keep the download-button footprint so folder rows align with files.
                <span className="size-6 shrink-0" aria-hidden />
              )}
            </div>
          )
        })}
      </div>
    </ScrollArea>
  )
}
