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

import {
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
  useCallback,
  type ReactNode,
} from "react"
import { useAtom } from "jotai"
import { useRouter, usePathname, useParams } from "next/navigation"
import { localeWithSideEffectsAtom } from "./atoms"
import { loadDictionary, getDictionary } from "./dictionary"
import { interpolate } from "./interpolate"
import { I18N_CONFIG, isValidLocale, type Locale } from "./config"
import type { TranslationKey } from "@/messages/_schema"

interface I18nContextValue {
  locale: Locale
  setLocale: (locale: Locale) => void
  t: (key: TranslationKey, params?: Record<string, string | number>) => string
  isLoading: boolean
}

const I18nContext = createContext<I18nContextValue | null>(null)

/**
 * Detect the effective locale from the URL [locale] segment.
 * This is the single source of truth for what language to display —
 * the proxy has already rewritten URLs without a prefix to /en/... so the
 * segment is always present server-side.
 */
function useUrlLocale(): Locale {
  const params = useParams<{ locale?: string }>()
  const raw = params?.locale
  return raw && isValidLocale(raw) ? raw : I18N_CONFIG.defaultLocale
}

/** Strip the locale prefix from a pathname, if one is present. */
function stripLocalePrefix(pathname: string): string {
  for (const loc of I18N_CONFIG.locales) {
    if (loc === I18N_CONFIG.defaultLocale) continue
    if (pathname.startsWith(`/${loc}/`) || pathname === `/${loc}`) {
      return pathname.slice(`/${loc}`.length) || "/"
    }
  }
  return pathname
}

/** Build a pathname for the given locale: no prefix for default, `/{loc}` otherwise. */
function withLocalePrefix(cleanPath: string, loc: Locale): string {
  return loc === I18N_CONFIG.defaultLocale ? cleanPath : `/${loc}${cleanPath}`
}

export function I18nProvider({ children }: { children: ReactNode }) {
  const [storedLocale, setLocaleAtom] = useAtom(localeWithSideEffectsAtom)
  const urlLocale = useUrlLocale()
  const router = useRouter()
  const pathname = usePathname()

  // URL is the single source of truth for the displayed locale.
  const locale = urlLocale

  // One-shot reconciliation on mount: if the user has a saved preference in
  // localStorage that disagrees with the URL, redirect to the saved preference.
  // This handles the case where the proxy did a best-effort Accept-Language
  // redirect on "/" but the user's actual saved choice differs from their
  // browser's default language.
  //
  // If no saved preference exists yet, accept whatever the URL says — it's
  // either an explicit link (trustworthy) or a first-visit Accept-Language
  // guess (the user hasn't expressed a preference, so the guess wins).
  const reconciledRef = useRef(false)
  useEffect(() => {
    if (reconciledRef.current) return
    reconciledRef.current = true
    if (storedLocale && storedLocale !== urlLocale) {
      const cleanPath = stripLocalePrefix(pathname)
      router.replace(withLocalePrefix(cleanPath, storedLocale))
    }
  }, [storedLocale, urlLocale, pathname, router])

  // Track dictionary readiness. For "en" the dict is pre-seeded synchronously.
  const [dictVersion, setDictVersion] = useState(() => (getDictionary(locale) ? 1 : 0))
  const isLoading = dictVersion === 0

  // Load dictionary on locale change. For "en" this resolves instantly from cache.
  useEffect(() => {
    let cancelled = false
    if (!getDictionary(locale)) {
      loadDictionary(locale).then(() => {
        if (!cancelled) setDictVersion((v) => v + 1)
      })
    } else {
      // Already in cache, bump version to ensure t() picks it up
      setDictVersion((v) => v + 1)
    }
    return () => {
      cancelled = true
    }
  }, [locale])

  // Sync <html lang> on mount and locale change
  useEffect(() => {
    document.documentElement.lang = locale
  }, [locale])

  /**
   * Switch locale: persist the user's choice to localStorage (so future visits
   * can override a wrong Accept-Language guess), then navigate to the
   * locale-prefixed (or un-prefixed for default) URL equivalent of the current
   * page. The URL is the authoritative signal — no cookie is involved.
   */
  const setLocale = useCallback(
    (newLocale: Locale) => {
      setLocaleAtom(newLocale)
      const cleanPath = stripLocalePrefix(pathname)
      router.push(withLocalePrefix(cleanPath, newLocale))
    },
    [setLocaleAtom, pathname, router],
  )

  const t = useCallback(
    (key: TranslationKey, params?: Record<string, string | number>): string => {
      const dict = getDictionary(locale)
      if (!dict) return key // fallback to key while loading
      const template = dict[key]
      if (!template) {
        if (process.env.NODE_ENV === "development") {
          console.warn(`[i18n] Missing key: "${key}" for locale "${locale}"`)
        }
        return key
      }
      return interpolate(template, params)
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [locale, dictVersion],
  )

  return (
    <I18nContext.Provider value={{ locale, setLocale, t, isLoading }}>
      {children}
    </I18nContext.Provider>
  )
}

/**
 * Hook to access translations in any component.
 *
 * Usage:
 *   const { t, locale, setLocale } = useTranslation()
 *   t("sandboxes.createTitle")
 *   t("sandboxes.deletedSuccess", { id: "abc123" })
 */
export function useTranslation(): I18nContextValue {
  const ctx = useContext(I18nContext)
  if (!ctx) throw new Error("useTranslation must be used within <I18nProvider>")
  return ctx
}
