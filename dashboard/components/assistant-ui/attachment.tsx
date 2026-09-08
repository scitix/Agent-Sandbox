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

'use client'

import {
  AttachmentPrimitive,
  ComposerPrimitive,
  MessagePrimitive,
  type TextMessagePartComponent,
  useAui,
  useAuiState,
} from '@assistant-ui/react'
import {
  FilePreviewSheet,
  type FilePreviewSource,
} from '@/components/assistant-ui/file-preview-sheet'
import {
  MarkdownContent,
  MarkdownText,
} from '@/components/assistant-ui/markdown-text'
import { readStagedAttachment } from '@/components/assistant-ui/runtime-provider'
import { TooltipIconButton } from '@/components/assistant-ui/tooltip-icon-button'
import {
  Avatar,
  AvatarFallback,
  AvatarImage,
} from '@/components/ui/avatar'
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { atomAssistantCurrentSessionId } from '@/lib/assistant/store'
import { downloadTextFile } from '@/lib/table-export'
import { cn } from '@/lib/utils'
import {
  type AttachmentMarker,
  parseAttachmentPart,
  sandboxNameFromPath,
} from '@/lib/assistant/attachment-marker'
import { stripPageMarkers } from '@/lib/assistant/page-context'
import { useAtomValue } from 'jotai'
import {
  DownloadIcon,
  EyeIcon,
  FileText,
  Loader2Icon,
  PlusIcon,
  XIcon,
} from 'lucide-react'
import { type FC, type PropsWithChildren, useEffect, useState, useMemo} from 'react'
import { useShallow } from 'zustand/shallow'

// A document attachment reaches the message as its own text part carrying only a
// marker — either the sandbox reference (proposal 0055) or, when staging was
// unavailable, the legacy inline form with the file in it. Both are parsed by
// `@/lib/assistant/attachment-marker` (the renderer lives next to the
// parser there), and neither belongs in the bubble as text: we render a chip
// instead and {@link UserMessagePartText} drops the part.

function withExtension(name: string): string {
  return /\.[a-z0-9]+$/i.test(name) ? name : `${name}.md`
}

function downloadAttachmentText(name: string, content: string) {
  downloadTextFile(content, withExtension(name), 'text/markdown;charset=utf-8;')
}

function downloadFileObject(file: File) {
  const url = URL.createObjectURL(file)
  const a = document.createElement('a')
  a.href = url
  a.download = file.name
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

const useFileSrc = (file: File | undefined) => {
  // Derived from the file rather than mirrored into state by an effect: the
  // URL is a pure function of it, and a mirror renders one frame with the
  // PREVIOUS file's url — briefly showing the wrong preview.
  const src = useMemo(
    () => (file ? URL.createObjectURL(file) : undefined),
    [file]
  )

  // The revoke is a real cleanup, so it stays an effect.
  useEffect(() => {
    if (!src) return
    const objectUrl = src
    return () => {
      URL.revokeObjectURL(objectUrl)
    }
  }, [file])

  return src
}

const useAttachmentSrc = () => {
  const { file, src } = useAuiState(
    useShallow((s): { file?: File; src?: string } => {
      if (s.attachment.type !== 'image') return {}
      if (s.attachment.file) return { file: s.attachment.file }
      const src = s.attachment.content?.filter(c => c.type === 'image')[0]
        ?.image
      if (!src) return {}
      return { src }
    })
  )

  return useFileSrc(file) ?? src
}

type AttachmentPreviewProps = {
  src: string
}

const AttachmentPreview: FC<AttachmentPreviewProps> = ({ src }) => {
  const [isLoaded, setIsLoaded] = useState(false)
  return (
    <img
      src={src}
      alt="Attachment preview"
      className={cn(
        'block h-auto max-h-[80vh] w-auto max-w-full object-contain',
        isLoaded
          ? 'aui-attachment-preview-image-loaded'
          : 'aui-attachment-preview-image-loading invisible'
      )}
      onLoad={() => setIsLoaded(true)}
    />
  )
}

const AttachmentPreviewDialog: FC<PropsWithChildren> = ({ children }) => {
  const src = useAttachmentSrc()

  if (!src) return children

  return (
    <Dialog>
      <DialogTrigger className="aui-attachment-preview-trigger hover:bg-accent/50 cursor-pointer transition-colors">
        {children}
      </DialogTrigger>
      <DialogContent className="aui-attachment-preview-dialog-content [&>button]:bg-foreground/60 [&_svg]:text-background [&>button]:hover:[&_svg]:text-destructive [&>button]:ring-0! p-2 sm:max-w-3xl [&>button]:rounded-full [&>button]:p-1 [&>button]:opacity-100">
        <DialogTitle className="aui-sr-only sr-only">
          Image Attachment Preview
        </DialogTitle>
        <div className="aui-attachment-preview bg-background relative mx-auto flex max-h-[80dvh] w-full items-center justify-center overflow-hidden">
          <AttachmentPreview src={src} />
        </div>
      </DialogContent>
    </Dialog>
  )
}

const AttachmentThumb: FC = () => {
  const src = useAttachmentSrc()

  return (
    <Avatar className="aui-attachment-tile-avatar h-full w-full rounded-none">
      <AvatarImage
        src={src}
        alt="Attachment preview"
        className="aui-attachment-tile-image object-cover"
      />
      <AvatarFallback>
        <FileText className="aui-attachment-tile-fallback-icon text-muted-foreground size-8" />
      </AvatarFallback>
    </Avatar>
  )
}

const AttachmentUI: FC = () => {
  const aui = useAui()
  const isComposer = aui.attachment.source !== 'message'

  const isImage = useAuiState(s => s.attachment.type === 'image')
  const typeLabel = useAuiState(s => {
    const type = s.attachment.type
    switch (type) {
      case 'image':
        return 'Image'
      case 'document':
        return 'Document'
      case 'file':
        return 'File'
      default:
        return type
    }
  })

  // Non-image attachments (a serialized table, a manifest, a diagnosis report)
  // are documents, not thumbnails: a compact pill with a small icon and the
  // name inline reads far better than a big tile. Clicking the body downloads
  // the file; the X removes it (composer only). Images keep the preview tile.
  if (!isImage) {
    return <DocumentAttachmentChip isComposer={isComposer} />
  }

  return (
    <Tooltip>
      <AttachmentPrimitive.Root className="aui-attachment-root aui-attachment-root-composer relative only:*:first:size-24">
        <AttachmentPreviewDialog>
          <TooltipTrigger
            render={
              <div
                className="aui-attachment-tile bg-muted size-14 cursor-pointer overflow-hidden rounded-[calc(var(--composer-radius)-var(--composer-padding))] border transition-opacity hover:opacity-75"
                role="button"
                tabIndex={0}
                aria-label={`${typeLabel} attachment`}
              />
            }
          >
            <AttachmentThumb />
          </TooltipTrigger>
        </AttachmentPreviewDialog>
        {isComposer && <AttachmentRemove />}
      </AttachmentPrimitive.Root>
      <TooltipContent side="top">
        <AttachmentPrimitive.Name />
      </TooltipContent>
    </Tooltip>
  )
}

const AttachmentRemove: FC<{ className?: string }> = ({ className }) => {
  return (
    <AttachmentPrimitive.Remove
      render={
        <TooltipIconButton
          tooltip="Remove file"
          className={cn(
            'aui-attachment-tile-remove text-muted-foreground hover:[&_svg]:text-destructive hover:bg-white! absolute end-1.5 top-1.5 size-3.5 rounded-full bg-white opacity-100 shadow-sm [&_svg]:text-black',
            className
          )}
          side="top"
        />
      }
    >
      <XIcon className="aui-attachment-remove-icon size-3 dark:stroke-[2.5px]" />
    </AttachmentPrimitive.Remove>
  )
}

/** Compact pill for a document attachment (composer pending or a live message
 *  attachment). The body downloads the file; the X removes it (composer only). */
const DocumentAttachmentChip: FC<{ isComposer: boolean }> = ({
  isComposer,
}) => {
  const file = useAuiState(s => s.attachment.file)
  const name = useAuiState(s => s.attachment.name)
  const contentText = useAuiState(s => {
    const part = s.attachment.content?.find(c => c.type === 'text') as
      | { text?: string }
      | undefined
    return part?.text
  })

  const onDownload = () => {
    if (file) return downloadFileObject(file)
    const parsed = parseAttachmentPart(contentText)
    if (parsed?.content != null)
      return downloadAttachmentText(parsed.name, parsed.content)
    if (contentText) downloadAttachmentText(name, contentText)
  }

  return (
    <AttachmentPrimitive.Root className="aui-attachment-chip bg-muted flex h-9 w-fit max-w-[16rem] items-center gap-1 rounded-md border pe-1 ps-2 text-xs">
      <Tooltip>
        <TooltipTrigger
          render={
            <button
              type="button"
              onClick={onDownload}
              className="flex min-w-0 items-center gap-1.5 py-1"
            />
          }
        >
          <FileText className="text-muted-foreground size-4 shrink-0" />
          <span className="max-w-[11rem] truncate">
            <AttachmentPrimitive.Name />
          </span>
          <DownloadIcon className="size-3 shrink-0 opacity-60" />
        </TooltipTrigger>
        <TooltipContent side="top">
          <AttachmentPrimitive.Name />
        </TooltipContent>
      </Tooltip>
      {isComposer && (
        <AttachmentPrimitive.Remove
          render={
            <button
              type="button"
              aria-label="Remove file"
              className="text-muted-foreground hover:bg-background hover:text-destructive ms-0.5 flex size-5 shrink-0 items-center justify-center rounded-sm"
            />
          }
        >
          <XIcon className="size-3.5" />
        </AttachmentPrimitive.Remove>
      )}
    </AttachmentPrimitive.Root>
  )
}

/** A downloadable chip for a legacy inline attachment (`<attachment …>…</…>`),
 *  where the content still travels in the message. */
const ProjectedAttachmentChip: FC<{ name: string; content: string }> = ({
  name,
  content,
}) => (
  <div className="aui-attachment-chip bg-muted flex h-9 w-fit max-w-[16rem] items-center rounded-md border pe-2 ps-2 text-xs">
    <button
      type="button"
      onClick={() => downloadAttachmentText(name, content)}
      title={name}
      className="flex min-w-0 items-center gap-1.5"
    >
      <FileText className="text-muted-foreground size-4 shrink-0" />
      <span className="max-w-[12rem] truncate">{name}</span>
      <DownloadIcon className="size-3 shrink-0 opacity-60" />
    </button>
  </div>
)

/** A chip for a sandbox-staged attachment (proposal 0055): the content lives in
 *  the sandbox, so download/preview read it back on demand from the opencode pod.
 *  Reads fail once the session (and its staged copy) is reclaimed — surfaced as an
 *  inline "expired" note rather than an error. */
const SandboxAttachmentChip: FC<{
  name: string
  path: string
  size?: string
  lines?: string
}> = ({ name, path, size, lines }) => {
  const [busy, setBusy] = useState(false)
  const [expired, setExpired] = useState(false)
  const [source, setSource] = useState<FilePreviewSource | null>(null)
  // The path no longer carries the session id; the attachment belongs to the
  // session whose thread is on screen, so read it back under the active session.
  const sessionID = useAtomValue(atomAssistantCurrentSessionId)
  const sandboxName = sandboxNameFromPath(path)

  const fetchContent = async (): Promise<string | null> => {
    if (!sessionID || !sandboxName) {
      setExpired(true)
      return null
    }
    setBusy(true)
    try {
      const content = await readStagedAttachment(sessionID, sandboxName)
      setExpired(false)
      return content
    } catch {
      setExpired(true)
      return null
    } finally {
      setBusy(false)
    }
  }

  const onDownload = async () => {
    const content = await fetchContent()
    if (content != null) {
      const filename = /\.[a-z0-9]+$/i.test(name) ? name : sandboxName || name
      downloadTextFile(content, filename, 'text/plain;charset=utf-8;')
    }
  }

  // Preview delegates to the shared FilePreviewSheet, which does its own lazy
  // load + loading/error states (markdown attachments now render rich).
  const onPreview = () => {
    if (!sessionID || !sandboxName) {
      setExpired(true)
      return
    }
    setSource({
      name,
      getText: () => readStagedAttachment(sessionID, sandboxName),
      onDownload,
    })
  }

  const meta = [size, lines && `${lines} lines`].filter(Boolean).join(' · ')

  return (
    <>
      <div className="aui-attachment-chip bg-muted flex h-9 w-fit max-w-[18rem] items-center gap-1 rounded-md border pe-1 ps-2 text-xs">
        <Tooltip>
          <TooltipTrigger
            render={
              <button
                type="button"
                onClick={onPreview}
                className="flex min-w-0 items-center gap-1.5 py-1"
              />
            }
          >
            {busy ? (
              <Loader2Icon className="text-muted-foreground size-4 shrink-0 animate-spin" />
            ) : (
              <FileText className="text-muted-foreground size-4 shrink-0" />
            )}
            <span className="max-w-[11rem] truncate">{name}</span>
          </TooltipTrigger>
          <TooltipContent side="top">
            {expired
              ? 'Attachment expired (session reclaimed)'
              : meta
                ? `${name} — ${meta}`
                : name}
          </TooltipContent>
        </Tooltip>
        <TooltipIconButton
          tooltip="Preview"
          side="top"
          onClick={onPreview}
          className="text-muted-foreground hover:bg-background size-5 shrink-0 rounded-sm"
        >
          <EyeIcon className="size-3.5" />
        </TooltipIconButton>
        <TooltipIconButton
          tooltip="Download"
          side="top"
          onClick={onDownload}
          className="text-muted-foreground hover:bg-background size-5 shrink-0 rounded-sm"
        >
          <DownloadIcon className="size-3.5" />
        </TooltipIconButton>
      </div>
      <FilePreviewSheet
        source={source}
        open={source != null}
        onOpenChange={open => !open && setSource(null)}
      />
    </>
  )
}

/** Renders chips for attachments that arrived as `<attachment …>` text parts in
 *  the message content (the round-tripped form). Placed above the bubble; the
 *  matching text parts are hidden in the bubble by {@link UserMessagePartText}. */
export const UserMessageAttachmentChips: FC = () => {
  // assistant-ui 0.15 dropped `useMessage`; the same read is a selector on the
  // unified `useAuiState`.
  const parts = useAuiState(s => s.message.content) as
    | ReadonlyArray<{ type: string; text?: string }>
    | undefined
  const chips = (parts ?? [])
    .map(p => (p.type === 'text' ? parseAttachmentPart(p.text) : null))
    .filter((v): v is AttachmentMarker => v !== null)
  if (chips.length === 0) return null
  return (
    <div className="aui-user-message-attachment-chips col-span-full col-start-1 row-start-1 flex w-full flex-row flex-wrap justify-end gap-2">
      {chips.map((c, i) =>
        c.path ? (
          <SandboxAttachmentChip
            key={i}
            name={c.name}
            path={c.path}
            size={c.size}
            lines={c.lines}
          />
        ) : (
          <ProjectedAttachmentChip
            key={i}
            name={c.name}
            content={c.content ?? ''}
          />
        )
      )}
    </div>
  )
}

/** Text-part renderer for user messages. Two wrappers are stripped rather than
 *  shown:
 *   - `<attachment …>` becomes a chip above the bubble (see
 *     {@link UserMessageAttachmentChips}), so the part renders nothing here;
 *   - the `<page …/>` marker the dashboard appends to every send is machine
 *     context, not something the user typed — it is surfaced in the message's
 *     More menu instead (see the thread's MessageContextMenu).
 *  Anything else renders as normal Markdown. Note MarkdownText reads the part
 *  from context, so a message carrying a marker has to go through
 *  MarkdownContent with the stripped string. */
export const UserMessagePartText: TextMessagePartComponent = ({ text }) => {
  if (parseAttachmentPart(text)) return null
  const stripped = stripPageMarkers(text)
  if (stripped === text) return <MarkdownText />
  return stripped ? <MarkdownContent content={stripped} /> : null
}

export const UserMessageAttachments: FC = () => {
  return (
    <div className="aui-user-message-attachments-end col-span-full col-start-1 row-start-1 flex w-full flex-row justify-end gap-2">
      <MessagePrimitive.Attachments>
        {() => <AttachmentUI />}
      </MessagePrimitive.Attachments>
    </div>
  )
}

export const ComposerAttachments: FC = () => {
  return (
    <div className="aui-composer-attachments flex w-full flex-row items-center gap-2 overflow-x-auto empty:hidden">
      <ComposerPrimitive.Attachments>
        {() => <AttachmentUI />}
      </ComposerPrimitive.Attachments>
    </div>
  )
}

export const ComposerAddAttachment: FC = () => {
  return (
    <ComposerPrimitive.AddAttachment
      render={
        <TooltipIconButton
          tooltip="Add Attachment"
          side="bottom"
          variant="secondary"
          size="icon"
          className="aui-composer-add-attachment hover:bg-muted-foreground/15 dark:border-muted-foreground/15 dark:hover:bg-muted-foreground/30 size-8 rounded-full p-1 text-xs font-semibold"
          aria-label="Add Attachment"
        />
      }
    >
      <PlusIcon className="aui-attachment-add-icon size-5 stroke-[1.5px]" />
    </ComposerPrimitive.AddAttachment>
  )
}
