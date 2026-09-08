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

// The harness picker: which agent backend a NEW conversation starts under.
//
// The pod serves every backend it is configured for, so this is a browser-side
// preference rather than a redeploy. Two things it deliberately does NOT do:
//
//   * It does not re-home the open conversation. A thread's backend is fixed
//     server-side at creation (`ThreadRef.backendId`), so switching decides what
//     the NEXT conversation uses and nothing else. Saying so in the menu matters
//     — a picker that looks like it applies to what you are reading, but does
//     not, is worse than no picker.
//   * It does not hide a backend the deployment cannot run. `/backends` reports
//     the unavailable ones with a reason, and they render disabled with that
//     reason as the tooltip: "no credential" is actionable, an absent entry
//     looks like the feature was never built.
import {
  BACKEND_MARKS,
  BackendMark,
} from '@/components/assistant-ui/backend-marks'
import { gateway } from '@/components/assistant-ui/gw/client'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useTranslation } from '@/lib/i18n'
import { atomAssistantBackend } from '@/lib/assistant/store'
import { cn } from '@/lib/utils'
import { useQuery } from '@tanstack/react-query'
import { useAtom } from 'jotai'
import { ChevronDownIcon } from 'lucide-react'

/** Every harness this deployment could run. Static per deployment: which ones
 *  exist is decided at pod boot, so the query never goes stale. */
function useBackendOptions() {
  const { data } = useQuery({
    queryKey: ['assistant-backends'],
    staleTime: Infinity,
    gcTime: Infinity,
    retry: 1,
    queryFn: () => gateway.backends(),
  })
  // Only ever offer what we can draw.
  return (data ?? []).filter(b => BACKEND_MARKS[b.id])
}

/**
 * The conversation menu's heading: which harness runs, and — where there is a
 * choice — a way to change it.
 *
 * Deliberately still a heading when there is only one backend. The icon-button
 * switcher below can hide itself because a toolbar with one fewer icon reads fine;
 * a menu whose title disappears does not, and "which agent am I talking to" is
 * worth stating even when it cannot be changed.
 */
export function BackendTitle({ className }: { className?: string }) {
  const { t } = useTranslation()
  const [selected, setSelected] = useAtom(atomAssistantBackend)
  const options = useBackendOptions()
  const current = BACKEND_MARKS[selected] ? selected : options[0]?.id
  const label = current ? BACKEND_MARKS[current]?.label : undefined

  if (options.length < 2) {
    return (
      <span
        className={cn(
          'flex min-w-0 items-center gap-1.5 text-[13px] font-medium',
          className
        )}
      >
        <BackendMark id={current} />
        <span className="truncate">{label ?? t('nav.groupAssistant')}</span>
      </span>
    )
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="sm"
            // `justify-start`, not the button default: callers stretch this
            // (the menu header gives it `flex-1` so the collapse button lands
            // on the gutter), and a centred label drifts away from the rows
            // below it as the column widens. Mark, name and chevron stay a
            // group on the left edge.
            className={cn(
              '-ms-2 h-8 min-w-0 justify-start gap-1.5 px-2',
              className
            )}
            aria-label={t('assistant.backend.switch')}
          />
        }
      >
        <BackendMark id={current} />
        <span className="truncate text-[13px] font-medium">{label}</span>
        <ChevronDownIcon className="text-muted-foreground size-3.5 shrink-0" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="min-w-56">
        <DropdownMenuGroup>
          <DropdownMenuLabel className="text-muted-foreground text-xs font-normal">
            {t('assistant.backend.appliesToNew')}
          </DropdownMenuLabel>
          {options.map(b => (
            <DropdownMenuItem
              key={b.id}
              disabled={!b.available}
              title={b.available ? undefined : b.reason}
              onClick={() => b.available && setSelected(b.id)}
              className={cn('gap-2', b.id === current && 'bg-accent')}
            >
              <BackendMark id={b.id} />
              <span className="flex-1">{BACKEND_MARKS[b.id].label}</span>
              {!b.available && (
                <span className="text-muted-foreground text-xs">
                  {t('assistant.backend.unavailable')}
                </span>
              )}
            </DropdownMenuItem>
          ))}
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export function BackendSwitcher({
  variant = 'ghost',
  className,
}: {
  /** Matches the rest of the toolbar. The assistant page mounts it `outline`,
   *  the sidebar `ghost`; hard-coding either makes the switcher the odd one out
   *  in the other place. */
  variant?: 'ghost' | 'outline'
  className?: string
}) {
  const { t } = useTranslation()
  const [selected, setSelected] = useAtom(atomAssistantBackend)
  const options = useBackendOptions()

  // With one harness there is nothing to pick, so the control stays out of the
  // toolbar entirely.
  if (options.length < 2) return null

  // A stale localStorage pick (a harness since removed) must not leave the
  // trigger blank.
  const current = BACKEND_MARKS[selected] ? selected : options[0].id

  return (
    <DropdownMenu>
      {/* A plain Button, deliberately — `TooltipButton` renders a
          TooltipProvider > Tooltip > [Trigger, Content] tree, and a Base UI menu
          trigger needs ONE element to take a ref on so it can anchor the popup
          (it throws Base UI error #31 otherwise). A dialog trigger tolerates it
          because it anchors nothing, which is why TooltipButton appears inside
          AlertDialogTrigger elsewhere but never inside a menu or popover trigger.
          The hint is a native `title`, matching every other menu trigger here. */}
      <DropdownMenuTrigger
        render={
          <Button
            variant={variant}
            size="icon"
            className={className}
            aria-label={t('assistant.backend.switch')}
            title={t('assistant.backend.switch')}
          />
        }
      >
        <BackendMark id={current} />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-56">
        {/* GroupLabel needs a group context (Base UI Menu.GroupLabel), so the
            label and the items it names have to share a Group — bare, it throws.
            Every other DropdownMenuLabel here is wrapped the same way. */}
        <DropdownMenuGroup>
          {/* States the scope up front: this is about the next conversation. */}
          <DropdownMenuLabel className="text-muted-foreground text-xs font-normal">
            {t('assistant.backend.appliesToNew')}
          </DropdownMenuLabel>
          {options.map(b => (
            <DropdownMenuItem
              key={b.id}
              disabled={!b.available}
              title={b.available ? undefined : b.reason}
              onClick={() => b.available && setSelected(b.id)}
              className={cn('gap-2', b.id === current && 'bg-accent')}
            >
              <BackendMark id={b.id} />
              <span className="flex-1">{BACKEND_MARKS[b.id].label}</span>
              {!b.available && (
                <span className="text-muted-foreground text-xs">
                  {t('assistant.backend.unavailable')}
                </span>
              )}
            </DropdownMenuItem>
          ))}
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
