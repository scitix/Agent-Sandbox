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

import { useParams } from "next/navigation"
import { isValidLocale, type Locale, I18N_CONFIG } from "@/lib/i18n/config"

/**
 * Returns the current locale from the [locale] URL segment.
 * Falls back to the default locale ("en") when outside a locale-scoped route.
 */
export function useLocale(): Locale {
  const params = useParams<{ locale?: string }>()
  const raw = params?.locale
  return raw && isValidLocale(raw) ? raw : I18N_CONFIG.defaultLocale
}
