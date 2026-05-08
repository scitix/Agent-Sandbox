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

import { Sparkles } from "lucide-react"
import { useAtom, useAtomValue } from "jotai"
import { SidebarMenuButton, SidebarMenuItem } from "@/components/ui/sidebar"
import { lastSeenVersionAtom, changelogDialogOpenAtom } from "@/lib/atoms"
import { hasUnseenVersion } from "@/lib/version"
import { latestVersion } from "@/lib/changelog"
import { cn } from "@/lib/utils"
import { useTranslation } from "@/lib/i18n"

const APP_VERSION = process.env.NEXT_PUBLIC_APP_VERSION ?? "0.0.0"

/**
 * Sidebar footer button that opens the What's New dialog.
 * Shows a pulsing red dot + "NEW" label when there's an unseen version.
 */
export function ChangelogTrigger() {
  const { t } = useTranslation()
  const lastSeen = useAtomValue(lastSeenVersionAtom)
  const [, setOpen] = useAtom(changelogDialogOpenAtom)
  const hasNew = hasUnseenVersion(APP_VERSION, lastSeen)

  return (
    <SidebarMenuItem>
      <SidebarMenuButton
        onClick={() => setOpen(true)}
        tooltip={`${t("nav.changelog")}${hasNew ? ` — ${t("changelog.newVersionAvailable", { version: latestVersion() })}` : ""}`}
        className="cursor-pointer"
      >
        <span className="relative">
          <Sparkles className={cn("h-4 w-4", hasNew && "text-brand")} />
          {hasNew && (
            <span
              className="absolute -top-1 -right-1 h-2 w-2 animate-pulse rounded-full bg-red-500"
              aria-label="New version available"
            />
          )}
        </span>
        <span>{t("nav.changelog")}</span>
        {hasNew && (
          <span className="ml-auto rounded bg-red-500 px-1 py-0.5 font-mono text-[9px] font-bold tracking-wider text-white uppercase">
            {t("status.new")}
          </span>
        )}
      </SidebarMenuButton>
    </SidebarMenuItem>
  )
}
