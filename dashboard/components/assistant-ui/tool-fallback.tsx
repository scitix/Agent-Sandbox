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
  type ToolCallMessagePartComponent,
  type ToolCallMessagePartStatus,
  useScrollLock,
} from '@assistant-ui/react'
import { TooltipIconButton } from '@/components/assistant-ui/tooltip-icon-button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { useTranslation } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import {
  AlertCircleIcon,
  CheckIcon,
  ChevronDownIcon,
  CopyIcon,
  LoaderIcon,
  XCircleIcon,
} from 'lucide-react'
import {
  createContext,
  memo,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
} from 'react'

const ANIMATION_DURATION = 200

/**
 * The card's open state, published by the root.
 *
 * The header is a row of three things — the trigger, a copy button, a chevron —
 * and only one of them can be the Collapsible's own trigger, so the chevron
 * toggles through this instead. Reading `open` from here rather than from a CSS
 * state selector also means the rotation does not depend on which data attribute
 * the collapsible library of the day happens to emit.
 */
const ToolFallbackOpenContext = createContext<{
  open: boolean
  toggle: () => void
} | null>(null)

export type ToolFallbackRootProps = Omit<
  React.ComponentProps<typeof Collapsible>,
  'open' | 'onOpenChange'
> & {
  open?: boolean
  onOpenChange?: (open: boolean) => void
  defaultOpen?: boolean
}

function ToolFallbackRoot({
  className,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  defaultOpen = false,
  children,
  ...props
}: ToolFallbackRootProps) {
  const collapsibleRef = useRef<HTMLDivElement>(null)
  const [uncontrolledOpen, setUncontrolledOpen] = useState(defaultOpen)
  const lockScroll = useScrollLock(collapsibleRef, ANIMATION_DURATION)

  const isControlled = controlledOpen !== undefined
  const isOpen = isControlled ? controlledOpen : uncontrolledOpen

  const handleOpenChange = useCallback(
    (open: boolean) => {
      if (!open) {
        lockScroll()
      }
      if (!isControlled) {
        setUncontrolledOpen(open)
      }
      controlledOnOpenChange?.(open)
    },
    [lockScroll, isControlled, controlledOnOpenChange]
  )

  const openState = useMemo(
    () => ({ open: isOpen, toggle: () => handleOpenChange(!isOpen) }),
    [isOpen, handleOpenChange]
  )

  return (
    <Collapsible
      ref={collapsibleRef}
      data-slot="tool-fallback-root"
      open={isOpen}
      onOpenChange={handleOpenChange}
      className={cn(
        'aui-tool-fallback-root group/tool-fallback-root w-full rounded-lg border py-3',
        className
      )}
      style={
        {
          '--animation-duration': `${ANIMATION_DURATION}ms`,
        } as React.CSSProperties
      }
      {...props}
    >
      <ToolFallbackOpenContext.Provider value={openState}>
        {children}
      </ToolFallbackOpenContext.Provider>
    </Collapsible>
  )
}

type ToolStatus = ToolCallMessagePartStatus['type']

const statusIconMap: Record<ToolStatus, React.ElementType> = {
  running: LoaderIcon,
  complete: CheckIcon,
  incomplete: XCircleIcon,
  'requires-action': AlertCircleIcon,
}

/** One-line gist of a call's arguments for the collapsed trigger row, so the
 *  command a tool actually ran is readable without expanding anything.
 *
 *  Arguments arrive as a JSON object; the interesting part is almost always a
 *  single field (bash's `command`, a path, a query), so a lone string value is
 *  unwrapped and everything else is rendered as `key=value`. Newlines collapse
 *  to spaces — this has to stay one line — and CSS truncation does the rest. */
function argsPreview(argsText?: string): string {
  if (!argsText) return ''
  const flatten = (s: string) => s.replace(/\s+/g, ' ').trim()
  let parsed: unknown
  try {
    parsed = JSON.parse(argsText)
  } catch {
    return flatten(argsText)
  }
  if (typeof parsed === 'string') return flatten(parsed)
  if (!parsed || typeof parsed !== 'object') return flatten(argsText)
  const entries = Object.entries(parsed as Record<string, unknown>)
  if (entries.length === 0) return ''
  if (entries.length === 1 && typeof entries[0][1] === 'string') {
    return flatten(entries[0][1])
  }
  return flatten(
    entries
      .map(([k, v]) => `${k}=${typeof v === 'string' ? v : JSON.stringify(v)}`)
      .join(' ')
  )
}

/**
 * Copy the arguments as the header shows them: the flattened one-liner, which for
 * the tools that matter IS a runnable command (`navix … --filter …`) — not the
 * JSON envelope it travelled in. What the row truncates, this yields in full.
 *
 * A SIBLING of the trigger rather than a child, because a button inside a button
 * is invalid HTML and the click would reach the collapsible instead. That is what
 * splits the header into three: trigger, this, then the chevron.
 */
function ToolFallbackCopyArgs({ value }: { value: string }) {
  const { t } = useTranslation()
  const { isCopied, handleCopy } = useCopyToClipboard({ text: value })
  return (
    <TooltipIconButton
      tooltip={t('assistant.tool.copyArgs')}
      data-slot="tool-fallback-copy-args"
      className="aui-tool-fallback-copy-args text-muted-foreground shrink-0"
      onClick={handleCopy}
    >
      {isCopied ? <CheckIcon /> : <CopyIcon />}
    </TooltipIconButton>
  )
}

function ToolFallbackTrigger({
  toolName,
  argsText,
  status,
  className,
  ...props
}: React.ComponentProps<typeof CollapsibleTrigger> & {
  toolName: string
  argsText?: string
  status?: ToolCallMessagePartStatus
}) {
  const { t } = useTranslation()
  const statusType = status?.type ?? 'complete'
  const isRunning = statusType === 'running'
  const isCancelled =
    status?.type === 'incomplete' && status.reason === 'cancelled'

  const Icon = statusIconMap[statusType]
  const label = isCancelled
    ? t('assistant.tool.cancelled')
    : t('assistant.tool.used')
  const preview = argsPreview(argsText)

  return (
    <div
      data-slot="tool-fallback-header"
      className="aui-tool-fallback-header flex w-full items-center gap-2 px-4"
    >
      <CollapsibleTrigger
        data-slot="tool-fallback-trigger"
        className={cn(
          'aui-tool-fallback-trigger group/trigger flex min-w-0 grow items-center gap-2 text-sm transition-colors',
          className
        )}
        {...props}
      >
        <Icon
          data-slot="tool-fallback-trigger-icon"
          className={cn(
            'aui-tool-fallback-trigger-icon size-4 shrink-0',
            isCancelled && 'text-muted-foreground',
            isRunning && 'animate-spin'
          )}
        />
        <span
          data-slot="tool-fallback-trigger-label"
          className={cn(
            'aui-tool-fallback-trigger-label-wrapper relative inline-block shrink-0 text-start leading-none',
            isCancelled && 'text-muted-foreground line-through'
          )}
        >
          <span>
            {label}: <b>{toolName}</b>
          </span>
          {isRunning && (
            <span
              aria-hidden
              data-slot="tool-fallback-trigger-shimmer"
              className="aui-tool-fallback-trigger-shimmer shimmer pointer-events-none absolute inset-0 motion-reduce:animate-none"
            >
              {label}: <b>{toolName}</b>
            </span>
          )}
        </span>
        {/* Stays visible when expanded: it is the flattened command, which the
            JSON block below is not, and it is what the copy button hands over. */}
        {preview && (
          <span
            data-slot="tool-fallback-trigger-args-preview"
            title={preview}
            className={cn(
              'aui-tool-fallback-trigger-args-preview text-muted-foreground min-w-0 grow truncate text-start font-mono text-xs leading-none',
              isCancelled && 'line-through'
            )}
          >
            {preview}
          </span>
        )}
      </CollapsibleTrigger>
      {preview && <ToolFallbackCopyArgs value={preview} />}
      <ToolFallbackChevron />
    </div>
  )
}

/**
 * Clickable chevron that is NOT the Collapsible's trigger.
 *
 * It duplicates a control that already exists, so it is hidden from assistive tech
 * and skipped by the keyboard — a second `Collapsible.Trigger` here would be a
 * nameless extra tab stop, because Base UI's `useButton` merges its own
 * `tabIndex=0` last and overrides any -1 passed in.
 */
function ToolFallbackChevron() {
  const state = useContext(ToolFallbackOpenContext)
  return (
    <button
      type="button"
      aria-hidden
      tabIndex={-1}
      onClick={state?.toggle}
      data-slot="tool-fallback-chevron-button"
      className="aui-tool-fallback-chevron-button text-muted-foreground shrink-0"
    >
      <ChevronDownIcon
        data-slot="tool-fallback-trigger-chevron"
        className={cn(
          'aui-tool-fallback-trigger-chevron size-4 shrink-0',
          'duration-(--animation-duration) transition-transform ease-out',
          state?.open ? 'rotate-0' : '-rotate-90'
        )}
      />
    </button>
  )
}

function ToolFallbackContent({
  className,
  children,
  ...props
}: React.ComponentProps<typeof CollapsibleContent>) {
  return (
    <CollapsibleContent
      data-slot="tool-fallback-content"
      className={cn(
        'aui-tool-fallback-content relative overflow-hidden text-sm outline-none',
        'group/collapsible-content ease-out',
        'data-[state=closed]:animate-collapsible-up',
        'data-[state=open]:animate-collapsible-down',
        'data-[state=closed]:fill-mode-forwards',
        'data-[state=closed]:pointer-events-none',
        'data-[state=open]:duration-(--animation-duration)',
        'data-[state=closed]:duration-(--animation-duration)',
        className
      )}
      {...props}
    >
      <div className="mt-3 flex flex-col gap-2 border-t pt-2">{children}</div>
    </CollapsibleContent>
  )
}

function ToolFallbackArgs({
  argsText,
  className,
  ...props
}: React.ComponentProps<'div'> & {
  argsText?: string
}) {
  if (!argsText) return null

  return (
    <div
      data-slot="tool-fallback-args"
      className={cn('aui-tool-fallback-args px-4', className)}
      {...props}
    >
      <pre className="aui-tool-fallback-args-value whitespace-pre-wrap">
        {argsText}
      </pre>
    </div>
  )
}

function ToolFallbackResult({
  result,
  className,
  ...props
}: React.ComponentProps<'div'> & {
  result?: unknown
}) {
  const { t } = useTranslation()
  if (result === undefined) return null

  return (
    <div
      data-slot="tool-fallback-result"
      className={cn(
        'aui-tool-fallback-result border-t border-dashed px-4 pt-2',
        className
      )}
      {...props}
    >
      <p className="aui-tool-fallback-result-header font-semibold">
        {t('assistant.tool.result')}
      </p>
      {/* A tool result can be megabytes (a node dump, a log tail). Cap it and let
          it scroll in place, so one big call can't push the rest of the turn —
          and the composer — off the screen. */}
      <pre className="aui-tool-fallback-result-content max-h-72 overflow-auto overscroll-contain whitespace-pre-wrap">
        {typeof result === 'string' ? result : JSON.stringify(result, null, 2)}
      </pre>
    </div>
  )
}

function ToolFallbackError({
  status,
  className,
  ...props
}: React.ComponentProps<'div'> & {
  status?: ToolCallMessagePartStatus
}) {
  const { t } = useTranslation()
  if (status?.type !== 'incomplete') return null

  const error = status.error
  const errorText = error
    ? typeof error === 'string'
      ? error
      : JSON.stringify(error)
    : null

  if (!errorText) return null

  const isCancelled = status.reason === 'cancelled'
  const headerText = isCancelled
    ? t('assistant.tool.cancelledReason')
    : t('assistant.tool.error')

  return (
    <div
      data-slot="tool-fallback-error"
      className={cn('aui-tool-fallback-error px-4', className)}
      {...props}
    >
      <p className="aui-tool-fallback-error-header text-muted-foreground font-semibold">
        {headerText}
      </p>
      <p className="aui-tool-fallback-error-reason text-muted-foreground">
        {errorText}
      </p>
    </div>
  )
}

const ToolFallbackImpl: ToolCallMessagePartComponent = ({
  toolName,
  argsText,
  result,
  status,
}) => {
  const isCancelled =
    status?.type === 'incomplete' && status.reason === 'cancelled'

  return (
    <ToolFallbackRoot
      className={cn(isCancelled && 'border-muted-foreground/30 bg-muted/30')}
    >
      <ToolFallbackTrigger
        toolName={toolName}
        argsText={argsText}
        status={status}
      />
      <ToolFallbackContent>
        <ToolFallbackError status={status} />
        <ToolFallbackArgs
          argsText={argsText}
          className={cn(isCancelled && 'opacity-60')}
        />
        {!isCancelled && <ToolFallbackResult result={result} />}
      </ToolFallbackContent>
    </ToolFallbackRoot>
  )
}

const ToolFallback = memo(
  ToolFallbackImpl
) as unknown as ToolCallMessagePartComponent & {
  Root: typeof ToolFallbackRoot
  Trigger: typeof ToolFallbackTrigger
  Content: typeof ToolFallbackContent
  Args: typeof ToolFallbackArgs
  Result: typeof ToolFallbackResult
  Error: typeof ToolFallbackError
}

ToolFallback.displayName = 'ToolFallback'
ToolFallback.Root = ToolFallbackRoot
ToolFallback.Trigger = ToolFallbackTrigger
ToolFallback.Content = ToolFallbackContent
ToolFallback.Args = ToolFallbackArgs
ToolFallback.Result = ToolFallbackResult
ToolFallback.Error = ToolFallbackError

export {
  ToolFallback,
  ToolFallbackRoot,
  ToolFallbackTrigger,
  ToolFallbackContent,
  ToolFallbackArgs,
  ToolFallbackResult,
  ToolFallbackError,
}
