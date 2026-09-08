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

// Date headings for a list of conversations.
//
// Shared by the menu's own history and by the read-only analysis list, so the two
// read as the same kind of thing — the second list is still a list of
// conversations, just someone else's.
import type { Locale, TranslationKey } from '@/lib/i18n'

/** Just the lookup, so this file needs nothing else from the i18n layer. */
type Translate = (
  key: TranslationKey,
  params?: Record<string, string | number>
) => string

const DAY_MS = 24 * 60 * 60 * 1000

/** Start of the local day `n` days ago. Grouping is by CALENDAR day, not by
 *  elapsed hours: a conversation from 23:50 yesterday belongs under "yesterday"
 *  at 00:10 today, however few minutes ago it was. */
function startOfDay(offsetDays = 0): number {
  const d = new Date()
  d.setHours(0, 0, 0, 0)
  return d.getTime() - offsetDays * DAY_MS
}

export interface DayGroup<T> {
  key: string
  label: string
  items: T[]
}

/**
 * Today / yesterday / the last week by date / everything older in one bucket.
 *
 * The tail is deliberately not itemised by date: past a week nobody is looking
 * for "the 14th", they are scrolling or searching, and a heading per day would
 * turn the list into mostly headings.
 *
 * Order follows the input, which both callers hand over newest-first — grouping
 * must not reorder, or a list sorted by recency stops being one.
 */
export function groupByDay<T extends { updatedAt: number }>(
  items: readonly T[],
  t: Translate,
  locale: Locale
): DayGroup<T>[] {
  const today = startOfDay()
  const yesterday = startOfDay(1)
  const weekAgo = startOfDay(6)
  const groups = new Map<string, DayGroup<T>>()
  const push = (key: string, label: string, item: T) => {
    const group = groups.get(key) ?? { key, label, items: [] }
    group.items.push(item)
    groups.set(key, group)
  }
  for (const item of items) {
    if (item.updatedAt >= today)
      push('today', t('assistant.history.today'), item)
    else if (item.updatedAt >= yesterday)
      push('yesterday', t('assistant.history.yesterday'), item)
    else if (item.updatedAt >= weekAgo) {
      const date = new Date(item.updatedAt)
      push(
        `d${date.toDateString()}`,
        date.toLocaleDateString(locale, { month: 'short', day: 'numeric' }),
        item
      )
    } else push('earlier', t('assistant.history.earlier'), item)
  }
  return [...groups.values()]
}
