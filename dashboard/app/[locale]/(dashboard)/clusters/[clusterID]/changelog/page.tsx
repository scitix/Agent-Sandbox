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

import { PageHeader } from "@/components/page-header"
import { ChangelogPageContent } from "@/components/changelog/changelog-content"
import { useTranslation } from "@/lib/i18n"

export default function ChangelogPage() {
  const { t } = useTranslation()

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={t("nav.changelog")} />
      <div className="flex-1 overflow-auto">
        <ChangelogPageContent />
      </div>
    </div>
  )
}
