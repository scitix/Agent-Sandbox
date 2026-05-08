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

import { atomWithStorage } from "jotai/utils"
import { atom } from "jotai"
import { I18N_CONFIG, type Locale } from "./config"

export const localeAtom = atomWithStorage<Locale | null>(I18N_CONFIG.storageKey, null)

/**
 * Derived atom that also updates <html lang> when the locale changes.
 * Components should use this via useTranslation(), not directly.
 */
export const localeWithSideEffectsAtom = atom(
  (get) => get(localeAtom),
  (_get, set, newLocale: Locale) => {
    set(localeAtom, newLocale)
    if (typeof document !== "undefined") {
      document.documentElement.lang = newLocale
    }
  },
)
