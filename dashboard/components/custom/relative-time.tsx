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

"use client"

import { format, formatDistanceToNow } from "date-fns"
import { enUS, zhCN, ja, ko, type Locale } from "date-fns/locale"
import { useEffect, useState } from "react"

import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"
import { useTranslation } from "@/lib/i18n"

const LOCALE_MAP: Record<string, Locale> = {
  zh: zhCN,
  ja: ja,
  ko: ko,
  en: enUS,
}

function resolveLocale(lang?: string): Locale {
  if (!lang) return enUS
  return LOCALE_MAP[lang] ?? LOCALE_MAP[lang.split("-")[0]] ?? enUS
}

interface RelativeTimeProps {
  /** ISO 8601 date string */
  date?: string | null
  /** BCP-47 language tag override. When omitted, uses the global i18n locale. */
  lang?: string
  className?: string
}

/**
 * Displays a relative time string (e.g. "3 minutes ago") with a tooltip
 * showing the exact datetime in "yyyy/MM/dd HH:mm:ss" format.
 * Updates every 10 seconds. Returns null when `date` is falsy.
 *
 * Automatically syncs with the global i18n locale when `lang` prop is omitted.
 */
export function RelativeTime({ date, lang, className }: RelativeTimeProps) {
  const { locale: i18nLocale } = useTranslation()
  const effectiveLang = lang ?? i18nLocale
  const locale = resolveLocale(effectiveLang)
  const parsed = date ? new Date(date) : null
  const isValid = parsed !== null && !isNaN(parsed.getTime())

  // Re-render every 10s so the relative string stays fresh. The tick is bumped
  // only from the interval callback (never synchronously in the effect body),
  // and `relative` is derived during render below.
  const [, setTick] = useState(0)
  useEffect(() => {
    if (!isValid) return
    const timer = setInterval(() => setTick((n) => n + 1), 10_000)
    return () => clearInterval(timer)
  }, [isValid])

  if (!parsed || !isValid) return null

  const relative = formatDistanceToNow(parsed, { locale, addSuffix: true })
  const exact = format(parsed, "MM/dd HH:mm:ss")

  return (
    <TooltipProvider delay={80}>
      <Tooltip>
        <TooltipTrigger
          className={cn("text-muted-foreground cursor-help font-mono text-xs", className)}
        >
          {exact}
        </TooltipTrigger>
        <TooltipContent side="top" className="font-mono text-xs">
          {relative}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
