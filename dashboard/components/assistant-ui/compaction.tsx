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

import { makeAssistantToolUI } from '@assistant-ui/react'
import { useTranslation } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { COMPACTION_TOOL_NAME } from '@/lib/assistant/agent-events'
import { FoldVerticalIcon } from 'lucide-react'
import { type FC, type ReactNode } from 'react'

/** A full-width labelled rule: a hairline with a centered chip. Deliberately quiet — it marks a boundary in the
 *  conversation, it isn't a message. */
function CompactionRule({
  icon,
  label,
  className,
}: {
  icon: ReactNode
  label: string
  className?: string
}) {
  return (
    <div
      data-slot="aui_compaction-rule"
      className={cn(
        'text-muted-foreground flex items-center gap-2 px-2 text-xs',
        className
      )}
    >
      <span className="bg-border h-px flex-1" />
      <span className="flex items-center gap-1.5 whitespace-nowrap">
        {icon}
        {label}
      </span>
      <span className="bg-border h-px flex-1" />
    </div>
  )
}

/** The marker for a compaction that already happened, rendered where it occurred
 *  in the thread — everything above it has been folded into a summary.
 *
 *  There is no manual compact action in this product, so every one of these is
 *  the harness folding the context on its own. That is exactly why the divider
 *  stays: without it the agent simply appears to have forgotten the earlier
 *  conversation. */
export const CompactionDivider: FC<{ auto?: boolean }> = ({ auto }) => {
  const { t } = useTranslation()
  return (
    <CompactionRule
      icon={<FoldVerticalIcon className="size-3.5" />}
      label={auto ? t('assistant.compactedAuto') : t('assistant.compacted')}
    />
  )
}

/**
 * The divider, mounted as the renderer for the gateway's synthetic compaction
 * "tool call" (see COMPACTION_TOOL_NAME).
 *
 * It is not a tool and the model can neither call it nor see it — a tool call is
 * simply the only part the AG-UI runtime places IN ORDER in the transcript,
 * which is the whole requirement here. `display: 'standalone'` keeps it out of
 * the collapsed "N tool calls" group: a boundary hidden behind a disclosure
 * explains nothing.
 */
export const CompactionToolUI = makeAssistantToolUI<{ auto?: boolean }, string>(
  {
    toolName: COMPACTION_TOOL_NAME,
    display: 'standalone',
    render: ({ args }) => <CompactionDivider auto={args?.auto !== false} />,
  }
)
