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

import { useTranslation } from '@/lib/i18n'
import { buildTableSnapshot } from '@/lib/table-export'
import { type Table } from '@tanstack/react-table'

import { AskAIButton } from './ask-ai-button'

/**
 * The Ask AI button shown in a table toolbar (in place of the export when the
 * assistant is enabled). The filtered, visible rows are serialized into a
 * structured `TableSnapshot` (title, active filters with their possible values,
 * rows) and handed over as an *attachment*; the prompt is a generic ask the
 * user can extend. Both are built lazily on click so a large table isn't
 * serialized on render.
 */
export function TableAskAIButton<TData>({
  table,
  title,
  slug,
}: {
  table: Table<TData>
  title: string
  /** Stable ASCII base for the sandbox file name (proposal 0055). Usually the
   *  route-derived export base (e.g. `prod-foo-diagnoses`) so the agent sees a
   *  meaningful path even when `title` is localized. */
  slug?: string
}) {
  const { t } = useTranslation()

  const isFiltered =
    (table.getState().columnFilters?.length ?? 0) > 0 ||
    !!table.getState().globalFilter

  const buildAttachment = () => {
    const snapshot = buildTableSnapshot(table, {
      title,
      url: isFiltered ? window.location.href : undefined,
      t,
    })
    const count = snapshot.matched
    const name = isFiltered
      ? t('table.attachmentNameFiltered', { title, count })
      : t('table.attachmentName', { title, count })
    // ASCII, path-safe base for the sandbox file name (the chip keeps the
    // localized `name`). Prefer the caller's route-derived slug; else sanitize
    // the title; else a stable fallback (proposal 0055).
    const base =
      (slug || title)
        .replace(/[^A-Za-z0-9._-]+/g, '-')
        .replace(/^[-.]+|[-.]+$/g, '')
        .slice(0, 48) || 'table'
    // The attachment content is the structured snapshot (JSON), wrapped and
    // delivered to the agent as a text part.
    return { name, markdown: JSON.stringify(snapshot), slug: `${base}.json` }
  }

  const buildPrompt = () => t('table.askAiPrompt', { title })

  return (
    <AskAIButton
      prompt={buildPrompt}
      attachment={buildAttachment}
      className="text-muted-foreground font-normal"
    >
      <span className="hidden text-xs sm:inline">{t('table.askAi')}</span>
    </AskAIButton>
  )
}
