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

import Link from "next/link"
import { KeyRound } from "lucide-react"

import { Button } from "@/components/ui/button"
import { useTranslation } from "@/lib/i18n"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { clusterPath } from "@/lib/cluster-path"

/**
 * Shown in place of content that needs an API key whose plaintext the platform
 * still holds. Two things need one: the Env docs (they render a ready-to-copy
 * snippet containing the key) and creating a sandbox via the E2B API (its auth
 * middleware takes API keys only, never the session JWT).
 *
 * `description` is passed in because the two cases need different wording and
 * `t()` only accepts literal keys — the caller resolves its own copy.
 */
export function ApiKeyRequiredNotice({ description }: { description: string }) {
  const { t } = useTranslation()
  const clusterID = useClusterID()
  const locale = useLocale()

  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-4 p-6 text-center">
      <div className="bg-muted flex h-12 w-12 items-center justify-center rounded-full">
        <KeyRound className="text-muted-foreground h-6 w-6" />
      </div>
      <div className="space-y-1">
        <p className="text-sm font-semibold">{t("envs.apiKeyRequired.title")}</p>
        <p className="text-muted-foreground max-w-md text-xs">{description}</p>
      </div>
      <Button size="sm" render={<Link href={clusterPath(clusterID, "api-keys", locale)} />}>
        {t("envs.apiKeyRequired.goToApiKeys")}
      </Button>
    </div>
  )
}
