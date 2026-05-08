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

import type { Locale } from "./config"
import type { TranslationKeys } from "@/messages/_schema"

// Pre-import ALL locale dictionaries synchronously so they're available on first render.
import enDict from "@/messages/en.json"
import zhHansDict from "@/messages/zh-Hans.json"
import zhHantDict from "@/messages/zh-Hant.json"

const cache = new Map<Locale, TranslationKeys>()

// Seed the cache with ALL locales immediately
cache.set("en", enDict as TranslationKeys)
cache.set("zh-Hans", zhHansDict as TranslationKeys)
cache.set("zh-Hant", zhHantDict as TranslationKeys)

/**
 * Load translation dictionary. With all locales pre-seeded, this always resolves
 * synchronously from cache. Kept async for future extensibility.
 */
export async function loadDictionary(locale: Locale): Promise<TranslationKeys> {
  if (cache.has(locale)) return cache.get(locale)!

  // Fallback for any future locale not pre-imported
  const dict = enDict as TranslationKeys
  cache.set(locale, dict)
  return dict
}

/** Synchronous access — works immediately for all pre-seeded locales */
export function getDictionary(locale: Locale): TranslationKeys | undefined {
  return cache.get(locale)
}
