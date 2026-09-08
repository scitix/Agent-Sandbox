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

import { MarkdownContent } from '@/components/assistant-ui/markdown-text'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'

import {
  imageMimeForName,
  isImageName,
  isMarkdownName,
} from '@/lib/file-icons'
import { useTranslation } from '@/lib/i18n'
import { formatBytes } from '@/lib/utils'
import { DownloadIcon, FileIcon, Loader2Icon } from 'lucide-react'
import { type FC, useEffect, useState } from 'react'

// A source of file content, resolved lazily. Exactly the loader matching the
// file's kind is used: images/downloads pull base64, text/markdown/code pull
// text. Either loader may throw (e.g. 410 sandbox reclaimed, 413 too large).
export interface FilePreviewSource {
  name: string
  sizeHint?: number
  getText?: () => Promise<string>
  getBytesBase64?: () => Promise<string>
  // Optional custom download (defaults to base64 → blob, else text → blob).
  onDownload?: () => void | Promise<void>
}

type Kind = 'image' | 'markdown' | 'code' | 'download'

function kindOf(source: FilePreviewSource): Kind {
  if (isImageName(source.name) && source.getBytesBase64) return 'image'
  if (isMarkdownName(source.name) && source.getText) return 'markdown'
  if (source.getText) return 'code'
  return 'download'
}

/** A right-side Sheet that previews a single file, reused by the workspace
 *  browser and by chat attachment chips. Dispatches on file type: images →
 *  <img>, markdown → rich render, text/code → CodeBlock, else a download card. */
export const FilePreviewSheet: FC<{
  source: FilePreviewSource | null
  open: boolean
  onOpenChange: (open: boolean) => void
}> = ({ source, open, onOpenChange }) => {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [text, setText] = useState<string | null>(null)
  const [imageSrc, setImageSrc] = useState<string | null>(null)
  const [errorKey, setErrorKey] = useState<
    'previewExpired' | 'previewUnsupported' | 'previewError' | null
  >(null)

  const kind = source ? kindOf(source) : 'download'

  useEffect(() => {
    if (!source || !open) return
    let cancelled = false
    // The previous file's content is discarded by DERIVATION (see `loadedFor`)
    // rather than by writing three resets here, which showed the old file's
    // text under the new file's name for a frame.
    const k = kindOf(source)
    const getBytes = source.getBytesBase64
    const getText = source.getText
    if (k === 'download') return
    // This effect IS the fetch, and the flag is what says it is in progress;
    // there is no render-time value to derive it from before the request
    // exists.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setLoading(true)
    const load = async () => {
      try {
        if (k === 'image' && getBytes) {
          const b64 = await getBytes()
          if (!cancelled)
            setImageSrc(`data:${imageMimeForName(source.name)};base64,${b64}`)
        } else if (getText) {
          const content = await getText()
          if (!cancelled) setText(content)
        }
      } catch (e) {
        if (cancelled) return
        const status = (e as { status?: number })?.status
        setErrorKey(
          status === 410
            ? 'previewExpired'
            : status === 413
              ? 'previewUnsupported'
              : 'previewError'
        )
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    void load()
    return () => {
      cancelled = true
    }
  }, [source, open])

  const download = () => {
    if (!source) return
    void (source.onDownload?.() ?? Promise.resolve())
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="xl:w-5xl w-3xl gap-0 p-0 sm:max-w-4xl"
      >
        <SheetHeader className="flex-row items-center justify-between gap-2 border-b">
          <SheetTitle className="min-w-0 flex-1 truncate">
            {source?.name}
            {source?.sizeHint != null && (
              <span className="text-muted-foreground ml-2 text-xs font-normal">
                {formatBytes(source.sizeHint)}
              </span>
            )}
          </SheetTitle>
          {source && (source.getBytesBase64 || source.onDownload) && (
            <Button
              variant="ghost"
              size="icon"
              className="mr-8 size-7 shrink-0"
              onClick={download}
              aria-label={t('workspace.download')}
            >
              <DownloadIcon className="size-4" />
            </Button>
          )}
        </SheetHeader>

        {loading ? (
          <div className="text-muted-foreground flex flex-1 items-center justify-center gap-2 text-sm">
            <Loader2Icon className="size-4 animate-spin" />
            {t('workspace.loading')}
          </div>
        ) : errorKey ? (
          <div className="text-muted-foreground flex flex-1 items-center justify-center px-6 text-center text-sm">
            {t(`workspace.${errorKey}`)}
          </div>
        ) : kind === 'image' && imageSrc ? (
          <div className="bg-muted/20 flex flex-1 items-center justify-center overflow-auto p-4">
            <img
              src={imageSrc}
              alt={source?.name}
              className="max-h-full max-w-full object-contain"
            />
          </div>
        ) : kind === 'markdown' && text != null ? (
          // min-h-0 lets this flex child shrink below content height so it can
          // scroll; overflow-auto handles both axes (wide code blocks / tables).
          <div className="min-h-0 flex-1 overflow-auto px-6 py-4 text-sm">
            <MarkdownContent content={text} />
          </div>
        ) : kind === 'code' && text != null ? (
          <div className="relative flex-1 overflow-hidden">
            <pre className="overflow-auto rounded-md border bg-muted/40 p-3 text-xs leading-relaxed"><code>{text ?? ""}</code></pre>
          </div>
        ) : (
          <div className="text-muted-foreground flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center text-sm">
            <FileIcon className="size-10 opacity-40" />
            <p>{t('workspace.previewUnsupported')}</p>
            {source && (source.getBytesBase64 || source.onDownload) && (
              <Button variant="outline" size="sm" onClick={download}>
                <DownloadIcon className="mr-1.5 size-4" />
                {t('workspace.download')}
              </Button>
            )}
          </div>
        )}
      </SheetContent>
    </Sheet>
  )
}
