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

import { I18N_CONFIG } from "@/lib/i18n/config"

/**
 * Tell Next.js which [locale] values are valid so it can statically generate them.
 */
export function generateStaticParams() {
  return I18N_CONFIG.locales.map((locale) => ({ locale }))
}

export default function LocaleLayout({
  children,
}: {
  children: React.ReactNode
  params: Promise<{ locale: string }>
}) {
  // The actual <html lang> is set client-side by I18nProvider via document.documentElement.lang
  // This layout simply passes children through — it exists to create the [locale] route segment
  return <>{children}</>
}
