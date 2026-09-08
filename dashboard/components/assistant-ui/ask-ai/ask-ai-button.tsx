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

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useCopyToClipboardWithText } from '@/hooks/use-copy-to-clipboard'
import { useTranslation } from '@/lib/i18n'
import {
  type AskAiButtonAction,
  type AskAttachment,
  atomAskAiButtonAction,
  atomAssistantEnabled,
} from '@/lib/assistant/store'
import { cn } from '@/lib/utils'
import { useAtomValue, useSetAtom } from 'jotai'
import { ChevronDownIcon, CopyIcon, SparklesIcon } from 'lucide-react'
import { ReactNode } from 'react'

import { useAskAI } from './use-ask-ai'

/** A value, or a thunk that produces it lazily at click time. Used so callers
 *  can defer expensive work (serializing a whole table) until the click. */
type OrLazy<T> = T | (() => T | undefined)

function resolveLazy<T>(value: OrLazy<T> | undefined): T | undefined {
  return typeof value === 'function' ? (value as () => T | undefined)() : value
}

/** The plain-text context a "Copy" click puts on the clipboard: the same prompt
 *  + attachment an "Ask AI" click would hand the assistant, concatenated so it
 *  can be pasted into any external tool. */
function buildCopyText(prompt?: string, attachment?: AskAttachment): string {
  return [prompt, attachment?.markdown].filter(Boolean).join('\n\n')
}

export interface AskAIButtonProps {
  /** Question text dropped into the composer (or copied). */
  prompt?: OrLazy<string>
  /** Content payload attached to the composer / copied (table, report, …). */
  attachment?: OrLazy<AskAttachment>
  /** The Ask-mode label (the Copy-mode label is always "Copy"). */
  children?: ReactNode
  className?: string
  /** Render the action icon only (no label); pass an `aria-label` for a11y. */
  iconOnly?: boolean
  'aria-label'?: string
}

/**
 * The base Ask AI button — the shared design/behavior every prompt variant in
 * this folder builds on. Renders nothing when the assistant is disabled. Prompt
 * variants live alongside this file; toolbars/pages import those, not this
 * directly.
 *
 * It's a split button: the primary click performs the globally-selected action
 * ("Ask AI" or "Copy"), and the chevron opens a dropdown to switch which one is
 * the default. The choice lives in a single global atom
 * ({@link atomAskAiButtonAction}), so every Ask AI button across the app shares
 * one preference — a user sets it once, not per button.
 *
 * `useAskAI` is deliberately runtime-free (only sets atoms), so this button is
 * safe to render on any page whether or not the assistant runtime is mounted —
 * with the lazy-connection model the runtime is unmounted while the panel is
 * closed, and clicking the button is precisely what opens the panel and mounts
 * it. Gate on `atomAssistantEnabled` alone.
 */
export function AskAIButton(props: AskAIButtonProps) {
  const assistantEnabled = useAtomValue(atomAssistantEnabled)
  if (!assistantEnabled) return null
  return <AskAIButtonInner {...props} />
}

function AskAIButtonInner({
  prompt,
  attachment,
  children = 'Ask AI',
  className,
  iconOnly,
  'aria-label': ariaLabel,
}: AskAIButtonProps) {
  const { t } = useTranslation()
  const { ask } = useAskAI()
  const { handleCopyWithText } = useCopyToClipboardWithText()
  const action = useAtomValue(atomAskAiButtonAction)
  const setAction = useSetAtom(atomAskAiButtonAction)

  const isCopy = action === 'copy'
  const Icon = isCopy ? CopyIcon : SparklesIcon
  const switchLabel = t('assistant.askAiSwitchAction')

  const handlePrimary = () => {
    // Resolve the lazy prompt/attachment once, at click time (may be expensive).
    const resolvedPrompt = resolveLazy(prompt)
    const resolvedAttachment = resolveLazy(attachment)
    if (isCopy) {
      handleCopyWithText(buildCopyText(resolvedPrompt, resolvedAttachment))
    } else {
      void ask({ prompt: resolvedPrompt, attachment: resolvedAttachment })
    }
  }

  return (
    <div
      className={cn(
        'border-border bg-background shadow-xs inline-flex h-9 items-stretch rounded-md border',
        'dark:border-input dark:bg-input/30'
      )}
    >
      <button
        type="button"
        onClick={handlePrimary}
        aria-label={ariaLabel}
        className={cn(
          'flex cursor-pointer items-center gap-1.5 rounded-l-md px-2.5 text-xs font-medium transition-colors',
          'hover:bg-muted hover:text-foreground dark:hover:bg-input/50',
          className
        )}
      >
        <Icon className="size-4" />
        {!iconOnly && <span>{isCopy ? t('common.copy') : children}</span>}
      </button>
      <div className="bg-border dark:bg-input w-px self-stretch" />
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <button
              type="button"
              aria-label={switchLabel}
              className="text-muted-foreground hover:bg-muted hover:text-foreground dark:hover:bg-input/50 flex cursor-pointer items-center rounded-r-md px-1.5 transition-colors"
            >
              <ChevronDownIcon className="size-3.5" />
            </button>
          }
        />
        <DropdownMenuContent align="end" className="min-w-40">
          <DropdownMenuRadioGroup
            value={action}
            onValueChange={value => setAction(value as AskAiButtonAction)}
          >
            {/* GroupLabel requires a group context (Base UI Menu.GroupLabel),
                so it must live inside the RadioGroup — which also associates it
                as the group's aria-labelledby. */}
            <DropdownMenuLabel>{switchLabel}</DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuRadioItem value="ask">
              <SparklesIcon className="size-4" />
              {t('table.askAi')}
            </DropdownMenuRadioItem>
            <DropdownMenuRadioItem value="copy">
              <CopyIcon className="size-4" />
              {t('common.copy')}
            </DropdownMenuRadioItem>
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}
