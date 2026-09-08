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
  type CodeHeaderProps,
  MarkdownTextPrimitive,
  unstable_memoizeMarkdownComponents as memoizeMarkdownComponents,
  useIsMarkdownCodeBlock,
} from '@assistant-ui/react-markdown'
import '@assistant-ui/react-markdown/styles/dot.css'
import { TooltipIconButton } from '@/components/assistant-ui/tooltip-icon-button'
import { cn } from '@/lib/utils'
import { CheckIcon, CopyIcon } from 'lucide-react'
import { type FC, memo, useState } from 'react'
import Markdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'


const MarkdownTextImpl = () => {
  return (
    <MarkdownTextPrimitive
      remarkPlugins={[remarkGfm]}
      className="aui-md"
      components={defaultComponents}
    />
  )
}

export const MarkdownText = memo(MarkdownTextImpl)

/** Render an arbitrary markdown STRING (not a chat message) with the same styling
 *  as {@link MarkdownText}. Used to preview `.md` workspace files / attachments.
 *  Reuses `defaultComponents`; outside the assistant-ui primitive
 *  `useIsMarkdownCodeBlock` safely defaults to false, so fenced code still
 *  renders (as monospace) rather than throwing. */
export const MarkdownContent: FC<{ content: string }> = ({ content }) => {
  return (
    <div className="aui-md">
      <Markdown remarkPlugins={[remarkGfm]} components={contentComponents}>
        {content}
      </Markdown>
    </div>
  )
}

const CodeHeader: FC<CodeHeaderProps> = ({ language, code }) => {
  const { isCopied, copyToClipboard } = useCopyToClipboard()
  const onCopy = () => {
    if (!code || isCopied) return
    copyToClipboard(code)
  }

  return (
    <div className="aui-code-header-root border-border/50 bg-muted/50 mt-2.5 flex items-center justify-between rounded-t-lg border border-b-0 px-3 py-1.5 text-xs">
      <span className="aui-code-header-language text-muted-foreground font-medium lowercase">
        {language}
      </span>
      <TooltipIconButton tooltip="Copy" onClick={onCopy}>
        {!isCopied && <CopyIcon />}
        {isCopied && <CheckIcon />}
      </TooltipIconButton>
    </div>
  )
}

const useCopyToClipboard = ({
  copiedDuration = 3000,
}: {
  copiedDuration?: number
} = {}) => {
  const [isCopied, setIsCopied] = useState<boolean>(false)

  const copyToClipboard = (value: string) => {
    if (!value || typeof navigator === 'undefined' || !navigator.clipboard) {
      return
    }

    navigator.clipboard.writeText(value).then(
      () => {
        setIsCopied(true)
        setTimeout(() => setIsCopied(false), copiedDuration)
      },
      () => {}
    )
  }

  return { isCopied, copyToClipboard }
}

const preClassName =
  'aui-md-pre border-border/50 bg-muted/30 overflow-x-auto rounded-b-lg rounded-t-none border border-t-0 p-3 text-xs leading-relaxed'

/** The shape of a fenced code block as react-markdown hands it to `pre`: one
 *  `code` child carrying the language class and the raw source. Typed narrowly
 *  here so the file needs no hast dependency. */
type FencedNode = {
  children?: {
    tagName?: string
    properties?: { className?: unknown }
    children?: { value?: unknown }[]
  }[]
}

/** The mermaid source of a fenced block, or null when the block is anything
 *  else. `MarkdownTextPrimitive` routes fenced blocks by language on its own
 *  (see `componentsByLanguage` above); plain `Markdown` does not, so the
 *  non-chat path recovers the language from the parsed node. */
const mermaidSourceOf = (node: unknown): string | null => {
  const code = (node as FencedNode | undefined)?.children?.[0]
  if (code?.tagName !== 'code') return null
  const names = code.properties?.className
  const isMermaid = Array.isArray(names)
    ? names.includes('language-mermaid')
    : names === 'language-mermaid'
  if (!isMermaid) return null
  const source = code.children?.[0]?.value
  return typeof source === 'string' && source.trim() ? source : null
}

const defaultComponents = memoizeMarkdownComponents({
  h1: ({ className, ...props }) => (
    <h1
      className={cn(
        'aui-md-h1 mb-2 scroll-m-20 text-base font-semibold first:mt-0 last:mb-0',
        className
      )}
      {...props}
    />
  ),
  h2: ({ className, ...props }) => (
    <h2
      className={cn(
        'aui-md-h2 mb-1.5 mt-3 scroll-m-20 text-sm font-semibold first:mt-0 last:mb-0',
        className
      )}
      {...props}
    />
  ),
  h3: ({ className, ...props }) => (
    <h3
      className={cn(
        'aui-md-h3 mb-1 mt-2.5 scroll-m-20 text-sm font-semibold first:mt-0 last:mb-0',
        className
      )}
      {...props}
    />
  ),
  h4: ({ className, ...props }) => (
    <h4
      className={cn(
        'aui-md-h4 mb-1 mt-2 scroll-m-20 text-sm font-medium first:mt-0 last:mb-0',
        className
      )}
      {...props}
    />
  ),
  h5: ({ className, ...props }) => (
    <h5
      className={cn(
        'aui-md-h5 mb-1 mt-2 text-sm font-medium first:mt-0 last:mb-0',
        className
      )}
      {...props}
    />
  ),
  h6: ({ className, ...props }) => (
    <h6
      className={cn(
        'aui-md-h6 mb-1 mt-2 text-sm font-medium first:mt-0 last:mb-0',
        className
      )}
      {...props}
    />
  ),
  p: ({ className, ...props }) => (
    <p
      className={cn(
        'aui-md-p my-2.5 leading-normal first:mt-0 last:mb-0',
        className
      )}
      {...props}
    />
  ),
  a: ({ className, ...props }) => (
    <a
      className={cn(
        'aui-md-a text-primary hover:text-primary/80 underline underline-offset-2',
        className
      )}
      {...props}
    />
  ),
  blockquote: ({ className, ...props }) => (
    <blockquote
      className={cn(
        'aui-md-blockquote border-muted-foreground/30 text-muted-foreground my-2.5 border-s-2 ps-3 italic',
        className
      )}
      {...props}
    />
  ),
  ul: ({ className, ...props }) => (
    <ul
      className={cn(
        'aui-md-ul marker:text-muted-foreground my-2 ms-4 list-disc [&>li]:mt-1',
        className
      )}
      {...props}
    />
  ),
  ol: ({ className, ...props }) => (
    <ol
      className={cn(
        'aui-md-ol marker:text-muted-foreground my-2 ms-4 list-decimal [&>li]:mt-1',
        className
      )}
      {...props}
    />
  ),
  hr: ({ className, ...props }) => (
    <hr
      className={cn('aui-md-hr border-muted-foreground/20 my-2', className)}
      {...props}
    />
  ),
  // A table the agent returns is the same kind of object as a table the app
  // renders, so it is drawn with the same recipe as the data grid: a panel with
  // a hairline edge, a recessed header band at 11px/500, 13px body rows, and
  // horizontal rules only (the grid has no vertical rules).
  //
  // `border-separate` is kept — the corners are rounded per-cell below, because
  // `overflow: hidden` does not clip a `<table>` reliably.
  table: ({ className, ...props }) => (
    <table
      className={cn(
        'aui-md-table bg-card ring-border my-3 w-full border-separate border-spacing-0 overflow-hidden rounded-xl text-[13px] ring-1',
        '[&_tr:last-child>td]:border-b-0',
        className
      )}
      {...props}
    />
  ),
  th: ({ className, ...props }) => (
    <th
      className={cn(
        'aui-md-th bg-muted text-muted-foreground border-border [[align=center]]:text-center [[align=right]]:text-right border-b px-3.5 py-2 text-start text-[11px] font-medium first:rounded-ss-xl last:rounded-se-xl',
        className
      )}
      {...props}
    />
  ),
  td: ({ className, ...props }) => (
    <td
      className={cn(
        'aui-md-td border-border [[align=center]]:text-center [[align=right]]:text-right border-b px-3.5 py-2 text-start align-middle',
        className
      )}
      {...props}
    />
  ),
  tr: ({ className, ...props }) => (
    <tr
      className={cn(
        'aui-md-tr hover:bg-muted m-0 p-0 transition-colors [&:last-child>td:first-child]:rounded-es-xl [&:last-child>td:last-child]:rounded-ee-xl',
        className
      )}
      {...props}
    />
  ),
  li: ({ className, ...props }) => (
    <li className={cn('aui-md-li leading-normal', className)} {...props} />
  ),
  sup: ({ className, ...props }) => (
    <sup
      className={cn('aui-md-sup [&>a]:text-xs [&>a]:no-underline', className)}
      {...props}
    />
  ),
  pre: ({ className, ...props }) => (
    <pre className={cn(preClassName, className)} {...props} />
  ),
  code: function Code({ className, ...props }) {
    const isCodeBlock = useIsMarkdownCodeBlock()
    return (
      <code
        className={cn(
          !isCodeBlock &&
            'aui-md-inline-code border-border/50 bg-muted/50 rounded-md border px-1.5 py-0.5 font-mono text-[0.85em]',
          className
        )}
        {...props}
      />
    )
  },
  CodeHeader,
})

// A mermaid fence renders as code rather than as a diagram. Showing the source
// is honest — the alternative is a renderer this build does not carry, and a
// blank box where a diagram was promised.
const MermaidAwarePre: Components['pre'] = ({ className, node, ...props }) => {
  void mermaidSourceOf(node)
  return <pre className={cn(preClassName, className)} {...props} />
}

/** Same components, plus the mermaid fence — the standalone renderer, since a
 *  string of markdown is never mid-stream and carries no code header. */
const contentComponents = {
  ...defaultComponents,
  pre: MermaidAwarePre,
}
