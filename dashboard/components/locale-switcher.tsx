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

import { Check } from "lucide-react"
import { useTranslation, I18N_CONFIG, type Locale } from "@/lib/i18n"
import { cn } from "@/lib/utils"

/** Human-readable label for each locale — always shown in the locale's own language */
const LOCALE_LABELS: Record<Locale, string> = {
  en: "English",
  "zh-Hans": "简体中文",
  "zh-Hant": "繁體中文",
}

/**
 * Language selector rendered as a list of selectable options.
 * Designed to be embedded in a settings/general page — no icons or flags.
 */
export function LocaleSwitcher() {
  const { locale, setLocale, t } = useTranslation()

  return (
    <div>
      <h3 className="text-foreground mb-1 font-mono text-sm font-bold tracking-wide uppercase">
        {t("general.language")}
      </h3>
      <p className="text-muted-foreground mb-3 text-xs">{t("general.languageDesc")}</p>
      <div className="flex flex-col gap-1.5">
        {I18N_CONFIG.locales.map((loc) => {
          const isActive = loc === locale
          return (
            <button
              key={loc}
              onClick={() => {
                if (!isActive) setLocale(loc)
              }}
              className={cn(
                "border-border flex items-center justify-between border px-3 py-2.5 text-left transition-colors",
                isActive
                  ? "bg-secondary border-brand/40"
                  : "bg-background hover:bg-secondary/60 cursor-pointer",
              )}
            >
              <div className="flex flex-col gap-0.5">
                <span
                  className={cn(
                    "font-mono text-sm",
                    isActive ? "text-foreground font-semibold" : "text-foreground",
                  )}
                >
                  {LOCALE_LABELS[loc]}
                </span>
                <span className="text-muted-foreground font-mono text-xs uppercase">{loc}</span>
              </div>
              {isActive && <Check className="text-brand h-4 w-4 shrink-0" />}
            </button>
          )
        })}
      </div>
    </div>
  )
}
