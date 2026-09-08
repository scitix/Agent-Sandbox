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
  type ReasoningGroupComponent,
  type ReasoningMessagePartComponent,
  useAuiState,
  useScrollLock,
} from '@assistant-ui/react'
import { MarkdownText } from '@/components/assistant-ui/markdown-text'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { cn } from '@/lib/utils'
import { type VariantProps, cva } from 'class-variance-authority'
import { BrainIcon, ChevronDownIcon } from 'lucide-react'
import { memo, useCallback, useEffect, useRef, useState } from 'react'

const ANIMATION_DURATION = 200

// No vertical margin: every container that stacks these blocks (the message
// content column, the chain-of-thought group) supplies the rhythm with `gap`.
// A `mb-*` here spaced reasoning→next but left next→reasoning flush, so a
// reasoning/tool-group alternation came out unevenly spaced.
//
// `rounded-2xl` matches the user bubble and the tool group: the three framed
// blocks a message is built from sit in one column, so three different corner
// radii read as three unrelated widgets rather than one conversation. The tool
// cards NESTED inside a group keep their smaller radius — an inner corner
// should be tighter than the frame that holds it.
const reasoningVariants = cva('aui-reasoning-root w-full', {
  variants: {
    variant: {
      outline: 'rounded-2xl border px-3 py-2',
      ghost: '',
      muted: 'bg-muted/50 rounded-2xl px-3 py-2',
    },
  },
  defaultVariants: {
    variant: 'outline',
  },
})

export type ReasoningRootProps = Omit<
  React.ComponentProps<typeof Collapsible>,
  'open' | 'onOpenChange'
> &
  VariantProps<typeof reasoningVariants> & {
    open?: boolean
    onOpenChange?: (open: boolean) => void
    defaultOpen?: boolean
  }

function ReasoningRoot({
  className,
  variant,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  defaultOpen = false,
  children,
  ...props
}: ReasoningRootProps) {
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

  return (
    <Collapsible
      ref={collapsibleRef}
      data-slot="reasoning-root"
      data-variant={variant}
      open={isOpen}
      onOpenChange={handleOpenChange}
      className={cn(
        'group/reasoning-root',
        reasoningVariants({ variant, className })
      )}
      style={
        {
          '--animation-duration': `${ANIMATION_DURATION}ms`,
        } as React.CSSProperties
      }
      {...props}
    >
      {children}
    </Collapsible>
  )
}

/**
 * The soft bottom edge of the reasoning scroller.
 *
 * Three things were wrong with it and all three had the same root: it was
 * rendered unconditionally, inside the collapsible content, in the wrong
 * colour.
 *
 *   * UNCONDITIONALLY, because the rules that were supposed to hide it keyed off
 *     `data-[state=open]` — an attribute Base UI never sets (it marks an open
 *     panel with `data-open`). So a dead selector left the gradient permanently
 *     on, over blocks with nothing to fade.
 *   * INSIDE THE CONTENT, so it stopped at the content's bottom and left the
 *     root's `py-2` and then the border below it: a gradient ending in mid-air.
 *   * IN `--background`, the canvas tone, while the conversation is painted
 *     `--card` — a grey wash over white.
 *
 * It is now owned by `ReasoningText`, which is the element that actually
 * scrolls, and shown only while that element HAS more to show. A fade means
 * "there is more below"; one that is always on means nothing.
 */
function ReasoningFade({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      data-slot="reasoning-fade"
      aria-hidden
      className={cn(
        'aui-reasoning-fade pointer-events-none absolute inset-x-0 bottom-0 z-10 h-6',
        'from-card bg-gradient-to-t to-transparent',
        'group-data-[variant=muted]/reasoning-root:from-muted/50',
        'duration-(--animation-duration) opacity-0 transition-opacity',
        'data-more:opacity-100',
        className
      )}
      {...props}
    />
  )
}

/**
 * Is this element scrolled short of its own end?
 *
 * Both halves matter: content taller than the box, AND the user not already at
 * the bottom. Re-measured on scroll and on resize, and resize is what catches
 * streaming — the text grows under the observer while the model is still
 * thinking, with no scroll event of its own.
 */
function useHasMoreBelow(ref: React.RefObject<HTMLElement | null>): boolean {
  const [more, setMore] = useState(false)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    // One pixel of slack: fractional layout makes an element scrolled to the
    // very bottom report a sub-pixel remainder, which would leave the fade up
    // for good.
    const measure = () =>
      setMore(el.scrollHeight - el.scrollTop - el.clientHeight > 1)
    measure()
    el.addEventListener('scroll', measure, { passive: true })
    const observer = new ResizeObserver(measure)
    observer.observe(el)
    // The scroller's own box can stay put while its content grows.
    if (el.firstElementChild) observer.observe(el.firstElementChild)
    return () => {
      el.removeEventListener('scroll', measure)
      observer.disconnect()
    }
  }, [ref])
  return more
}

function ReasoningTrigger({
  active,
  duration,
  className,
  ...props
}: React.ComponentProps<typeof CollapsibleTrigger> & {
  active?: boolean
  duration?: number
}) {
  const durationText = duration ? ` (${duration}s)` : ''

  return (
    <CollapsibleTrigger
      data-slot="reasoning-trigger"
      className={cn(
        'aui-reasoning-trigger group/trigger text-muted-foreground hover:text-foreground flex max-w-[75%] items-center gap-2 py-1 text-sm transition-colors',
        className
      )}
      {...props}
    >
      <BrainIcon
        data-slot="reasoning-trigger-icon"
        className="aui-reasoning-trigger-icon size-4 shrink-0"
      />
      <span
        data-slot="reasoning-trigger-label"
        className="aui-reasoning-trigger-label-wrapper relative inline-block leading-none"
      >
        <span>Reasoning{durationText}</span>
        {active ? (
          <span
            aria-hidden
            data-slot="reasoning-trigger-shimmer"
            className="aui-reasoning-trigger-shimmer shimmer pointer-events-none absolute inset-0 motion-reduce:animate-none"
          >
            Reasoning{durationText}
          </span>
        ) : null}
      </span>
      <ChevronDownIcon
        data-slot="reasoning-trigger-chevron"
        className={cn(
          'aui-reasoning-trigger-chevron mt-0.5 size-4 shrink-0',
          'duration-(--animation-duration) transition-transform ease-out',
          // Base UI marks an open trigger with `data-panel-open`; it sets no
          // `data-state`, so the previous pair of selectors never matched.
          'group-data-panel-open/trigger:rotate-0 -rotate-90'
        )}
      />
    </CollapsibleTrigger>
  )
}

function ReasoningContent({
  className,
  children,
  ...props
}: React.ComponentProps<typeof CollapsibleContent>) {
  return (
    <CollapsibleContent
      data-slot="reasoning-content"
      className={cn(
        'aui-reasoning-content text-muted-foreground relative overflow-hidden text-sm outline-none',
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
      {children}
    </CollapsibleContent>
  )
}

function ReasoningText({
  className,
  children,
  ...props
}: React.ComponentProps<'div'>) {
  const scroller = useRef<HTMLDivElement>(null)
  const more = useHasMoreBelow(scroller)
  return (
    <div className="relative">
      <div
        ref={scroller}
        data-slot="reasoning-text"
        className={cn(
          'aui-reasoning-text relative z-0 max-h-64 overflow-y-auto pb-2 ps-6 pt-2 leading-relaxed',
          'transform-gpu transition-[transform,opacity]',
          className
        )}
        {...props}
      >
        {/* One element for the ResizeObserver to watch: the scroller's own box
            stops growing at `max-h-64`, so only the content inside it reports
            that more text arrived while the model is still thinking. */}
        <div className="space-y-4">{children}</div>
      </div>
      <ReasoningFade {...(more ? { 'data-more': '' } : {})} />
    </div>
  )
}

const ReasoningImpl: ReasoningMessagePartComponent = () => <MarkdownText />

const ReasoningGroupImpl: ReasoningGroupComponent = ({
  children,
  startIndex,
  endIndex,
}) => {
  const isReasoningStreaming = useAuiState(s => {
    if (s.message.status?.type !== 'running') return false
    const lastIndex = s.message.parts.length - 1
    if (lastIndex < 0) return false
    const lastType = s.message.parts[lastIndex]?.type
    if (lastType !== 'reasoning') return false
    return lastIndex >= startIndex && lastIndex <= endIndex
  })

  return (
    <ReasoningRoot defaultOpen={isReasoningStreaming}>
      <ReasoningTrigger active={isReasoningStreaming} />
      <ReasoningContent aria-busy={isReasoningStreaming}>
        <ReasoningText>{children}</ReasoningText>
      </ReasoningContent>
    </ReasoningRoot>
  )
}

const Reasoning = memo(
  ReasoningImpl
) as unknown as ReasoningMessagePartComponent & {
  Root: typeof ReasoningRoot
  Trigger: typeof ReasoningTrigger
  Content: typeof ReasoningContent
  Text: typeof ReasoningText
  Fade: typeof ReasoningFade
}

Reasoning.displayName = 'Reasoning'
Reasoning.Root = ReasoningRoot
Reasoning.Trigger = ReasoningTrigger
Reasoning.Content = ReasoningContent
Reasoning.Text = ReasoningText
Reasoning.Fade = ReasoningFade

/**
 * @deprecated This wrapper targets the legacy `components.ReasoningGroup`
 * prop on `<MessagePrimitive.Parts>`. Use `<MessagePrimitive.GroupedParts>`
 * with a `groupBy` returning `"group-reasoning"` and compose `ReasoningRoot`
 * / `ReasoningTrigger` / `ReasoningContent` / `ReasoningText` directly.
 * See `thread.tsx` for an example.
 */
const ReasoningGroup = memo(ReasoningGroupImpl)
ReasoningGroup.displayName = 'ReasoningGroup'

export {
  Reasoning,
  ReasoningGroup,
  ReasoningRoot,
  ReasoningTrigger,
  ReasoningContent,
  ReasoningText,
  ReasoningFade,
  reasoningVariants,
}
