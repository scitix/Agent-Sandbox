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

// The assistant's own menu: which agent is running, the two things you start from
// (a new conversation, the settings), and everything you have talked about.
//
// It is a surface of the PAGE, not of the app — hence a panel-coloured column
// beside the conversation rather than another rail. The app's navigation sidebar
// is dark and permanent; this one is light, collapsible, and about one page's
// contents.
import { BackendTitle } from '@/components/assistant-ui/backend-switcher'
import { SessionHistoryList } from '@/components/assistant-ui/session-history'
import TooltipButton from '@/components/button/tooltip-button'
import { useTranslation } from '@/lib/i18n'
import type { AssistantPageView } from '@/lib/assistant/store'
import { cn } from '@/lib/utils'
import {
  AlignLeftIcon,
  Loader2Icon,
  SettingsIcon,
  SquarePenIcon,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'

export interface AssistantMenuProps {
  onCollapse: () => void
  onNewSession: () => void
  newSessionBusy?: boolean
  /** Which non-conversation page is open, or null for the conversation. */
  page: AssistantPageView | null
  onOpenPage: (page: AssistantPageView) => void
  onOpenChat: () => void
}

export function AssistantMenu({
  onCollapse,
  onNewSession,
  newSessionBusy,
  page,
  onOpenPage,
  onOpenChat,
}: AssistantMenuProps) {
  const { t } = useTranslation()
  // Only offered where there is something to read: the gateway names the bot
  // users it will show, and a deployment that configured none has no page.
  return (
    // The canvas colour, with the conversation floating on it as a panel — the
    // inverse of the app's rail, which is the darkest surface on screen. Two
    // "sidebars" side by side must not read as the same kind of thing.
    <aside className="bg-background flex h-full min-h-0 w-full flex-col">
      {/* Same height and gutter as the top navigator, and the same trick for the
          edge button: `-me-2` pulls the icon's box out so the GLYPH lines up with
          the 24px gutter rather than the button's padding. */}
      {/* `pt-2` matches the inset of the floating conversation beside it, so the
          menu's header row and the conversation's sit on the same line. */}
      <div className="h-13 flex shrink-0 items-center gap-2 px-6 pt-2">
        <BackendTitle className="flex-1" />
        <TooltipButton
          variant="ghost"
          size="icon"
          className="-me-2 shrink-0 rounded-full"
          onClick={onCollapse}
          aria-label={t('assistant.menu.collapse')}
          tooltip={t('assistant.menu.collapse')}
        >
          <AlignLeftIcon className="h-4 w-4" />
        </TooltipButton>
      </div>

      {/* px-4 + a row's own px-2 puts every label on the 24px gutter, while the
          hover fill still bleeds past it. */}
      <div className="flex flex-col gap-0.5 px-4 pb-2">
        <MenuEntry
          icon={newSessionBusy ? Loader2Icon : SquarePenIcon}
          spinning={newSessionBusy}
          label={t('assistant.newSession')}
          onClick={() => {
            // A new conversation belongs in the conversation view, not behind
            // whatever the settings were showing.
            onOpenChat()
            onNewSession()
          }}
        />
        {/* Always says the same thing and always goes to the same place. It used
            to flip to "back to chat", which made the one button both the way in
            and the way out — and left the way out unreachable from anywhere
            else, so picking a conversation switched the thread behind a settings
            page that stayed put. Leaving is now the URL's job. */}
        <MenuEntry
          icon={SettingsIcon}
          label={t('assistant.settings.title')}
          active={page === 'config'}
          onClick={() => onOpenPage('config')}
        />
      </div>

      {/* Inset, not full-bleed: the rule separates two groups of rows, and the
          rows themselves stop short of the edge. */}
      <div className="mx-4 border-t pt-2" />
      <SessionHistoryList onOpenChat={onOpenChat} />
    </aside>
  )
}

function MenuEntry({
  icon: Icon,
  label,
  onClick,
  active,
  spinning,
}: {
  icon: LucideIcon
  label: string
  onClick: () => void
  active?: boolean
  spinning?: boolean
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'hover:bg-muted flex items-center gap-2 rounded-lg px-2 py-1.5 text-left text-[13px] transition-colors',
        active && 'bg-muted'
      )}
    >
      <Icon className={cn('size-4 shrink-0', spinning && 'animate-spin')} />
      <span className="truncate">{label}</span>
    </button>
  )
}
